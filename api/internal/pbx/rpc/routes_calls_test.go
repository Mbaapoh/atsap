package rpc_test

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

func codeOf(t *testing.T, err error) connect.Code {
	t.Helper()
	var ce *connect.Error
	require.True(t, errors.As(err, &ce), "a connect error: %v", err)
	return ce.Code()
}

// The trunk and route reads and deletes reach the service with the ids
// and page the caller sent, return exactly what it answers, and refuse a
// malformed id before reaching it.
func TestTrunksAndRoutes_OnTheWire(t *testing.T) {
	e := newEnv(t)
	trunk := shareddomain.NewTrunkID()
	e.svc.trunkViews = []ports.TrunkView{{ID: trunk, Name: "Carrier", Host: "sip.carrier.example", Port: 5060, ChannelLimit: 4, Health: domain.HealthHealthy}}
	listed, err := e.h.ListTrunks(e.ctx, connect.NewRequest(&atsapbxv1.ListTrunksRequest{TenantId: e.tenant.String(), Limit: 10, Offset: 20}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetTrunks(), 1)
	assert.Equal(t, trunk.String(), listed.Msg.GetTrunks()[0].GetTrunkId())
	assert.Equal(t, ports.Page{Limit: 10, Offset: 20}, e.svc.lastPage)

	_, err = e.h.DeleteTrunk(e.ctx, connect.NewRequest(&atsapbxv1.DeleteTrunkRequest{TenantId: e.tenant.String(), TrunkId: "not-an-id"}))
	assert.Equal(t, connect.CodeInvalidArgument, codeOf(t, err))
	_, err = e.h.DeleteTrunk(e.ctx, connect.NewRequest(&atsapbxv1.DeleteTrunkRequest{TenantId: e.tenant.String(), TrunkId: trunk.String()}))
	require.NoError(t, err)
	assert.Equal(t, trunk, e.svc.lastTrunkID)

	route := shareddomain.NewRouteID()
	e.svc.routeView = ports.RouteView{ID: route, PrefixPattern: "+44", TrunkID: trunk, CostPerMinute: 1200, Priority: 2}
	_, err = e.h.CreateRoute(e.ctx, connect.NewRequest(&atsapbxv1.CreateRouteRequest{TenantId: e.tenant.String(), PrefixPattern: "+44", TrunkId: "bad"}))
	assert.Equal(t, connect.CodeInvalidArgument, codeOf(t, err))
	created, err := e.h.CreateRoute(e.ctx, connect.NewRequest(&atsapbxv1.CreateRouteRequest{
		TenantId: e.tenant.String(), PrefixPattern: "+44", TrunkId: trunk.String(), CostPerMinuteMicros: 1200, Priority: 2,
	}))
	require.NoError(t, err)
	assert.Equal(t, ports.CreateRouteCommand{TenantID: e.tenant, PrefixPattern: "+44", TrunkID: trunk, CostPerMinute: 1200, Priority: 2}, e.svc.lastCreateRoute)
	assert.Equal(t, &atsapbxv1.Route{RouteId: route.String(), PrefixPattern: "+44", TrunkId: trunk.String(), CostPerMinuteMicros: 1200, Priority: 2},
		created.Msg.GetRoute())

	e.svc.routeViews = []ports.RouteView{e.svc.routeView}
	routes, err := e.h.ListRoutes(e.ctx, connect.NewRequest(&atsapbxv1.ListRoutesRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
	assert.Len(t, routes.Msg.GetRoutes(), 1)

	_, err = e.h.DeleteRoute(e.ctx, connect.NewRequest(&atsapbxv1.DeleteRouteRequest{TenantId: e.tenant.String(), RouteId: "bad"}))
	assert.Equal(t, connect.CodeInvalidArgument, codeOf(t, err))
	_, err = e.h.DeleteRoute(e.ctx, connect.NewRequest(&atsapbxv1.DeleteRouteRequest{TenantId: e.tenant.String(), RouteId: route.String()}))
	require.NoError(t, err)
	assert.Equal(t, route, e.svc.lastRouteID)

	e.svc.err = fmt.Errorf("trunk %s: %w", trunk, ports.ErrNotFound)
	_, err = e.h.DeleteRoute(e.ctx, connect.NewRequest(&atsapbxv1.DeleteRouteRequest{TenantId: e.tenant.String(), RouteId: route.String()}))
	assert.Equal(t, connect.CodeNotFound, codeOf(t, err))
}

// PlaceCall and HangupCall carry exactly the caller's ids and destination,
// and each placement precondition maps to a fixed, actionable message.
func TestPlaceAndHangupCall_OnTheWire(t *testing.T) {
	e := newEnv(t)
	ext := shareddomain.NewExtensionID()
	call := shareddomain.NewCallID()
	e.svc.placed = ports.PlacedCall{CallID: call}

	_, err := e.h.PlaceCall(e.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{TenantId: e.tenant.String(), ExtensionId: "bad", Destination: "+442071234567"}))
	assert.Equal(t, connect.CodeInvalidArgument, codeOf(t, err))
	placed, err := e.h.PlaceCall(e.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{TenantId: e.tenant.String(), ExtensionId: ext.String(), Destination: "+442071234567"}))
	require.NoError(t, err)
	assert.Equal(t, call.String(), placed.Msg.GetCallId())
	assert.Equal(t, ports.PlaceCallCommand{TenantID: e.tenant, ExtensionID: ext, Destination: "+442071234567"}, e.svc.lastPlaceCall)

	for cause, want := range map[error]string{
		domain.ErrUnroutable:                                  "destination is unroutable",
		ports.ErrTrunkCapacity:                                "trunk capacity",
		ports.ErrCallerNotRegistered:                          "no registered device",
		fmt.Errorf("%w: over_capacity", ports.ErrCallRefused): "over_capacity",
	} {
		e.svc.err = fmt.Errorf("place: %w", cause)
		_, err := e.h.PlaceCall(e.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{TenantId: e.tenant.String(), ExtensionId: ext.String(), Destination: "+1"}))
		var ce *connect.Error
		require.True(t, errors.As(err, &ce))
		assert.Equal(t, connect.CodeFailedPrecondition, ce.Code(), cause.Error())
		assert.Contains(t, ce.Message(), want)
	}
	e.svc.err = errors.New("pgx: connection refused")
	_, err = e.h.PlaceCall(e.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{TenantId: e.tenant.String(), ExtensionId: ext.String(), Destination: "+1"}))
	assert.Equal(t, connect.CodeInternal, codeOf(t, err))
	assert.NotContains(t, err.Error(), "pgx", "internal text never leaves")

	e.svc.err = nil
	_, err = e.h.HangupCall(e.ctx, connect.NewRequest(&atsapbxv1.HangupCallRequest{TenantId: e.tenant.String(), CallId: "bad"}))
	assert.Equal(t, connect.CodeInvalidArgument, codeOf(t, err))
	_, err = e.h.HangupCall(e.ctx, connect.NewRequest(&atsapbxv1.HangupCallRequest{TenantId: e.tenant.String(), CallId: call.String()}))
	require.NoError(t, err)
	assert.Equal(t, call, e.svc.lastCallID)
	e.svc.err = fmt.Errorf("call %s: %w", call, ports.ErrNotFound)
	_, err = e.h.HangupCall(e.ctx, connect.NewRequest(&atsapbxv1.HangupCallRequest{TenantId: e.tenant.String(), CallId: call.String()}))
	assert.Equal(t, connect.CodeNotFound, codeOf(t, err))
}
