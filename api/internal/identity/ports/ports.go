// Package ports defines the identity context's hexagonal port
// interfaces — the inbound IdentityService fixed in
// docs/hld/04-bounded-contexts.md §7, plus the outbound storage ports
// its implementation needs. This package has no implementation:
// application implements the inbound port, postgres implements the
// outbound ones, and neither domain nor application depends on a
// concrete adapter (docs/hld/01-architecture.md §1.2).
package ports

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/identity/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// Errors every implementation of these ports reports in common, so
// callers can branch on cause without matching message text.
//
// A note on how these reach a client: authentication failures are all
// surfaced to the caller as the same unauthenticated result. Telling
// someone whether a username exists, or whether their tenant is
// suspended rather than their password wrong, is reconnaissance
// (INV-10). The distinction exists for logs and tests, not for
// responses.
var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPrincipalNotActive = errors.New("principal is not active")
	ErrTenantNotActive    = errors.New("tenant is not active")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrUsernameTaken      = errors.New("username already exists in this tenant")
	ErrResidencyImmutable = errors.New("residency zone cannot be changed once set")
)

// AuthToken is what a successful authentication yields.
type AuthToken struct {
	// Token is the signed bearer token. It is credential material:
	// never log it, never audit it, never persist it.
	Token       string
	ExpiresAt   time.Time
	TenantID    shareddomain.TenantID
	PrincipalID shareddomain.PrincipalID
}

// TenantContext is the authenticated identity carried downstream: into
// RLS (SET LOCAL app.tenant_id), into authorization, and into audit.
//
// It is produced only by ValidateToken or API-key validation. Callers
// never construct one — a hand-built TenantContext would be an
// authorization decision made in the wrong place.
type TenantContext struct {
	TenantID      shareddomain.TenantID
	PrincipalID   shareddomain.PrincipalID
	Roles         []string
	ResidencyZone string
	// Scopes are the distinct binding scopes the principal holds
	// (domain.ScopeTenant, domain.ScopeSystem, or narrower). They are
	// re-derived from the database on every ValidateToken, so they are
	// never stale for longer than the request that loaded them. The
	// interceptor consults them to tell a platform operator from a
	// tenant's own administrator; handlers consult them (and the fresh
	// binding check in AuthorizeAction) before every mutation.
	Scopes []string
}

// HasScope reports whether the context carries scope s.
func (c *TenantContext) HasScope(s string) bool {
	for _, scope := range c.Scopes {
		if scope == s {
			return true
		}
	}
	return false
}

// IdentityService is the identity context's inbound port — exactly the
// shape in docs/hld/04-bounded-contexts.md §7.
type IdentityService interface {
	// AuthenticateUser verifies a username and password within a tenant
	// and issues a token. Every failure mode returns
	// ErrInvalidCredentials to the caller.
	AuthenticateUser(ctx context.Context, tenantID shareddomain.TenantID, username, password string) (*AuthToken, error)

	// ValidateToken checks a token's signature and expiry AND the live
	// status of its principal and tenant, so disabling an account takes
	// effect on the next request rather than at token expiry
	// (LLD-02 §10.2).
	ValidateToken(ctx context.Context, tokenString string) (*TenantContext, error)

	// AuthorizeAction reports whether a principal may perform action on
	// a resource. It returns ErrPermissionDenied unless a role binding
	// matches — absence of a binding is denial (LLD-02 §10.1).
	AuthorizeAction(ctx context.Context, principal shareddomain.PrincipalID, action, resource string) error

	// RecordAudit writes one immutable audit record.
	RecordAudit(ctx context.Context, entry domain.AuditEntry) error
}

// TenantStore persists tenants. Tenants are the root of tenancy and have
// no tenant_id of their own, so this port is not tenant-scoped — the
// only identity store that isn't.
type TenantStore interface {
	CreateTenant(ctx context.Context, tenant domain.Tenant) error
	GetTenant(ctx context.Context, id shareddomain.TenantID) (domain.Tenant, error)
	SetTenantStatus(ctx context.Context, id shareddomain.TenantID, status domain.TenantStatus) error
}

// PrincipalStore persists principals within a tenant.
type PrincipalStore interface {
	CreatePrincipal(ctx context.Context, principal domain.Principal) error
	GetPrincipal(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID) (domain.Principal, error)
	// GetPrincipalByUsername resolves a login. It returns ErrNotFound
	// for an unknown username; the caller converts that into the same
	// ErrInvalidCredentials a wrong password produces.
	GetPrincipalByUsername(ctx context.Context, tenantID shareddomain.TenantID, username string) (domain.Principal, error)
	SetPrincipalStatus(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID, status domain.PrincipalStatus) error
}

// RoleBindingStore persists role grants.
type RoleBindingStore interface {
	CreateRoleBinding(ctx context.Context, binding domain.RoleBinding) error
	ListRoleBindings(ctx context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID) ([]domain.RoleBinding, error)
	DeleteRoleBinding(ctx context.Context, binding domain.RoleBinding) error
}

// ApiKeyStore persists API key records — digests only, never key
// material.
type ApiKeyStore interface {
	CreateApiKey(ctx context.Context, key domain.ApiKey) error
	// GetApiKeyByHash resolves a presented key's digest within a tenant.
	//
	// The tenant is a parameter because api_keys is RLS-protected: a
	// lookup by digest alone returns nothing with no tenant context set.
	// Callers read the tenant from the presented key itself
	// (application.TenantFromAPIKey), which keeps this tenant-scoped
	// like every other read rather than needing a privileged role that
	// can see every tenant's credentials. Naming a tenant authenticates
	// nothing — the digest comparison does that.
	GetApiKeyByHash(ctx context.Context, tenantID shareddomain.TenantID, keyHash string) (domain.ApiKey, error)
	RevokeApiKey(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ApiKeyID, revokedAt time.Time) error
}

// AuditStore appends audit records.
//
// Deliberately append-and-read only: there is no Update and no Delete,
// so no caller — and no future caller — has a path to alter history
// (AC-01.3/01.4, LLD-02 §10.3). Immutability enforced by the absence of
// a method is stronger than immutability enforced by a check.
type AuditStore interface {
	AppendAudit(ctx context.Context, entry domain.AuditEntry) error

	// AppendAuditTx writes one record inside a transaction the caller
	// already owns, so a mutation and the record of it commit together.
	//
	// This exists so that other bounded contexts do not have to write
	// audit_logs themselves to get atomicity. The table belongs to
	// identity; a cross-context table write would put two contexts in
	// charge of one schema (HLD 04 §10.1). The caller must have scoped tx
	// to entry.TenantID already — this method deliberately does not set
	// the tenant context inside someone else's transaction.
	AppendAuditTx(ctx context.Context, tx pgx.Tx, entry domain.AuditEntry) error

	ListAudit(ctx context.Context, tenantID shareddomain.TenantID, limit int) ([]domain.AuditEntry, error)
}

// ClientAddress extracts the caller's IP for audit records. Returns nil
// when the address is unknown (a system-initiated mutation, say), which
// audit records as a null ip_address rather than a fabricated one.
type ClientAddress func(ctx context.Context) net.IP
