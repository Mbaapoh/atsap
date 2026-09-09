package ari

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
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
	_, err := c.streamOnce(ctx, wsURL, handler)
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

	_, err := c.streamOnce(context.Background(), "ws://127.0.0.1:1/events", func(Event) {})
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

// TestStreamOnce_DoesNotLeakAGoroutinePerReconnect covers the defect
// that turns a transient engine fault into a process needing a restart.
//
// streamOnce starts a watcher goroutine so a cancelled context unblocks
// ReadMessage. Before 2026-09-09 that goroutine parked on ctx.Done()
// forever, so it outlived the connection it was watching. On its own
// that is a small leak; during a reconnect storm — which is precisely
// when it happens — streamOnce runs repeatedly and the leak grows with
// every retry, each goroutine pinning a closed connection.
//
// Measured rather than reasoned about: goroutines before and after fifty
// failed dials, with a settle window because the runtime does not retire
// them synchronously.
func TestStreamOnce_DoesNotLeakAGoroutinePerReconnect(t *testing.T) {
	c := New("http://127.0.0.1:1", "u", "p", "app")
	ctx := context.Background()

	// Warm up so one-off runtime goroutines are not counted as leaks.
	for i := 0; i < 5; i++ {
		_, _ = c.streamOnce(ctx, "ws://127.0.0.1:1/events", func(Event) {})
	}
	settle()
	before := runtime.NumGoroutine()

	const attempts = 50
	for i := 0; i < attempts; i++ {
		_, _ = c.streamOnce(ctx, "ws://127.0.0.1:1/events", func(Event) {})
	}
	settle()
	after := runtime.NumGoroutine()

	if after-before > 5 {
		t.Fatalf("goroutines grew from %d to %d over %d failed dials: streamOnce leaks per reconnect",
			before, after, attempts)
	}
}

// TestStreamOnce_ReleasesTheWatcherWhenTheStreamEndsOnItsOwn is the same
// property on the path that actually matters: a connection that dies
// while the context is still live. The watcher must be released by the
// read loop exiting, not only by cancellation.
func TestStreamOnce_ReleasesTheWatcherWhenTheStreamEndsOnItsOwn(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"StasisStart"}`))
		_ = conn.Close() // ends the stream while the context stays live
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/events"

	c := New("http://ignored", "u", "p", "app")
	// A context that is never cancelled: if the watcher only exits on
	// ctx.Done(), it never exits at all.
	ctx := context.Background()

	settle()
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		_, _ = c.streamOnce(ctx, wsURL, func(Event) {})
	}
	settle()

	if after := runtime.NumGoroutine(); after-before > 5 {
		t.Fatalf("goroutines grew from %d to %d: the watcher is not released when the stream ends on its own",
			before, after)
	}
}

// TestJitteredSpreadsRetries covers the property that keeps a recovering
// engine recovering: two processes that lost the connection at the same
// instant must not retry at the same instant.
func TestJitteredSpreadsRetries(t *testing.T) {
	const d = time.Second

	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		got := jittered(d)
		if got < 0 || got >= d {
			t.Fatalf("jittered(%v) = %v, want [0, %v)", d, got, d)
		}
		seen[got] = true
	}

	// Full jitter over a second has nanosecond resolution, so repeats
	// across 200 draws would mean it is not actually random.
	if len(seen) < 190 {
		t.Errorf("jittered produced only %d distinct waits in 200 draws — retries would still synchronise", len(seen))
	}

	if jittered(0) != 0 {
		t.Error("jittered(0) must be 0, not a panic or a negative wait")
	}
}

// TestHandshakeTimeoutIsShorterThanTheLibraryDefault pins the number that
// caused the outage. gorilla's DefaultDialer waits 45 seconds, which is
// long enough for a stalled engine to stall every caller waiting on it —
// including tests whose own deadlines cannot fire while blocked in the
// handshake.
func TestHandshakeTimeoutIsShorterThanTheLibraryDefault(t *testing.T) {
	if handshakeTimeout >= websocket.DefaultDialer.HandshakeTimeout {
		t.Fatalf("handshakeTimeout = %v, must be well under gorilla's default %v",
			handshakeTimeout, websocket.DefaultDialer.HandshakeTimeout)
	}
	if handshakeTimeout > 15*time.Second {
		t.Errorf("handshakeTimeout = %v: too long for a local engine", handshakeTimeout)
	}
}

// TestRESTClientIsBounded asserts the REST client cannot hang.
//
// &http.Client{} has no timeout. Every call here is context-bounded, but
// in production that context is the application's lifetime, so a stalled
// engine would stall call setup indefinitely rather than failing it.
func TestRESTClientIsBounded(t *testing.T) {
	c := New("http://127.0.0.1:1", "u", "p", "app")

	if c.http.Timeout <= 0 {
		t.Fatal("the ARI HTTP client must have a Timeout: without one a stalled engine hangs the caller")
	}
	if c.http.Timeout > 30*time.Second {
		t.Errorf("Timeout = %v: too long to be a useful bound", c.http.Timeout)
	}

	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected a *http.Transport so the dial and header timeouts can be set")
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Error("ResponseHeaderTimeout must be set: it is what catches 'connection accepted, nothing written'")
	}
	if tr.IdleConnTimeout <= 0 {
		t.Error("IdleConnTimeout must be set: idle sockets outliving the engine's session pool is how a client exhausts it")
	}
	if tr.MaxIdleConnsPerHost < 4 {
		t.Errorf("MaxIdleConnsPerHost = %d: Go's default of 2 churns connections against a single-host engine", tr.MaxIdleConnsPerHost)
	}
}

// TestRESTCallFailsFastWhenTheEngineNeverAnswers is the behavioural half
// of the above: a server that accepts the connection and writes nothing
// must produce an error in seconds, not block.
//
// This is the exact 2026-09-09 failure mode, reproduced.
func TestRESTCallFailsFastWhenTheEngineNeverAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Accept connections and never respond.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn // held open, deliberately unanswered
		}
	}()

	c := New("http://"+ln.Addr().String(), "u", "p", "app")

	start := time.Now()
	// context.Background() on purpose: the bound must come from the
	// client, because in production the caller's context is long-lived.
	_, err = c.ListChannels(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from an engine that never answers")
	}
	if elapsed > 20*time.Second {
		t.Fatalf("took %v to fail: the client is not bounding the request", elapsed)
	}
}

func settle() {
	for i := 0; i < 3; i++ {
		runtime.Gosched()
		time.Sleep(50 * time.Millisecond)
	}
}
