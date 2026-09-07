package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var errProbeDown = errors.New("probe is down")

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestNew_HealthzHandler(t *testing.T) {
	srv := New(":0")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "ok" {
		t.Errorf("body = %q, want %q", got, "ok")
	}
}

func TestReadyz_NoCheckersIs503(t *testing.T) {
	srv := New(":0")

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if got := w.Body.String(); got != `{"status":"unconfigured","checks":{}}` {
		t.Errorf("body = %q, want unconfigured report", got)
	}
}

func TestReadyz_AllOKIs200(t *testing.T) {
	srv := NewWithChecks(":0", []Checker{
		{Name: "postgres", Check: func(ctx context.Context) error { return nil }},
		{Name: "nats", Check: func(ctx context.Context) error { return nil }},
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	for _, want := range []string{`"postgres":"ok"`, `"nats":"ok"`, `"status":"ok"`} {
		if got := w.Body.String(); !contains(got, want) {
			t.Errorf("body = %q, want it to contain %q", got, want)
		}
	}
}

func TestReadyz_OneFailingIs503AndNamesIt(t *testing.T) {
	srv := NewWithChecks(":0", []Checker{
		{Name: "postgres", Check: func(ctx context.Context) error { return nil }},
		{Name: "ari", Check: func(ctx context.Context) error { return errProbeDown }},
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if got := w.Body.String(); !contains(got, `"ari":"error"`) || !contains(got, `"postgres":"ok"`) {
		t.Errorf("body = %q, want per-check breakdown", got)
	}
	if got := w.Body.String(); contains(got, "probe is down") {
		t.Errorf("body = %q, must not leak checker error text (D-39)", got)
	}
}

func TestNew_ServerSettings(t *testing.T) {
	srv := New(":9999")

	if srv.Addr != ":9999" {
		t.Errorf("Addr = %q, want %q", srv.Addr, ":9999")
	}
	if srv.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, 5*time.Second)
	}
}

func TestNew_UnknownRoute(t *testing.T) {
	srv := New(":0")

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestShutdown(t *testing.T) {
	srv := New(":0")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	// Give the server a moment to start accepting.
	time.Sleep(10 * time.Millisecond)

	if err := Shutdown(context.Background(), srv, time.Second); err != nil {
		t.Fatalf("Shutdown() returned error: %v", err)
	}

	select {
	case err := <-serveErr:
		if err != http.ErrServerClosed {
			t.Errorf("Serve() returned %v, want %v", err, http.ErrServerClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not return after Shutdown()")
	}
}
