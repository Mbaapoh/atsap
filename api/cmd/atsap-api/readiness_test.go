package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/server"
	"atsap-api/internal/telephony/acl/ari"
)

type fakeEvents struct{ err error }

func (f fakeEvents) EventsHealth() error { return f.err }

// TestCallEventsChecker covers phase-a-hardening task 4.2 (D-73): the
// instance is ready only while it receives call events, and /readyz names
// the check that failed.
func TestCallEventsChecker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		health error
		status int
		check  string
	}{
		{"receiving events", nil, http.StatusOK, "ok"},
		{"stream down", ari.ErrEventsDisconnected, http.StatusServiceUnavailable, "error"},
		{"application taken over", ari.ErrApplicationReplaced, http.StatusServiceUnavailable, "error"},
		{"any other failure", errors.New("boom"), http.StatusServiceUnavailable, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := callEventsChecker(fakeEvents{err: tc.health})
			assert.Equal(t, "call-events", checker.Name)

			srv := server.NewWithChecks(":0", []server.Checker{checker})
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			assert.Equal(t, tc.status, rec.Code)
			var body struct {
				Checks map[string]string `json:"checks"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tc.check, body.Checks["call-events"])
		})
	}
}
