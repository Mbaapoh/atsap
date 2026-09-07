// Package server exposes the app's own HTTP endpoints (health checks and
// readiness), separate from Asterisk's HTTP server used for ARI.
//
// Liveness (/healthz) answers 200 whenever the process serves HTTP — it
// says nothing about dependencies. Readiness (/readyz) aggregates the
// registered Checkers, one per backing dependency (Postgres, NATS, ARI,
// AMI once wired in cmd/atsap-api), and answers 200 only when every
// checker passes. Compose healthchecks and any orchestrator must probe
// /readyz, never /healthz, to decide whether this instance may take
// traffic (docs/hld/09-failure-model.md §6).
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Checker is a named dependency probe for /readyz. Check must be fast
// and side-effect free (a ping, a status fetch); /readyz bounds it with
// a timeout. It must never leak secrets in its error: error text is
// NOT exposed in the response (only "ok"/"error"), so log details at
// the call site instead (D-39).
type Checker struct {
	Name  string
	Check func(ctx context.Context) error
}

// New builds the HTTP server with liveness only. Prefer NewWithChecks:
// an app with no registered checkers reports /readyz 503
// ("unconfigured") rather than a misleading 200.
func New(addr string) *http.Server {
	return NewWithChecks(addr, nil)
}

// NewWithChecks builds the HTTP server with liveness (/healthz, always
// 200 while serving) and readiness (/readyz, 200 only when every
// registered Checker passes, 503 otherwise).
func NewWithChecks(addr string, checkers []Checker) *http.Server {
	return NewWithHandlers(addr, checkers, nil)
}

// NewWithHandlers is NewWithChecks plus additional routes (e.g. the
// ConnectRPC API mount from cmd/atsap-api) on the same mux — every
// route this process serves lives on one HTTP server and one health
// surface, never a second listener with its own liveness story. handlers
// maps a URL pattern (as passed to http.ServeMux.Handle) to its handler.
func NewWithHandlers(addr string, checkers []Checker, handlers map[string]http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeReadiness(w, r, checkers)
	})
	for pattern, h := range handlers {
		mux.Handle(pattern, h)
	}

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// readyReport is the /readyz response body.
type readyReport struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func writeReadiness(w http.ResponseWriter, r *http.Request, checkers []Checker) {
	report := readyReport{Status: "ok", Checks: map[string]string{}}
	if len(checkers) == 0 {
		report.Status = "unconfigured"
		writeReadyJSON(w, http.StatusServiceUnavailable, report)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	for _, c := range checkers {
		if err := c.Check(ctx); err != nil {
			report.Status = "not-ready"
			report.Checks[c.Name] = "error"
			continue
		}
		report.Checks[c.Name] = "ok"
	}

	status := http.StatusOK
	if report.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeReadyJSON(w, status, report)
}

func writeReadyJSON(w http.ResponseWriter, status int, report readyReport) {
	body, err := json.Marshal(report)
	if err != nil {
		// Marshaling a fixed-shape struct cannot fail in practice; if it
		// ever does, fail closed rather than serve a half-written body.
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Shutdown gracefully stops the server, waiting up to the given timeout.
func Shutdown(ctx context.Context, srv *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
