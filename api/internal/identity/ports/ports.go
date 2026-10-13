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
	// ErrAlreadyMember refuses a second membership of one tenant for one
	// person (identity/user-accounts).
	ErrAlreadyMember = errors.New("already a member of this tenant")
	// ErrEmailTaken is a second account for one normalized email. The
	// application resolves it to the existing person; it never reaches a
	// client, since that would reveal the account exists (D-61).
	ErrEmailTaken = errors.New("an account already exists for this email")
	// ErrUserNotActive is a disabled person (platform scope only).
	ErrUserNotActive = errors.New("user is not active")
	// ErrThrottled refuses an attempt while its key is blocked (D-64).
	// Callers read the delay from a *ThrottledError.
	ErrThrottled = errors.New("too many attempts")
	// ErrDomainClaimed refuses a second provider's claim on a domain.
	ErrDomainClaimed = errors.New("domain already claimed by another identity provider")
	// ErrAlreadyLinked refuses linking a (provider, subject) twice.
	ErrAlreadyLinked = errors.New("external identity already linked")
	// ErrPasswordChangeRequired is a must-change credential being used
	// for anything but changing it.
	ErrPasswordChangeRequired = errors.New("password change required")
	// ErrSessionNotUsable is a refresh secret that cannot be used:
	// unknown, expired, revoked, or replayed. Callers never learn which.
	ErrSessionNotUsable   = errors.New("session cannot be refreshed")
	ErrResidencyImmutable = errors.New("residency zone cannot be changed once set")

	// ErrLastAdministrator refuses revoking a tenant's only remaining
	// tenant-administrator binding: no tenant may be left without anyone
	// able to administer it (console-apis-phase-a, identity/public-api).
	ErrLastAdministrator = errors.New("cannot revoke the last tenant administrator")

	// ErrEntitlementExceeded is the installation lacking the licence for
	// an operation, as distinct from the caller lacking the permission.
	// It is never folded into ErrPermissionDenied: a licence refusal in
	// the RBAC audit trail would send an access-control investigation
	// after events that have nothing to do with access control, and tell
	// an administrator to check their roles when they need to check their
	// licence (LLD-02 §4a.1, AC-06.14).
	ErrEntitlementExceeded = errors.New("licence entitlement exceeded")

	// ErrNotLicensed is an installation with no licence applied: no
	// tenant is provisioned through the API before activation (D-72).
	// The tenant-cap port reports it; the operator's bootstrap does not
	// consult that port.
	ErrNotLicensed = errors.New("no licence is applied")
)

// ThrottledError carries how long a throttled caller must wait.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string { return ErrThrottled.Error() }

// Unwrap lets errors.Is(err, ErrThrottled) match.
func (e *ThrottledError) Unwrap() error { return ErrThrottled }

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

	// UserID is the person behind the membership; SessionID the console
	// session the token was issued under (zero for API keys and the
	// deprecated AuthenticateUser). No other context reads either: they
	// exist so identity can check the session on every request (D-62).
	UserID    domain.UserID
	SessionID domain.SessionID
	// PasswordChangeOnly marks a token issued for a must-change
	// credential: the interceptor admits it to ChangePassword and SignOut
	// and nothing else (D-64).
	PasswordChangeOnly bool
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

// TenantCap supplies the installation's tenant cap (LLD-02 §4a.1, D-51).
//
// The cap is published by licensing, which identity may not call: both
// are Tier-0 and depend on nothing (HLD 04 §10.1). So identity owns this
// port and the composition root satisfies it — the cap is handed in,
// never fetched. It is a number and nothing more; identity has no reason
// to know what an edition is.
//
// Zero means unlimited. An error fails provisioning closed: an
// installation that cannot read its licence does not get to guess.
type TenantCap interface {
	MaxTenants(ctx context.Context) (int, error)
}

// TenantCapFunc adapts a function to TenantCap.
type TenantCapFunc func(ctx context.Context) (int, error)

// MaxTenants calls f.
func (f TenantCapFunc) MaxTenants(ctx context.Context) (int, error) { return f(ctx) }

// TenantStore persists tenants. Tenants are the root of tenancy and have
// no tenant_id of their own, so this port is not tenant-scoped — the
// only identity store that isn't.
type TenantStore interface {
	// CreateTenant inserts tenant unless maxTenants (when non-zero)
	// tenants already exist, in which case it returns
	// ErrEntitlementExceeded and inserts nothing. The count and the
	// insert are one atomic step, so two concurrent provisionings cannot
	// both take the last slot.
	CreateTenant(ctx context.Context, tenant domain.Tenant, maxTenants int) error
	GetTenant(ctx context.Context, id shareddomain.TenantID) (domain.Tenant, error)
	SetTenantStatus(ctx context.Context, id shareddomain.TenantID, status domain.TenantStatus) error
}

// PrincipalStore persists memberships within a tenant. Email and
// DisplayName on a returned Principal are the member's user's, read
// through a join; they are never written here.
type PrincipalStore interface {
	// CreatePrincipal inserts a membership. A second membership of the
	// same tenant for the same user is ErrAlreadyMember.
	CreatePrincipal(ctx context.Context, principal domain.Principal) error
	GetPrincipal(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID) (domain.Principal, error)
	// GetPrincipalByUser resolves a person's membership of one tenant.
	GetPrincipalByUser(ctx context.Context, tenantID shareddomain.TenantID, userID domain.UserID) (domain.Principal, error)
	SetPrincipalStatus(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID, status domain.PrincipalStatus) error
	// ListPrincipals returns one page of the tenant's members by email.
	ListPrincipals(ctx context.Context, tenantID shareddomain.TenantID, limit, offset int) ([]domain.Principal, error)
}

// UserStore persists people (D-61). Users are installation-wide: these
// reads run before any tenant is known.
type UserStore interface {
	// CreateUser inserts a person and, when u.Password is set, their
	// password credential, atomically. A taken email is ErrEmailTaken.
	CreateUser(ctx context.Context, u domain.User) error
	GetUser(ctx context.Context, id domain.UserID) (domain.User, error)
	// GetUserByEmail takes a normalized email; ErrNotFound when absent.
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	// SetPassword replaces the person's password credential.
	SetPassword(ctx context.Context, id domain.UserID, cred domain.PasswordCredential) error
	SetUserStatus(ctx context.Context, id domain.UserID, status domain.UserStatus) error
	// ListMemberships returns every membership of a person, in every
	// tenant, with that membership's roles. It reads under the person's
	// own identity (a self-only RLS policy), never tenant-blind.
	ListMemberships(ctx context.Context, id domain.UserID) ([]domain.Membership, error)
}

// SessionStore persists console sessions (D-62).
type SessionStore interface {
	CreateSession(ctx context.Context, s domain.Session) error
	GetSession(ctx context.Context, id domain.SessionID) (domain.Session, error)
	// UpdateSessionByDigest locks the session holding digest as its
	// current or previous refresh digest, passes it to fn, and stores
	// what fn returns — so two concurrent refreshes cannot both rotate.
	// ErrNotFound when no session holds the digest. When fn returns an
	// error alongside a session, the session is still stored (a replay
	// is refused *and* the revocation persists).
	UpdateSessionByDigest(ctx context.Context, digest string, fn func(domain.Session) (domain.Session, error)) (domain.Session, error)
	// RevokeUserSessions ends every live session of a person except
	// keep (zero = none), returning how many it ended.
	RevokeUserSessions(ctx context.Context, user domain.UserID, keep domain.SessionID, at time.Time, reason string) (int, error)
	RevokeSession(ctx context.Context, id domain.SessionID, at time.Time, reason string) error
}

// ResetTicket is a stored reset or invitation secret: its digest only.
type ResetTicket struct {
	UserID    domain.UserID
	Digest    string
	Purpose   domain.TicketPurpose
	ExpiresAt time.Time
	CreatedAt time.Time
}

// ResetTicketStore persists emailed secrets (D-64).
type ResetTicketStore interface {
	CreateTicket(ctx context.Context, t ResetTicket) error
	// CountTickets counts a person's tickets of a purpose created since.
	CountTickets(ctx context.Context, user domain.UserID, purpose domain.TicketPurpose, since time.Time) (int, error)
	// RedeemTicket atomically marks an unexpired, unused ticket used, sets
	// the person's password to cred, and revokes every session of theirs.
	// ErrNotFound for any ticket that cannot be redeemed.
	RedeemTicket(ctx context.Context, digest string, cred domain.PasswordCredential, at time.Time) (domain.UserID, domain.TicketPurpose, error)
}

// IdentityProviderStore persists the SSO-ready provider model (D-63).
type IdentityProviderStore interface {
	CreateProvider(ctx context.Context, p domain.IdentityProvider) error
	// ClaimDomain records a provider's claim; ErrDomainClaimed if another
	// provider holds the domain.
	ClaimDomain(ctx context.Context, claim domain.DomainClaim) error
	VerifyDomain(ctx context.Context, domainName string, at time.Time) error
	// ProviderForDomain returns the domain's verified claim and its
	// provider, or ErrNotFound when no verified claim exists.
	ProviderForDomain(ctx context.Context, domainName string) (domain.DomainClaim, domain.IdentityProvider, error)
	// LinkExternalIdentity links (provider, subject) to a user;
	// ErrAlreadyLinked when that pair is linked already.
	LinkExternalIdentity(ctx context.Context, e domain.ExternalIdentity) error
	UserForExternalIdentity(ctx context.Context, provider domain.IdentityProviderID, subject string) (domain.UserID, error)
}

// AuthEventStore appends authentication history. Append and read only:
// no update or delete path exists, by construction (D-64).
type AuthEventStore interface {
	AppendAuthEvent(ctx context.Context, e domain.AuthEvent) error
	ListAuthEvents(ctx context.Context, user domain.UserID, limit int) ([]domain.AuthEvent, error)
}

// LoginThrottle is the throttle's state (D-64). Keys are digests; the
// policy that turns failures into a delay is the domain's.
type LoginThrottle interface {
	// BlockedFor returns the longest remaining block across keys at now.
	BlockedFor(ctx context.Context, keys []string, now time.Time) (time.Duration, error)
	// RecordFailure counts one failure on each key and blocks it for
	// policy.Delay(failures) from now.
	RecordFailure(ctx context.Context, keys []string, now time.Time, policy domain.ThrottlePolicy) error
	// Clear resets a key, after a success.
	Clear(ctx context.Context, key string) error
}

// LiveState is everything ValidateToken re-checks on every request,
// read in one transaction (design D4).
type LiveState struct {
	Principal  domain.Principal
	Tenant     domain.Tenant
	UserStatus domain.UserStatus
	MustChange bool
	Bindings   []domain.RoleBinding
	// Session is nil when no session was asked for.
	Session *domain.Session
}

// LivenessReader reads a LiveState. ErrNotFound when the principal or
// the session does not exist.
type LivenessReader interface {
	LiveState(ctx context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, session domain.SessionID) (LiveState, error)
}

// Mailer sends one plain-text message. Identity composes every message;
// the adapter only delivers it (design D10). The body may hold a link
// secret, so an adapter must never log it.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// FederatedAssertion is what an identity provider asserts about a
// sign-in. Email is informational only: linking is by Issuer+Subject.
type FederatedAssertion struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
}

// FederatedAuthenticator is the SSO seam (D-63). Declared now, with no
// adapter: OIDC is its first implementation, in identity-oidc.
type FederatedAuthenticator interface {
	// Begin returns where to send the browser, and the state that must
	// come back.
	Begin(ctx context.Context, provider domain.IdentityProvider, returnTo string) (redirectURL, state string, err error)
	// Complete verifies the provider's callback and returns its assertion.
	Complete(ctx context.Context, provider domain.IdentityProvider, state string, callback map[string]string) (FederatedAssertion, error)
}

// RoleBindingStore persists role grants.
type RoleBindingStore interface {
	CreateRoleBinding(ctx context.Context, binding domain.RoleBinding) error
	ListRoleBindings(ctx context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID) ([]domain.RoleBinding, error)
	DeleteRoleBinding(ctx context.Context, binding domain.RoleBinding) error
	// ListRoleBindingsFor returns the bindings of several principals in
	// one query, keyed by principal.
	ListRoleBindingsFor(ctx context.Context, tenantID shareddomain.TenantID, principalIDs []shareddomain.PrincipalID) (map[shareddomain.PrincipalID][]domain.RoleBinding, error)
	// RevokeRoleBinding deletes one binding. It reports ErrNotFound when
	// no such binding exists, and ErrLastAdministrator when it is the
	// tenant's only tenant-scope TENANT_ADMIN binding — decided under a
	// per-tenant lock, so two concurrent revocations cannot strip both of
	// the last two administrators.
	RevokeRoleBinding(ctx context.Context, binding domain.RoleBinding) error
}

// APIKeyStore persists API key records — digests only, never key
// material.
type APIKeyStore interface {
	CreateAPIKey(ctx context.Context, key domain.APIKey) error
	// GetAPIKeyByHash resolves a presented key's digest within a tenant.
	//
	// The tenant is a parameter because api_keys is RLS-protected: a
	// lookup by digest alone returns nothing with no tenant context set.
	// Callers read the tenant from the presented key itself
	// (application.TenantFromAPIKey), which keeps this tenant-scoped
	// like every other read rather than needing a privileged role that
	// can see every tenant's credentials. Naming a tenant authenticates
	// nothing — the digest comparison does that.
	GetAPIKeyByHash(ctx context.Context, tenantID shareddomain.TenantID, keyHash string) (domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.APIKeyID, revokedAt time.Time) error
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
