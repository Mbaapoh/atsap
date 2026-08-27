// Package server exposes the app's own HTTP endpoints (health checks and
// readiness), separate from Asterisk's HTTP server used for ARI.
package server

import (
	"context"
	"net/http"
	"time"
)

// New builds the HTTP server for health/readiness probes.
func New(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// Shutdown gracefully stops the server, waiting up to the given timeout.
func Shutdown(ctx context.Context, srv *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
