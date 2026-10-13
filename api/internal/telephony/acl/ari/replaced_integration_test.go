//go:build integration

package ari

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// rigARI returns the dev rig's ARI address and credentials: the
// environment first, then the repository's .env, then .env.example
// (whose values are the rig's published development defaults).
func rigARI(t *testing.T) (url, user, password string) {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "..")
	vars := map[string]string{}
	for _, name := range []string{".env.example", ".env"} { // .env wins
		f, err := os.Open(filepath.Join(root, name))
		if err != nil {
			continue
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			if k, v, ok := strings.Cut(s.Text(), "="); ok && strings.HasPrefix(k, "ARI_") {
				vars[k] = v
			}
		}
		_ = f.Close()
	}
	get := func(k, fallback string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		if v := vars[k]; v != "" {
			return v
		}
		return fallback
	}
	password = get("ARI_PASSWORD", "")
	if password == "" {
		t.Skip("no ARI credentials: set ARI_PASSWORD or run from the repository with the dev rig")
	}
	return get("ARI_TEST_URL", "http://localhost:8088/ari"), get("ARI_USERNAME", "voipapp"), password
}

// TestStreamEvents_RealEngineReportsTheTakeover registers one Stasis
// application twice against the rig's real Asterisk — under a throwaway
// name, never the running app's — and requires the first stream to stop
// with ErrApplicationReplaced (phase-a-hardening task 4.1, D-73). It is
// what proves Asterisk sends ApplicationReplaced to the losing connection,
// which the unit tests can only assume.
func TestStreamEvents_RealEngineReportsTheTakeover(t *testing.T) {
	url, user, password := rigARI(t)
	app := "phase-a-takeover-" + time.Now().Format("150405.000000")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	first := New(url, user, password, app)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- first.StreamEvents(ctx, func(Event) {}, logger) }()

	deadline := time.Now().Add(5 * time.Second)
	for first.EventsHealth() != nil {
		if time.Now().After(deadline) {
			t.Fatalf("the first stream never connected: %v", first.EventsHealth())
		}
		time.Sleep(20 * time.Millisecond)
	}

	second := New(url, user, password, app)
	secondCtx, stopSecond := context.WithCancel(context.Background())
	defer stopSecond()
	go func() { _ = second.StreamEvents(secondCtx, func(Event) {}, logger) }()

	select {
	case err := <-stopped:
		if !errors.Is(err, ErrApplicationReplaced) {
			t.Fatalf("first stream stopped with %v, want ErrApplicationReplaced", err)
		}
	case <-ctx.Done():
		t.Fatal("the engine never told the first stream it was replaced")
	}
	if err := first.EventsHealth(); !errors.Is(err, ErrApplicationReplaced) {
		t.Errorf("EventsHealth() = %v, want ErrApplicationReplaced", err)
	}
}
