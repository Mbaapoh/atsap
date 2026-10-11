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

// Migration 0010 (identity-accounts-and-sessions task 2.1, design D2):
// principals become memberships of installation-wide users without
// losing an id, binding, key or audit row; compatible duplicates merge;
// conflicting duplicates abort with nothing changed; down then up
// round-trips.

const versionBeforeAccounts = 9

type v9Principal struct {
	id     domain.PrincipalID
	tenant domain.TenantID
	user   string
	email  string
	hash   string
}

func seedV9(t *testing.T, pool *postgres.Pool, rows []v9Principal) {
	t.Helper()
	ctx := context.Background()
	seen := map[domain.TenantID]bool{}
	for _, r := range rows {
		if !seen[r.tenant] {
			_, err := pool.Unwrap().Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, r.tenant.String(), "t-"+r.tenant.String()[:8])
			require.NoError(t, err)
			seen[r.tenant] = true
		}
		require.NoError(t, pool.WithTenant(ctx, r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO principals (id, tenant_id, username, email, password_hash, role)
				VALUES ($1, $2, $3, $4, $5, 'TENANT_ADMIN')`, r.id.String(), r.tenant.String(), r.user, r.email, r.hash); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
				VALUES ($1, $2, 'TENANT_ADMIN', 'tenant')`, r.id.String(), r.tenant.String()); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO api_keys (tenant_id, principal_id, key_hash)
				VALUES ($1::uuid, $2::uuid, md5($1::text)||md5($2::text))`, r.tenant.String(), r.id.String()); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, actor_id, actor_type, action, resource_type, resource_id)
				VALUES ($1::uuid, $2::uuid, 'principal', 'principal.create', 'principal', $2::text)`, r.tenant.String(), r.id.String())
			return err
		}))
	}
}

func atVersion9(t *testing.T) (*postgres.Pool, string) {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	_ = postgres.MigrateDownAll(databaseURL, migrationsDir(t))
	t.Cleanup(func() { _ = postgres.MigrateDownAll(databaseURL, migrationsDir(t)) })
	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), versionBeforeAccounts))
	pool, err := postgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, databaseURL
}

func TestAccountsMigration_PrincipalsBecomeMembershipsIntact(t *testing.T) {
	pool, databaseURL := atVersion9(t)
	ctx := context.Background()
	tA, tB := domain.NewTenantID(), domain.NewTenantID()
	ana1 := v9Principal{domain.NewPrincipalID(), tA, "ana", "Ana@Example.com", "$argon2id$same"}
	ana2 := v9Principal{domain.NewPrincipalID(), tB, "ana.b", " ana@example.COM ", "$argon2id$same"}
	bob := v9Principal{domain.NewPrincipalID(), tA, "bob", "bob@example.com", "$argon2id$bob"}
	seedV9(t, pool, []v9Principal{ana1, ana2, bob})

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)))

	var users int
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users))
	assert.Equal(t, 2, users, "compatible duplicates merge: Ana once, Bob once")

	var anaUser, anaSecret string
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT u.id, c.secret FROM users u
		JOIN user_credentials c ON c.user_id = u.id AND c.kind = 'password' WHERE u.email = 'ana@example.com'`).
		Scan(&anaUser, &anaSecret))
	assert.Equal(t, "$argon2id$same", anaSecret, "the password moves to the user, unchanged")

	for _, p := range []v9Principal{ana1, ana2, bob} {
		var userID string
		var bindings, keys, audits int
		require.NoError(t, pool.WithTenant(ctx, p.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT user_id FROM principals WHERE id = $1`, p.id.String()).Scan(&userID); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM role_bindings WHERE principal_id = $1`, p.id.String()).Scan(&bindings); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE principal_id = $1`, p.id.String()).Scan(&keys); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE resource_id = $1`, p.id.String()).Scan(&audits)
		}), "principal %s keeps its id", p.id)
		assert.Equal(t, 1, bindings, "binding kept")
		assert.Equal(t, 1, keys, "api key kept")
		assert.Equal(t, 1, audits, "audit row kept")
		if p.email != bob.email {
			assert.Equal(t, anaUser, userID, "both of Ana's principals are memberships of one user")
		}
	}

	t.Run("a person reads their own memberships across tenants and nothing else", func(t *testing.T) {
		var mine, all int
		tx, err := pool.Unwrap().Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, anaUser)
		require.NoError(t, err)
		require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&mine))
		require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM role_bindings`).Scan(&all))
		assert.Equal(t, 2, mine, "Ana sees her two memberships, not Bob's")
		assert.Equal(t, 2, all, "and only her memberships' bindings")
	})

	t.Run("principals still fail closed with no context", func(t *testing.T) {
		var n int
		require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&n))
		assert.Zero(t, n)
	})
}

func TestAccountsMigration_ConflictingDuplicatesAbortUnchanged(t *testing.T) {
	pool, databaseURL := atVersion9(t)
	ctx := context.Background()
	p1 := v9Principal{domain.NewPrincipalID(), domain.NewTenantID(), "ana", "ana@example.com", "$argon2id$one"}
	p2 := v9Principal{domain.NewPrincipalID(), domain.NewTenantID(), "ana", "ANA@example.com", "$argon2id$two"}
	seedV9(t, pool, []v9Principal{p1, p2})

	err := postgres.MigrateUp(databaseURL, migrationsDir(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), p1.id.String(), "the failure names both principals")
	assert.Contains(t, err.Error(), p2.id.String())
	require.NoError(t, postgres.ForceVersion(databaseURL, migrationsDir(t), versionBeforeAccounts))

	var usersTable int
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'users'`).Scan(&usersTable))
	assert.Zero(t, usersTable, "nothing was created")
	var hash string
	require.NoError(t, pool.WithTenant(ctx, p1.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT password_hash FROM principals WHERE id = $1`, p1.id.String()).Scan(&hash)
	}))
	assert.Equal(t, p1.hash, hash, "nothing was changed")
}

func TestAccountsMigration_DownThenUpRoundTrips(t *testing.T) {
	pool, databaseURL := atVersion9(t)
	ctx := context.Background()
	p := v9Principal{domain.NewPrincipalID(), domain.NewTenantID(), "ana", "ana@example.com", "$argon2id$x"}
	seedV9(t, pool, []v9Principal{p})

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)))
	require.NoError(t, postgres.MigrateTo(databaseURL, migrationsDir(t), versionBeforeAccounts))

	var username, email, hash string
	require.NoError(t, pool.WithTenant(ctx, p.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT username, email, password_hash FROM principals WHERE id = $1`, p.id.String()).
			Scan(&username, &email, &hash)
	}))
	assert.Equal(t, "ana", username)
	assert.Equal(t, "ana@example.com", email)
	assert.Equal(t, p.hash, hash)

	require.NoError(t, postgres.MigrateUp(databaseURL, migrationsDir(t)), "and up again")
	var users int
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users))
	assert.Equal(t, 1, users)
}
