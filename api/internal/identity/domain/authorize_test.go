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
