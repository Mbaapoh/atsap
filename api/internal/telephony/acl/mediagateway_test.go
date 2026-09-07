package acl_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/ports"
)

func newTestGateway(t *testing.T, handler http.HandlerFunc) *acl.MediaGatewayAdapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := ari.New(srv.URL, "u", "p", "voip-app")
	return acl.NewMediaGatewayAdapter(client)
}

func TestMediaGatewayAdapter_Originate(t *testing.T) {
	var gotEndpoint, gotChannelID string
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		gotEndpoint = r.URL.Query().Get("endpoint")
		gotChannelID = r.URL.Query().Get("channelId")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chan1"}`))
	})

	ref, err := gw.Originate(context.Background(), ports.OriginateRequest{
		EndpointURI: "PJSIP/1000", ChannelID: "atsa-part-1",
	})
	require.NoError(t, err)
	assert.Equal(t, ports.ChannelRef("chan1"), ref)
	assert.Equal(t, "PJSIP/1000", gotEndpoint)
	assert.Equal(t, "atsa-part-1", gotChannelID)
}

func TestMediaGatewayAdapter_Originate_Error(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := gw.Originate(context.Background(), ports.OriginateRequest{EndpointURI: "PJSIP/1000"})
	assert.Error(t, err)
}

func TestMediaGatewayAdapter_CreateBridge(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"bridge1"}`))
	})
	id, err := gw.CreateBridge(context.Background(), "mixing")
	require.NoError(t, err)
	assert.Equal(t, ports.BridgeID("bridge1"), id)
}

func TestMediaGatewayAdapter_CreateBridge_Error(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := gw.CreateBridge(context.Background(), "mixing")
	assert.Error(t, err)
}

func TestMediaGatewayAdapter_AddChannelToBridge(t *testing.T) {
	var gotPath string
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := gw.AddChannelToBridge(context.Background(), "bridge1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "/bridges/bridge1/addChannel", gotPath)
}

func TestMediaGatewayAdapter_AddChannelToBridge_Error(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	err := gw.AddChannelToBridge(context.Background(), "bridge1", "chan1")
	assert.Error(t, err)
}

func TestMediaGatewayAdapter_DestroyBridge(t *testing.T) {
	var gotMethod string
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	err := gw.DestroyBridge(context.Background(), "bridge1")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, gotMethod)
}

func TestMediaGatewayAdapter_DestroyBridge_Error(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	err := gw.DestroyBridge(context.Background(), "bridge1")
	assert.Error(t, err)
}

func TestMediaGatewayAdapter_StartPlayback(t *testing.T) {
	var gotMedia string
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		gotMedia = r.URL.Query().Get("media")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pb1"}`))
	})
	err := gw.StartPlayback(context.Background(), "chan1", "sound:hello")
	require.NoError(t, err)
	assert.Equal(t, "sound:hello", gotMedia)
}

func TestMediaGatewayAdapter_StartPlayback_Error(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	err := gw.StartPlayback(context.Background(), "chan1", "sound:hello")
	assert.Error(t, err)
}

func TestMediaGatewayAdapter_StartSnoop_NotImplemented(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("StartSnoop must not make an ARI call in this change")
	})
	_, err := gw.StartSnoop(context.Background(), "chan1", ports.SnoopRequest{})
	assert.Error(t, err)
}

func TestMediaGatewayAdapter_DestroyChannel(t *testing.T) {
	var gotMethod, gotPath string
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	err := gw.DestroyChannel(context.Background(), "chan1")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/channels/chan1", gotPath)
}

func TestMediaGatewayAdapter_DestroyChannel_Error(t *testing.T) {
	gw := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	err := gw.DestroyChannel(context.Background(), "chan1")
	assert.Error(t, err)
}
