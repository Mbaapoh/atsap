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
func resetSchema(t *testing.T, databaseURL string) {
	t.Helper()
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)))
	t.Cleanup(func() {
		_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	})
}

func TestMigrateUpDown(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

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

	err = pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = ANY($1)`,
		[]string{"principals", "role_bindings", "api_keys", "audit_logs"},
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 4, tableCount, "all four identity tables should exist after migrate up")

	require.NoError(t, postgres.MigrateDownAll(databaseURL, migrationsDir(t)))

	err = pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = ANY($1)`,
		[]string{"calls", "principals", "audit_logs"},
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 0, tableCount, "tables should be gone after migrate down")
}

// TestMigrateUp_FromVersionTwoDatabase is the reason 0002 was tombstoned
// rather than deleted: golang-migrate resolves the next version from the
// files present, so a database sitting at version 2 — every developer's
// local stack, since the dev seed was dev-only — could not migrate
// forward if version 2's files were gone. This drives a database to
// exactly version 2 and then migrates up, which is the case a
// clean-slate test never exercises.
func TestMigrateUp_FromVersionTwoDatabase(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	t.Cleanup(func() { _ = postgres.MigrateDownAll(databaseURL, migrationsDir(t)) })

	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), 2),
		"a version-2 database must be reachable to reproduce the case at all")

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)),
		"migrating up from version 2 must succeed — the tombstone keeps the chain resolvable")

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	var tableCount int
	err = pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = 'principals'`,
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 1, tableCount, "version 3 must apply on top of a version-2 database")
}

func TestMigrateUp_NoDevTenantSeeded(t *testing.T) {
	// The 0002 dev-tenant seed became an empty tombstone when
	// identity-auth-rbac landed real provisioning: migrating up must now
	// leave no tenant behind in any environment, with no gate to set.
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	var count int
	err = pool.Unwrap().QueryRow(context.Background(), `SELECT count(*) FROM tenants`).Scan(&count)
	require.NoError(t, err)
	assert.Zero(t, count, "no migration may conjure a tenant; provisioning is identity's job")
}

// TestRLSIsolation is the RLS Isolation Test named in
// docs/hld/03-domain-model.md §6: a query issued without app.tenant_id
// set must return zero rows, even though matching rows exist.
func TestRLSIsolation(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

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

// TestRLSIsolation_IdentityTables extends the RLS Isolation Test to every
// table the identity context owns. Each is checked in all three states —
// no tenant context, the wrong tenant's context, the owning tenant's
// context — because "RLS enabled" without a working policy silently
// returns rows to the owner (the LLD-01 lesson) and would pass a test
// that only asserted the owner can read.
func TestRLSIsolation_IdentityTables(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()
	tenantA := domain.NewTenantID()
	tenantB := domain.NewTenantID()
	for _, id := range []domain.TenantID{tenantA, tenantB} {
		_, err := pool.Unwrap().Exec(ctx,
			`INSERT INTO tenants (id, name) VALUES ($1, $2)`, id.String(), "tenant-"+id.String())
		require.NoError(t, err)
	}

	principalA := domain.NewPrincipalID()
	err = pool.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO principals (id, tenant_id, username, email, password_hash)
			 VALUES ($1, $2, 'admin', 'admin@example.test', 'not-a-real-hash')`,
			principalA.String(), tenantA.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
			 VALUES ($1, $2, 'TENANT_ADMIN', 'tenant')`,
			principalA.String(), tenantA.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO api_keys (tenant_id, principal_id, key_hash)
			 VALUES ($1, $2, repeat('a', 64))`,
			tenantA.String(), principalA.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO audit_logs (tenant_id, actor_id, actor_type, action, resource_type, resource_id)
			 VALUES ($1, $2, 'principal', 'principal.create', 'principal', $3)`,
			tenantA.String(), principalA.String(), principalA.String())
		return err
	})
	require.NoError(t, err)

	for _, table := range []string{"principals", "role_bindings", "api_keys", "audit_logs"} {
		t.Run(table, func(t *testing.T) {
			query := `SELECT count(*) FROM ` + table // #nosec G202 -- fixed test table names, not input

			var count int
			require.NoError(t, pool.Unwrap().QueryRow(ctx, query).Scan(&count))
			assert.Zero(t, count, "%s must fail closed with no app.tenant_id set", table)

			require.NoError(t, pool.WithTenant(ctx, tenantB, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, query).Scan(&count)
			}))
			assert.Zero(t, count, "tenant B must not see tenant A's %s row", table)

			require.NoError(t, pool.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, query).Scan(&count)
			}))
			assert.Equal(t, 1, count, "tenant A must see its own %s row", table)
		})
	}
}

// TestOutboxWorkerRole verifies the design.md "Outbox worker runs as a
// narrow BYPASSRLS platform role" exception: the worker role reads across
// tenants on outbox, but sees nothing on calls/call_participants.
func TestOutboxWorkerRole(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

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
