package domain

import (
	"strings"

	shareddomain "atsap-api/internal/shared/domain"
)

// ScopeTenant is tenant-wide authority: every resource belonging to the
// binding's own tenant, and nothing outside it.
const ScopeTenant = "tenant"

// ScopeSystem is platform-operator authority, held by no tenant's own
// administrators. It is a separate scope rather than a special tenant so
// that "can this principal act across tenants" is answerable without
// inspecting which tenant they belong to.
const ScopeSystem = "system"

// Wildcard matches any action within a binding's scope. It is
// deliberately not usable as a scope value: an unscoped
// match-everything grant is precisely what LLD-02 §10.1 rules out.
const Wildcard = "*"

// IsAuthorized reports whether bindings permit principal to perform
// action on a resource owned by resourceTenant.
//
// Deny-by-default: this returns true only when some binding matches. No
// binding, no permission — an unknown action is refused rather than
// falling through to an allowance (LLD-02 §10.1, OWASP A01).
//
// Pure: bindings are passed in, never loaded here, so the rule can be
// tested exhaustively without a database and applied identically
// wherever a caller has already loaded them.
func IsAuthorized(bindings []RoleBinding, action string, resourceTenant shareddomain.TenantID, permissions map[string][]string) bool {
	for _, b := range bindings {
		if !bindingCoversTenant(b, resourceTenant) {
			continue
		}
		if roleGrants(permissions, b.Role, action) {
			return true
		}
	}
	return false
}

// IsAuthorizedSystem reports whether bindings permit action through a
// platform (system-scope) binding. A tenant-scoped binding never
// qualifies, no matter how broad its role: creating a tenant is
// installation authority, and a customer's own administrator must not
// hold it (LLD-02 §10.1; identity-api spec "Creating a tenant requires
// authority over the installation").
func IsAuthorizedSystem(bindings []RoleBinding, action string, permissions map[string][]string) bool {
	for _, b := range bindings {
		if b.Scope != ScopeSystem {
			continue
		}
		if roleGrants(permissions, b.Role, action) {
			return true
		}
	}
	return false
}

// bindingCoversTenant reports whether a binding reaches the tenant that
// owns the resource. A tenant-scoped binding reaches only its own
// tenant — a tenant administrator is still confined to their tenant,
// which is the whole point of the scope existing.
func bindingCoversTenant(b RoleBinding, resourceTenant shareddomain.TenantID) bool {
	if b.Scope == ScopeSystem {
		return true
	}
	return b.TenantID == resourceTenant
}

// roleGrants reports whether role permits action under the supplied
// permission table. A role's entry may list the wildcard, which grants
// every action *within the binding's scope* — never across scopes,
// since scope is checked separately and first.
func roleGrants(permissions map[string][]string, role, action string) bool {
	for _, granted := range permissions[role] {
		if granted == Wildcard || granted == action {
			return true
		}
		// A trailing-wildcard prefix such as "principal.*" grants every
		// action in that family without granting unrelated families.
		if strings.HasSuffix(granted, ".*") &&
			strings.HasPrefix(action, strings.TrimSuffix(granted, "*")) {
			return true
		}
	}
	return false
}
