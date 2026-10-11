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
	"google.golang.org/protobuf/reflect/protoreflect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/rpc"
	shareddomain "atsap-api/internal/shared/domain"
)

// --- 4.5: refusals on the wire ------------------------------------------

func TestPlaceCallRPC_RefusalCodes(t *testing.T) {
	p := newPlacer(t)
	api := rpc.NewPbxHandler(p.svc)
	tr := p.trunk(t, "Only", 1, 0)
	p.route(t, "44", tr, 0)
	place := func(ext, dest string) error {
		_, err := api.PlaceCall(p.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{ExtensionId: ext, Destination: dest}))
		return err
	}
	me := p.from.Extension.ID.String()

	err := place(me, "3312345")
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "unroutable")

	require.NoError(t, place(me, "4420"))
	err = place(me, "4421")
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "trunk capacity")

	silent := p.create(t, "1002")
	err = place(silent.Extension.ID.String(), "4420")
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "registered device")

	err = place(me, "44#1")
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, otherCtx := p.otherTenant(t)
	_, errForeign := api.PlaceCall(otherCtx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{ExtensionId: me, Destination: "4420"}))
	_, errMissing := api.PlaceCall(otherCtx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{
		ExtensionId: shareddomain.NewExtensionID().String(), Destination: "4420"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(errForeign))
	assert.Equal(t, errMissing.Error(), errForeign.Error(), "another tenant's extension reads as nonexistent")

	p.authz.deny = identityports.ErrPermissionDenied
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(place(me, "4420")))
}

func TestPlaceCallRPC_ScreeningRefusalCarriesTheReason(t *testing.T) {
	p := newPlacer(t)
	api := rpc.NewPbxHandler(p.svc)
	p.route(t, "44", p.trunk(t, "Only", 5, 0), 0)
	p.calls.screenOut = "capacity verdict rejected the call"

	_, err := api.PlaceCall(p.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{
		ExtensionId: p.from.Extension.ID.String(), Destination: "4420"}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "capacity verdict rejected the call")
}

// --- 4.6: DoD 9 — the wire cannot reach the bypass ------------------------

// fillEvery sets every field of a message to a non-zero value, so a
// future field that somehow fed the bypass would be exercised too.
func fillEvery(m proto.Message, strs map[string]string) {
	r := m.ProtoReflect()
	fields := r.Descriptor().Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		switch f.Kind() {
		case protoreflect.StringKind:
			v, ok := strs[string(f.Name())]
			if !ok {
				v = "x"
			}
			r.Set(f, protoreflect.ValueOfString(v))
		case protoreflect.BoolKind:
			r.Set(f, protoreflect.ValueOfBool(true))
		case protoreflect.Int32Kind, protoreflect.Int64Kind:
			r.Set(f, protoreflect.ValueOfInt64(1))
		}
	}
}

func TestPlaceCallRPC_BypassComesOnlyFromTheDialPlan(t *testing.T) {
	p := newPlacer(t)
	api := rpc.NewPbxHandler(p.svc)
	tr := p.trunk(t, "Only", 5, 0)
	p.route(t, "11", tr, 0)
	p.route(t, "44", tr, 0)

	for dest, want := range map[string]bool{"4420": false, "1120": false, "112": true} {
		req := &atsapbxv1.PlaceCallRequest{}
		fillEvery(req, map[string]string{
			"tenant_id": p.tenant.String(), "extension_id": p.from.Extension.ID.String(), "destination": dest,
		})
		// Round-trip through the wire encoding, as a real request arrives.
		raw, err := proto.Marshal(req)
		require.NoError(t, err)
		decoded := &atsapbxv1.PlaceCallRequest{}
		require.NoError(t, proto.Unmarshal(raw, decoded))

		_, err = api.PlaceCall(p.ctx, connect.NewRequest(decoded))
		require.NoError(t, err, dest)
		assert.Equal(t, want, p.calls.last().BypassScreening,
			"%s: exempt from checks only if the dial plan matched an emergency number", dest)
	}
}

// --- 4.7: DoD 13 — nothing engine-shaped reaches a caller -----------------

func TestPlaceCallRPC_NoEngineIdentifierReachesACaller(t *testing.T) {
	p := newPlacer(t)
	api := rpc.NewPbxHandler(p.svc)
	p.route(t, "44", p.trunk(t, "Only", 1, 0), 0)
	me := p.from.Extension.ID.String()
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

	placed, err := api.PlaceCall(p.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{ExtensionId: me, Destination: "4420"}))
	require.NoError(t, err)
	capture(placed.Msg, nil)
	hung, err := api.HangupCall(p.ctx, connect.NewRequest(&atsapbxv1.HangupCallRequest{CallId: placed.Msg.GetCallId()}))
	capture(hung.Msg, err)

	for _, dest := range []string{"3312345", "44#"} {
		_, err := api.PlaceCall(p.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{ExtensionId: me, Destination: dest}))
		capture(nil, err)
	}
	_, err = api.HangupCall(p.ctx, connect.NewRequest(&atsapbxv1.HangupCallRequest{CallId: shareddomain.NewCallID().String()}))
	capture(nil, err)
	p.calls.failNext = errors.New(`ari originate PJSIP/4420@t_0123456789abcdef0123456789abcdef: context stasis-in transport-udp failed`)
	_, err = api.PlaceCall(p.ctx, connect.NewRequest(&atsapbxv1.PlaceCallRequest{ExtensionId: me, Destination: "4420"}))
	require.Error(t, err)
	capture(nil, err)

	identifier := regexp.MustCompile(`[et]_[0-9a-f]{32}`)
	for _, text := range rendered {
		for _, f := range []string{"pjsip", "stasis", "transport-", "ps_", "ari "} {
			assert.NotContains(t, strings.ToLower(text), f, "a caller saw %q in %s", f, text)
		}
		assert.False(t, identifier.MatchString(text), "a caller saw an engine identifier in %s", text)
	}
	assert.Len(t, rendered, 6)
}
