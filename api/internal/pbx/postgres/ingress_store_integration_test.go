//go:build integration

package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxdomain "atsap-api/internal/pbx/domain"
	pbxingress "atsap-api/internal/pbx/ingress"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	telephonyports "atsap-api/internal/telephony/ports"
)

// D-77 across two tenants, through the real store and the attributor: two
// tenants' trunks recognise one carrier address; each tenant's number
// reaches that tenant's trunk; a number in both is refused.
func TestIngressStore_AttributesAcrossTenants(t *testing.T) {
	h := newStore(t)
	numbers := pbxpostgres.NewInboundNumberStore()
	extA := newExtension(h.tenantA, "1000", "Alice", pbxdomain.DeviceSIP)
	require.NoError(t, h.insert(t, extA))
	extB := newExtension(h.tenantB, "1000", "Bob", pbxdomain.DeviceSIP)
	require.NoError(t, h.insert(t, extB))

	trA := newTrunk(h.tenantA)
	trA.ResolvedAddresses = []netip.Addr{netip.MustParseAddr("203.0.113.10")}
	h.insertTrunk(t, trA)
	trB := newTrunk(h.tenantB)
	trB.InboundAddresses = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/28")}
	h.insertTrunk(t, trB)

	for _, n := range []pbxdomain.InboundNumber{
		{ID: uuid.New(), TenantID: h.tenantA, Number: pbxdomain.NormalizeDialled("+442071230001"), ExtensionID: extA.ID},
		{ID: uuid.New(), TenantID: h.tenantB, Number: pbxdomain.NormalizeDialled("+442071230002"), ExtensionID: extB.ID},
		{ID: uuid.New(), TenantID: h.tenantA, Number: pbxdomain.NormalizeDialled("+442071230009"), ExtensionID: extA.ID},
		{ID: uuid.New(), TenantID: h.tenantB, Number: pbxdomain.NormalizeDialled("+442071230009"), ExtensionID: extB.ID},
	} {
		require.NoError(t, h.run(n.TenantID, func(ctx context.Context, tx pgx.Tx) error { return numbers.Insert(ctx, tx, n) }))
	}

	attributor := pbxingress.NewAttributor(pbxpostgres.NewIngressStore(h.pool), slog.New(slog.NewTextHandler(io.Discard, nil)))
	attribute := func(dialled string) telephonyports.Attribution {
		t.Helper()
		got, err := attributor.Attribute(context.Background(), telephonyports.AttributionRequest{
			SourceEndpoint: pbxdomain.CarrierIngressEndpoint, SourceAddress: "203.0.113.10:5060", Dialled: dialled,
		})
		require.NoError(t, err)
		return got
	}

	got := attribute("+442071230001")
	assert.Equal(t, h.tenantA, got.TenantID)
	assert.Equal(t, pbxdomain.TrunkEndpointIdentifier(trA.ID), got.SourceEndpoint)
	got = attribute("+442071230002")
	assert.Equal(t, h.tenantB, got.TenantID)
	assert.Equal(t, pbxdomain.TrunkEndpointIdentifier(trB.ID), got.SourceEndpoint)
	assert.True(t, attribute("+442071230009").Refused, "a number in both tenants is refused")
	assert.True(t, attribute("+442071239999").Refused, "a number in neither")
}
