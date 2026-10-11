package application_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/memstore"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

var svcNow = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// --- in-memory fakes ------------------------------------------------
//
// Fakes rather than mocks: the assertions are about observable outcomes
// (a token validates, exactly one audit row exists), not about which
// methods were called in which order.

type harness struct {
	svc    *application.Service
	stores *memstore.Store
	logs   *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	stores := memstore.New()

	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc := application.NewService(application.StoresOf(stores), stores,
		application.NewPasswordHasher(), issuer, logger).
		WithClock(func() time.Time { return svcNow })

	return &harness{svc: svc, stores: stores, logs: logs}
}

// seedTenantAndPrincipal provisions a tenant and an active principal.
func (h *harness) seedTenantAndPrincipal(t *testing.T, username, password string) (domain.Tenant, domain.Principal) {
	t.Helper()
	ctx := context.Background()

	tenant, err := h.svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)

	principal, err := h.svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, username+"@example.test", username, password, "AGENT")
	require.NoError(t, err)

	return tenant, principal
}

// --- 6.1 provisioning + audit ---------------------------------------

func TestProvisionTenant_WritesExactlyOneAuditRecord(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	tenant, err := h.svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	assert.Equal(t, domain.TenantActive, tenant.Status)
	assert.Equal(t, "EU", tenant.ResidencyZone)

	require.Len(t, h.stores.Audit, 1, "exactly one audit record per mutation")
	entry := h.stores.Audit[0]
	assert.Equal(t, "tenant.create", entry.Action)
	assert.Equal(t, tenant.ID.String(), entry.ResourceID)
	assert.Equal(t, "EU", entry.AfterState["residency_zone"])
	assert.Nil(t, entry.BeforeState, "a creation has no before state")
}

func TestProvisionTenant_RequiresResidencyZone(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.ProvisionTenant(context.Background(), application.SystemActor(), "Acme", "")
	assert.Error(t, err, "residency zone can never be blank (LLD-02 §10.6)")
	assert.Empty(t, h.stores.Audit, "a refused mutation writes no audit record")
}

func TestProvisionPrincipal_AuditCarriesNoCredential(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	tenant, err := h.svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)

	const password = "a-very-distinctive-password"
	principal, err := h.svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice@example.test", "Alice", password, "AGENT")
	require.NoError(t, err)

	require.Len(t, h.stores.Audit, 2)
	entry := h.stores.Audit[1]
	assert.Equal(t, "principal.create", entry.Action)

	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), password, "the audit record must not carry the password")
	user, err := h.stores.GetUser(ctx, principal.UserID)
	require.NoError(t, err)
	require.NotNil(t, user.Password)
	assert.NotContains(t, string(raw), user.Password.Hash, "nor its hash")
}

func TestProvisionPrincipal_RejectsWeakPassword(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	tenant, err := h.svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)

	_, err = h.svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice@example.test", "Alice", "short", "AGENT")
	assert.ErrorIs(t, err, domain.ErrPasswordTooShort)
	assert.Len(t, h.stores.Audit, 1, "only the tenant creation was audited; the refused mutation was not")
}

func TestSetStatus_AuditsBeforeAndAfter(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	require.NoError(t, h.svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantSuspended))
	require.NoError(t, h.svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))

	tenantEntry := h.stores.Audit[len(h.stores.Audit)-2]
	assert.Equal(t, "ACTIVE", tenantEntry.BeforeState["status"])
	assert.Equal(t, "SUSPENDED", tenantEntry.AfterState["status"])

	principalEntry := h.stores.Audit[len(h.stores.Audit)-1]
	assert.Equal(t, "ACTIVE", principalEntry.BeforeState["status"])
	assert.Equal(t, "DISABLED", principalEntry.AfterState["status"])
}

func TestSetStatus_RejectsInvalidStatus(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	assert.Error(t, h.svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantStatus("DELETED")))
	assert.Error(t, h.svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalStatus("LOCKED")))
}

// TestAuditFailure_DoesNotFailTheMutation: the change already happened;
// reporting failure would invite a retry of something already applied.
// It must be loudly logged instead.
func TestAuditFailure_DoesNotFailTheMutation(t *testing.T) {
	h := newHarness(t)
	h.stores.FailAudit = errors.New("audit store unavailable")

	tenant, err := h.svc.ProvisionTenant(context.Background(), application.SystemActor(), "Acme", "EU")
	require.NoError(t, err, "the tenant was created; the audit write is what failed")
	assert.NotEmpty(t, tenant.ID)
	assert.Contains(t, h.logs.String(), "audit write failed")
	assert.Contains(t, h.logs.String(), `"level":"ERROR"`)
}

// --- 6.2 authentication ---------------------------------------------

func TestAuthenticateUser_Succeeds(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice@example.test", "correct horse battery")
	require.NoError(t, err)
	assert.Equal(t, principal.ID, tok.PrincipalID)
	assert.Equal(t, tenant.ID, tok.TenantID)
	assert.Equal(t, svcNow.Add(application.DefaultTokenLifetime), tok.ExpiresAt)
	assert.NotEmpty(t, tok.Token)
}

// TestAuthenticateUser_AllFailuresLookIdentical is INV-10 in practice:
// a caller must not be able to tell an unknown username from a wrong
// password from a suspended tenant.
func TestAuthenticateUser_AllFailuresLookIdentical(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown username", func(t *testing.T) {
		h := newHarness(t)
		tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "nobody@example.test", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("wrong password", func(t *testing.T) {
		h := newHarness(t)
		tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice@example.test", "wrong password entirely")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("disabled principal", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		require.NoError(t, h.svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))

		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice@example.test", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials,
			"a disabled account must be indistinguishable from a wrong password")
	})

	t.Run("suspended tenant", func(t *testing.T) {
		h := newHarness(t)
		tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		require.NoError(t, h.svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantSuspended))

		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice@example.test", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})
}

func TestAuthenticateUser_CorruptStoredHashIsAnOperationalFault(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	require.NoError(t, h.stores.SetPassword(context.Background(), principal.UserID,
		domain.PasswordCredential{Hash: "not-a-valid-phc-string"}))

	_, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice@example.test", "correct horse battery")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "the caller still learns nothing")
	assert.Contains(t, h.logs.String(), "stored password hash is unusable")
	assert.Contains(t, h.logs.String(), `"level":"ERROR"`, "but operators are told loudly")
}

// --- 6.3 token validation reflects live status ----------------------

func TestValidateToken_Succeeds(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice@example.test", "correct horse battery")
	require.NoError(t, err)

	tc, err := h.svc.ValidateToken(context.Background(), tok.Token)
	require.NoError(t, err)
	assert.Equal(t, tenant.ID, tc.TenantID)
	assert.Equal(t, principal.ID, tc.PrincipalID)
	assert.Equal(t, "EU", tc.ResidencyZone, "residency travels in the tenant context")
	assert.Contains(t, tc.Roles, "AGENT")
}

// TestValidateToken_DisablingTakesEffectImmediately is LLD-02 §10.2: the
// live-status check is why disabling an account does not wait for the
// token to expire.
func TestValidateToken_DisablingTakesEffectImmediately(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice@example.test", "correct horse battery")
	require.NoError(t, err)
	_, err = h.svc.ValidateToken(ctx, tok.Token)
	require.NoError(t, err, "the token works before the account is disabled")

	require.NoError(t, h.svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))

	_, err = h.svc.ValidateToken(ctx, tok.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials,
		"the same unexpired token must stop working on the very next request")
}

func TestValidateToken_SuspendingTenantTakesEffectImmediately(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice@example.test", "correct horse battery")
	require.NoError(t, err)

	require.NoError(t, h.svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantSuspended))

	_, err = h.svc.ValidateToken(ctx, tok.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
}

func TestValidateToken_RejectsGarbage(t *testing.T) {
	h := newHarness(t)

	for _, token := range []string{"", "not-a-token", "a.b.c"} {
		_, err := h.svc.ValidateToken(context.Background(), token)
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	}
}

// --- api key authentication -----------------------------------------

func TestAuthenticateAPIKey(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	raw, _, err := h.svc.IssueAPIKey(ctx, application.SystemActor(), tenant.ID, principal.ID, nil)
	require.NoError(t, err)

	tc, err := h.svc.AuthenticateAPIKey(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, tenant.ID, tc.TenantID)
	assert.Equal(t, principal.ID, tc.PrincipalID)

	t.Run("audit records the issue without the key", func(t *testing.T) {
		entry := h.stores.Audit[len(h.stores.Audit)-1]
		assert.Equal(t, "apikey.issue", entry.Action)
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), raw)
	})

	t.Run("expired key is refused", func(t *testing.T) {
		past := svcNow.Add(-time.Hour)
		expiredRaw, _, err := h.svc.IssueAPIKey(ctx, application.SystemActor(), tenant.ID, principal.ID, &past)
		require.NoError(t, err)

		_, err = h.svc.AuthenticateAPIKey(ctx, expiredRaw)
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("forged key naming a real tenant is refused", func(t *testing.T) {
		forged := "atsa_" + tenant.ID.String() + "_" + strings.Repeat("A", 43)
		_, err := h.svc.AuthenticateAPIKey(ctx, forged)
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("malformed key is refused", func(t *testing.T) {
		_, err := h.svc.AuthenticateAPIKey(ctx, "nonsense")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})
}

// --- 6.4 authorization ----------------------------------------------

func TestAuthorizeAction(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	// A principal provisioned with a role now carries the matching
	// binding, so "no binding" needs one provisioned without a role —
	// otherwise this asserts nothing.
	t.Run("denied with no binding", func(t *testing.T) {
		unbound, err := h.svc.ProvisionPrincipal(ctx, application.SystemActor(),
			tenant.ID, "unbound@example.test", "Unbound", "unbound-password-1", "")
		require.NoError(t, err)

		err = h.svc.AuthorizeAction(ctx, unbound.ID, "call.read", tenant.ID.String())
		assert.ErrorIs(t, err, ports.ErrPermissionDenied, "deny by default")
	})

	// The converse of the above, and the reason the binding is created:
	// a principal provisioned WITH a role must actually hold it, rather
	// than carrying a role column that authorizes nothing.
	t.Run("a provisioned role is real authority", func(t *testing.T) {
		assert.NoError(t, h.svc.AuthorizeAction(ctx, principal.ID, "call.read", tenant.ID.String()),
			"seeded principal has role AGENT, which grants call.read")
	})

	require.NoError(t, h.svc.GrantRole(ctx, application.SystemActor(), tenant.ID, principal.ID, "AGENT", domain.ScopeTenant))

	t.Run("permitted once bound", func(t *testing.T) {
		assert.NoError(t, h.svc.AuthorizeAction(ctx, principal.ID, "call.read", tenant.ID.String()))
	})

	t.Run("still denied for an unbound action", func(t *testing.T) {
		err := h.svc.AuthorizeAction(ctx, principal.ID, "tenant.suspend", tenant.ID.String())
		assert.ErrorIs(t, err, ports.ErrPermissionDenied)
	})

	t.Run("denied against another tenant's resource", func(t *testing.T) {
		other := shareddomain.NewTenantID()
		err := h.svc.AuthorizeAction(ctx, principal.ID, "call.read", other.String())
		assert.ErrorIs(t, err, ports.ErrPermissionDenied)
	})

	t.Run("granting a role is audited", func(t *testing.T) {
		entry := h.stores.Audit[len(h.stores.Audit)-1]
		assert.Equal(t, "principal.role.grant", entry.Action)
		assert.Equal(t, "AGENT", entry.AfterState["role"])
	})
}

// TestAuthorizeAction_StoreFailureDenies: an unavailable binding store
// must never widen access.
func TestAuthorizeAction_StoreFailureDenies(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
	require.NoError(t, h.svc.GrantRole(ctx, application.SystemActor(), tenant.ID, principal.ID, "TENANT_ADMIN", domain.ScopeTenant))

	h.stores.FailBindings = errors.New("database unavailable")

	err := h.svc.AuthorizeAction(ctx, principal.ID, "call.read", tenant.ID.String())
	assert.ErrorIs(t, err, ports.ErrPermissionDenied, "fail closed, not open")
	assert.Contains(t, h.logs.String(), "could not load role bindings, denying")
}

func TestAuthorizeAction_RejectsNonTenantResource(t *testing.T) {
	h := newHarness(t)
	_, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	err := h.svc.AuthorizeAction(context.Background(), principal.ID, "call.read", "not-a-tenant-id")
	assert.ErrorIs(t, err, ports.ErrPermissionDenied)
}

// --- 6.5 failed-auth logging ----------------------------------------

// TestFailedAuth_IsLoggedWithoutCredentials is LLD-02 §10.8 / OWASP A09:
// brute force and credential stuffing are invisible without these logs,
// and the logs are themselves a leak if they carry what was attempted.
func TestFailedAuth_IsLoggedWithoutCredentials(t *testing.T) {
	h := newHarness(t)
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	const attempted = "hunter2-the-attempted-password"
	_, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice@example.test", attempted)
	require.ErrorIs(t, err, ports.ErrInvalidCredentials)

	logs := h.logs.String()
	assert.Contains(t, logs, "authentication failed")
	assert.Contains(t, logs, `"level":"WARN"`)
	assert.Contains(t, logs, "wrong password", "the reason is recorded for operators")
	assert.Contains(t, logs, "alice", "and who was attempted")
	assert.NotContains(t, logs, attempted, "but never the credential itself")
}

func TestFailedAuth_TokenRejectionIsLoggedWithoutTheToken(t *testing.T) {
	h := newHarness(t)
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice@example.test", "correct horse battery")
	require.NoError(t, err)

	h.logs.Reset()
	expired := h.svc.WithClock(func() time.Time { return svcNow.Add(2 * application.DefaultTokenLifetime) })
	_, err = expired.ValidateToken(context.Background(), tok.Token)
	require.ErrorIs(t, err, ports.ErrInvalidCredentials)

	logs := h.logs.String()
	assert.Contains(t, logs, "token rejected")
	assert.NotContains(t, logs, tok.Token, "the token is credential material and must not be logged")
}

// TestRevokeAPIKey covers a path that had no test until 2026-09-09.
// Revocation is a security operation: the interesting assertions are
// that it reaches the store, that it is auditable, and that a failure
// leaves no misleading audit record behind.
func TestRevokeAPIKey(t *testing.T) {
	t.Run("revocation reaches the store and is audited", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "admin", "correct-horse-battery-staple")
		keyID := shareddomain.NewApiKeyID()

		err := h.svc.RevokeAPIKey(context.Background(),
			application.PrincipalActor(principal.ID, nil), tenant.ID, keyID)

		require.NoError(t, err)
		require.Len(t, h.stores.Revoked, 1, "the revocation must reach the store, not just return nil")
		assert.Equal(t, keyID, h.stores.Revoked[0].KeyID)
		assert.Equal(t, tenant.ID, h.stores.Revoked[0].TenantID,
			"revocation is tenant-scoped — a key is never revoked across tenants")

		var revokeEntries []domain.AuditEntry
		for _, e := range h.stores.Audit {
			if e.Action == "apikey.revoke" {
				revokeEntries = append(revokeEntries, e)
			}
		}
		require.Len(t, revokeEntries, 1, "a successful mutation writes exactly one audit record (API.md §3a)")
		assert.Equal(t, "api_key", revokeEntries[0].ResourceType)
		assert.Equal(t, keyID.String(), revokeEntries[0].ResourceID)
	})

	t.Run("a store failure propagates and writes no audit record", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "admin", "correct-horse-battery-staple")
		h.stores.FailRevoke = errors.New("database unavailable")
		before := len(h.stores.Audit)

		err := h.svc.RevokeAPIKey(context.Background(),
			application.PrincipalActor(principal.ID, nil), tenant.ID, shareddomain.NewApiKeyID())

		require.Error(t, err)
		assert.Len(t, h.stores.Audit, before,
			"a refused mutation writes no audit record (API.md §3a) — an audit trail claiming a revocation that did not happen is worse than none")
		assert.Empty(t, h.stores.Revoked)
	})

	t.Run("the audit record carries no key material", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "admin", "correct-horse-battery-staple")

		require.NoError(t, h.svc.RevokeAPIKey(context.Background(),
			application.PrincipalActor(principal.ID, nil), tenant.ID, shareddomain.NewApiKeyID()))

		for _, e := range h.stores.Audit {
			raw, err := json.Marshal(e)
			require.NoError(t, err)
			assert.NotContains(t, strings.ToLower(string(raw)), "secret",
				"no credential material in an audit record (LLD-02 §10.3, D-39)")
			assert.NotContains(t, strings.ToLower(string(raw)), "hash")
		}
	})
}

// TestAuthorizeSystem is the service-level half of the system-scope rule
// tested in identity/domain. It had no test until 2026-09-09.
//
// What it adds over the domain test: that the service loads bindings
// against the principal's HOME tenant, and that a store failure denies
// rather than permits — an unavailable binding store must never widen
// access.
func TestAuthorizeSystem(t *testing.T) {
	ctx := context.Background()

	t.Run("a system-scoped binding is authorized", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "operator", "correct-horse-battery-staple")
		require.NoError(t, h.stores.CreateRoleBinding(ctx, domain.RoleBinding{
			PrincipalID: principal.ID, TenantID: tenant.ID,
			Role: "PLATFORM_ADMIN", Scope: domain.ScopeSystem,
		}))

		assert.NoError(t, h.svc.AuthorizeSystem(ctx, principal.ID, tenant.ID, "tenant.create"))
	})

	t.Run("a tenant-scoped binding is refused however broad the role", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "tenantadmin", "correct-horse-battery-staple")
		require.NoError(t, h.stores.CreateRoleBinding(ctx, domain.RoleBinding{
			PrincipalID: principal.ID, TenantID: tenant.ID,
			Role: "TENANT_ADMIN", Scope: domain.ScopeTenant,
		}))

		err := h.svc.AuthorizeSystem(ctx, principal.ID, tenant.ID, "tenant.create")

		assert.ErrorIs(t, err, ports.ErrPermissionDenied,
			"a customer's own administrator must never hold installation authority")
	})

	t.Run("no bindings is refused", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "nobody", "correct-horse-battery-staple")

		assert.ErrorIs(t, h.svc.AuthorizeSystem(ctx, principal.ID, tenant.ID, "tenant.create"),
			ports.ErrPermissionDenied)
	})

	t.Run("a binding-store failure denies rather than permits", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "operator", "correct-horse-battery-staple")
		require.NoError(t, h.stores.CreateRoleBinding(ctx, domain.RoleBinding{
			PrincipalID: principal.ID, TenantID: tenant.ID,
			Role: "PLATFORM_ADMIN", Scope: domain.ScopeSystem,
		}))
		h.stores.FailBindings = errors.New("database unavailable")

		err := h.svc.AuthorizeSystem(ctx, principal.ID, tenant.ID, "tenant.create")

		assert.ErrorIs(t, err, ports.ErrPermissionDenied,
			"an unavailable store must never widen access — fail closed")
	})
}
