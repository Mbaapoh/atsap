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
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

var svcNow = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// --- in-memory fakes ------------------------------------------------
//
// Fakes rather than mocks: the assertions are about observable outcomes
// (a token validates, exactly one audit row exists), not about which
// methods were called in which order.

type fakeStores struct {
	tenants    map[string]domain.Tenant
	principals map[string]domain.Principal
	bindings   []domain.RoleBinding
	apiKeys    map[string]domain.ApiKey
	audit      []domain.AuditEntry

	failAudit    error
	failBindings error
}

func newFakeStores() *fakeStores {
	return &fakeStores{
		tenants:    map[string]domain.Tenant{},
		principals: map[string]domain.Principal{},
		apiKeys:    map[string]domain.ApiKey{},
	}
}

func (f *fakeStores) CreateTenant(_ context.Context, t domain.Tenant) error {
	f.tenants[t.ID.String()] = t
	return nil
}

func (f *fakeStores) GetTenant(_ context.Context, id shareddomain.TenantID) (domain.Tenant, error) {
	t, ok := f.tenants[id.String()]
	if !ok {
		return domain.Tenant{}, ports.ErrNotFound
	}
	return t, nil
}

func (f *fakeStores) SetTenantStatus(_ context.Context, id shareddomain.TenantID, status domain.TenantStatus) error {
	t, ok := f.tenants[id.String()]
	if !ok {
		return ports.ErrNotFound
	}
	t.Status = status
	f.tenants[id.String()] = t
	return nil
}

func (f *fakeStores) CreatePrincipal(_ context.Context, p domain.Principal) error {
	for _, existing := range f.principals {
		if existing.TenantID == p.TenantID && existing.Username == p.Username {
			return ports.ErrUsernameTaken
		}
	}
	f.principals[p.ID.String()] = p
	return nil
}

func (f *fakeStores) GetPrincipal(_ context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID) (domain.Principal, error) {
	p, ok := f.principals[id.String()]
	if !ok || p.TenantID != tenantID {
		return domain.Principal{}, ports.ErrNotFound
	}
	return p, nil
}

func (f *fakeStores) GetPrincipalByUsername(_ context.Context, tenantID shareddomain.TenantID, username string) (domain.Principal, error) {
	for _, p := range f.principals {
		if p.TenantID == tenantID && p.Username == username {
			return p, nil
		}
	}
	return domain.Principal{}, ports.ErrNotFound
}

func (f *fakeStores) SetPrincipalStatus(_ context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID, status domain.PrincipalStatus) error {
	p, ok := f.principals[id.String()]
	if !ok || p.TenantID != tenantID {
		return ports.ErrNotFound
	}
	p.Status = status
	f.principals[id.String()] = p
	return nil
}

func (f *fakeStores) CreateRoleBinding(_ context.Context, b domain.RoleBinding) error {
	f.bindings = append(f.bindings, b)
	return nil
}

func (f *fakeStores) ListRoleBindings(_ context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID) ([]domain.RoleBinding, error) {
	if f.failBindings != nil {
		return nil, f.failBindings
	}
	var out []domain.RoleBinding
	for _, b := range f.bindings {
		if b.TenantID == tenantID && b.PrincipalID == principalID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeStores) DeleteRoleBinding(context.Context, domain.RoleBinding) error { return nil }

func (f *fakeStores) CreateApiKey(_ context.Context, k domain.ApiKey) error {
	f.apiKeys[k.KeyHash] = k
	return nil
}

func (f *fakeStores) GetApiKeyByHash(_ context.Context, tenantID shareddomain.TenantID, keyHash string) (domain.ApiKey, error) {
	k, ok := f.apiKeys[keyHash]
	if !ok || k.TenantID != tenantID {
		return domain.ApiKey{}, ports.ErrNotFound
	}
	return k, nil
}

func (f *fakeStores) RevokeApiKey(_ context.Context, _ shareddomain.TenantID, _ shareddomain.ApiKeyID, _ time.Time) error {
	return nil
}

func (f *fakeStores) AppendAudit(_ context.Context, e domain.AuditEntry) error {
	if f.failAudit != nil {
		return f.failAudit
	}
	f.audit = append(f.audit, e)
	return nil
}

func (f *fakeStores) ListAudit(_ context.Context, tenantID shareddomain.TenantID, _ int) ([]domain.AuditEntry, error) {
	var out []domain.AuditEntry
	for _, e := range f.audit {
		if e.TenantID == tenantID {
			out = append(out, e)
		}
	}
	return out, nil
}

type harness struct {
	svc    *application.Service
	stores *fakeStores
	logs   *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	stores := newFakeStores()

	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc := application.NewService(stores, stores, stores, stores, stores,
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
		tenant.ID, username, username+"@example.test", password, "AGENT")
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

	require.Len(t, h.stores.audit, 1, "exactly one audit record per mutation")
	entry := h.stores.audit[0]
	assert.Equal(t, "tenant.create", entry.Action)
	assert.Equal(t, tenant.ID.String(), entry.ResourceID)
	assert.Equal(t, "EU", entry.AfterState["residency_zone"])
	assert.Nil(t, entry.BeforeState, "a creation has no before state")
}

func TestProvisionTenant_RequiresResidencyZone(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.ProvisionTenant(context.Background(), application.SystemActor(), "Acme", "")
	assert.Error(t, err, "residency zone can never be blank (LLD-02 §10.6)")
	assert.Empty(t, h.stores.audit, "a refused mutation writes no audit record")
}

func TestProvisionPrincipal_AuditCarriesNoCredential(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	tenant, err := h.svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)

	const password = "a-very-distinctive-password"
	principal, err := h.svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", password, "AGENT")
	require.NoError(t, err)

	require.Len(t, h.stores.audit, 2)
	entry := h.stores.audit[1]
	assert.Equal(t, "principal.create", entry.Action)

	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), password, "the audit record must not carry the password")
	assert.NotContains(t, string(raw), principal.PasswordHash, "nor its hash")
}

func TestProvisionPrincipal_RejectsWeakPassword(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	tenant, err := h.svc.ProvisionTenant(ctx, application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)

	_, err = h.svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, "alice", "alice@example.test", "short", "AGENT")
	assert.ErrorIs(t, err, domain.ErrPasswordTooShort)
	assert.Len(t, h.stores.audit, 1, "only the tenant creation was audited; the refused mutation was not")
}

func TestSetStatus_AuditsBeforeAndAfter(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	require.NoError(t, h.svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantSuspended))
	require.NoError(t, h.svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))

	tenantEntry := h.stores.audit[len(h.stores.audit)-2]
	assert.Equal(t, "ACTIVE", tenantEntry.BeforeState["status"])
	assert.Equal(t, "SUSPENDED", tenantEntry.AfterState["status"])

	principalEntry := h.stores.audit[len(h.stores.audit)-1]
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
	h.stores.failAudit = errors.New("audit store unavailable")

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

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
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
		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "nobody", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("wrong password", func(t *testing.T) {
		h := newHarness(t)
		tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice", "wrong password entirely")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})

	t.Run("disabled principal", func(t *testing.T) {
		h := newHarness(t)
		tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		require.NoError(t, h.svc.SetPrincipalStatus(ctx, application.SystemActor(), tenant.ID, principal.ID, domain.PrincipalDisabled))

		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials,
			"a disabled account must be indistinguishable from a wrong password")
	})

	t.Run("suspended tenant", func(t *testing.T) {
		h := newHarness(t)
		tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
		require.NoError(t, h.svc.SetTenantStatus(ctx, application.SystemActor(), tenant.ID, domain.TenantSuspended))

		_, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})
}

func TestAuthenticateUser_CorruptStoredHashIsAnOperationalFault(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	broken := h.stores.principals[principal.ID.String()]
	broken.PasswordHash = "not-a-valid-phc-string"
	h.stores.principals[principal.ID.String()] = broken

	_, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "the caller still learns nothing")
	assert.Contains(t, h.logs.String(), "stored password hash is unusable")
	assert.Contains(t, h.logs.String(), `"level":"ERROR"`, "but operators are told loudly")
}

// --- 6.3 token validation reflects live status ----------------------

func TestValidateToken_Succeeds(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
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

	tok, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
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

	tok, err := h.svc.AuthenticateUser(ctx, tenant.ID, "alice", "correct horse battery")
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
		entry := h.stores.audit[len(h.stores.audit)-1]
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
			tenant.ID, "unbound", "unbound@example.test", "unbound-password-1", "")
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
		entry := h.stores.audit[len(h.stores.audit)-1]
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

	h.stores.failBindings = errors.New("database unavailable")

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
	_, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", attempted)
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

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	h.logs.Reset()
	expired := h.svc.WithClock(func() time.Time { return svcNow.Add(2 * application.DefaultTokenLifetime) })
	_, err = expired.ValidateToken(context.Background(), tok.Token)
	require.ErrorIs(t, err, ports.ErrInvalidCredentials)

	logs := h.logs.String()
	assert.Contains(t, logs, "token rejected")
	assert.NotContains(t, logs, tok.Token, "the token is credential material and must not be logged")
}
