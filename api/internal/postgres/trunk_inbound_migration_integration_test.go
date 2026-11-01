//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/postgres"
	"atsap-api/internal/shared/domain"
)

// Migration 0014 (pbx-carrier-addresses task 3.1, D-76): the inbound
// column arrives empty; addresses recorded under D-75 that cannot be a
// carrier (a "localhost" trunk's loopback) are dropped and the rest kept;
// down then up round-trips.
func TestTrunkInboundMigration_StripsNonCarrierAddresses(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	t.Cleanup(func() { _ = postgres.MigrateDownAll(databaseURL, migrationsDir(t)) })
	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), 13))
	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx := context.Background()

	tenant := domain.NewTenantID()
	_, err = pool.Unwrap().Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, 'carriers')`, tenant.String())
	require.NoError(t, err)
	seed := map[string]string{
		"localhost":           "{127.0.0.1,::1}",
		"mixed.example":       "{127.0.0.1,203.0.113.10,fe80::1}",
		"sip.carrier.example": "{203.0.113.20,2001:db8::20}",
		"unresolved.example":  "{}",
	}
	require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		for host, addrs := range seed {
			if _, err := tx.Exec(ctx, `INSERT INTO carrier_trunks (tenant_id, name, host, resolved_addresses) VALUES ($1, $2, $2, $3::inet[])`,
				tenant.String(), host, addrs); err != nil {
				return err
			}
		}
		return nil
	}))
	read := func() map[string]string {
		got := map[string]string{}
		require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT host, resolved_addresses::text, inbound_addresses::text FROM carrier_trunks`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var host, addrs, inbound string
				if err := rows.Scan(&host, &addrs, &inbound); err != nil {
					return err
				}
				assert.Equal(t, "{}", inbound, "%s: no inbound address until a partner enters one", host)
				got[host] = addrs
			}
			return rows.Err()
		}))
		return got
	}

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)))
	want := map[string]string{
		"localhost":           "{}",
		"mixed.example":       "{203.0.113.10}",
		"sip.carrier.example": "{203.0.113.20,2001:db8::20}",
		"unresolved.example":  "{}",
	}
	assert.Equal(t, want, read())

	require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE carrier_trunks SET health_status = 'UNHEALTHY', health_reason = 'ADDRESS_NOT_ALLOWED', health_observed_at = now() WHERE host = 'localhost'`)
		return err
	}), "the new reason is accepted")

	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), 13), "down to 13")
	var reason string
	require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT health_reason FROM carrier_trunks WHERE host = 'localhost'`).Scan(&reason)
	}))
	assert.Equal(t, "NO_RESPONSE", reason, "what the previous version reported for such a trunk")

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)), "and up again")
	assert.Equal(t, want, read())
}
