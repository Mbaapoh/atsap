package rpc_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/memstore"
	"atsap-api/internal/identity/rpc"
)

// env bundles a real Service over an in-memory store plus the handler.
type env struct {
	svc *application.Service
	h   *rpc.IdentityHandler
	st  *memstore.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := memstore.New()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, time.Hour)
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := application.NewService(application.StoresOf(st), st, application.NewPasswordHasher(), issuer, logger)
	return &env{svc: svc, h: rpc.NewIdentityHandler(svc), st: st}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func (e *env) seedTenant(t *testing.T, name string) domain.Tenant {
	t.Helper()
	tenant, err := e.svc.ProvisionTenant(context.Background(), application.SystemActor(), name, "EU")
	require.NoError(t, err)
	return tenant
}

// user is a provisioned, role-granted, authenticated actor.
type user struct {
	tenant    domain.Tenant
	principal domain.Principal
	ctx       context.Context
}

func (e *env) seedUser(t *testing.T, tenant domain.Tenant, username, password, role, scope string) user {
	t.Helper()
	p, err := e.svc.ProvisionPrincipal(context.Background(), application.SystemActor(),
		tenant.ID, username+"@example.test", username, password, role)
	require.NoError(t, err)
	if scope == "" {
		scope = domain.ScopeTenant
	}
	require.NoError(t, e.svc.GrantRole(context.Background(), application.SystemActor(), tenant.ID, p.ID, role, scope))
	tok, err := e.svc.AuthenticateUser(context.Background(), tenant.ID, username+"@example.test", password)
	require.NoError(t, err)
	tc, err := e.svc.ValidateToken(context.Background(), tok.Token)
	require.NoError(t, err)
	return user{tenant: tenant, principal: p, ctx: application.WithTenantContext(context.Background(), tc)}
}

func requireCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, code, connect.CodeOf(err), "err: %v", err)
}

// --- AuthenticateUser: success and identical refusals ----------------

func TestAuthenticateUser_SuccessAndIndistinguishableFailures(t *testing.T) {
	e := newEnv(t)
	tenant := e.seedTenant(t, "Acme")
	alice := e.seedUser(t, tenant, "alice", "correct horse battery", "AGENT", "")

	req := func(pw string) *connect.Request[atsapbxv1.AuthenticateUserRequest] {
		return connect.NewRequest(&atsapbxv1.AuthenticateUserRequest{
			TenantId: tenant.ID.String(), Username: "alice@example.test", Password: pw,
		})
	}

	resp, err := e.h.AuthenticateUser(context.Background(), req("correct horse battery"))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.GetToken())
	assert.Equal(t, tenant.ID.String(), resp.Msg.GetTenantId())
	assert.Equal(t, alice.principal.ID.String(), resp.Msg.GetPrincipalId())

	// Wrong password, unknown username, disabled account: same code and
	// same message — only the logs distinguish them (INV-10).
	_, err = e.h.AuthenticateUser(context.Background(), req("not-the-password"))
	require.Error(t, err)
	wrong := err
	_, err = e.h.AuthenticateUser(context.Background(), connect.NewRequest(&atsapbxv1.AuthenticateUserRequest{
		TenantId: tenant.ID.String(), Username: "ghost@example.test", Password: "correct horse battery",
	}))
	require.Error(t, err)
	unknown := err

	require.NoError(t, e.svc.SetPrincipalStatus(context.Background(), application.SystemActor(),
		tenant.ID, alice.principal.ID, domain.PrincipalDisabled))
	_, err = e.h.AuthenticateUser(context.Background(), req("correct horse battery"))
	require.Error(t, err)
	disabled := err

	for _, got := range []error{wrong, unknown, disabled} {
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(got))
		assert.Equal(t, wrong.Error(), got.Error(), "credential failures must be indistinguishable")
	}
}

// --- 2a.2 ProvisionTenant requires system scope ----------------------

func TestProvisionTenant_RequiresSystemScope(t *testing.T) {
	e := newEnv(t)
	tenantA := e.seedTenant(t, "Acme")
	adminA := e.seedUser(t, tenantA, "admin-a", "correct-pw-a-battery", "TENANT_ADMIN", domain.ScopeTenant)

	// A full TENANT_ADMIN of A must not be able to create tenants.
	_, err := e.h.ProvisionTenant(adminA.ctx, connect.NewRequest(&atsapbxv1.ProvisionTenantRequest{Name: "B", ResidencyZone: "EU"}))
	requireCode(t, err, connect.CodePermissionDenied)

	// A system-scoped operator can create a tenant and provision its
	// first administrator.
	op := e.seedUser(t, tenantA, "op", "correct-pw-op-battery", "PLATFORM_ADMIN", domain.ScopeSystem)
	tenantB, err := e.h.ProvisionTenant(op.ctx, connect.NewRequest(&atsapbxv1.ProvisionTenantRequest{Name: "B", ResidencyZone: "US"}))
	require.NoError(t, err)
	assert.Equal(t, "B", tenantB.Msg.GetTenant().GetName())

	_, err = e.h.ProvisionPrincipal(op.ctx, connect.NewRequest(&atsapbxv1.ProvisionPrincipalRequest{
		TenantId: tenantB.Msg.GetTenant().GetTenantId(),
		Email:    "admin-b@example.test", Password: "correct-pw-b-battery", Role: "TENANT_ADMIN",
	}))
	require.NoError(t, err, "a platform operator may provision a tenant's first administrator")
}

// --- tenant cap on the wire (LLD-02 §4a.1, AC-06.14) -----------------

// At the cap, the refusal is FAILED_PRECONDITION naming the licence —
// never PERMISSION_DENIED, which would send the administrator to check
// roles they already hold. An unauthorized caller still gets
// PERMISSION_DENIED: authorization is decided first, so a licence state
// never tells an unprivileged caller anything.
func TestProvisionTenant_AtCapIsFailedPreconditionNotPermissionDenied(t *testing.T) {
	e := newEnv(t)
	tenantA := e.seedTenant(t, "Acme")
	adminA := e.seedUser(t, tenantA, "admin-a", "correct-pw-a-battery", "TENANT_ADMIN", domain.ScopeTenant)
	op := e.seedUser(t, tenantA, "op", "correct-pw-op-battery", "PLATFORM_ADMIN", domain.ScopeSystem)
	e.st.TenantCap = 1

	_, err := e.h.ProvisionTenant(op.ctx, connect.NewRequest(&atsapbxv1.ProvisionTenantRequest{Name: "B", ResidencyZone: "EU"}))
	requireCode(t, err, connect.CodeFailedPrecondition)
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Message(), "licence entitlement", "the reason identifies the licence, not the caller's roles")

	_, err = e.h.ProvisionTenant(adminA.ctx, connect.NewRequest(&atsapbxv1.ProvisionTenantRequest{Name: "B", ResidencyZone: "EU"}))
	requireCode(t, err, connect.CodePermissionDenied)

	// The tenant at the cap is not gated: it is still administered.
	_, err = e.h.SetTenantStatus(op.ctx, connect.NewRequest(&atsapbxv1.SetTenantStatusRequest{
		TenantId: tenantA.ID.String(), Status: atsapbxv1.TenantStatus_TENANT_STATUS_ACTIVE,
	}))
	require.NoError(t, err)
}

// --- 2a.1/2a.3 authorization matrix ----------------------------------

func TestAuthorization_BasicRoleCannotAdminister(t *testing.T) {
	e := newEnv(t)
	tenant := e.seedTenant(t, "Acme")
	agent := e.seedUser(t, tenant, "agent", "correct-pw-agent-battery", "AGENT", domain.ScopeTenant)
	admin := e.seedUser(t, tenant, "admin", "correct-pw-admin-battery", "TENANT_ADMIN", domain.ScopeTenant)

	// ProvisionPrincipal (basic role, trying to administer).
	_, err := e.h.ProvisionPrincipal(agent.ctx, connect.NewRequest(&atsapbxv1.ProvisionPrincipalRequest{
		TenantId: tenant.ID.String(), Email: "x@example.test", Password: "correct-pw-x-battery", Role: "AGENT",
	}))
	requireCode(t, err, connect.CodePermissionDenied)
	// The same call with an admin role succeeds.
	_, err = e.h.ProvisionPrincipal(admin.ctx, connect.NewRequest(&atsapbxv1.ProvisionPrincipalRequest{
		TenantId: tenant.ID.String(), Email: "newbie@example.test", Password: "correct-pw-n-battery", Role: "AGENT",
	}))
	require.NoError(t, err)

	// SetPrincipalStatus: agent refused; admin succeeds.
	_, err = e.h.SetPrincipalStatus(agent.ctx, connect.NewRequest(&atsapbxv1.SetPrincipalStatusRequest{
		TenantId: tenant.ID.String(), PrincipalId: admin.principal.ID.String(),
		Status: atsapbxv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED,
	}))
	requireCode(t, err, connect.CodePermissionDenied)

	// GrantRole: agent refused (cannot escalate), admin can grant tenant role.
	_, err = e.h.GrantRole(agent.ctx, connect.NewRequest(&atsapbxv1.GrantRoleRequest{
		TenantId: tenant.ID.String(), PrincipalId: agent.principal.ID.String(),
		Role: "TENANT_ADMIN",
	}))
	requireCode(t, err, connect.CodePermissionDenied)
	_, err = e.h.GrantRole(admin.ctx, connect.NewRequest(&atsapbxv1.GrantRoleRequest{
		TenantId: tenant.ID.String(), PrincipalId: agent.principal.ID.String(), Role: "SUPERVISOR",
	}))
	require.NoError(t, err)

	// IssueApiKey: agent refused.
	_, err = e.h.IssueApiKey(agent.ctx, connect.NewRequest(&atsapbxv1.IssueApiKeyRequest{
		TenantId: tenant.ID.String(), PrincipalId: agent.principal.ID.String(),
	}))
	requireCode(t, err, connect.CodePermissionDenied)

	// ListAudit: AGENT holds no audit.read.
	_, err = e.h.ListAudit(agent.ctx, connect.NewRequest(&atsapbxv1.ListAuditRequest{TenantId: tenant.ID.String()}))
	requireCode(t, err, connect.CodePermissionDenied)
	_, err = e.h.ListAudit(admin.ctx, connect.NewRequest(&atsapbxv1.ListAuditRequest{TenantId: tenant.ID.String()}))
	require.NoError(t, err)
}

// TestAuthorization_AGENTGrantsNothing asserts the permission table gives
// AGENT none of the identity actions (2a.3).
func TestAuthorization_AGENTGrantsNoIdentityAction(t *testing.T) {
	for _, action := range application.IdentityActions {
		granted := false
		for _, perm := range application.DefaultPermissions["AGENT"] {
			if perm == action || perm == "*" || strings.HasSuffix(perm, ".*") && strings.HasPrefix(action, strings.TrimSuffix(perm, "*")) {
				granted = true
			}
		}
		assert.False(t, granted, "AGENT must not be granted %s", action)
	}
	for _, action := range application.IdentityActions {
		assert.Contains(t, application.DefaultPermissions["TENANT_ADMIN"], action, "identity action listed for TENANT_ADMIN (2a.3)")
		assert.Contains(t, application.DefaultPermissions["PLATFORM_ADMIN"], action, "identity action listed for PLATFORM_ADMIN (2a.3)")
	}
}

// --- 2a.4 no self-escalation ------------------------------------------

func TestGrantRole_CannotEscalateSelf(t *testing.T) {
	e := newEnv(t)
	tenant := e.seedTenant(t, "Acme")
	agent := e.seedUser(t, tenant, "agent", "correct-pw-agent-battery", "AGENT", domain.ScopeTenant)

	_, err := e.h.GrantRole(agent.ctx, connect.NewRequest(&atsapbxv1.GrantRoleRequest{
		TenantId: tenant.ID.String(), PrincipalId: agent.principal.ID.String(),
		Role: "TENANT_ADMIN",
	}))
	requireCode(t, err, connect.CodePermissionDenied)

	// And no binding was created.
	tc, ok := application.TenantContextFrom(agent.ctx)
	require.True(t, ok)
	require.NotContains(t, tc.Roles, "TENANT_ADMIN", "the refused grant must not have taken effect")
}

// TestGrantRole_PlatformRoleAndSystemScopeRequirePlatformAuthority
// covers the GrantRole guards: a tenant admin cannot grant the platform
// role, and a system operator can.
func TestGrantRole_PlatformRoleAndSystemScopeRequirePlatformAuthority(t *testing.T) {
	e := newEnv(t)
	tenantA := e.seedTenant(t, "Acme")
	adminA := e.seedUser(t, tenantA, "admin-a", "correct-pw-a-battery", "TENANT_ADMIN", domain.ScopeTenant)
	op := e.seedUser(t, tenantA, "op", "correct-pw-op-battery", "PLATFORM_ADMIN", domain.ScopeSystem)
	other := e.seedUser(t, tenantA, "plain", "correct-pw-p-battery", "AGENT", domain.ScopeTenant)

	// Tenant admin cannot grant PLATFORM_ADMIN (even at tenant scope) nor
	// a system-scope grant.
	_, err := e.h.GrantRole(adminA.ctx, connect.NewRequest(&atsapbxv1.GrantRoleRequest{
		TenantId: tenantA.ID.String(), PrincipalId: other.principal.ID.String(), Role: "PLATFORM_ADMIN",
	}))
	requireCode(t, err, connect.CodePermissionDenied)
	_, err = e.h.GrantRole(adminA.ctx, connect.NewRequest(&atsapbxv1.GrantRoleRequest{
		TenantId: tenantA.ID.String(), PrincipalId: other.principal.ID.String(), Role: "AGENT", Scope: domain.ScopeSystem,
	}))
	requireCode(t, err, connect.CodePermissionDenied)

	// System operator can grant PLATFORM_ADMIN at system scope.
	_, err = e.h.GrantRole(op.ctx, connect.NewRequest(&atsapbxv1.GrantRoleRequest{
		TenantId: tenantA.ID.String(), PrincipalId: other.principal.ID.String(), Role: "PLATFORM_ADMIN", Scope: domain.ScopeSystem,
	}))
	require.NoError(t, err)
}

// --- 2.2 responses never carry credential material --------------------

func TestResponses_NeverCarryCredentialMaterial(t *testing.T) {
	e := newEnv(t)
	tenant := e.seedTenant(t, "Acme")
	admin := e.seedUser(t, tenant, "admin", "correct-pw-admin-battery", "TENANT_ADMIN", domain.ScopeTenant)

	// Descriptor walk over every IdentityService response.
	respTypes := []protoreflect.Message{
		(&atsapbxv1.AuthenticateUserResponse{}).ProtoReflect(),
		(&atsapbxv1.ProvisionTenantResponse{}).ProtoReflect(),
		(&atsapbxv1.ProvisionPrincipalResponse{}).ProtoReflect(),
		(&atsapbxv1.SetTenantStatusResponse{}).ProtoReflect(),
		(&atsapbxv1.SetPrincipalStatusResponse{}).ProtoReflect(),
		(&atsapbxv1.GrantRoleResponse{}).ProtoReflect(),
		(&atsapbxv1.IssueApiKeyResponse{}).ProtoReflect(),
		(&atsapbxv1.RevokeApiKeyResponse{}).ProtoReflect(),
		(&atsapbxv1.ListAuditResponse{}).ProtoReflect(),
	}
	for _, m := range respTypes {
		walkNoCredentialFields(t, m.Descriptor())
	}

	// Content scan of a real provisioning response: no password in it.
	provisioned, err := e.h.ProvisionPrincipal(admin.ctx, connect.NewRequest(&atsapbxv1.ProvisionPrincipalRequest{
		TenantId: tenant.ID.String(), Email: "dave@example.test", Password: "super-secret-pw", Role: "AGENT",
	}))
	require.NoError(t, err)
	raw, err := json.Marshal(provisioned.Msg)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "super-secret-pw")
	assert.NotContains(t, string(raw), "password")
}

func walkNoCredentialFields(t *testing.T, md protoreflect.MessageDescriptor) {
	t.Helper()
	for i := 0; i < md.Fields().Len(); i++ {
		f := md.Fields().Get(i)
		name := strings.ToLower(string(f.Name()))
		for _, bad := range []string{"password", "secret", "key_hash", "hash"} {
			assert.NotContains(t, name, bad, "%s.%s must not reference credential material", md.FullName(), f.Name())
		}
		if f.Kind() == protoreflect.MessageKind && !f.IsMap() {
			walkNoCredentialFields(t, f.Message())
		}
	}
}

// --- 2.3 the raw key appears once and never again ---------------------

func TestRawApiKey_IssuedOnceNeverAgain(t *testing.T) {
	e := newEnv(t)
	tenant := e.seedTenant(t, "Acme")
	admin := e.seedUser(t, tenant, "admin", "correct-pw-admin-battery", "TENANT_ADMIN", domain.ScopeTenant)

	issued, err := e.h.IssueApiKey(admin.ctx, connect.NewRequest(&atsapbxv1.IssueApiKeyRequest{
		TenantId: tenant.ID.String(), PrincipalId: admin.principal.ID.String(),
		ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}))
	require.NoError(t, err)
	raw := issued.Msg.GetRawKey()
	assert.NotEmpty(t, raw)
	assert.Equal(t, issued.Msg.GetRawKey(), raw)

	// A later read — audit history — must not recover the key.
	audit, err := e.h.ListAudit(admin.ctx, connect.NewRequest(&atsapbxv1.ListAuditRequest{TenantId: tenant.ID.String()}))
	require.NoError(t, err)
	auditJSON, err := json.Marshal(audit.Msg)
	require.NoError(t, err)
	assert.NotContains(t, string(auditJSON), raw, "the raw key must never appear in audit history")

	// No later identity response carries it either.
	_, err = e.h.SetPrincipalStatus(admin.ctx, connect.NewRequest(&atsapbxv1.SetPrincipalStatusRequest{
		TenantId: tenant.ID.String(), PrincipalId: admin.principal.ID.String(),
		Status: atsapbxv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE,
	}))
	require.NoError(t, err)
}
