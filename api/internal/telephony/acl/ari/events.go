package ari

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Event is one message from the ARI event WebSocket. Type identifies the
// event (e.g. "StasisStart", "ChannelHangupRequest"); Raw holds the full
// JSON payload for callers that need additional fields.
type Event struct {
	Type string
	Raw  json.RawMessage
}

// EventHandler is invoked for every event received on the stream.
type EventHandler func(Event)

// Reconnect policy for the event stream.
//
// The shape here — capped exponential backoff with full jitter, and a
// reset once a connection has proven itself — is the standard answer to
// a reconnect storm, and each part earns its place:
//
//   - Without a CAP, an outage that lasts overnight leaves the stream
//     retrying hours apart and the engine effectively unmonitored.
//   - Without JITTER, every process that lost the connection at the same
//     moment retries at the same moment. That is a thundering herd, and
//     against an engine with a finite HTTP session pool it is how a
//     recoverable blip becomes an outage that will not clear: each
//     synchronised wave consumes the sessions the next wave needs.
//   - Without a RESET, a stream that ran for a week and then dropped once
//     reconnects on the backoff it inherited from whenever it last had
//     trouble, rather than immediately.
const (
	// handshakeTimeout replaces websocket.DefaultDialer's 45 seconds.
	//
	// Forty-five seconds is far too long for a local engine, and it is
	// the number that turned a stalled Asterisk into a stalled process on
	// 2026-09-09: callers blocked in the handshake for three quarters of a
	// minute per attempt while their own deadlines sat unchecked.
	handshakeTimeout = 10 * time.Second

	baseBackoff = time.Second
	maxBackoff  = 30 * time.Second

	// stableConnection is how long a stream must survive to count as
	// healthy and reset the backoff. Long enough that a connection which
	// is accepted and immediately dropped — the failure mode that
	// produces a storm — does not qualify.
	stableConnection = 30 * time.Second
)

// StreamEvents connects to the ARI WebSocket for this client's Stasis app
// and dispatches events to handler until the context is cancelled,
// reconnecting with jittered backoff whenever the connection drops.
//
// It returns only when ctx is done. A dropped connection is expected
// operational noise, not a reason to give up: the engine restarting must
// leave the platform reconnecting rather than deaf.
func (c *Client) StreamEvents(ctx context.Context, handler EventHandler, logger *slog.Logger) error {
	wsURL := c.eventsURL()
	backoff := baseBackoff

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		connectedFor, err := c.streamOnce(ctx, wsURL, handler)
		if ctx.Err() != nil {
			// Cancellation is how this loop is meant to end, so it is not
			// reported as a stream failure.
			return ctx.Err()
		}

		// A connection that lasted is evidence the engine is healthy and
		// this was a blip: start the next outage from the bottom rather
		// than from wherever the last one climbed to.
		if connectedFor >= stableConnection {
			backoff = baseBackoff
		}

		wait := jittered(backoff)
		logger.Warn("ari: event stream disconnected, retrying",
			"error", err, "connected_for", connectedFor.Round(time.Second), "retry_in", wait.Round(time.Millisecond))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		if backoff < maxBackoff {
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// jittered returns a wait uniformly distributed in [0, d) — "full
// jitter" (AWS's term; Google SRE calls the same thing randomised
// backoff).
//
// Full rather than partial jitter, deliberately: it spreads retries the
// most, and the cost of an occasionally very short wait is one extra
// attempt, while the cost of synchronised retries is the herd this
// exists to prevent.
func jittered(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)))
}

// streamOnce holds one connection until it fails, returning how long it
// lasted so the caller can tell a blip from a persistent outage.
func (c *Client) streamOnce(ctx context.Context, wsURL string, handler EventHandler) (time.Duration, error) {
	dialer := &websocket.Dialer{
		HandshakeTimeout: handshakeTimeout,
		Proxy:            http.ProxyFromEnvironment,
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if resp != nil {
		// gorilla returns the HTTP response when the handshake is
		// rejected. Draining and closing it returns the connection to the
		// pool instead of abandoning a socket per failed attempt — which,
		// during a reconnect storm, is a socket per attempt against an
		// engine whose session pool is finite.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	if err != nil {
		return 0, fmt.Errorf("ari: dial event stream: %w", err)
	}
	defer func() { _ = conn.Close() }()

	connectedAt := time.Now()

	// Close the connection when the context is cancelled, so a blocked
	// ReadMessage returns.
	//
	// done releases this goroutine when the read loop exits on its own.
	// Without it the goroutine parks on ctx.Done() forever, and since a
	// reconnect storm runs this function repeatedly, the leak grows with
	// every retry — each leaked goroutine pinning a closed connection.
	// That is the shape of leak that turns a transient engine fault into
	// a process that has to be restarted.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return time.Since(connectedAt), fmt.Errorf("ari: read event: %w", err)
		}

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		handler(Event{Type: envelope.Type, Raw: data})
	}
}

func (c *Client) eventsURL() string {
	base := strings.Replace(c.baseURL, "http", "ws", 1)
	q := url.Values{}
	q.Set("app", c.appName)
	q.Set("api_key", c.username+":"+c.password)
	q.Set("subscribeAll", "true")
	return base + "/events?" + q.Encode()
}
