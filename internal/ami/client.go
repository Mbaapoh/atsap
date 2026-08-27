// Package ami implements a minimal client for the Asterisk Manager
// Interface (AMI): a line-oriented, key/value protocol over TCP used for
// authenticating, issuing actions, and receiving call/system events.
package ami

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

// Message is a single AMI packet: an ordered set of key/value fields as
// sent by Asterisk (an unsolicited Event) or received in response to an
// Action.
type Message map[string]string

// EventHandler is invoked for every unsolicited Event message received
// from Asterisk (e.g. "Newchannel", "Hangup", "DialEnd").
type EventHandler func(Message)

// Client is a connected AMI session.
type Client struct {
	addr     string
	username string
	password string

	mu   sync.Mutex
	conn net.Conn

	onEvent EventHandler
	logger  *slog.Logger
}

// New creates an AMI client. Call Connect to open the session and log in.
func New(addr, username, password string, onEvent EventHandler, logger *slog.Logger) *Client {
	return &Client{
		addr:     addr,
		username: username,
		password: password,
		onEvent:  onEvent,
		logger:   logger,
	}
}

// Connect dials Asterisk, performs the Login action, and starts the
// background read loop that dispatches events to the configured handler.
// It blocks until the context is cancelled or the connection is lost.
func (c *Client) Connect(ctx context.Context) error {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return fmt.Errorf("ami: dial %s: %w", c.addr, err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	reader := bufio.NewReader(conn)

	// Asterisk sends a banner line ("Asterisk Call Manager/x.x.x") before
	// the first response.
	if _, err := reader.ReadString('\n'); err != nil {
		return fmt.Errorf("ami: read banner: %w", err)
	}

	loginID := "login"
	if err := c.send(Message{
		"Action":   "Login",
		"ActionID": loginID,
		"Username": c.username,
		"Secret":   c.password,
	}); err != nil {
		return fmt.Errorf("ami: send login: %w", err)
	}

	resp, err := readMessage(reader)
	if err != nil {
		return fmt.Errorf("ami: read login response: %w", err)
	}
	if resp["Response"] != "Success" {
		return fmt.Errorf("ami: login failed: %s", resp["Message"])
	}
	c.logger.Info("ami: connected and authenticated", "addr", c.addr)

	return c.readLoop(ctx, reader)
}

func (c *Client) readLoop(ctx context.Context, reader *bufio.Reader) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := readMessage(reader)
		if err != nil {
			return fmt.Errorf("ami: connection closed: %w", err)
		}
		if len(msg) == 0 {
			continue
		}
		if _, ok := msg["Event"]; ok && c.onEvent != nil {
			c.onEvent(msg)
		}
	}
}

// Send issues an Action to Asterisk. It does not wait for the matching
// response; pair it with an ActionID and inspect events/responses in your
// EventHandler if you need the result.
func (c *Client) Send(action Message) error {
	return c.send(action)
}

func (c *Client) send(fields Message) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("ami: not connected")
	}

	var b strings.Builder
	for k, v := range fields {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n")

	_, err := conn.Write([]byte(b.String()))
	return err
}

// Close terminates the AMI connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// readMessage reads one AMI packet: consecutive "Key: Value" lines
// terminated by a blank line.
func readMessage(r *bufio.Reader) (Message, error) {
	tp := textproto.NewReader(r)
	msg := Message{}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			return msg, nil
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		msg[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
}
