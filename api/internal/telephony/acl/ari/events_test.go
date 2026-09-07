package ari

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClient_EventsURL(t *testing.T) {
	c := New("http://asterisk:8088/ari", "voipapp", "devpassword123", "voip-app")
	got := c.eventsURL()

	if !strings.HasPrefix(got, "ws://asterisk:8088/ari/events?") {
		t.Fatalf("eventsURL() = %q, want ws:// scheme and /events path", got)
	}
	if !strings.Contains(got, "app=voip-app") {
		t.Errorf("eventsURL() = %q, want app=voip-app", got)
	}
	if !strings.Contains(got, "subscribeAll=true") {
		t.Errorf("eventsURL() = %q, want subscribeAll=true", got)
	}
	if !strings.Contains(got, "api_key=voipapp%3Adevpassword123") {
		t.Errorf("eventsURL() = %q, want api_key=voipapp:devpassword123 (encoded)", got)
	}
}

func TestClient_StreamOnce_DispatchesEvents(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		messages := []string{
			`{"type":"StasisStart","channel":{"id":"chan1"}}`,
			`{"type":"ChannelHangupRequest","channel":{"id":"chan1"}}`,
		}
		for _, m := range messages {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(m)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p", "voip-app")
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/events"

	var mu sync.Mutex
	var got []Event
	handler := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The server closes the connection after sending both messages, so
	// streamOnce is expected to return a "read event" error once the
	// stream ends.
	err := c.streamOnce(ctx, wsURL, handler)
	if err == nil {
		t.Fatal("streamOnce() expected error when server closes connection, got nil")
	}
	if !strings.Contains(err.Error(), "ari: read event") {
		t.Errorf("streamOnce() error = %q, want it to mention read event", err.Error())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("handler invoked %d times, want 2", len(got))
	}
	if got[0].Type != "StasisStart" {
		t.Errorf("got[0].Type = %q, want StasisStart", got[0].Type)
	}
	if got[1].Type != "ChannelHangupRequest" {
		t.Errorf("got[1].Type = %q, want ChannelHangupRequest", got[1].Type)
	}
}

func TestClient_StreamOnce_DialError(t *testing.T) {
	c := New("http://unused", "u", "p", "voip-app")

	err := c.streamOnce(context.Background(), "ws://127.0.0.1:1/events", func(Event) {})
	if err == nil {
		t.Fatal("streamOnce() expected dial error, got nil")
	}
	if !strings.Contains(err.Error(), "ari: dial event stream") {
		t.Errorf("streamOnce() error = %q, want it to mention dial event stream", err.Error())
	}
}

func TestClient_StreamEvents_StopsOnContextCancel(t *testing.T) {
	c := New("http://127.0.0.1:1", "u", "p", "voip-app")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := c.StreamEvents(ctx, func(Event) {}, logger)
	if err != context.Canceled {
		t.Errorf("StreamEvents() error = %v, want context.Canceled", err)
	}
}
