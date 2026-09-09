//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLicensingSchema_IsInstallationScopedNotTenantScoped is task 3.2
// and LLD-08 DoD 6.
//
// licensing_state deliberately has no tenant_id and no row-level
// security: a licence governs one INSTALLATION however many tenants it
// serves (T-1), which makes it the one documented exception to D-24
// seam 1's "every table carries tenant_id".
//
// The test exists because that exception looks exactly like an
// oversight. Somebody auditing the schema for tenant isolation will find
// a table with no tenant_id and no policy, and the obliging fix is to
// add both — which would tie a licence to a tenant and quietly break the
// Operator licence, where one entitlement covers unlimited tenants.
// Adding either now fails here, and this comment is the answer to why.
func TestLicensingSchema_IsInstallationScopedNotTenantScoped(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	t.Run("has no tenant_id column", func(t *testing.T) {
		var n int
		require.NoError(t, conn.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.columns
			  WHERE table_name = 'licensing_state' AND column_name = 'tenant_id'`,
		).Scan(&n))

		assert.Zero(t, n,
			"licensing_state must not gain a tenant_id: a licence is installation-scoped (T-1). "+
				"If you are adding one to satisfy a tenant-isolation audit, the exception is deliberate — see D-24 and LLD-08 §5")
	})

	t.Run("has no row-level security", func(t *testing.T) {
		var enabled, forced bool
		require.NoError(t, conn.QueryRow(ctx,
			`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = 'licensing_state'`,
		).Scan(&enabled, &forced))

		assert.False(t, enabled, "RLS on licensing_state would hide the installation's own licence from it")
		assert.False(t, forced)
	})

	t.Run("has no isolation policy", func(t *testing.T) {
		var n int
		require.NoError(t, conn.QueryRow(ctx,
			`SELECT count(*) FROM pg_policies WHERE tablename = 'licensing_state'`,
		).Scan(&n))

		assert.Zero(t, n)
	})
}

// TestLicensingSchema_CarriesTheSignedPayload is D-53: the entitlement of
// record is the signed payload, and the readable claims are a cache.
//
// Asserted at the schema level because the columns are what make the
// design possible at all. Without signed_payload and signature there is
// nothing to re-verify on load, and a partner editing `capacity` in
// their own database would simply be believed.
func TestLicensingSchema_CarriesTheSignedPayload(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	for _, col := range []string{"signed_payload", "signature"} {
		t.Run(col+" is present and NOT NULL", func(t *testing.T) {
			var dataType, nullable string
			require.NoError(t, conn.QueryRow(ctx,
				`SELECT data_type, is_nullable FROM information_schema.columns
				  WHERE table_name = 'licensing_state' AND column_name = $1`, col,
			).Scan(&dataType, &nullable))

			assert.Equal(t, "bytea", dataType)
			assert.Equal(t, "NO", nullable,
				"a row without its signature could not be re-verified, so it must be impossible to write one (D-53)")
		})
	}

	t.Run("the published caps are stored", func(t *testing.T) {
		for _, col := range []string{"max_tenants", "max_extensions"} {
			var n int
			require.NoError(t, conn.QueryRow(ctx,
				`SELECT count(*) FROM information_schema.columns
				  WHERE table_name = 'licensing_state' AND column_name = $1`, col,
			).Scan(&n))
			assert.Equal(t, 1, n, "%s is entitlement data other contexts enforce (D-49, D-51)", col)
		}
	})
}

// TestLicensingSchema_NoRowIsSeeded is D-52. A fresh installation is in
// Setup — administration available, no call path — and it gets there by
// having no licence at all.
//
// A seeded row would be an unsigned entitlement at rest, which is
// precisely what signed_payload exists to prevent, and it would also
// erase the distinction between Setup and Degraded that LLD-08 DoD 11
// depends on.
func TestLicensingSchema_NoRowIsSeeded(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	var n int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM licensing_state`).Scan(&n))

	assert.Zero(t, n,
		"no migration may seed a licence: a fresh installation is in Setup until a signed token is applied (D-52)")
}

// TestLicensingSchema_EngineRoleCannotReadIt keeps the engine out of the
// commercial data. Asterisk reads ps_* to place calls and has no reason
// to know what the installation is licensed for; 0005 grants it nothing,
// and the absence of a grant is the enforcement.
//
// Asserted rather than assumed because 0004 grants that role four
// tables, so "asterisk_engine can read this" is the ambient expectation
// a reader brings to the schema.
func TestLicensingSchema_EngineRoleCannotReadIt(t *testing.T) {
	resetSchema(t, testDatabaseURL(t))

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, engineDatabaseURL(t))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	var n int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM licensing_state`).Scan(&n)

	// Asserting only "it errored" would let this pass because the table
	// was missing rather than because access was denied, which proves
	// nothing about the control. 42501 is insufficient_privilege; 42P01
	// (undefined_table) must fail loudly — the LLD-01 lesson, applied
	// here for the same reason 0004's engine test applies it.
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "expected a Postgres error reading licensing_state, got %v", err)
	assert.Equal(t, "42501", pgErr.Code,
		"reading licensing_state as the engine must fail with insufficient_privilege, not %s (%s)",
		pgErr.Code, pgErr.Message)
}
