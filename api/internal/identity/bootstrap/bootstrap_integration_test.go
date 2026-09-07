//go:build integration

package bootstrap_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/bootstrap"
	identitypostgres "atsap-api/internal/identity/postgres"
	corepostgres "atsap-api/internal/postgres"
	"atsap-api/internal/logging"
)

// migrationsDir resolves api/migrations from this test file's location.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
}

// adminURL returns a superuser connection for creating and dropping the
// scratch database, overridable for CI.
func adminURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("POSTGRES_ADMIN_URL"); v != "" {
		return v
	}
	return "postgres://asterisk:devpassword123@localhost:15432/asterisk?sslmode=disable"
}

func TestBootstrap_EmptyAndAlreadyBootstrapped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL(t))
	require.NoError(t, err)
	defer admin.Close(ctx)

	// Fresh scratch database, migrated, so this test never depends on the
	// shared dev database's current tenant state.
	dbName := fmt.Sprintf("atsapbx_bootstrap_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s OWNER atsapbx_app`, dbName))
	require.NoError(t, err)
	dropDB := func() {
		_, _ = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, dbName))
	}
	defer dropDB()

	dsn := fmt.Sprintf("postgres://atsapbx_app:devpassword123@localhost:15432/%s?sslmode=disable", dbName)
	pool, err := corepostgres.Open(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, corepostgres.MigrateUp(dsn, migrationsDir(t)))

	logger := logging.New("error")
	store := identitypostgres.NewStore(pool)
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)
	svc := application.NewService(store, store, store, store, store,
		application.NewPasswordHasher(), issuer, logger)

	// 1. Empty installation: bootstrap succeeds.
	tenantID, err := bootstrap.Run(ctx, store, svc, bootstrap.Params{
		TenantName:    "First Co",
		ResidencyZone: "EU",
		Username:      "root",
		Email:         "root@first.test",
		Password:      "a-correct-bootstrap-password",
	})
	require.NoError(t, err)
	require.False(t, tenantID.IsZero())

	// The administrator can authenticate over the wire surface (via the
	// service) — the pair the spec scenario requires.
	tok, err := svc.AuthenticateUser(ctx, tenantID, "root", "a-correct-bootstrap-password")
	require.NoError(t, err, "bootstrapped administrator must be able to authenticate")
	require.NotEmpty(t, tok.Token)

	// 2. Already bootstrapped: refused, so the path cannot mint a second
	// administrator.
	_, err = bootstrap.Run(ctx, store, svc, bootstrap.Params{
		TenantName:    "Second Co",
		ResidencyZone: "EU",
		Username:      "root2",
		Email:         "root2@second.test",
		Password:      "another-correct-password",
	})
	require.ErrorIs(t, err, bootstrap.ErrAlreadyBootstrapped)
}
