//go:build integration

package application_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/rpc"
	shareddomain "atsap-api/internal/shared/domain"
)

// --- 3.1: the RPC surface over the real service --------------------------

func TestTrunkRPCs_LifecycleThroughTheWire(t *testing.T) {
	h := newHarness(t)
	api := rpc.NewPbxHandler(h.svc)

	created, err := api.CreateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{
		Name: "Primary", Host: "sip.carrier.example", AuthUsername: "acct", Secret: carrierSecret, ChannelLimit: 30,
	}))
	require.NoError(t, err)
	tr := created.Msg.GetTrunk()
	assert.Equal(t, int32(5060), tr.GetPort(), "an unset port is the SIP default")
	assert.True(t, tr.GetHasSecret())
	assert.Equal(t, atsapbxv1.TrunkHealth_TRUNK_HEALTH_HEALTHY, tr.GetHealth())

	route, err := api.CreateRoute(h.ctx, connect.NewRequest(&atsapbxv1.CreateRouteRequest{
		PrefixPattern: "+44", TrunkId: tr.GetTrunkId(), CostPerMinuteMicros: 12000,
	}))
	require.NoError(t, err)

	routes, err := api.ListRoutes(h.ctx, connect.NewRequest(&atsapbxv1.ListRoutesRequest{}))
	require.NoError(t, err)
	require.Len(t, routes.Msg.GetRoutes(), 1)
	assert.Equal(t, route.Msg.GetRoute().GetRouteId(), routes.Msg.GetRoutes()[0].GetRouteId())

	updated, err := api.UpdateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.UpdateTrunkRequest{
		TrunkId: tr.GetTrunkId(), Name: "Renamed", Host: "sip.carrier.example", AuthUsername: "acct", ChannelLimit: 10,
	}))
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Msg.GetTrunk().GetName())
	assert.True(t, updated.Msg.GetTrunk().GetHasSecret(), "no secret supplied: kept")

	_, err = api.DeleteRoute(h.ctx, connect.NewRequest(&atsapbxv1.DeleteRouteRequest{RouteId: route.Msg.GetRoute().GetRouteId()}))
	require.NoError(t, err)
	_, err = api.DeleteTrunk(h.ctx, connect.NewRequest(&atsapbxv1.DeleteTrunkRequest{TrunkId: tr.GetTrunkId()}))
	require.NoError(t, err)
	trunks, err := api.ListTrunks(h.ctx, connect.NewRequest(&atsapbxv1.ListTrunksRequest{}))
	require.NoError(t, err)
	assert.Empty(t, trunks.Msg.GetTrunks())
}

func TestTrunkRPCs_RefusalsOnTheWire(t *testing.T) {
	h := newHarness(t)
	api := rpc.NewPbxHandler(h.svc)
	owned := h.createTrunk(t, "Primary")

	// Invalid input names the offending field.
	_, err := api.CreateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{
		Name: "Bad", Host: "sip.carrier.example", Secret: "orphan", ChannelLimit: 1,
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "username", "the reason identifies the offending field")
	_, err = api.CreateRoute(h.ctx, connect.NewRequest(&atsapbxv1.CreateRouteRequest{
		PrefixPattern: "4[0-9]", TrunkId: owned.ID.String(),
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "prefix pattern")

	// Cross-tenant answers exactly as nonexistent, by code AND message.
	otherID, otherCtx := h.otherTenant(t)
	_ = otherID
	foreign := connect.NewRequest(&atsapbxv1.DeleteTrunkRequest{TrunkId: owned.ID.String()})
	missing := connect.NewRequest(&atsapbxv1.DeleteTrunkRequest{TrunkId: shareddomain.NewTrunkID().String()})
	_, errForeign := api.DeleteTrunk(otherCtx, foreign)
	_, errMissing := api.DeleteTrunk(otherCtx, missing)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(errForeign))
	assert.Equal(t, errMissing.Error(), errForeign.Error(), "indistinguishable from a nonexistent trunk")

	// A body naming another tenant is refused before the service runs.
	_, err = api.ListTrunks(h.ctx, connect.NewRequest(&atsapbxv1.ListTrunksRequest{TenantId: otherID.String()}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// Unauthorized is PERMISSION_DENIED, and nothing is written.
	h.authz.deny = identityports.ErrPermissionDenied
	_, err = api.CreateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{
		Name: "Nope", Host: "sip.carrier.example", ChannelLimit: 1,
	}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.Equal(t, 1, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks`))
}

// --- 3.2: no engine identifier reaches any caller ------------------------

// Every response and every error the trunk/route surface can produce is
// rendered and searched for the engine's vocabulary: the projected id,
// the dialplan context, transport names and table names (LLD-03 DoD 13,
// PRD principle 4).
func TestTrunkRPCs_NoEngineIdentifierReachesACaller(t *testing.T) {
	h := newHarness(t)
	api := rpc.NewPbxHandler(h.svc)
	var rendered []string
	capture := func(m proto.Message, err error) {
		if err != nil {
			rendered = append(rendered, err.Error())
			return
		}
		b, mErr := protojson.Marshal(m)
		require.NoError(t, mErr)
		rendered = append(rendered, string(b))
	}

	c, err := api.CreateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{
		Name: "Primary", Host: "sip.carrier.example", AuthUsername: "acct", Secret: carrierSecret, ChannelLimit: 5,
	}))
	require.NoError(t, err)
	capture(c.Msg, nil)
	trunkID := c.Msg.GetTrunk().GetTrunkId()

	r, err := api.CreateRoute(h.ctx, connect.NewRequest(&atsapbxv1.CreateRouteRequest{PrefixPattern: "44", TrunkId: trunkID}))
	capture(r.Msg, err)
	lt, err := api.ListTrunks(h.ctx, connect.NewRequest(&atsapbxv1.ListTrunksRequest{}))
	capture(lt.Msg, err)
	lr, err := api.ListRoutes(h.ctx, connect.NewRequest(&atsapbxv1.ListRoutesRequest{}))
	capture(lr.Msg, err)
	u, err := api.UpdateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.UpdateTrunkRequest{
		TrunkId: trunkID, Name: "Primary", Host: "sip.carrier.example", AuthUsername: "acct", ChannelLimit: 5,
	}))
	capture(u.Msg, err)

	// Errors: invalid input, not found, and an internal engine failure.
	_, err = api.CreateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{Name: "x", Host: "bad host"}))
	capture(nil, err)
	_, err = api.DeleteTrunk(h.ctx, connect.NewRequest(&atsapbxv1.DeleteTrunkRequest{TrunkId: shareddomain.NewTrunkID().String()}))
	capture(nil, err)
	h.projector.failProjectTrunk = errors.New(`insert ps_endpoints: duplicate key "t_00ff" context stasis-in transport-udp`)
	_, err = api.CreateTrunk(h.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{
		Name: "Second", Host: "sip.carrier.example", ChannelLimit: 5,
	}))
	require.Error(t, err)
	capture(nil, err)

	id, err := shareddomain.ParseTrunkID(trunkID)
	require.NoError(t, err)
	forbidden := []string{domain.TrunkEndpointIdentifier(id), "stasis", "transport-", "ps_", carrierSecret}
	// Any projected identifier at all, not only this trunk's.
	identifierShape := regexp.MustCompile(`[et]_[0-9a-f]{32}`)
	for _, text := range rendered {
		for _, f := range forbidden {
			assert.NotContains(t, strings.ToLower(text), strings.ToLower(f), "a caller saw %q in %s", f, text)
		}
		assert.False(t, identifierShape.MatchString(text), "a caller saw an engine identifier in %s", text)
	}
	assert.Len(t, rendered, 8)
}
