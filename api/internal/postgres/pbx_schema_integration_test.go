//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/postgres"
)

// engineDatabaseURL returns the URL for the media engine's own database
// role — the identity Asterisk connects as to read the PJSIP Realtime
// projection (D-47, design D4).
func engineDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("ENGINE_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://asterisk_engine:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

// TestPbxSchema_ExtensionsIsTenantScopedAndForced covers task 1.1: the
// domain table carries tenant_id, has RLS both ENABLED and FORCED, and has
// its isolation policy. FORCE matters specifically because the app owns
// the table and Postgres exempts an owner from its own policies otherwise
// — the LLD-01 lesson.
func TestPbxSchema_ExtensionsIsTenantScopedAndForced(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	var hasTenantID bool
	require.NoError(t, pool.Unwrap().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_name = 'extensions' AND column_name = 'tenant_id')`,
	).Scan(&hasTenantID))
	assert.True(t, hasTenantID, "extensions must carry tenant_id (D-24 seam 1)")

	var enabled, forced bool
	require.NoError(t, pool.Unwrap().QueryRow(ctx,
		`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = 'extensions'`,
	).Scan(&enabled, &forced))
	assert.True(t, enabled, "extensions must have RLS enabled")
	assert.True(t, forced, "extensions must have RLS FORCED: the owner is exempt from its own policies otherwise")

	var policies int
	require.NoError(t, pool.Unwrap().QueryRow(ctx,
		`SELECT count(*) FROM pg_policies
		 WHERE tablename = 'extensions' AND policyname = 'tenant_isolation_extensions'`,
	).Scan(&policies))
	assert.Equal(t, 1, policies, "extensions must have its tenant isolation policy")

	// The column rename is the point of LLD-03 §7.3: SIP digest auth
	// cannot use a one-way slow hash, so a column named password_hash
	// here would describe something that cannot be built.
	var hasSecretDigest, hasPasswordHash bool
	require.NoError(t, pool.Unwrap().QueryRow(ctx,
		`SELECT
		   EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_name = 'extensions' AND column_name = 'secret_digest'),
		   EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_name = 'extensions' AND column_name = 'password_hash')`,
	).Scan(&hasSecretDigest, &hasPasswordHash))
	assert.True(t, hasSecretDigest, "the credential column is secret_digest")
	assert.False(t, hasPasswordHash, "password_hash must not exist: it names something SIP cannot do")
}

// TestPbxSchema_ProjectionTablesHaveNoTenancy covers task 1.2. The absence
// of tenant_id and RLS on ps_* is deliberate and load-bearing, not an
// oversight: Asterisk connects as its own role and cannot set a tenant
// context, so a policy would hide every row from the only reader that
// needs them. Asserted explicitly so it can never be "fixed" by someone
// applying the D-24 rule mechanically — the same treatment
// licensing_state's missing tenant_id gets.
func TestPbxSchema_ProjectionTablesHaveNoTenancy(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	ctx := context.Background()

	for _, table := range []string{"ps_endpoints", "ps_auths", "ps_aors"} {
		t.Run(table, func(t *testing.T) {
			var exists bool
			require.NoError(t, pool.Unwrap().QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM information_schema.tables
				 WHERE table_schema = 'public' AND table_name = $1)`, table,
			).Scan(&exists))
			require.True(t, exists, "%s must exist after migrate up", table)

			var hasTenantID bool
			require.NoError(t, pool.Unwrap().QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM information_schema.columns
				 WHERE table_name = $1 AND column_name = 'tenant_id')`, table,
			).Scan(&hasTenantID))
			assert.False(t, hasTenantID,
				"%s must NOT have tenant_id: isolation here is by construction (globally unique ids), not by column", table)

			var enabled bool
			require.NoError(t, pool.Unwrap().QueryRow(ctx,
				`SELECT relrowsecurity FROM pg_class WHERE relname = $1`, table,
			).Scan(&enabled))
			assert.False(t, enabled,
				"%s must NOT have RLS: the engine cannot set app.tenant_id, so a policy would hide every row from it", table)
		})
	}
}

// TestPbxSchema_EngineRoleReadsOnlyTheProjection covers tasks 1.3 and 9.5,
// and is the test that turns design D4 from an intention into a control.
// The engine's isolation from tenant data is the absence of any grant, so
// the absence is what gets asserted.
func TestPbxSchema_EngineRoleReadsOnlyTheProjection(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, engineDatabaseURL(t))
	require.NoError(t, err, "the asterisk_engine role must exist and be able to connect (deploy/postgres/init/02-atsapbx.sql)")
	defer func() { _ = conn.Close(ctx) }()

	for _, table := range []string{"ps_endpoints", "ps_auths", "ps_aors"} {
		var n int
		err := conn.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		assert.NoError(t, err, "the engine must be able to read %s — it is what the engine is for", table)
	}

	for _, table := range []string{"extensions", "principals", "calls", "tenants", "audit_logs"} {
		var n int
		err := conn.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		require.Error(t, err, "the engine must NOT be able to read %s: it has no grant on any domain table", table)

		// Asserting only "it errored" would let this test pass because the
		// table was missing rather than because access was denied — which
		// proves nothing about the control. 42501 is insufficient_privilege;
		// 42P01 (undefined_table) must fail the test, loudly.
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, "expected a Postgres error for %s, got %v", table, err)
		assert.Equal(t, "42501", pgErr.Code,
			"reading %s must fail with insufficient_privilege, not %s (%s) — otherwise this test is vacuous",
			table, pgErr.Code, pgErr.Message)
	}
}
