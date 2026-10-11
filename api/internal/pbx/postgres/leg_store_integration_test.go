//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxdomain "atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

var legs = pbxpostgres.NewCallLegStore()

func TestCallLegStore_LifecycleAndTenancy(t *testing.T) {
	h := newStore(t)
	tr := newTrunk(h.tenantA)
	h.insertTrunk(t, tr)
	routeID := shareddomain.NewRouteID()
	now := time.Now().UTC().Truncate(time.Microsecond)

	outboundCall := shareddomain.NewCallID()
	outbound := pbxdomain.PlacedCallLeg{ID: uuid.New(), TenantID: h.tenantA,
		ExtensionID: shareddomain.NewExtensionID(), TrunkID: &tr.ID, RouteID: &routeID,
		Destination: "+442071234567", CreatedAt: now}
	internal := pbxdomain.PlacedCallLeg{ID: uuid.New(), TenantID: h.tenantA,
		ExtensionID: shareddomain.NewExtensionID(), Destination: "1005", CreatedAt: now}
	for _, l := range []pbxdomain.PlacedCallLeg{outbound, internal} {
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return legs.Insert(ctx, tx, l) }))
	}
	// The call id arrives after the channel is reserved.
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return legs.AttachCall(ctx, tx, outbound.ID, outboundCall)
	}))
	outbound.CallID = &outboundCall

	var got pbxdomain.PlacedCallLeg
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, err = legs.Get(ctx, tx, outboundCall)
		return err
	}))
	assert.Equal(t, outbound, got, "the matched route and trunk are on record")

	live := func() int {
		var n int
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			l, err := legs.LiveOnTrunk(ctx, tx, tr.ID)
			n = len(l)
			return err
		}))
		return n
	}
	assert.Equal(t, 1, live(), "an internal call occupies no trunk channel")

	released := now.Add(time.Minute)
	for range 2 { // releasing twice is a no-op
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			return legs.Release(ctx, tx, outbound.ID, released)
		}))
	}
	assert.Zero(t, live())
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, err = legs.Get(ctx, tx, outboundCall)
		return err
	}))
	assert.Equal(t, released, *got.ReleasedAt, "the first release time is kept")

	// Another tenant's call reads exactly like one never placed.
	for _, id := range []shareddomain.CallID{outboundCall, shareddomain.NewCallID()} {
		err := h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error { _, err := legs.Get(ctx, tx, id); return err })
		assert.ErrorIs(t, err, ports.ErrNotFound)
	}
}
