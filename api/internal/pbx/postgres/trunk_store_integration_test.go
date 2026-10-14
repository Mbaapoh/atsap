//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxdomain "atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

var (
	trunks = pbxpostgres.NewTrunkStore()
	routes = pbxpostgres.NewRouteStore()
)

// noResponse is an observation of a carrier that did not answer.
func noResponse() pbxdomain.HealthObservation {
	return pbxdomain.HealthObservation{Health: pbxdomain.HealthUnhealthy, Reason: pbxdomain.ReasonNoResponse, At: time.Now().UTC().Truncate(time.Microsecond)}
}

func newTrunk(tenantID shareddomain.TenantID) pbxdomain.Trunk {
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := shareddomain.NewTrunkID()
	return pbxdomain.Trunk{
		ID: id, TenantID: tenantID, Name: "carrier", Host: "sip.carrier.example", Port: 5060,
		AuthUsername: "acct", CredentialRef: pbxdomain.TrunkCredentialRef(id),
		ChannelLimit: 30, Priority: 1, Health: pbxdomain.HealthUnknown, CreatedAt: now, UpdatedAt: now,
	}
}

func (h *harness) insertTrunk(t *testing.T, tr pbxdomain.Trunk) {
	t.Helper()
	require.NoError(t, h.run(tr.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return trunks.Insert(ctx, tx, tr)
	}))
}

func (h *harness) getTrunk(tenantID shareddomain.TenantID, id shareddomain.TrunkID) (pbxdomain.Trunk, error) {
	var tr pbxdomain.Trunk
	err := h.run(tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		tr, err = trunks.Get(ctx, tx, id)
		return err
	})
	return tr, err
}

func TestTrunkStore_CreateReadUpdateDeleteList(t *testing.T) {
	h := newStore(t)
	tr := newTrunk(h.tenantA)
	h.insertTrunk(t, tr)

	got, err := h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, tr, got)

	ipAuth := newTrunk(h.tenantA)
	ipAuth.AuthUsername, ipAuth.CredentialRef, ipAuth.Priority = "", "", 0
	h.insertTrunk(t, ipAuth)
	got, err = h.getTrunk(h.tenantA, ipAuth.ID)
	require.NoError(t, err)
	assert.Empty(t, got.AuthUsername, "an absent username round-trips as empty, not as a literal")
	assert.Empty(t, got.CredentialRef)

	tr.Name, tr.Port, tr.Priority = "renamed", 5080, 7
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return trunks.Update(ctx, tx, tr) }))
	got, err = h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)
	assert.Equal(t, 5080, got.Port)

	var listed []pbxdomain.Trunk
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		listed, err = trunks.List(ctx, tx, ports.Page{Limit: 10})
		return err
	}))
	require.Len(t, listed, 2)
	assert.Equal(t, ipAuth.ID, listed[0].ID, "listed by priority")

	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return trunks.Delete(ctx, tx, tr.ID) }))
	_, err = h.getTrunk(h.tenantA, tr.ID)
	require.ErrorIs(t, err, ports.ErrNotFound)
	err = h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return trunks.Delete(ctx, tx, tr.ID) })
	require.ErrorIs(t, err, ports.ErrNotFound, "a delete that removed nothing fails closed")
}

// Another tenant's trunk is indistinguishable from a nonexistent one on
// every path, including the health writer.
func TestTrunkStore_CrossTenantIsIndistinguishableFromNonexistent(t *testing.T) {
	h := newStore(t)
	tr := newTrunk(h.tenantA)
	h.insertTrunk(t, tr)
	missing := shareddomain.NewTrunkID()

	for name, id := range map[string]shareddomain.TrunkID{"foreign": tr.ID, "nonexistent": missing} {
		t.Run(name, func(t *testing.T) {
			_, err := h.getTrunk(h.tenantB, id)
			assert.ErrorIs(t, err, ports.ErrNotFound)

			u := tr
			u.ID, u.TenantID = id, h.tenantB
			assert.ErrorIs(t, h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error { return trunks.Update(ctx, tx, u) }), ports.ErrNotFound)
			assert.ErrorIs(t, h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error { return trunks.Delete(ctx, tx, id) }), ports.ErrNotFound)
			assert.ErrorIs(t, h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error {
				return trunks.SetHealth(ctx, tx, id, noResponse())
			}), ports.ErrNotFound)
		})
	}

	var listed []pbxdomain.Trunk
	require.NoError(t, h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		listed, err = trunks.List(ctx, tx, ports.Page{Limit: 10})
		return err
	}))
	assert.Empty(t, listed)

	got, err := h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, pbxdomain.HealthUnknown, got.Health, "tenant B's attempts changed nothing")
}

// Configuration and observation have separate writers: Update cannot
// carry a health value in.
func TestTrunkStore_UpdateNeverWritesHealth(t *testing.T) {
	h := newStore(t)
	tr := newTrunk(h.tenantA)
	h.insertTrunk(t, tr)

	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return trunks.SetHealth(ctx, tx, tr.ID, noResponse())
	}))
	tr.Health = pbxdomain.HealthHealthy // a caller "setting" health through configuration
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return trunks.Update(ctx, tx, tr) }))

	got, err := h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, pbxdomain.HealthUnhealthy, got.Health)
}

func TestRouteStore_LifecycleAndTenancy(t *testing.T) {
	h := newStore(t)
	trA, trB := newTrunk(h.tenantA), newTrunk(h.tenantB)
	h.insertTrunk(t, trA)
	h.insertTrunk(t, trB)

	newRoute := func(pattern string, trunkID shareddomain.TrunkID) pbxdomain.CarrierRoute {
		return pbxdomain.CarrierRoute{ID: shareddomain.NewRouteID(), TenantID: h.tenantA, PrefixPattern: pattern,
			TrunkID: trunkID, CostPerMinute: 1200, Priority: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	}
	r1, r2 := newRoute("44", trA.ID), newRoute("+1", trA.ID)
	for _, r := range []pbxdomain.CarrierRoute{r1, r2} {
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return routes.Insert(ctx, tx, r) }))
	}

	// A route pointing at tenant B's trunk gets the nonexistent-trunk answer.
	foreign := newRoute("33", trB.ID)
	err := h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return routes.Insert(ctx, tx, foreign) })
	require.ErrorIs(t, err, ports.ErrNotFound)
	nowhere := newRoute("33", shareddomain.NewTrunkID())
	err = h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return routes.Insert(ctx, tx, nowhere) })
	require.ErrorIs(t, err, ports.ErrNotFound)

	var got pbxdomain.CarrierRoute
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, err = routes.Get(ctx, tx, r1.ID)
		return err
	}))
	assert.Equal(t, r1, got)

	err = h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error { _, err := routes.Get(ctx, tx, r1.ID); return err })
	assert.ErrorIs(t, err, ports.ErrNotFound, "another tenant's route reads as nonexistent")
	err = h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error { return routes.Delete(ctx, tx, r1.ID) })
	assert.ErrorIs(t, err, ports.ErrNotFound)

	var removed []pbxdomain.CarrierRoute
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		removed, err = routes.DeleteByTrunk(ctx, tx, trA.ID)
		return err
	}))
	assert.Len(t, removed, 2, "DeleteByTrunk returns what it removed, for the audit trail")

	var listed []pbxdomain.CarrierRoute
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		listed, err = routes.List(ctx, tx, ports.Page{Limit: 10})
		return err
	}))
	assert.Empty(t, listed)
}

// A check's result is recorded only while the trunk still has the address
// that was checked; a result for an address since changed, or for a trunk
// since deleted, is dropped (pbx-trunk-health-probing design D3).
func TestTrunkStore_CheckedHealthIsGuardedByAddress(t *testing.T) {
	h := newStore(t)
	tr := newTrunk(h.tenantA)
	h.insertTrunk(t, tr)

	got, err := h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, pbxdomain.HealthUnknown, got.Health)
	assert.Empty(t, got.HealthReason)
	assert.Nil(t, got.HealthObservedAt, "never observed")

	set := func(host string, port int, obs pbxdomain.HealthObservation) bool {
		var written bool
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			written, err = trunks.SetCheckedHealth(ctx, tx, tr.ID, host, port, obs)
			return err
		}))
		return written
	}
	answering := pbxdomain.HealthObservation{Health: pbxdomain.HealthHealthy, Reason: pbxdomain.ReasonAnswering, At: time.Now().UTC().Truncate(time.Microsecond)}
	assert.True(t, set(tr.Host, tr.Port, answering))
	got, err = h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, pbxdomain.HealthHealthy, got.Health)
	assert.Equal(t, pbxdomain.ReasonAnswering, got.HealthReason)
	require.NotNil(t, got.HealthObservedAt)
	assert.True(t, answering.At.Equal(*got.HealthObservedAt))

	assert.False(t, set("old.carrier.example", tr.Port, noResponse()), "the address changed since the check")
	assert.False(t, set(tr.Host, 5080, noResponse()), "the port changed since the check")
	got, err = h.getTrunk(h.tenantA, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, pbxdomain.HealthHealthy, got.Health, "a stale result changed nothing")

	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return trunks.Delete(ctx, tx, tr.ID) }))
	assert.False(t, set(tr.Host, tr.Port, noResponse()), "a deleted trunk takes no result")
}

// Unknown is where a trunk starts, never an observation; an observation
// needs a reason and a time.
func TestTrunkStore_RefusesWhatIsNotAnObservation(t *testing.T) {
	h := newStore(t)
	tr := newTrunk(h.tenantA)
	h.insertTrunk(t, tr)
	now := time.Now().UTC()
	for name, obs := range map[string]pbxdomain.HealthObservation{
		"unknown":   {Health: pbxdomain.HealthUnknown, Reason: pbxdomain.ReasonAnswering, At: now},
		"no reason": {Health: pbxdomain.HealthHealthy, At: now},
		"no time":   {Health: pbxdomain.HealthHealthy, Reason: pbxdomain.ReasonAnswering},
	} {
		t.Run(name, func(t *testing.T) {
			err := h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error { return trunks.SetHealth(ctx, tx, tr.ID, obs) })
			assert.ErrorIs(t, err, ports.ErrInvalidInput)
		})
	}
}
