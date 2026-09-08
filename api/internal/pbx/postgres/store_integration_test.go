//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxdomain "atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpostgres "atsap-api/internal/pbx/postgres"
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

// harness resets the schema and provides a store, a pool and two
// provisioned tenants, so every test starts from a known state
// regardless of order. Tenants are seeded first because
// extensions.tenant_id references tenants(id).
type harness struct {
	store   *pbxpostgres.Store
	pool    *corepostgres.Pool
	tenantA shareddomain.TenantID
	tenantB shareddomain.TenantID
}

func newStore(t *testing.T) *harness {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	_ = corepostgres.MigrateDownAll(databaseURL, migrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(databaseURL, migrationsDir(t)))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(databaseURL, migrationsDir(t)) })

	pool, err := corepostgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	tenantA, tenantB := shareddomain.NewTenantID(), shareddomain.NewTenantID()
	for _, id := range []shareddomain.TenantID{tenantA, tenantB} {
		_, err := pool.Unwrap().Exec(context.Background(),
			`INSERT INTO tenants (id, name) VALUES ($1, $2)`, id.String(), "tenant-"+id.String())
		require.NoError(t, err)
	}

	return &harness{store: pbxpostgres.NewStore(), pool: pool, tenantA: tenantA, tenantB: tenantB}
}

// run executes fn inside a transaction scoped to tenantID — the same
// arrangement the application layer will use (corepostgres.Pool.WithTenant
// handing the already-open, RLS-scoped tx to the store).
func (h *harness) run(tenantID shareddomain.TenantID, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return h.pool.WithTenant(context.Background(), tenantID, fn)
}

func (h *harness) insert(t *testing.T, ext pbxdomain.Extension) error {
	t.Helper()
	return h.run(ext.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return h.store.Insert(ctx, tx, ext)
	})
}

func (h *harness) update(t *testing.T, ext pbxdomain.Extension) error {
	t.Helper()
	return h.run(ext.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return h.store.Update(ctx, tx, ext)
	})
}

func (h *harness) get(t *testing.T, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (pbxdomain.Extension, error) {
	t.Helper()
	var ext pbxdomain.Extension
	err := h.run(tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ext, err = h.store.Get(ctx, tx, tenantID, id)
		return err
	})
	return ext, err
}

func (h *harness) delete(t *testing.T, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) error {
	t.Helper()
	return h.run(tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return h.store.Delete(ctx, tx, tenantID, id)
	})
}

func (h *harness) list(t *testing.T, tenantID shareddomain.TenantID, page ports.Page) ([]pbxdomain.Extension, error) {
	t.Helper()
	var extensions []pbxdomain.Extension
	err := h.run(tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		extensions, err = h.store.List(ctx, tx, tenantID, page)
		return err
	})
	return extensions, err
}

// newExtension builds a domain.Extension with generated-looking fields:
// an auth username derived from the extension id (never from the number)
// and a 32-hex-character MD5 HA1 digest.
func newExtension(tenantID shareddomain.TenantID, number, displayName string, deviceType pbxdomain.DeviceType) pbxdomain.Extension {
	id := shareddomain.NewExtensionID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	return pbxdomain.Extension{
		ID:           id,
		TenantID:     tenantID,
		Number:       number,
		DisplayName:  displayName,
		AuthUsername: "u" + strings.ReplaceAll(id.String(), "-", ""),
		SecretDigest: strings.Repeat("0", 32),
		DeviceType:   deviceType,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// TestExtensionStore_RoundTrip is task 3.2: an extension created, read,
// updated and deleted under one tenant's context, all through the real
// store API.
func TestExtensionStore_RoundTrip(t *testing.T) {
	h := newStore(t)

	ext := newExtension(h.tenantA, "1000", "Alice", pbxdomain.DeviceSIP)
	require.NoError(t, h.insert(t, ext))

	got, err := h.get(t, h.tenantA, ext.ID)
	require.NoError(t, err)
	assert.Equal(t, ext.ID, got.ID)
	assert.Equal(t, h.tenantA, got.TenantID)
	assert.Equal(t, "1000", got.Number)
	assert.Equal(t, "Alice", got.DisplayName)
	assert.Equal(t, ext.AuthUsername, got.AuthUsername)
	assert.Equal(t, ext.SecretDigest, got.SecretDigest)
	assert.Equal(t, pbxdomain.DeviceSIP, got.DeviceType)
	assert.True(t, got.CreatedAt.Equal(ext.CreatedAt), "created_at must round-trip")
	assert.True(t, got.UpdatedAt.Equal(ext.UpdatedAt), "updated_at must round-trip")

	listed, err := h.list(t, h.tenantA, ports.Page{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Len(t, listed, 1, "a created extension appears in a subsequent listing")
	assert.Equal(t, ext.ID, listed[0].ID)

	updated := ext
	updated.DisplayName = "Alice B."
	updated.DeviceType = pbxdomain.DeviceWebRTC
	updated.UpdatedAt = updated.CreatedAt.Add(time.Hour)
	require.NoError(t, h.update(t, updated))

	got, err = h.get(t, h.tenantA, ext.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice B.", got.DisplayName)
	assert.Equal(t, pbxdomain.DeviceWebRTC, got.DeviceType)
	assert.Equal(t, "1000", got.Number, "the update must not disturb the number")
	assert.True(t, got.UpdatedAt.Equal(updated.UpdatedAt), "the update must persist the new updated_at")

	require.NoError(t, h.delete(t, h.tenantA, ext.ID))

	_, err = h.get(t, h.tenantA, ext.ID)
	assert.ErrorIs(t, err, ports.ErrNotFound, "a deleted extension reads as nonexistent")

	listed, err = h.list(t, h.tenantA, ports.Page{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Empty(t, listed, "a deleted extension no longer appears in a listing")
}

// TestExtensionStore_NumberUniquePerTenantNotGlobally is task 3.3 and the
// spec scenario "Extension numbers are unique per tenant, not globally"
// at the storage layer: the same number in two tenants is two unrelated
// extensions, while a duplicate inside one tenant is refused.
func TestExtensionStore_NumberUniquePerTenantNotGlobally(t *testing.T) {
	h := newStore(t)

	inA := newExtension(h.tenantA, "1000", "Alice", pbxdomain.DeviceSIP)
	inB := newExtension(h.tenantB, "1000", "Bob", pbxdomain.DeviceWebRTC)
	require.NoError(t, h.insert(t, inA))
	require.NoError(t, h.insert(t, inB), "the same number in another tenant is an independent extension")

	fromA, err := h.get(t, h.tenantA, inA.ID)
	require.NoError(t, err)
	fromB, err := h.get(t, h.tenantB, inB.ID)
	require.NoError(t, err)
	assert.NotEqual(t, fromA.ID, fromB.ID, "they must be independent extensions")
	assert.Equal(t, "1000", fromA.Number)
	assert.Equal(t, "1000", fromB.Number)

	dup := newExtension(h.tenantA, "1000", "Impostor", pbxdomain.DeviceSIP)
	err = h.insert(t, dup)
	assert.ErrorIs(t, err, ports.ErrNumberTaken, "a duplicate number within one tenant is rejected")

	_, err = h.get(t, h.tenantA, dup.ID)
	assert.ErrorIs(t, err, ports.ErrNotFound, "the rejected duplicate must not exist")

	listed, err := h.list(t, h.tenantA, ports.Page{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, listed, 1, "the rejected duplicate must not have created a second row in tenant A")
	assert.Equal(t, inA.ID, listed[0].ID)
}

// TestExtensionStore_CrossTenantReadsFindNothing is task 3.4: RLS is what
// isolates extensions, and this is that guarantee asserted through the
// store — a row written under tenant A's context is invisible under
// tenant B's context, and reads, updates and deletes of it answer exactly
// as they would for an id that exists nowhere.
func TestExtensionStore_CrossTenantReadsFindNothing(t *testing.T) {
	h := newStore(t)

	ext := newExtension(h.tenantA, "1000", "Alice", pbxdomain.DeviceSIP)
	require.NoError(t, h.insert(t, ext))

	_, err := h.get(t, h.tenantB, ext.ID)
	assert.ErrorIs(t, err, ports.ErrNotFound,
		"tenant B asking for tenant A's extension by its real id gets the same answer as for one that does not exist")

	_, err = h.get(t, h.tenantA, shareddomain.NewExtensionID())
	assert.ErrorIs(t, err, ports.ErrNotFound,
		"and that answer is identical to a truly nonexistent id")

	// A cross-tenant update must fail and leave the row untouched.
	malicious := ext
	malicious.DisplayName = "pwned"
	err = h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		return h.store.Update(ctx, tx, malicious)
	})
	assert.ErrorIs(t, err, ports.ErrNotFound, "tenant B cannot update tenant A's extension")

	// A cross-tenant delete must FAIL, not succeed silently (design D9).
	// The projection tables have no RLS, so a caller told "deleted" would
	// go on to deprovision tenant A's endpoint in ps_*. ErrNotFound is
	// also exactly what a nonexistent id returns, so failing here reveals
	// nothing about tenant A (INV-10).
	err = h.delete(t, h.tenantB, ext.ID)
	assert.ErrorIs(t, err, ports.ErrNotFound,
		"tenant B's delete of tenant A's extension must fail closed, so the caller never reaches the unguarded projection")

	err = h.delete(t, h.tenantA, shareddomain.NewExtensionID())
	assert.ErrorIs(t, err, ports.ErrNotFound,
		"and a genuinely nonexistent id returns the identical error, so the two are indistinguishable")

	surviving, err := h.get(t, h.tenantA, ext.ID)
	require.NoError(t, err, "tenant A's extension must survive tenant B's delete attempt")
	assert.Equal(t, "Alice", surviving.DisplayName)

	// Listings are tenant-scoped too.
	fromB, err := h.list(t, h.tenantB, ports.Page{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Empty(t, fromB, "tenant B cannot see tenant A's extension in a listing")

	fromA, err := h.list(t, h.tenantA, ports.Page{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Len(t, fromA, 1)
	assert.Equal(t, ext.ID, fromA[0].ID, "tenant A still sees its own extension")
}
