// Package mail delivers outbound email over SMTP. It is shared
// infrastructure, like internal/nats: identity owns the Mailer port and
// composes every message, and the composition root adapts this package to
// that port (identity-accounts-and-sessions design D10). Standard library
// only — net/smtp — so no new dependency (TOOLSET §6).
//
// A message body may hold a link secret. Nothing here logs a body, a
// recipient's full address, or a credential.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Config is how to reach the SMTP server.
type Config struct {
	Addr     string // host:port
	From     string // "Name <address>" or a bare address
	User     string // optional; with Password enables PLAIN auth
	Password string
	// Insecure allows a server that does not offer STARTTLS. Never set
	// outside a dev rig: without TLS the body — and its link — crosses the
	// network in clear.
	Insecure bool
	Timeout  time.Duration
}

// Sender sends plain-text messages.
type Sender struct {
	cfg  Config
	from *mail.Address
}

// New validates cfg and returns a Sender.
func New(cfg Config) (*Sender, error) {
	if cfg.Addr == "" {
		return nil, errors.New("smtp address is required")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp from address: %w", err)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &Sender{cfg: cfg, from: from}, nil
}

// Send delivers one message.
func (s *Sender) Send(ctx context.Context, to, subject, body string) error {
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("recipient address: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.New("subject may not contain line breaks")
	}

	host, _, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("smtp address: %w", err)
	}
	dialer := net.Dialer{Timeout: s.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	} else if !s.cfg.Insecure {
		return errors.New("smtp server does not offer STARTTLS; refusing to send in clear")
	}
	if s.cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Password, host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	msg := "From: " + s.from.String() + "\r\n" +
		"To: " + rcpt.String() + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp end data: %w", err)
	}
	return c.Quit()
}
