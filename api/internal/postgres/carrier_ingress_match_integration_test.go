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

// carrier_ingress_match (migration 0016, D-77) is the one cross-tenant
// read: for a source address and a dialled number it returns the
// (tenant, trunk) pairs whose trunk recognises the address and whose
// tenant has the number — and leaves nothing readable behind.
func TestCarrierIngressMatch(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	t.Cleanup(func() { _ = postgres.MigrateDownAll(databaseURL, migrationsDir(t)) })
	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)))
	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx := context.Background()

	type seeded struct{ tenant, trunk string }
	seed := func(name, resolved, inbound string, numbers ...string) seeded {
		t.Helper()
		tenant := domain.NewTenantID()
		_, err := pool.Unwrap().Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenant.String(), name)
		require.NoError(t, err)
		var trunk string
		require.NoError(t, pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO carrier_trunks (tenant_id, name, host, resolved_addresses, inbound_addresses)
				VALUES ($1, $2, 'sip.carrier.example', $3::inet[], $4::cidr[]) RETURNING id`, tenant.String(), name, resolved, inbound).Scan(&trunk); err != nil {
				return err
			}
			var ext string
			if err := tx.QueryRow(ctx, `INSERT INTO extensions (tenant_id, extension_number, display_name, auth_username, secret_digest, device_type)
				VALUES ($1, '1001', 'x', $2, 'd', 'SIP') RETURNING id`, tenant.String(), "e_"+trunk[:8]).Scan(&ext); err != nil {
				return err
			}
			for _, n := range numbers {
				if _, err := tx.Exec(ctx, `INSERT INTO inbound_numbers (id, tenant_id, number, extension_id) VALUES (gen_random_uuid(), $1, $2, $3)`, tenant.String(), n, ext); err != nil {
					return err
				}
			}
			return nil
		}))
		return seeded{tenant.String(), trunk}
	}
	a := seed("Tenant A", "{203.0.113.10}", "{}", "+442071230001", "+442071230009")
	b := seed("Tenant B", "{}", "{203.0.113.0/28}", "+442071230002", "+442071230009")
	c := seed("Tenant C", "{198.51.100.7}", "{}", "+442071230003")

	match := func(source, dialled string) []seeded {
		t.Helper()
		var out []seeded
		require.NoError(t, pgx.BeginFunc(ctx, pool.Unwrap(), func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT tenant_id::text, trunk_id::text FROM carrier_ingress_match($1::inet, $2)`, source, dialled)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var s seeded
				if err := rows.Scan(&s.tenant, &s.trunk); err != nil {
					return err
				}
				out = append(out, s)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			// Nothing stays readable after the call, in the same transaction.
			var visible int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM carrier_trunks`).Scan(&visible); err != nil {
				return err
			}
			assert.Zero(t, visible, "the lookup leaves no tenant's rows readable")
			return nil
		}))
		return out
	}

	assert.Equal(t, []seeded{a}, match("203.0.113.10", "+442071230001"), "A's address and A's number")
	assert.Equal(t, []seeded{b}, match("203.0.113.10", "+442071230002"), "the same address is in B's range; B's number")
	assert.Equal(t, []seeded{b}, match("203.0.113.5", "+442071230002"), "a range match")
	assert.Empty(t, match("203.0.113.5", "+442071230001"), "A's number, but A's trunk does not recognise .5")
	assert.Empty(t, match("203.0.113.10", "+442071230003"), "C's number, but C's trunk does not recognise this source")
	assert.Equal(t, []seeded{c}, match("198.51.100.7", "+442071230003"))
	assert.Empty(t, match("192.0.2.1", "+442071230001"), "an address no trunk recognises")
	ambiguous := match("203.0.113.10", "+442071230009")
	assert.ElementsMatch(t, []seeded{a, b}, ambiguous, "both tenants have the number: both returned, the caller refuses")

	var visible int
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM inbound_numbers`).Scan(&visible))
	assert.Zero(t, visible, "outside the function, RLS hides every tenant's numbers as before")
}
