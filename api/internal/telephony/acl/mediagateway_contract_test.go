package acl_test

// Contract hook: runs the engine-agnostic mediatest suite against the ARI
// adapter wired to an httptest transport. A future engine proves fit the
// same way — construct its adapter against its own test double in its own
// (AST-exempt) package test and call mediatest.Run/RunFailure. This file
// lives in package acl_test inside the acl directory, which the boundary
// gate (api/internal/archtest) explicitly exempts.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/mediatest"
)

// wireCapture records what Originate placed on the wire for mediatest's
// D-19 token and timeout scenarios.
type wireCapture struct {
	channelID string
	variables map[string]string
	timeout   string
}

func (w *wireCapture) LastOriginate() (string, map[string]string, string) {
	return w.channelID, w.variables, w.timeout
}

func successMux(t *testing.T, cap *wireCapture) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/channels":
			cap.channelID = r.URL.Query().Get("channelId")
			cap.timeout = r.URL.Query().Get("timeout")
			var body struct {
				Variables map[string]string `json:"variables"`
			}
			if data, err := io.ReadAll(r.Body); err == nil && len(data) > 0 {
				_ = json.Unmarshal(data, &body)
			}
			cap.variables = body.Variables
			_, _ = w.Write([]byte(`{"id":"chan-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/bridges":
			_, _ = w.Write([]byte(`{"id":"bridge-1"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/addChannel"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/play"):
			_, _ = w.Write([]byte(`{"id":"pb-1"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected ARI call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func failingMux(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"message":"boom"}`))
}

func newContractGateway(t *testing.T, handler http.HandlerFunc) *acl.MediaGatewayAdapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return acl.NewMediaGatewayAdapter(ari.New(srv.URL, "u", "p", "voip-app"))
}

func TestMediaGatewayAdapter_Contract(t *testing.T) {
	cap := &wireCapture{}
	mediatest.Run(t, newContractGateway(t, successMux(t, cap)), cap)
}

func TestMediaGatewayAdapter_ContractFailure(t *testing.T) {
	mediatest.RunFailure(t, newContractGateway(t, failingMux))
}
