//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

func newTenant(name string) domain.Tenant {
	return domain.Tenant{
		ID: shareddomain.NewTenantID(), Name: name, Status: domain.TenantActive,
		ResidencyZone: "EU", CreatedAt: time.Now().UTC(),
	}
}

// newStore provisions two tenants, so the installation starts at 2.
func TestCreateTenant_RefusesAtTheCap(t *testing.T) {
	store, _, _ := newStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateTenant(ctx, newTenant("third"), 3), "2 exist, cap 3: within")

	refused := newTenant("fourth")
	err := store.CreateTenant(ctx, refused, 3)
	require.ErrorIs(t, err, ports.ErrEntitlementExceeded)
	_, err = store.GetTenant(ctx, refused.ID)
	require.ErrorIs(t, err, ports.ErrNotFound, "a refused tenant leaves no row")

	require.NoError(t, store.CreateTenant(ctx, newTenant("uncapped"), 0), "zero is unlimited")
}

// Two provisionings racing for the last slot must not both commit. At
// READ COMMITTED a bare count-then-insert lets both racers see the same
// count, so the check must count only after taking a lock every
// provisioning takes.
//
// Proved deterministically rather than by launching goroutines and
// hoping they interleave — a goroutine race passed with the lock
// removed, which made it no proof at all. Instead: hold the lock, show
// CreateTenant waits on it, take the last slot meanwhile, release, and
// require the waiter to count afresh and refuse.
func TestCreateTenant_CountsOnlyUnderTheProvisioningLock(t *testing.T) {
	store, _, _ := newStore(t)
	ctx := context.Background()
	const maxTenants = 3 // two exist: exactly one slot left

	conn, err := pgx.Connect(ctx, testDatabaseURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })
	holder, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(identitypostgres.TenantCapLock))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- store.CreateTenant(ctx, newTenant("waiter"), maxTenants) }()

	select {
	case err := <-done:
		t.Fatalf("CreateTenant finished (%v) while another provisioning held the lock: "+
			"it counted without serializing, so two racers could both take the last slot", err)
	case <-time.After(300 * time.Millisecond):
	}

	winner := newTenant("winner")
	_, err = holder.Exec(ctx, `
		INSERT INTO tenants (id, name, status, residency_zone, created_at) VALUES ($1, $2, $3, $4, $5)
	`, winner.ID.String(), winner.Name, string(winner.Status), winner.ResidencyZone, winner.CreatedAt)
	require.NoError(t, err)
	require.NoError(t, holder.Commit(ctx))

	select {
	case err := <-done:
		require.ErrorIs(t, err, ports.ErrEntitlementExceeded,
			"the waiter must count after the lock is released and find the slot taken")
	case <-time.After(5 * time.Second):
		t.Fatal("CreateTenant never resumed after the lock was released")
	}
}
