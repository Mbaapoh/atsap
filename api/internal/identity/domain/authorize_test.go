package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/identity/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// permissions is a representative role table. The vocabulary itself is
// deliberately not fixed by this change (design.md Open Questions) — the
// rule under test is how bindings and scopes combine, which holds
// whatever the roles end up being called.
var permissions = map[string][]string{
	"AGENT":          {"call.read"},
	"SUPERVISOR":     {"call.read", "call.terminate", "principal.*"},
	"TENANT_ADMIN":   {domain.Wildcard},
	"PLATFORM_ADMIN": {domain.Wildcard},
}

func TestIsAuthorized(t *testing.T) {
	tenantA := shareddomain.NewTenantID()
	tenantB := shareddomain.NewTenantID()
	principal := shareddomain.NewPrincipalID()

	binding := func(role, scope string, tenant shareddomain.TenantID) domain.RoleBinding {
		return domain.RoleBinding{PrincipalID: principal, TenantID: tenant, Role: role, Scope: scope}
	}

	tests := []struct {
		name           string
		bindings       []domain.RoleBinding
		action         string
		resourceTenant shareddomain.TenantID
		want           bool
	}{
		{
			name:           "no bindings at all is denied",
			bindings:       nil,
			action:         "call.read",
			resourceTenant: tenantA,
			want:           false,
		},
		{
			name:           "binding exists but does not cover the action",
			bindings:       []domain.RoleBinding{binding("AGENT", domain.ScopeTenant, tenantA)},
			action:         "call.terminate",
			resourceTenant: tenantA,
			want:           false,
		},
		{
			name:           "exact action match is permitted",
			bindings:       []domain.RoleBinding{binding("AGENT", domain.ScopeTenant, tenantA)},
			action:         "call.read",
			resourceTenant: tenantA,
			want:           true,
		},
		{
			name:           "unknown action is denied, not defaulted open",
			bindings:       []domain.RoleBinding{binding("AGENT", domain.ScopeTenant, tenantA)},
			action:         "some.action.invented.later",
			resourceTenant: tenantA,
			want:           false,
		},
		{
			name:           "family wildcard grants within its family",
			bindings:       []domain.RoleBinding{binding("SUPERVISOR", domain.ScopeTenant, tenantA)},
			action:         "principal.disable",
			resourceTenant: tenantA,
			want:           true,
		},
		{
			name:           "family wildcard does not leak to another family",
			bindings:       []domain.RoleBinding{binding("SUPERVISOR", domain.ScopeTenant, tenantA)},
			action:         "tenant.suspend",
			resourceTenant: tenantA,
			want:           false,
		},
		{
			name:           "tenant admin wildcard covers its own tenant",
			bindings:       []domain.RoleBinding{binding("TENANT_ADMIN", domain.ScopeTenant, tenantA)},
			action:         "tenant.suspend",
			resourceTenant: tenantA,
			want:           true,
		},
		{
			// The headline cross-tenant case: a wildcard is bounded by
			// its scope, so tenant A's administrator is refused against
			// tenant B exactly like any other principal of tenant A.
			name:           "tenant admin wildcard stops at its own tenant boundary",
			bindings:       []domain.RoleBinding{binding("TENANT_ADMIN", domain.ScopeTenant, tenantA)},
			action:         "tenant.suspend",
			resourceTenant: tenantB,
			want:           false,
		},
		{
			name:           "system scope reaches across tenants",
			bindings:       []domain.RoleBinding{binding("PLATFORM_ADMIN", domain.ScopeSystem, tenantA)},
			action:         "tenant.suspend",
			resourceTenant: tenantB,
			want:           true,
		},
		{
			name:           "a role with no permission entry grants nothing",
			bindings:       []domain.RoleBinding{binding("ROLE_WITH_NO_ENTRY", domain.ScopeTenant, tenantA)},
			action:         "call.read",
			resourceTenant: tenantA,
			want:           false,
		},
		{
			name: "one matching binding among several is enough",
			bindings: []domain.RoleBinding{
				binding("AGENT", domain.ScopeTenant, tenantB),
				binding("SUPERVISOR", domain.ScopeTenant, tenantA),
			},
			action:         "call.terminate",
			resourceTenant: tenantA,
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsAuthorized(tt.bindings, tt.action, tt.resourceTenant, permissions)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsAuthorized_EmptyPermissionTableDeniesEverything: a
// misconfiguration that loses the permission table must fail closed, not
// open. Worth its own test because "no data" is exactly the state a
// bootstrapping or partially-failed load leaves behind.
func TestIsAuthorized_EmptyPermissionTableDeniesEverything(t *testing.T) {
	tenant := shareddomain.NewTenantID()
	bindings := []domain.RoleBinding{{TenantID: tenant, Role: "TENANT_ADMIN", Scope: domain.ScopeTenant}}

	assert.False(t, domain.IsAuthorized(bindings, "call.read", tenant, nil))
	assert.False(t, domain.IsAuthorized(bindings, "call.read", tenant, map[string][]string{}))
}

// TestIsAuthorizedSystem covers the system-scope rule, which had no test
// at all until 2026-09-09 despite being the guard that keeps
// installation authority out of a customer's hands.
//
// The property it protects: creating a tenant is installation authority,
// and a tenant's own administrator must never hold it — however broad
// their role (LLD-02 §10.1; identity-api spec "Creating a tenant
// requires authority over the installation").
//
// The failure this catches is silent. Inverting the scope check leaves
// every other identity test passing while a TENANT_ADMIN gains the
// ability to provision tenants, which is privilege escalation across the
// installation's most important boundary.
func TestIsAuthorizedSystem(t *testing.T) {
	tenantA := shareddomain.NewTenantID()
	tenantB := shareddomain.NewTenantID()
	principal := shareddomain.NewPrincipalID()

	binding := func(role, scope string, tenant shareddomain.TenantID) domain.RoleBinding {
		return domain.RoleBinding{PrincipalID: principal, TenantID: tenant, Role: role, Scope: scope}
	}

	t.Run("a system-scoped binding with the permission is authorized", func(t *testing.T) {
		bindings := []domain.RoleBinding{binding("PLATFORM_ADMIN", domain.ScopeSystem, tenantA)}
		assert.True(t, domain.IsAuthorizedSystem(bindings, "tenant.create", permissions))
	})

	t.Run("a TENANT_ADMIN with a wildcard role is still refused", func(t *testing.T) {
		// The heart of it: TENANT_ADMIN grants "*", so the role check
		// passes. Only the scope stops it.
		bindings := []domain.RoleBinding{binding("TENANT_ADMIN", domain.ScopeTenant, tenantA)}
		assert.False(t, domain.IsAuthorizedSystem(bindings, "tenant.create", permissions),
			"a tenant administrator must never hold installation authority, however broad their role")
	})

	t.Run("a tenant-scoped binding is refused whichever tenant it names", func(t *testing.T) {
		for _, tenant := range []shareddomain.TenantID{tenantA, tenantB} {
			bindings := []domain.RoleBinding{binding("PLATFORM_ADMIN", domain.ScopeTenant, tenant)}
			assert.False(t, domain.IsAuthorizedSystem(bindings, "tenant.create", permissions),
				"scope decides this, not which tenant the binding names")
		}
	})

	t.Run("no bindings is refused", func(t *testing.T) {
		assert.False(t, domain.IsAuthorizedSystem(nil, "tenant.create", permissions))
		assert.False(t, domain.IsAuthorizedSystem([]domain.RoleBinding{}, "tenant.create", permissions))
	})

	t.Run("a system binding without the permission is refused", func(t *testing.T) {
		bindings := []domain.RoleBinding{binding("AGENT", domain.ScopeSystem, tenantA)}
		assert.False(t, domain.IsAuthorizedSystem(bindings, "tenant.create", permissions),
			"system scope is necessary, not sufficient — the role must still grant the action")
	})

	t.Run("an unknown action is refused even at system scope", func(t *testing.T) {
		bindings := []domain.RoleBinding{binding("AGENT", domain.ScopeSystem, tenantA)}
		assert.False(t, domain.IsAuthorizedSystem(bindings, "action.nobody.defined", permissions),
			"deny by default (LLD-02 §10.1, OWASP A01)")
	})

	t.Run("an empty permission table denies everything", func(t *testing.T) {
		bindings := []domain.RoleBinding{binding("PLATFORM_ADMIN", domain.ScopeSystem, tenantA)}
		assert.False(t, domain.IsAuthorizedSystem(bindings, "tenant.create", nil))
		assert.False(t, domain.IsAuthorizedSystem(bindings, "tenant.create", map[string][]string{}))
	})

	t.Run("a mixed set is authorized only by its system binding", func(t *testing.T) {
		mixed := []domain.RoleBinding{
			binding("TENANT_ADMIN", domain.ScopeTenant, tenantA),
			binding("PLATFORM_ADMIN", domain.ScopeSystem, tenantB),
		}
		assert.True(t, domain.IsAuthorizedSystem(mixed, "tenant.create", permissions))

		onlyTenant := []domain.RoleBinding{binding("TENANT_ADMIN", domain.ScopeTenant, tenantA)}
		assert.False(t, domain.IsAuthorizedSystem(onlyTenant, "tenant.create", permissions))
	})

	t.Run("an unrecognised scope value is refused", func(t *testing.T) {
		// Neither "tenant" nor "system": a typo or a future scope must
		// fail closed rather than fall through to an allowance.
		bindings := []domain.RoleBinding{binding("PLATFORM_ADMIN", "superuser", tenantA)}
		assert.False(t, domain.IsAuthorizedSystem(bindings, "tenant.create", permissions))
	})
}

// TestSystemAndTenantScopeAreNotInterchangeable pins the relationship
// between the two entry points, so a refactor that unified them would
// have to fail this test first. System scope satisfies both questions;
// tenant scope satisfies only the tenant one.
func TestSystemAndTenantScopeAreNotInterchangeable(t *testing.T) {
	tenant := shareddomain.NewTenantID()
	system := []domain.RoleBinding{{TenantID: tenant, Role: "PLATFORM_ADMIN", Scope: domain.ScopeSystem}}
	tenantScoped := []domain.RoleBinding{{TenantID: tenant, Role: "TENANT_ADMIN", Scope: domain.ScopeTenant}}

	assert.True(t, domain.IsAuthorized(system, "tenant.create", tenant, permissions),
		"a platform operator may also act within a tenant")
	assert.True(t, domain.IsAuthorizedSystem(system, "tenant.create", permissions))

	assert.True(t, domain.IsAuthorized(tenantScoped, "tenant.create", tenant, permissions),
		"tenant scope satisfies the tenant-scoped question")
	assert.False(t, domain.IsAuthorizedSystem(tenantScoped, "tenant.create", permissions),
		"tenant scope must never satisfy the installation-authority question")
}
