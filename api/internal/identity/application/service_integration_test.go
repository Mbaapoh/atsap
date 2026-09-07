//go:build integration

package application_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	corepostgres "atsap-api/internal/postgres"
)

// These tests run the orchestrator against the real Postgres store —
// the fakes in service_test.go prove the logic, this proves the same
// behaviour survives RLS, real constraints, and real serialization.

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

func integrationMigrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
}

func newLiveHarness(t *testing.T) (*application.Service, *identitypostgres.Store, *bytes.Buffer) {
	t.Helper()
	databaseURL := integrationDatabaseURL(t)
	_ = corepostgres.MigrateDownAll(databaseURL, integrationMigrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(databaseURL, integrationMigrationsDir(t)))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(databaseURL, integrationMigrationsDir(t)) })

	pool, err := corepostgres.Open(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	store := identitypostgres.NewStore(pool)

	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc := application.NewService(store, store, store, store, store,
		application.NewPasswordHasher(), issuer, logger)

	return svc, store, logs
}

// TestLive_ProvisionAuthenticateValidate is the end-to-end path this
// change exists to deliver: provision a tenant and a principal, sign in,
// and use the resulting token — all against real storage.
func TestLive_ProvisionAuthenticateValidate(t *testing.T) {
	svc, store, _ := newLiveHarness(t)
	ctx := context.Background()

	tenant, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)

	principal, err := svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", "correct horse battery", "AGENT")
	require.NoError(t, err)

	tok, err := svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)
	assert.Equal(t, principal.ID, tok.PrincipalID)

	tc, err := svc.ValidateToken(ctx, tok.Token)
	require.NoError(t, err)
	assert.Equal(t, tenant.ID, tc.TenantID)
	assert.Equal(t, "EU", tc.ResidencyZone)

	// Exactly one audit record per mutation, durably stored.
	entries, err := store.ListAudit(ctx, tenant.ID, 100)
	require.NoError(t, err)
	require.Len(t, entries, 2, "one for the tenant, one for the principal — and none for the sign-in")

	byAction := map[string]domain.AuditEntry{}
	for _, e := range entries {
		byAction[e.Action] = e
	}
	require.Contains(t, byAction, "tenant.create")
	require.Contains(t, byAction, "principal.create")
	assert.Equal(t, "alice", byAction["principal.create"].AfterState["username"])

	// And nothing in the durable audit trail carries the credential.
	raw, err := json.Marshal(entries)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "correct horse battery")
	assert.NotContains(t, string(raw), principal.PasswordHash)
}

// TestLive_UsernameOverlapAcrossTenants is AC-01.2 end to end: the same
// username in two tenants yields independent principals with
// independent sessions.
func TestLive_UsernameOverlapAcrossTenants(t *testing.T) {
	svc, _, _ := newLiveHarness(t)
	ctx := context.Background()

	first, err := svc.ProvisionTenant(ctx, application.SystemActor(), "First", "EU")
	require.NoError(t, err)
	second, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Second", "EU")
	require.NoError(t, err)

	_, err = svc.ProvisionPrincipal(ctx, application.SystemActor(), first.ID, "admin", "a@example.test", "first-tenant-password", "TENANT_ADMIN")
	require.NoError(t, err)
	_, err = svc.ProvisionPrincipal(ctx, application.SystemActor(), second.ID, "admin", "b@example.test", "second-tenant-password", "TENANT_ADMIN")
	require.NoError(t, err)

	firstTok, err := svc.AuthenticateUser(ctx, first.ID, "admin", "first-tenant-password")
	require.NoError(t, err)
	secondTok, err := svc.AuthenticateUser(ctx, second.ID, "admin", "second-tenant-password")
	require.NoError(t, err)

	assert.NotEqual(t, firstTok.PrincipalID, secondTok.PrincipalID, "independent principals")

	// Each tenant's password must not work for the other's account.
	_, err = svc.AuthenticateUser(ctx, first.ID, "admin", "second-tenant-password")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "independent sessions and credentials")

	firstCtx, err := svc.ValidateToken(ctx, firstTok.Token)
	require.NoError(t, err)
	assert.Equal(t, first.ID, firstCtx.TenantID, "each token stays bound to its own tenant")
}

// TestLive_AuthenticationFailureModes covers the four ways a sign-in can
// fail, against real storage, all returning the same outcome.
func TestLive_AuthenticationFailureModes(t *testing.T) {
	svc, _, _ := newLiveHarness(t)
	ctx := context.Background()

	tenant, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	principal, err := svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", "correct horse battery", "AGENT")
	require.NoError(t, err)

	t.Run("unknown username", func(t *testing.T) {
		_, err := svc.AuthenticateUser(ctx, tenant.ID, "nobody", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := svc.AuthenticateUser(ctx, tenant.ID, "alice", "wrong password entirely")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("disabled principal", func(t *testing.T) {
		require.NoError(t, svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))
		_, err := svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)

		require.NoError(t, svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalActive))
		_, err = svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
		assert.NoError(t, err, "re-enabling restores access — suspension is reversible")
	})

	t.Run("suspended tenant", func(t *testing.T) {
		require.NoError(t, svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantSuspended))
		_, err := svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)

		require.NoError(t, svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantActive))
		_, err = svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
		assert.NoError(t, err, "reactivating restores access, with identities intact")
	})
}

// TestLive_DisablingInvalidatesAnExistingToken is LLD-02 §10.2 against
// real storage: no waiting for expiry.
func TestLive_DisablingInvalidatesAnExistingToken(t *testing.T) {
	svc, _, _ := newLiveHarness(t)
	ctx := context.Background()

	tenant, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	principal, err := svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", "correct horse battery", "AGENT")
	require.NoError(t, err)

	tok, err := svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)
	_, err = svc.ValidateToken(ctx, tok.Token)
	require.NoError(t, err)

	require.NoError(t, svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))

	_, err = svc.ValidateToken(ctx, tok.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials,
		"the unexpired token stops working on the next request, not at expiry")
}

// TestLive_AuthorizeDenyByDefault is 6.4 against a real store.
func TestLive_AuthorizeDenyByDefault(t *testing.T) {
	svc, _, _ := newLiveHarness(t)
	ctx := context.Background()

	tenant, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	other, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Other", "EU")
	require.NoError(t, err)
	// Provisioned with no role, so it genuinely holds no binding — a
	// principal provisioned WITH a role now carries the matching one,
	// which would make this assertion vacuous.
	principal, err := svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", "correct horse battery", "")
	require.NoError(t, err)

	assert.ErrorIs(t, svc.AuthorizeAction(ctx, principal.ID, "call.read", tenant.ID.String()),
		ports.ErrPermissionDenied, "no binding, no permission")

	require.NoError(t, svc.GrantRole(ctx, application.SystemActor(), tenant.ID, principal.ID, "AGENT", domain.ScopeTenant))
	assert.NoError(t, svc.AuthorizeAction(ctx, principal.ID, "call.read", tenant.ID.String()))

	assert.ErrorIs(t, svc.AuthorizeAction(ctx, principal.ID, "tenant.suspend", tenant.ID.String()),
		ports.ErrPermissionDenied, "an unbound action stays denied")
	assert.ErrorIs(t, svc.AuthorizeAction(ctx, principal.ID, "call.read", other.ID.String()),
		ports.ErrPermissionDenied, "and the grant does not reach another tenant")
}

// TestLive_APIKeyAuthentication exercises the tenant-embedded key format
// against RLS — the arrangement that replaced the unscoped lookup.
func TestLive_APIKeyAuthentication(t *testing.T) {
	svc, store, _ := newLiveHarness(t)
	ctx := context.Background()

	tenant, err := svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	principal, err := svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", "correct horse battery", "AGENT")
	require.NoError(t, err)

	raw, key, err := svc.IssueAPIKey(ctx, application.SystemActor(), tenant.ID, principal.ID, nil)
	require.NoError(t, err)

	tc, err := svc.AuthenticateAPIKey(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, tenant.ID, tc.TenantID)
	assert.Equal(t, principal.ID, tc.PrincipalID)

	require.NoError(t, store.RevokeApiKey(ctx, tenant.ID, key.ID, time.Now().UTC()))
	_, err = svc.AuthenticateAPIKey(ctx, raw)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "a revoked key stops working immediately")
}
