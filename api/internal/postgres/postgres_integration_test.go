//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/postgres"
	"atsap-api/internal/shared/domain"
)

// testDatabaseURL returns the app database URL for the dev Postgres
// instance the integration suite runs against, overridable via
// DATABASE_URL for CI.
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
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// resetSchema drops and reapplies every migration so each test starts
// from a known, empty state, independent of test execution order.
func resetSchema(t *testing.T, databaseURL string, seedDevTenant bool) {
	t.Helper()
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t), seedDevTenant))
	t.Cleanup(func() {
		_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	})
}

func TestMigrateUpDown(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL, false)

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	var tableCount int
	err = pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = ANY($1)`,
		[]string{"tenants", "calls", "call_participants", "channel_history", "usage_seconds", "outbox"},
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 6, tableCount, "all six telephony-core tables should exist after migrate up")

	require.NoError(t, postgres.MigrateDownAll(databaseURL, migrationsDir(t)))

	err = pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = 'calls'`,
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 0, tableCount, "tables should be gone after migrate down")
}

func TestMigrateUp_DevTenantSeedGate(t *testing.T) {
	databaseURL := testDatabaseURL(t)

	t.Run("seed disabled: no dev tenant row", func(t *testing.T) {
		resetSchema(t, databaseURL, false)
		pool, err := postgres.Open(context.Background(), databaseURL)
		require.NoError(t, err)
		defer pool.Close()

		var count int
		err = pool.Unwrap().QueryRow(context.Background(), `SELECT count(*) FROM tenants`).Scan(&count)
		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("seed enabled: dev tenant row present", func(t *testing.T) {
		resetSchema(t, databaseURL, true)
		pool, err := postgres.Open(context.Background(), databaseURL)
		require.NoError(t, err)
		defer pool.Close()

		var name string
		err = pool.Unwrap().QueryRow(context.Background(),
			`SELECT name FROM tenants WHERE id = '00000000-0000-0000-0000-000000000001'`,
		).Scan(&name)
		require.NoError(t, err)
		assert.Equal(t, "Dev Tenant", name)
	})
}

// TestRLSIsolation is the RLS Isolation Test named in
// docs/hld/03-domain-model.md §6: a query issued without app.tenant_id
// set must return zero rows, even though matching rows exist.
func TestRLSIsolation(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL, false)

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()
	tenantA := domain.NewTenantID()
	tenantB := domain.NewTenantID()

	seedTenant := func(id domain.TenantID) {
		_, err := pool.Unwrap().Exec(ctx,
			`INSERT INTO tenants (id, name) VALUES ($1, $2)`, id.String(), "tenant-"+id.String())
		require.NoError(t, err)
	}
	seedTenant(tenantA)
	seedTenant(tenantB)

	err = pool.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO calls (id, tenant_id, direction, state, source_number, dest_number)
			 VALUES (gen_random_uuid(), $1, 'OUTBOUND', 'Initiated', '1000', '1001')`, tenantA.String())
		return err
	})
	require.NoError(t, err)

	t.Run("no tenant context set: zero rows", func(t *testing.T) {
		var count int
		err := pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM calls`).Scan(&count)
		require.NoError(t, err)
		assert.Zero(t, count, "RLS must fail closed with no app.tenant_id set")
	})

	t.Run("tenant B context set: sees zero rows of tenant A's call", func(t *testing.T) {
		var count int
		err := pool.WithTenant(ctx, tenantB, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM calls`).Scan(&count)
		})
		require.NoError(t, err)
		assert.Zero(t, count, "tenant B must not see tenant A's call")
	})

	t.Run("tenant A context set: sees its own call", func(t *testing.T) {
		var count int
		err := pool.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM calls`).Scan(&count)
		})
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

// TestOutboxWorkerRole verifies the design.md "Outbox worker runs as a
// narrow BYPASSRLS platform role" exception: the worker role reads across
// tenants on outbox, but sees nothing on calls/call_participants.
func TestOutboxWorkerRole(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL, false)

	appPool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer appPool.Close()

	ctx := context.Background()
	tenantA := domain.NewTenantID()
	_, err = appPool.Unwrap().Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantA.String(), "tenant-a")
	require.NoError(t, err)

	err = appPool.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO calls (id, tenant_id, direction, state, source_number, dest_number)
			 VALUES (gen_random_uuid(), $1, 'OUTBOUND', 'Initiated', '1000', '1001')`, tenantA.String())
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO outbox (id, tenant_id, event_type, aggregate_id, payload)
			 VALUES (gen_random_uuid(), $1, 'call.initiated', gen_random_uuid(), '{}'::jsonb)`, tenantA.String())
		return err
	})
	require.NoError(t, err)

	workerURL := "postgres://atsap_outbox_worker:devpassword123@localhost:15432/atsapbx?sslmode=disable"
	workerPool, err := postgres.Open(ctx, workerURL)
	require.NoError(t, err)
	defer workerPool.Close()

	var outboxCount int
	err = workerPool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, outboxCount, "worker role must read across tenants on outbox (BYPASSRLS)")

	var dummy int
	err = workerPool.Unwrap().QueryRow(ctx, `SELECT 1 FROM calls LIMIT 1`).Scan(&dummy)
	assert.Error(t, err, "worker role must have no privilege on calls at all")
}
