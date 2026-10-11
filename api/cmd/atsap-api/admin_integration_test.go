//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/config"
	identityapp "atsap-api/internal/identity/application"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/logging"
	corepostgres "atsap-api/internal/postgres"
)

// TestAdminResetPassword drives `atsap-api admin reset-password` (design
// D15, D-66): the temporary password is printed once and logged nowhere,
// must-change is set, sessions end, and each of the person's tenants gets
// one system audit row. An unknown email exits non-zero, generically.
func TestAdminResetPassword(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"
	}
	_, file, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	_ = corepostgres.MigrateDownAll(databaseURL, migrations)
	require.NoError(t, corepostgres.MigrateUp(databaseURL, migrations))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(databaseURL, migrations) })

	ctx := context.Background()
	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	cfg := config.Config{DatabaseURL: databaseURL, JWTPrivateKeyHex: hex.EncodeToString(bytes.Repeat([]byte{7}, 32)), LogLevel: "debug"}
	svc, store, err := newIdentity(cfg, pool, noTenantCap, logging.New("error"))
	require.NoError(t, err)
	acme, err := svc.ProvisionTenant(ctx, identityapp.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	other, err := svc.ProvisionTenant(ctx, identityapp.SystemActor(), "Other", "EU")
	require.NoError(t, err)
	_, err = svc.ProvisionPrincipal(ctx, identityapp.SystemActor(), acme.ID, "admin@example.test", "", "forgotten-password", "TENANT_ADMIN")
	require.NoError(t, err)
	_, err = svc.ProvisionPrincipal(ctx, identityapp.SystemActor(), other.ID, "admin@example.test", "", "", "AGENT")
	require.NoError(t, err)
	signed, err := svc.SignIn(ctx, "admin@example.test", "forgotten-password")
	require.NoError(t, err)

	// Capture the process's stderr too: the logger writes there.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stderr := os.Stderr
	os.Stderr = w
	var out, errOut bytes.Buffer
	code := adminResetPassword(ctx, cfg, pool, "Admin@Example.test", &out, &errOut)
	_ = w.Close()
	os.Stderr = stderr
	var logged bytes.Buffer
	_, _ = logged.ReadFrom(r)

	require.Equal(t, 0, code, errOut.String())
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	temporary := lines[len(lines)-1]
	assert.Len(t, temporary, 20)
	assert.Equal(t, 1, strings.Count(out.String(), temporary), "shown once")
	assert.NotContains(t, logged.String(), temporary, "never logged")
	assert.NotContains(t, errOut.String(), temporary)

	_, err = svc.RefreshSession(ctx, signed.RefreshSecret, &acme.ID)
	assert.ErrorIs(t, err, identityports.ErrSessionNotUsable, "every session ended")
	res, err := svc.SignIn(ctx, "admin@example.test", temporary)
	require.NoError(t, err)
	assert.True(t, res.PasswordChangeOnly, "must change at next sign-in")

	entries, err := store.ListAudit(ctx, acme.ID, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, countAction(entries, "principal.password.reset"))
	entries, err = store.ListAudit(ctx, other.ID, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, countAction(entries, "principal.password.reset"), "one system audit row per tenant")

	errOut.Reset()
	out.Reset()
	assert.Equal(t, 1, adminResetPassword(ctx, cfg, pool, "nobody@example.test", &out, &errOut))
	assert.Equal(t, "no account with that email\n", errOut.String())
	assert.Empty(t, out.String())
}

func countAction(entries []identitydomain.AuditEntry, action string) int {
	n := 0
	for _, e := range entries {
		if e.Action == action && e.ActorType == identitydomain.ActorSystem {
			n++
		}
	}
	return n
}
