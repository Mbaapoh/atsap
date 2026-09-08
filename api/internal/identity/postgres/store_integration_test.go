//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
}

// newStore resets the schema and returns a Store plus two provisioned
// tenants, so every test starts from a known state regardless of order.
func newStore(t *testing.T) (*identitypostgres.Store, shareddomain.TenantID, shareddomain.TenantID) {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	_ = corepostgres.MigrateDownAll(databaseURL, migrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(databaseURL, migrationsDir(t)))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(databaseURL, migrationsDir(t)) })

	pool, err := corepostgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	store := identitypostgres.NewStore(pool)
	tenantA, tenantB := shareddomain.NewTenantID(), shareddomain.NewTenantID()
	for _, id := range []shareddomain.TenantID{tenantA, tenantB} {
		require.NoError(t, store.CreateTenant(context.Background(), domain.Tenant{
			ID: id, Name: "tenant-" + id.String(), Status: domain.TenantActive, ResidencyZone: "EU",
		}))
	}
	return store, tenantA, tenantB
}

func newPrincipal(tenantID shareddomain.TenantID, username string) domain.Principal {
	return domain.Principal{
		ID: shareddomain.NewPrincipalID(), TenantID: tenantID,
		Username: username, Email: username + "@example.test",
		PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		Role:         "AGENT", Status: domain.PrincipalActive,
	}
}

func TestStore_TenantRoundTrip(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()

	got, err := store.GetTenant(ctx, tenantA)
	require.NoError(t, err)
	assert.Equal(t, tenantA, got.ID)
	assert.Equal(t, domain.TenantActive, got.Status)
	assert.Equal(t, "EU", got.ResidencyZone)

	require.NoError(t, store.SetTenantStatus(ctx, tenantA, domain.TenantSuspended))
	got, err = store.GetTenant(ctx, tenantA)
	require.NoError(t, err)
	assert.Equal(t, domain.TenantSuspended, got.Status)
	assert.Equal(t, "EU", got.ResidencyZone, "suspension must not disturb residency")

	_, err = store.GetTenant(ctx, shareddomain.NewTenantID())
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestStore_PrincipalRoundTrip(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()

	p := newPrincipal(tenantA, "alice")
	require.NoError(t, store.CreatePrincipal(ctx, p))

	byID, err := store.GetPrincipal(ctx, tenantA, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.Username, byID.Username)
	assert.Equal(t, p.PasswordHash, byID.PasswordHash)
	assert.Equal(t, domain.PrincipalActive, byID.Status)

	byName, err := store.GetPrincipalByUsername(ctx, tenantA, "alice")
	require.NoError(t, err)
	assert.Equal(t, p.ID, byName.ID)

	require.NoError(t, store.SetPrincipalStatus(ctx, tenantA, p.ID, domain.PrincipalDisabled))
	byID, err = store.GetPrincipal(ctx, tenantA, p.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PrincipalDisabled, byID.Status)
}

// TestStore_UsernamesAreUniquePerTenantNotGlobally is AC-01.2 at the
// storage layer: the same username in two tenants must be two unrelated
// principals, while a duplicate inside one tenant must be refused.
func TestStore_UsernamesAreUniquePerTenantNotGlobally(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()

	inA := newPrincipal(tenantA, "admin")
	inB := newPrincipal(tenantB, "admin")
	require.NoError(t, store.CreatePrincipal(ctx, inA))
	require.NoError(t, store.CreatePrincipal(ctx, inB), "the same username in another tenant is a different account")

	fromA, err := store.GetPrincipalByUsername(ctx, tenantA, "admin")
	require.NoError(t, err)
	fromB, err := store.GetPrincipalByUsername(ctx, tenantB, "admin")
	require.NoError(t, err)
	assert.NotEqual(t, fromA.ID, fromB.ID, "they must be independent principals")

	dup := newPrincipal(tenantA, "admin")
	err = store.CreatePrincipal(ctx, dup)
	assert.ErrorIs(t, err, ports.ErrUsernameTaken, "a duplicate within one tenant is refused")
}

// TestStore_CrossTenantReadsFindNothing: RLS is what enforces this, but
// the store is the thing callers use, so the guarantee is asserted here
// too — through the real API rather than raw SQL.
func TestStore_CrossTenantReadsFindNothing(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()

	p := newPrincipal(tenantA, "alice")
	require.NoError(t, store.CreatePrincipal(ctx, p))

	_, err := store.GetPrincipal(ctx, tenantB, p.ID)
	assert.ErrorIs(t, err, ports.ErrNotFound,
		"tenant B asking for tenant A's principal by its real id gets the same answer as for one that does not exist")

	_, err = store.GetPrincipalByUsername(ctx, tenantB, "alice")
	assert.ErrorIs(t, err, ports.ErrNotFound)

	err = store.SetPrincipalStatus(ctx, tenantB, p.ID, domain.PrincipalDisabled)
	assert.ErrorIs(t, err, ports.ErrNotFound, "nor may tenant B disable tenant A's principal")
}

func TestStore_RoleBindings(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()

	p := newPrincipal(tenantA, "alice")
	require.NoError(t, store.CreatePrincipal(ctx, p))

	binding := domain.RoleBinding{
		PrincipalID: p.ID, TenantID: tenantA, Role: "SUPERVISOR", Scope: domain.ScopeTenant,
	}
	require.NoError(t, store.CreateRoleBinding(ctx, binding))
	require.NoError(t, store.CreateRoleBinding(ctx, binding), "re-granting an identical binding is a no-op")

	got, err := store.ListRoleBindings(ctx, tenantA, p.ID)
	require.NoError(t, err)
	require.Len(t, got, 1, "the duplicate grant must not have created a second row")
	assert.Equal(t, "SUPERVISOR", got[0].Role)

	fromB, err := store.ListRoleBindings(ctx, tenantB, p.ID)
	require.NoError(t, err)
	assert.Empty(t, fromB, "tenant B cannot see tenant A's grants")

	require.NoError(t, store.DeleteRoleBinding(ctx, binding))
	got, err = store.ListRoleBindings(ctx, tenantA, p.ID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestStore_ApiKeys(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()

	p := newPrincipal(tenantA, "alice")
	require.NoError(t, store.CreatePrincipal(ctx, p))

	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	key := domain.ApiKey{
		ID: shareddomain.NewApiKeyID(), TenantID: tenantA, PrincipalID: p.ID,
		KeyHash: strings.Repeat("b", 64), ExpiresAt: &expires,
	}
	require.NoError(t, store.CreateApiKey(ctx, key))

	got, err := store.GetApiKeyByHash(ctx, tenantA, key.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, key.ID, got.ID)
	assert.Equal(t, tenantA, got.TenantID, "the digest resolves to its owning tenant")
	assert.True(t, got.Usable(time.Now()))

	revokedAt := time.Now().UTC()
	require.NoError(t, store.RevokeApiKey(ctx, tenantA, key.ID, revokedAt))

	got, err = store.GetApiKeyByHash(ctx, tenantA, key.KeyHash)
	require.NoError(t, err)
	assert.False(t, got.Usable(time.Now().Add(time.Second)), "a revoked key stops being usable")

	_, err = store.GetApiKeyByHash(ctx, tenantA, strings.Repeat("c", 64))
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestStore_AuditRoundTrip(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	entry := domain.NewAuditEntry(
		tenantA, uuid.New(), domain.ActorPrincipal,
		"principal.create", "principal", "p-1",
		nil, map[string]any{"username": "alice", "password_hash": "should-not-survive"},
		nil, now,
	)
	require.NoError(t, store.AppendAudit(ctx, entry))

	got, err := store.ListAudit(ctx, tenantA, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "principal.create", got[0].Action)
	assert.Equal(t, domain.ActorPrincipal, got[0].ActorType)
	assert.Equal(t, "alice", got[0].AfterState["username"])
	assert.Equal(t, "[REDACTED]", got[0].AfterState["password_hash"],
		"redaction happened at construction, so the stored row never held the secret")

	fromB, err := store.ListAudit(ctx, tenantB, 10)
	require.NoError(t, err)
	assert.Empty(t, fromB, "audit history is tenant-scoped")
}

// TestAuditStore_ExposesNoMutationPath is AC-01.4 enforced structurally:
// the port and its implementation offer no way to alter history, so
// "no role can edit an audit record" is guaranteed by there being no
// method to call rather than by a check that could be bypassed.
func TestAuditStore_ExposesNoMutationPath(t *testing.T) {
	storeType := reflect.TypeOf(&identitypostgres.Store{})

	for i := range storeType.NumMethod() {
		name := storeType.Method(i).Name
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "audit") {
			continue
		}
		assert.NotContains(t, lower, "update", "audit method %s implies mutation", name)
		assert.NotContains(t, lower, "delete", "audit method %s implies mutation", name)
		assert.NotContains(t, lower, "set", "audit method %s implies mutation", name)
	}

	// Every method on the port must be an append or a read. Counting them
	// was the original form of this check; it tripped when pbx-core needed
	// AppendAuditTx to write its audit row inside its own transaction, and
	// a count cannot tell a legitimate third appender from a smuggled
	// mutator. Asserting the property directly is what AC-01.4 actually
	// requires: immutability is the absence of an update or delete path,
	// not a fixed method count.
	portType := reflect.TypeOf((*ports.AuditStore)(nil)).Elem()
	require.Positive(t, portType.NumMethod())
	for i := range portType.NumMethod() {
		name := portType.Method(i).Name
		lower := strings.ToLower(name)
		assert.True(t,
			strings.HasPrefix(lower, "append") || strings.HasPrefix(lower, "list") || strings.HasPrefix(lower, "get"),
			"AuditStore method %s is neither an append nor a read — history must stay append-only (AC-01.4)", name)
		for _, forbidden := range []string{"update", "delete", "set", "remove", "purge", "truncate"} {
			assert.NotContains(t, lower, forbidden,
				"AuditStore method %s implies mutation — there must be no path to alter history", name)
		}
	}
}

// TestStore_AuditRowSurvivesUnchanged: having established there is no
// mutation path in Go, confirm the row itself is stable across reads.
func TestStore_AuditRowSurvivesUnchanged(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, store.AppendAudit(ctx, domain.NewSystemAuditEntry(
		tenantA, "tenant.suspend", "tenant", tenantA.String(),
		map[string]any{"status": "ACTIVE"}, map[string]any{"status": "SUSPENDED"}, now)))

	first, err := store.ListAudit(ctx, tenantA, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := store.ListAudit(ctx, tenantA, 10)
	require.NoError(t, err)
	require.Len(t, second, 1)

	assert.Equal(t, first[0].ID, second[0].ID)
	assert.Equal(t, first[0].AfterState, second[0].AfterState)
	assert.Equal(t, domain.SystemActorID, second[0].ActorID, "a system mutation records the sentinel actor")
	assert.Equal(t, domain.ActorSystem, second[0].ActorType)
}
