// Package postgres wires the atsapbx application database: connection
// pooling, schema migrations, and the tenant-scoping helper every
// tenant-facing query must use so PostgreSQL Row-Level Security enforces
// isolation (docs/hld/01-architecture.md §4).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"atsap-api/internal/shared/domain"
)

// migrateURL rewrites a standard postgres://... DATABASE_URL into the
// pgx5://... form golang-migrate's pgx/v5 driver requires, so callers
// (config, .env.example) only ever need to know one canonical URL scheme.
func migrateURL(databaseURL string) string {
	if rest, ok := strings.CutPrefix(databaseURL, "postgres://"); ok {
		return "pgx5://" + rest
	}
	if rest, ok := strings.CutPrefix(databaseURL, "postgresql://"); ok {
		return "pgx5://" + rest
	}
	return databaseURL
}

// Pool wraps a pgxpool.Pool. It exists so tenant-scoped helpers live next
// to the pool they operate on, rather than as free functions taking a
// bare *pgxpool.Pool.
type Pool struct {
	pool *pgxpool.Pool
}

// Open creates a connection pool for databaseURL. Callers must call
// Close when done.
func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Pool{pool: pool}, nil
}

// Close releases all connections in the pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// Unwrap returns the underlying *pgxpool.Pool for callers (repositories)
// that need direct query access.
func (p *Pool) Unwrap() *pgxpool.Pool {
	return p.pool
}

// WithTenant runs fn inside a transaction with app.tenant_id set for the
// duration of that transaction, so every Row-Level Security policy scopes
// correctly (docs/hld/01-architecture.md §4). fn must not start its own
// transaction on the pool; it receives the already-open transaction.
func (p *Pool) WithTenant(ctx context.Context, tenantID domain.TenantID, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if tenantID.IsZero() {
		return errors.New("postgres: tenant id is required")
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// set_config(..., is_local=true) is SET LOCAL's parameterized form —
	// plain "SET LOCAL app.tenant_id = $1" is not valid Postgres syntax,
	// since SET does not accept bind parameters. is_local=true scopes it
	// to this transaction so it never leaks to the next query on a
	// pooled connection.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// MigrateUp applies all pending migrations from dir.
//
// It takes no seed gate: version 2 (the dev-tenant seed) became an empty
// tombstone when identity-auth-rbac landed real tenant provisioning, so
// there is nothing left to gate — every environment runs the same
// migrations to the same version. Tenants are provisioned through
// identity, not conjured by a migration.
func MigrateUp(databaseURL, dir string) error {
	m, err := migrate.New("file://"+dir, migrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateTo migrates to one specific schema version. Used by tests that
// need to reproduce an upgrade from a particular starting state (a
// database left at the retired dev-seed version, say); never part of the
// application's own startup, which always migrates all the way up.
func MigrateTo(databaseURL, dir string, version uint) error {
	m, err := migrate.New("file://"+dir, migrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate to version %d: %w", version, err)
	}
	return nil
}

// MigrateDownAll rolls back every applied migration. Used by tests and
// local teardown; never called against a shared environment.
func MigrateDownAll(databaseURL, dir string) error {
	m, err := migrate.New("file://"+dir, migrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}
