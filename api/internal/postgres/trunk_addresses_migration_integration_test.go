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

// Migration 0013 (pbx-trunk-host-resolution task 3.1, D-75): every
// existing IP-host trunk is recognised by its own address straight away;
// a name, including one that looks numeric, waits for the first check;
// down then up round-trips.
func TestTrunkAddressesMigration_IPHostsAreBackfilled(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	t.Cleanup(func() { _ = postgres.MigrateDownAll(databaseURL, migrationsDir(t)) })
	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), 12))
	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx := context.Background()

	tenant := domain.NewTenantID()
	_, err = pool.Unwrap().Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, 'carriers')`, tenant.String())
	require.NoError(t, err)
	hosts := []string{"203.0.113.10", "2001:db8::10", "sip.carrier.example", "1.2.3", "locslhodt"}
	ids := map[string]string{}
	require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		for _, h := range hosts {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO carrier_trunks (tenant_id, name, host) VALUES ($1, $2, $2) RETURNING id`,
				tenant.String(), h).Scan(&id); err != nil {
				return err
			}
			ids[h] = id
		}
		return nil
	}))

	addresses := func() map[string]string {
		got := map[string]string{}
		require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT host, resolved_addresses::text, resolved_at IS NULL FROM carrier_trunks`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var host, addrs string
				var notResolved bool
				if err := rows.Scan(&host, &addrs, &notResolved); err != nil {
					return err
				}
				assert.True(t, notResolved, "%s: no name has been resolved by a migration", host)
				got[host] = addrs
			}
			return rows.Err()
		}))
		return got
	}

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)))
	want := map[string]string{
		"203.0.113.10":        "{203.0.113.10}",
		"2001:db8::10":        "{2001:db8::10}",
		"sip.carrier.example": "{}",
		"1.2.3":               "{}",
		"locslhodt":           "{}",
	}
	assert.Equal(t, want, addresses())

	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), 12), "down to 12")
	var columns int
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'carrier_trunks' AND column_name IN ('resolved_addresses', 'resolved_at')`).Scan(&columns))
	assert.Zero(t, columns, "down removes both columns")

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)), "and up again")
	assert.Equal(t, want, addresses())
	assert.Len(t, ids, len(hosts))
}
