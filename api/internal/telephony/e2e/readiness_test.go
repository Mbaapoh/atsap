//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestReadiness_ReportsCallEvents covers phase-a-hardening task 4.2
// (D-73) on the running app: /readyz includes the call-events check and
// it passes, because the app holds its Stasis application and its event
// stream is up. (Before this, readiness checked Postgres and NATS only,
// and an app receiving no call events still reported ready.)
func TestReadiness_ReportsCallEvents(t *testing.T) {
	resp, err := e2eHTTP.Get(appAPI + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /readyz: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body.Status != "ok" {
		t.Fatalf("/readyz = %d %+v, want 200 ok", resp.StatusCode, body)
	}
	if got := body.Checks["call-events"]; got != "ok" {
		t.Errorf("call-events check = %q, want ok (checks: %v)", got, body.Checks)
	}
}
