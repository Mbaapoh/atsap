package ari

import (
	"context"
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

	id, err := c.Originate(context.Background(), "PJSIP/1000", "Test <1000>")
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

	if _, err := c.Originate(context.Background(), "PJSIP/1000", ""); err != nil {
		t.Fatalf("Originate() returned error: %v", err)
	}
	if sawCallerID {
		t.Error("callerId query param present, want absent when callerID is empty")
	}
}
