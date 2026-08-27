package ari

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// StreamEvents connects to the ARI WebSocket for this client's Stasis app
// and dispatches events to handler until the context is cancelled or the
// connection drops, in which case it reconnects with backoff.
func (c *Client) StreamEvents(ctx context.Context, handler EventHandler, logger *slog.Logger) error {
	wsURL := c.eventsURL()

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := c.streamOnce(ctx, wsURL, handler); err != nil {
			logger.Warn("ari: event stream disconnected, retrying", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (c *Client) streamOnce(ctx context.Context, wsURL string, handler EventHandler) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ari: dial event stream: %w", err)
	}
	defer func() { _ = conn.Close() }()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ari: read event: %w", err)
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
