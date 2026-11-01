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

// pbx-carrier-addresses (D-76): the inbound signalling addresses travel
// from both write requests to the service exactly as entered, come back
// on reads, and a refusal names the field without echoing anything but
// the caller's own input.
func TestTrunkInboundAddresses_OnTheWire(t *testing.T) {
	e := newEnv(t)
	e.svc.trunkView = ports.TrunkView{ID: shareddomain.NewTrunkID(), Name: "Carrier", Host: "sip.carrier.example", Port: 5060,
		ChannelLimit: 2, Health: domain.HealthUnknown, InboundAddresses: []string{"198.51.100.0/24", "203.0.113.9"}}

	created, err := e.h.CreateTrunk(e.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{
		TenantId: e.tenant.String(), Name: "Carrier", Host: "sip.carrier.example", ChannelLimit: 2,
		InboundAddresses: []string{"203.0.113.9", "198.51.100.0/24"},
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"203.0.113.9", "198.51.100.0/24"}, e.svc.lastCreateTrunk.InboundAddresses, "passed on as entered; the domain parses")
	assert.Equal(t, []string{"198.51.100.0/24", "203.0.113.9"}, created.Msg.GetTrunk().GetInboundAddresses())

	_, err = e.h.UpdateTrunk(e.ctx, connect.NewRequest(&atsapbxv1.UpdateTrunkRequest{
		TenantId: e.tenant.String(), TrunkId: e.svc.trunkView.ID.String(), Name: "Carrier", Host: "sip.carrier.example", ChannelLimit: 2,
	}))
	require.NoError(t, err)
	assert.Empty(t, e.svc.lastUpdateTrunk.InboundAddresses, "an update that supplies none replaces the list with none")

	for _, cause := range []error{domain.ErrInboundAddressNotCarrier, domain.ErrTrunkHostNotCarrier, domain.ErrInboundAddressTooBroad} {
		e.svc.err = fmt.Errorf("%w: %w: entry 2 127.0.0.0/16", ports.ErrInvalidInput, cause)
		_, err = e.h.CreateTrunk(e.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{TenantId: e.tenant.String(), Name: "x", Host: "localhost", ChannelLimit: 1}))
		var ce *connect.Error
		require.True(t, errors.As(err, &ce), cause.Error())
		assert.Equal(t, connect.CodeInvalidArgument, ce.Code(), cause.Error())
		assert.Contains(t, ce.Message(), cause.Error(), "the refusal names what to fix")
		assert.NotContains(t, ce.Message(), "invalid input", "no internal prefix leaks")
	}
}

func TestToProtoTrunk_AddressNotAllowed(t *testing.T) {
	e := newEnv(t)
	e.svc.trunkView = ports.TrunkView{ID: shareddomain.NewTrunkID(), Health: domain.HealthUnhealthy, HealthReason: domain.ReasonAddressNotAllowed}
	got, err := e.h.CreateTrunk(e.ctx, connect.NewRequest(&atsapbxv1.CreateTrunkRequest{TenantId: e.tenant.String(), Name: "x", Host: "loop.example", ChannelLimit: 1}))
	require.NoError(t, err)
	assert.Equal(t, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_ADDRESS_NOT_ALLOWED, got.Msg.GetTrunk().GetHealthReason())
}
