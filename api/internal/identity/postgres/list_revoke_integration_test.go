//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

func admin(t *testing.T, store *identitypostgres.Store, tenant shareddomain.TenantID, username string) domain.Principal {
	t.Helper()
	p := newPrincipal(t, store, tenant, username)
	p.Role = "TENANT_ADMIN"
	require.NoError(t, store.CreatePrincipal(context.Background(), p))
	require.NoError(t, store.CreateRoleBinding(context.Background(), domain.RoleBinding{
		PrincipalID: p.ID, TenantID: tenant, Role: "TENANT_ADMIN", Scope: domain.ScopeTenant, CreatedAt: p.CreatedAt}))
	return p
}

func TestListPrincipals_PagedIsolatedAndHashless(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()
	a1, a2 := admin(t, store, tenantA, "alice"), admin(t, store, tenantA, "bob")
	admin(t, store, tenantB, "mallory")

	got, err := store.ListPrincipals(ctx, tenantA, 10, 0)
	require.NoError(t, err)
	require.Len(t, got, 2, "tenant B's user never appears")
	assert.Equal(t, "alice@example.test", got[0].Email, "ordered by email")
	page2, err := store.ListPrincipals(ctx, tenantA, 1, 1)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, "bob@example.test", page2[0].Email)

	bindings, err := store.ListRoleBindingsFor(ctx, tenantA, []shareddomain.PrincipalID{a1.ID, a2.ID})
	require.NoError(t, err)
	assert.Len(t, bindings[a1.ID], 1)
	assert.Len(t, bindings[a2.ID], 1)
}

func TestRevokeRoleBinding_LastAdministratorIsKept(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	a1, a2 := admin(t, store, tenantA, "alice"), admin(t, store, tenantA, "bob")
	bind := func(p domain.Principal) domain.RoleBinding {
		return domain.RoleBinding{PrincipalID: p.ID, TenantID: tenantA, Role: "TENANT_ADMIN", Scope: domain.ScopeTenant}
	}

	require.NoError(t, store.RevokeRoleBinding(ctx, bind(a1)), "one of two administrators may go")
	require.ErrorIs(t, store.RevokeRoleBinding(ctx, bind(a2)), ports.ErrLastAdministrator)
	require.ErrorIs(t, store.RevokeRoleBinding(ctx, bind(a1)), ports.ErrNotFound, "a binding already gone is not found")
	left, err := store.ListRoleBindings(ctx, tenantA, a2.ID)
	require.NoError(t, err)
	assert.Len(t, left, 1, "the last administrator's binding remains")
}

// Two revocations racing for the last two administrators must not both
// succeed. Proved deterministically: a holder takes the tenant's lock and
// removes one administrator's binding; the second revocation must wait,
// then count afresh and refuse. Goroutine races pass by luck.
func TestRevokeRoleBinding_SerializesOnTheTenantLock(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	a1, a2 := admin(t, store, tenantA, "alice"), admin(t, store, tenantA, "bob")

	pool, err := corepostgres.Open(ctx, testDatabaseURL(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- pool.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('identity.admins:' || $1, 0))`,
				tenantA.String()); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM role_bindings WHERE principal_id = $1`, a1.ID.String()); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	done := make(chan error, 1)
	go func() {
		done <- store.RevokeRoleBinding(ctx, domain.RoleBinding{PrincipalID: a2.ID, TenantID: tenantA,
			Role: "TENANT_ADMIN", Scope: domain.ScopeTenant})
	}()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("RevokeRoleBinding finished (%v) while another revocation held the tenant's lock", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-holder)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ports.ErrLastAdministrator, "counted after the holder committed: bob is now the last")
	case <-time.After(5 * time.Second):
		t.Fatal("RevokeRoleBinding never resumed")
	}
}
