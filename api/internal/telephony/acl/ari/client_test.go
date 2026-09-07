package ari

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "voipapp", "devpassword123", "voip-app")
	return c, srv
}

func TestClient_AppName(t *testing.T) {
	c := New("http://asterisk:8088/ari", "u", "p", "voip-app")
	if got := c.AppName(); got != "voip-app" {
		t.Errorf("AppName() = %q, want %q", got, "voip-app")
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	c := New("http://asterisk:8088/ari/", "u", "p", "voip-app")
	if c.baseURL != "http://asterisk:8088/ari" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestClient_AnswerChannel(t *testing.T) {
	var gotMethod, gotPath, gotUser, gotPass string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.AnswerChannel(context.Background(), "chan1"); err != nil {
		t.Fatalf("AnswerChannel() returned error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/channels/chan1/answer" {
		t.Errorf("path = %q, want /channels/chan1/answer", gotPath)
	}
	if gotUser != "voipapp" || gotPass != "devpassword123" {
		t.Errorf("basic auth = %q/%q, want voipapp/devpassword123", gotUser, gotPass)
	}
}

func TestClient_AnswerChannel_Error(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("channel not found"))
	})

	err := c.AnswerChannel(context.Background(), "missing")
	if err == nil {
		t.Fatal("AnswerChannel() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "channel not found") {
		t.Errorf("error = %q, want it to mention status and body", err.Error())
	}
}

func TestClient_HangupChannel_WithReason(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.HangupChannel(context.Background(), "chan1", "busy"); err != nil {
		t.Fatalf("HangupChannel() returned error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/channels/chan1" {
		t.Errorf("path = %q, want /channels/chan1", gotPath)
	}
	if gotQuery != "reason=busy" {
		t.Errorf("query = %q, want reason=busy", gotQuery)
	}
}

func TestClient_HangupChannel_NoReason(t *testing.T) {
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.HangupChannel(context.Background(), "chan1", ""); err != nil {
		t.Fatalf("HangupChannel() returned error: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
}

func TestClient_Play(t *testing.T) {
	var gotPath, gotMedia string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMedia = r.URL.Query().Get("media")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pb1"}`))
	})

	id, err := c.Play(context.Background(), "chan1", "sound:hello-world")
	if err != nil {
		t.Fatalf("Play() returned error: %v", err)
	}
	if id != "pb1" {
		t.Errorf("id = %q, want pb1", id)
	}
	if gotPath != "/channels/chan1/play" {
		t.Errorf("path = %q, want /channels/chan1/play", gotPath)
	}
	if gotMedia != "sound:hello-world" {
		t.Errorf("media = %q, want sound:hello-world", gotMedia)
	}
}

func TestClient_Play_DecodeError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})

	if _, err := c.Play(context.Background(), "chan1", "sound:hello-world"); err == nil {
		t.Fatal("Play() expected decode error, got nil")
	}
}

func TestClient_Originate(t *testing.T) {
	var gotEndpoint, gotApp, gotCallerID string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotEndpoint = r.URL.Query().Get("endpoint")
		gotApp = r.URL.Query().Get("app")
		gotCallerID = r.URL.Query().Get("callerId")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chan42"}`))
	})

	id, err := c.Originate(context.Background(), OriginateRequest{Endpoint: "PJSIP/1000", CallerID: "Test <1000>"})
	if err != nil {
		t.Fatalf("Originate() returned error: %v", err)
	}
	if id != "chan42" {
		t.Errorf("id = %q, want chan42", id)
	}
	if gotEndpoint != "PJSIP/1000" {
		t.Errorf("endpoint = %q, want PJSIP/1000", gotEndpoint)
	}
	if gotApp != "voip-app" {
		t.Errorf("app = %q, want voip-app", gotApp)
	}
	if gotCallerID != "Test <1000>" {
		t.Errorf("callerId = %q, want Test <1000>", gotCallerID)
	}
}

func TestClient_Originate_NoCallerID(t *testing.T) {
	var sawCallerID bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, sawCallerID = r.URL.Query()["callerId"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chan1"}`))
	})

	if _, err := c.Originate(context.Background(), OriginateRequest{Endpoint: "PJSIP/1000"}); err != nil {
		t.Fatalf("Originate() returned error: %v", err)
	}
	if sawCallerID {
		t.Error("callerId query param present, want absent when callerID is empty")
	}
}

// TestClient_Originate_ChannelIDAndVariables covers D-19 correlation:
// an explicit channelId query param and a variables JSON body.
func TestClient_Originate_ChannelIDAndVariables(t *testing.T) {
	var gotChannelID string
	var gotBody []byte
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotChannelID = r.URL.Query().Get("channelId")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"atsa-part-1"}`))
	})

	req := OriginateRequest{
		Endpoint:  "PJSIP/1001",
		ChannelID: "atsa-part-1-uuid",
		Variables: map[string]string{"ATSA_PARTICIPANT_ID": "participant-1"},
	}
	if _, err := c.Originate(context.Background(), req); err != nil {
		t.Fatalf("Originate() returned error: %v", err)
	}
	if gotChannelID != "atsa-part-1-uuid" {
		t.Errorf("channelId = %q, want atsa-part-1-uuid", gotChannelID)
	}
	var decoded struct {
		Variables map[string]string `json:"variables"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if decoded.Variables["ATSA_PARTICIPANT_ID"] != "participant-1" {
		t.Errorf("variables[ATSA_PARTICIPANT_ID] = %q, want participant-1", decoded.Variables["ATSA_PARTICIPANT_ID"])
	}
}

// TestClient_Originate_Timeout covers the alerting-timeout injection
// that makes the spec's "Destination does not answer" scenario
// reachable (LLD-01 task 5.7).
func TestClient_Originate_Timeout(t *testing.T) {
	var gotTimeout string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotTimeout = r.URL.Query().Get("timeout")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chan1"}`))
	})

	if _, err := c.Originate(context.Background(), OriginateRequest{Endpoint: "PJSIP/1000", TimeoutSeconds: 20}); err != nil {
		t.Fatalf("Originate() returned error: %v", err)
	}
	if gotTimeout != "20" {
		t.Errorf("timeout = %q, want 20", gotTimeout)
	}
}

func TestClient_Originate_NoTimeout(t *testing.T) {
	var sawTimeout bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, sawTimeout = r.URL.Query()["timeout"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chan1"}`))
	})

	if _, err := c.Originate(context.Background(), OriginateRequest{Endpoint: "PJSIP/1000"}); err != nil {
		t.Fatalf("Originate() returned error: %v", err)
	}
	if sawTimeout {
		t.Error("timeout query param present, want absent when TimeoutSeconds is zero")
	}
}

func TestClient_Originate_EncodeVariablesError(t *testing.T) {
	// A channel that cannot be JSON-marshalled would only happen with a
	// pathological map; instead cover the decode-error path for the
	// response, which shares the same error-wrapping style.
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})
	if _, err := c.Originate(context.Background(), OriginateRequest{Endpoint: "PJSIP/1000", Variables: map[string]string{"K": "V"}}); err == nil {
		t.Fatal("Originate() expected decode error, got nil")
	}
}

func TestClient_CreateBridge(t *testing.T) {
	var gotPath, gotType string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotType = r.URL.Query().Get("type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"bridge1"}`))
	})

	id, err := c.CreateBridge(context.Background(), "mixing")
	if err != nil {
		t.Fatalf("CreateBridge() returned error: %v", err)
	}
	if id != "bridge1" {
		t.Errorf("id = %q, want bridge1", id)
	}
	if gotPath != "/bridges" {
		t.Errorf("path = %q, want /bridges", gotPath)
	}
	if gotType != "mixing" {
		t.Errorf("type = %q, want mixing", gotType)
	}
}

func TestClient_AddChannelToBridge(t *testing.T) {
	var gotPath, gotChannel string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotChannel = r.URL.Query().Get("channel")
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.AddChannelToBridge(context.Background(), "bridge1", "chan1"); err != nil {
		t.Fatalf("AddChannelToBridge() returned error: %v", err)
	}
	if gotPath != "/bridges/bridge1/addChannel" {
		t.Errorf("path = %q, want /bridges/bridge1/addChannel", gotPath)
	}
	if gotChannel != "chan1" {
		t.Errorf("channel = %q, want chan1", gotChannel)
	}
}

func TestClient_DestroyBridge(t *testing.T) {
	var gotMethod, gotPath string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DestroyBridge(context.Background(), "bridge1"); err != nil {
		t.Fatalf("DestroyBridge() returned error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/bridges/bridge1" {
		t.Errorf("path = %q, want /bridges/bridge1", gotPath)
	}
}

func TestClient_GetChannelVariable(t *testing.T) {
	var gotPath, gotVar string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVar = r.URL.Query().Get("variable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value":"participant-1"}`))
	})

	v, err := c.GetChannelVariable(context.Background(), "chan1", "ATSA_PARTICIPANT_ID")
	if err != nil {
		t.Fatalf("GetChannelVariable() returned error: %v", err)
	}
	if v != "participant-1" {
		t.Errorf("value = %q, want participant-1", v)
	}
	if gotPath != "/channels/chan1/variable" {
		t.Errorf("path = %q, want /channels/chan1/variable", gotPath)
	}
	if gotVar != "ATSA_PARTICIPANT_ID" {
		t.Errorf("variable = %q, want ATSA_PARTICIPANT_ID", gotVar)
	}
}

func TestClient_GetChannelVariable_DecodeError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})
	if _, err := c.GetChannelVariable(context.Background(), "chan1", "X"); err == nil {
		t.Fatal("GetChannelVariable() expected decode error, got nil")
	}
}

func TestClient_SetChannelVariable(t *testing.T) {
	var gotPath, gotVar, gotVal string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVar = r.URL.Query().Get("variable")
		gotVal = r.URL.Query().Get("value")
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.SetChannelVariable(context.Background(), "chan1", "ATSA_PARTICIPANT_ID", "participant-1"); err != nil {
		t.Fatalf("SetChannelVariable() returned error: %v", err)
	}
	if gotPath != "/channels/chan1/variable" {
		t.Errorf("path = %q, want /channels/chan1/variable", gotPath)
	}
	if gotVar != "ATSA_PARTICIPANT_ID" || gotVal != "participant-1" {
		t.Errorf("variable/value = %q/%q, want ATSA_PARTICIPANT_ID/participant-1", gotVar, gotVal)
	}
}
