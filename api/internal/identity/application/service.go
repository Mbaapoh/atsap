package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// DefaultPermissions is the role-to-action table this change ships with.
// The vocabulary is deliberately provisional (design.md Open Questions):
// pbx-core is what will give roles most of their actions. What is fixed
// here is the *rule* — a role grants only what is listed, and anything
// unlisted is denied.
// IdentityActions are the actions the identity API authorizes, named so
// they appear in DefaultPermissions verbatim (not only via wildcard) and
// so a table-driven permission test can iterate the whole set. Each is
// what the audit trail records for the mutation (service.go record
// calls), keeping authorization and audit on one vocabulary.
var IdentityActions = []string{
	"tenant.create",
	"tenant.status.set",
	"principal.create",
	"principal.status.set",
	"principal.role.grant",
	"principal.role.revoke",
	"principal.password.reset",
	"user.status.set",
	"apikey.issue",
	"apikey.revoke",
	"audit.read",
}

var DefaultPermissions = map[string][]string{
	"AGENT":          {"call.read"},
	"SUPERVISOR":     {"call.read", "call.terminate", "principal.read"},
	"TENANT_ADMIN":   append([]string{domain.Wildcard}, IdentityActions...),
	"PLATFORM_ADMIN": append([]string{domain.Wildcard}, IdentityActions...),
}

// Stores bundles every storage port the service orchestrates.
type Stores struct {
	Tenants    ports.TenantStore
	Principals ports.PrincipalStore
	Bindings   ports.RoleBindingStore
	ApiKeys    ports.ApiKeyStore
	Audit      ports.AuditStore
	Users      ports.UserStore
	Sessions   ports.SessionStore
	Tickets    ports.ResetTicketStore
	Providers  ports.IdentityProviderStore
	Events     ports.AuthEventStore
	Throttle   ports.LoginThrottle
	Liveness   ports.LivenessReader
}

// AllStores is one adapter implementing every storage port, as the
// Postgres store and the in-memory test store both do.
type AllStores interface {
	ports.TenantStore
	ports.PrincipalStore
	ports.RoleBindingStore
	ports.ApiKeyStore
	ports.AuditStore
	ports.UserStore
	ports.SessionStore
	ports.ResetTicketStore
	ports.IdentityProviderStore
	ports.AuthEventStore
	ports.LoginThrottle
	ports.LivenessReader
}

// StoresOf fills every port from one adapter.
func StoresOf(a AllStores) Stores {
	return Stores{Tenants: a, Principals: a, Bindings: a, ApiKeys: a, Audit: a, Users: a, Sessions: a,
		Tickets: a, Providers: a, Events: a, Throttle: a, Liveness: a}
}

// Service implements ports.IdentityService, and the console account use
// cases of revision 2 (accounts.go).
type Service struct {
	tenants     ports.TenantStore
	tenantCap   ports.TenantCap
	principals  ports.PrincipalStore
	bindings    ports.RoleBindingStore
	apiKeys     ports.ApiKeyStore
	audit       ports.AuditStore
	users       ports.UserStore
	sessions    ports.SessionStore
	tickets     ports.ResetTicketStore
	providers   ports.IdentityProviderStore
	events      ports.AuthEventStore
	throttle    ports.LoginThrottle
	liveness    ports.LivenessReader
	hasher      *PasswordHasher
	tokens      *TokenIssuer
	permissions map[string][]string
	logger      *slog.Logger
	now         func() time.Time

	mailer     ports.Mailer
	consoleURL string
	policy     domain.ThrottlePolicy
	// dummyHash is verified against when there is no account or no
	// password, so an unknown email costs the same Argon2id work as a
	// wrong password (LLD-02 §10.10).
	dummyHash string
}

var _ ports.IdentityService = (*Service)(nil)

// NewService wires the identity orchestrator.
func NewService(stores Stores, tenantCap ports.TenantCap, hasher *PasswordHasher, tokens *TokenIssuer, logger *slog.Logger) *Service {
	dummy, err := hasher.Hash("not-a-real-password:timing-equalizer")
	if err != nil {
		// Hashing a constant cannot fail short of a broken RNG; without a
		// dummy hash the timing guarantee is gone, so fail loudly.
		panic(fmt.Sprintf("identity: cannot compute dummy hash: %v", err))
	}
	return &Service{
		tenants: stores.Tenants, tenantCap: tenantCap, principals: stores.Principals, bindings: stores.Bindings,
		apiKeys: stores.ApiKeys, audit: stores.Audit, users: stores.Users, sessions: stores.Sessions,
		tickets: stores.Tickets, providers: stores.Providers, events: stores.Events, throttle: stores.Throttle,
		liveness: stores.Liveness, hasher: hasher, tokens: tokens,
		permissions: DefaultPermissions, logger: logger, now: time.Now,
		mailer: logMailer{logger: logger}, policy: domain.DefaultThrottlePolicy, dummyHash: dummy,
	}
}

// WithMailer sets how reset and invitation mail is sent, and the console
// base URL its links point at. Without it, a logging mailer records that
// a mail would have been sent — never the link (design D10).
func (s *Service) WithMailer(m ports.Mailer, consoleURL string) *Service {
	clone := *s
	if m != nil {
		clone.mailer = m
	}
	clone.consoleURL = strings.TrimRight(consoleURL, "/")
	return &clone
}

// WithThrottlePolicy replaces D-64's default policy, for tests.
func (s *Service) WithThrottlePolicy(p domain.ThrottlePolicy) *Service {
	clone := *s
	clone.policy = p
	return &clone
}

// WithClock replaces the service's time source, for deterministic tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	clone := *s
	clone.now = now
	clone.tokens = s.tokens.WithClock(now)
	return &clone
}

// --- provisioning --------------------------------------------------

// ProvisionTenant creates a tenant and records the mutation.
//
// It refuses with ports.ErrEntitlementExceeded once the installation
// holds as many tenants as its licence allows (LLD-02 §4a.1). The cap
// constrains provisioning only: reads are never gated, nothing existing
// is removed or hidden when the cap falls below the count, and RLS is
// unaffected in every mode (D-51).
func (s *Service) ProvisionTenant(ctx context.Context, actor Actor, name, residencyZone string) (domain.Tenant, error) {
	if residencyZone == "" {
		return domain.Tenant{}, errors.New("residency zone is required and cannot be blank")
	}
	maxTenants, err := s.tenantCap.MaxTenants(ctx)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("read tenant cap: %w", err)
	}

	// CreatedAt is set here and inserted explicitly rather than left to
	// the column default: the value returned to the caller must be the
	// value stored. Relying on DEFAULT NOW() meant the response carried
	// the zero time (0001-01-01), which a client would parse as a real
	// date.
	tenant := domain.Tenant{
		ID: shareddomain.NewTenantID(), Name: name,
		Status: domain.TenantActive, ResidencyZone: residencyZone,
		CreatedAt: s.now().UTC(),
	}
	if err := s.tenants.CreateTenant(ctx, tenant, maxTenants); err != nil {
		return domain.Tenant{}, fmt.Errorf("provision tenant: %w", err)
	}

	s.record(ctx, actor, tenant.ID, "tenant.create", "tenant", tenant.ID.String(), nil, map[string]any{
		"name": tenant.Name, "status": string(tenant.Status), "residency_zone": tenant.ResidencyZone,
	})
	return tenant, nil
}

// ProvisionPrincipal adds a person to a tenant by email (D-61).
//
// A new email creates the person — with password if one is given,
// otherwise without one and with an emailed invitation. An existing email
// creates only the membership; a supplied password is NOT applied, since
// the person keeps their own. Both cases return the same response apart
// from identifiers, so the administrator never learns that the person
// already had an account, let alone where (identity/user-accounts). The
// password, when given, is validated in both cases for the same reason.
func (s *Service) ProvisionPrincipal(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, email, displayName, password, role string) (domain.Principal, error) {
	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		return domain.Principal{}, err
	}
	if password != "" {
		if err := domain.ValidatePassword(password); err != nil {
			return domain.Principal{}, err
		}
	}
	now := s.now().UTC()

	user, created, err := s.findOrCreateUser(ctx, normalized, displayName, password, now)
	if err != nil {
		return domain.Principal{}, err
	}

	principal := domain.Principal{
		ID: shareddomain.NewPrincipalID(), TenantID: tenantID, UserID: user.ID,
		Email: normalized, DisplayName: displayName,
		Role: role, Status: domain.PrincipalActive,
		CreatedAt: now,
	}
	if err := s.principals.CreatePrincipal(ctx, principal); err != nil {
		return domain.Principal{}, err
	}

	// Create the binding the role names, so `role` is authority rather
	// than a label. Without this a principal provisioned as
	// "TENANT_ADMIN" holds no bindings and is denied everything, which
	// is a trap for anyone reading the field and expecting it to mean
	// something. Authorization still consults bindings only — this makes
	// the two agree instead of making the column authoritative.
	if role != "" {
		binding := domain.RoleBinding{
			PrincipalID: principal.ID, TenantID: tenantID,
			Role: role, Scope: domain.ScopeTenant, CreatedAt: principal.CreatedAt,
		}
		if err := s.bindings.CreateRoleBinding(ctx, binding); err != nil {
			return domain.Principal{}, fmt.Errorf("bind provisioned role: %w", err)
		}
	}

	// The audit record names the member and its role. It carries no
	// password and no hash — NewAuditEntry would strip them anyway, but
	// they are not passed in the first place — and nothing about whether
	// the person already existed.
	s.record(ctx, actor, tenantID, "principal.create", "principal", principal.ID.String(), nil, map[string]any{
		"email": normalized, "role": role, "status": string(principal.Status),
	})

	if created && password == "" {
		s.sendTicket(ctx, user, domain.TicketInvite)
	}
	return principal, nil
}

// findOrCreateUser returns the person for a normalized email, creating
// them when absent. created reports whether this call created them.
func (s *Service) findOrCreateUser(ctx context.Context, email, displayName, password string, now time.Time) (domain.User, bool, error) {
	if u, err := s.users.GetUserByEmail(ctx, email); err == nil {
		return u, false, nil
	} else if !errors.Is(err, ports.ErrNotFound) {
		return domain.User{}, false, err
	}
	user := domain.User{ID: domain.NewUserID(), Email: email, DisplayName: displayName, Status: domain.UserActive, CreatedAt: now}
	if password != "" {
		hash, err := s.hasher.Hash(password)
		if err != nil {
			return domain.User{}, false, fmt.Errorf("hash password: %w", err)
		}
		user.Password = &domain.PasswordCredential{Hash: hash, ChangedAt: now}
	}
	err := s.users.CreateUser(ctx, user)
	if errors.Is(err, ports.ErrEmailTaken) {
		// Another request created the person first: they exist, so this is
		// a membership of an existing person.
		u, gerr := s.users.GetUserByEmail(ctx, email)
		return u, false, gerr
	}
	if err != nil {
		return domain.User{}, false, err
	}
	return user, true, nil
}

// ListedPrincipal is one row of a user listing: the account, without its
// password hash, and every role binding it holds.
type ListedPrincipal struct {
	Principal domain.Principal
	Bindings  []domain.RoleBinding
}

// ListPrincipals returns one page of a tenant's users with their role
// bindings (console-apis-phase-a design D4). Two queries for the page,
// never one per user.
func (s *Service) ListPrincipals(ctx context.Context, tenantID shareddomain.TenantID, limit, offset int) ([]ListedPrincipal, error) {
	principals, err := s.principals.ListPrincipals(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	ids := make([]shareddomain.PrincipalID, len(principals))
	for i, p := range principals {
		ids[i] = p.ID
	}
	bindings, err := s.bindings.ListRoleBindingsFor(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ListedPrincipal, len(principals))
	for i, p := range principals {
		out[i] = ListedPrincipal{Principal: p, Bindings: bindings[p.ID]}
	}
	return out, nil
}

// RevokeRole removes one role binding and records the revocation. The
// last tenant-administrator binding of a tenant is refused
// (ports.ErrLastAdministrator), decided under the store's per-tenant
// lock. Authorization is the caller's — the RPC applies GrantRole's rules.
func (s *Service) RevokeRole(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, role, scope string) error {
	if scope == "" {
		scope = domain.ScopeTenant
	}
	b := domain.RoleBinding{PrincipalID: principalID, TenantID: tenantID, Role: role, Scope: scope}
	if err := s.bindings.RevokeRoleBinding(ctx, b); err != nil {
		return err
	}
	s.record(ctx, actor, tenantID, "principal.role.revoke", "principal", principalID.String(),
		map[string]any{"role": role, "scope": scope}, nil)
	return nil
}

// SetTenantStatus suspends or reactivates a tenant.
func (s *Service) SetTenantStatus(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, status domain.TenantStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid tenant status %q", status)
	}
	before, err := s.tenants.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if err := s.tenants.SetTenantStatus(ctx, tenantID, status); err != nil {
		return err
	}

	s.record(ctx, actor, tenantID, "tenant.status.set", "tenant", tenantID.String(),
		map[string]any{"status": string(before.Status)},
		map[string]any{"status": string(status)})
	return nil
}

// SetPrincipalStatus disables or reactivates a principal.
func (s *Service) SetPrincipalStatus(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, id shareddomain.PrincipalID, status domain.PrincipalStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid principal status %q", status)
	}
	before, err := s.principals.GetPrincipal(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.principals.SetPrincipalStatus(ctx, tenantID, id, status); err != nil {
		return err
	}

	s.record(ctx, actor, tenantID, "principal.status.set", "principal", id.String(),
		map[string]any{"status": string(before.Status)},
		map[string]any{"status": string(status)})
	return nil
}

// RevokeAPIKey makes a key unusable immediately and records the
// revocation. The key's digest stays in place (history), but the record
// is no longer usable for authentication.
func (s *Service) RevokeAPIKey(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, keyID shareddomain.ApiKeyID) error {
	now := s.now().UTC()
	if err := s.apiKeys.RevokeApiKey(ctx, tenantID, keyID, now); err != nil {
		return err
	}
	s.record(ctx, actor, tenantID, "apikey.revoke", "api_key", keyID.String(), nil,
		map[string]any{"revoked_at": now.UTC().Format(time.RFC3339)})
	return nil
}

// ListAudit returns the tenant's most recent audit records, newest
// first. limit is applied by the store; a non-positive limit means the
// store's default bound.
func (s *Service) ListAudit(ctx context.Context, tenantID shareddomain.TenantID, limit int) ([]domain.AuditEntry, error) {
	return s.audit.ListAudit(ctx, tenantID, limit)
}

// GrantRole binds a role to a principal.
func (s *Service) GrantRole(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, role, scope string) error {
	if scope == "" {
		scope = domain.ScopeTenant
	}
	binding := domain.RoleBinding{
		PrincipalID: principalID, TenantID: tenantID, Role: role, Scope: scope,
	}
	if err := s.bindings.CreateRoleBinding(ctx, binding); err != nil {
		return err
	}

	s.record(ctx, actor, tenantID, "principal.role.grant", "principal", principalID.String(), nil,
		map[string]any{"role": role, "scope": scope})
	return nil
}

// IssueAPIKey creates a machine credential. The raw key is returned
// once, here, and never again — it is not stored and cannot be
// recovered.
func (s *Service) IssueAPIKey(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, expiresAt *time.Time) (rawKey string, key domain.ApiKey, err error) {
	raw, digest, err := GenerateAPIKey(tenantID)
	if err != nil {
		return "", domain.ApiKey{}, err
	}

	key = domain.ApiKey{
		ID: shareddomain.NewApiKeyID(), TenantID: tenantID, PrincipalID: principalID,
		KeyHash: digest, ExpiresAt: expiresAt,
	}
	if err := s.apiKeys.CreateApiKey(ctx, key); err != nil {
		return "", domain.ApiKey{}, err
	}

	// Neither the raw key nor its digest goes into the audit record: the
	// fact a key was issued is what matters, not its value.
	s.record(ctx, actor, tenantID, "apikey.issue", "api_key", key.ID.String(), nil,
		map[string]any{"principal_id": principalID.String()})
	return raw, key, nil
}

// --- authentication ------------------------------------------------

// AuthenticateUser verifies an email and password for one tenant and
// issues a token tied to no session. Deprecated for people — the console
// signs in with SignIn — and kept for machines and harnesses (LLD-02 §6).
// It is throttled exactly like SignIn.
//
// Every failure — unknown email, wrong password, no membership, disabled
// membership or person, suspended tenant — returns
// ports.ErrInvalidCredentials, after the same Argon2id work. The cause is
// logged, never returned (INV-10).
func (s *Service) AuthenticateUser(ctx context.Context, tenantID shareddomain.TenantID, email, password string) (*ports.AuthToken, error) {
	user, err := s.checkPassword(ctx, email, password)
	if err != nil {
		return nil, err
	}
	principal, err := s.principals.GetPrincipalByUser(ctx, tenantID, user.ID)
	if err != nil {
		s.warnAuthFailure(ctx, "no membership of the tenant", tenantID, user.Email, err)
		return nil, ports.ErrInvalidCredentials
	}
	return s.issueAccess(ctx, tenantID, principal.ID, domain.SessionID{})
}

// issueAccess issues an access token for a membership after re-checking
// it is live, and for a session when one is given.
func (s *Service) issueAccess(ctx context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, session domain.SessionID) (*ports.AuthToken, error) {
	st, err := s.liveness.LiveState(ctx, tenantID, principalID, domain.SessionID{})
	if err != nil {
		s.warnAuthFailure(ctx, "membership missing", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	if err := liveError(st); err != nil {
		s.warnAuthFailure(ctx, "account not active", tenantID, st.Principal.Email, err)
		return nil, ports.ErrInvalidCredentials
	}
	token, err := s.tokens.IssueFor(tenantID, principalID, rolesOf(st.Bindings), st.Principal.UserID, session)
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}
	return &ports.AuthToken{
		Token:       token,
		ExpiresAt:   s.now().UTC().Add(s.tokens.lifetime),
		TenantID:    tenantID,
		PrincipalID: principalID,
	}, nil
}

// AuthenticateAPIKey resolves a presented API key to a tenant context.
//
// The key names its own tenant, which is what makes the digest lookup a
// tenant-scoped read (see GenerateAPIKey). Naming a tenant proves
// nothing; the digest comparison does.
func (s *Service) AuthenticateAPIKey(ctx context.Context, rawKey string) (*ports.TenantContext, error) {
	tenantID, err := TenantFromAPIKey(rawKey)
	if err != nil {
		s.warnAuthFailure(ctx, "malformed api key", shareddomain.TenantID{}, "", err)
		return nil, ports.ErrInvalidCredentials
	}

	key, err := s.apiKeys.GetApiKeyByHash(ctx, tenantID, HashAPIKey(rawKey))
	if err != nil {
		s.warnAuthFailure(ctx, "unknown api key", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	if !key.Usable(s.now()) {
		s.warnAuthFailure(ctx, "expired or revoked api key", tenantID, "", nil)
		return nil, ports.ErrInvalidCredentials
	}

	st, err := s.liveness.LiveState(ctx, tenantID, key.PrincipalID, domain.SessionID{})
	if err != nil {
		s.warnAuthFailure(ctx, "api key principal missing", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	if err := liveError(st); err != nil {
		s.warnAuthFailure(ctx, "api key account not active", tenantID, st.Principal.Email, err)
		return nil, ports.ErrInvalidCredentials
	}
	return tenantContextOf(st, domain.SessionID{}), nil
}

// ValidateToken checks a token's signature and expiry, then — in one
// transaction — the live status of its membership, tenant, person and
// session (LLD-02 §10.2, design D4).
//
// The live check is what makes disabling an account, signing out, or a
// password change take effect on the next request rather than whenever
// the token happens to expire. It costs a read per request, deliberately:
// the alternative is a window in which a revoked token keeps working.
func (s *Service) ValidateToken(ctx context.Context, tokenString string) (*ports.TenantContext, error) {
	claims, err := s.tokens.Validate(tokenString)
	if err != nil {
		s.warnAuthFailure(ctx, "token rejected", shareddomain.TenantID{}, "", err)
		return nil, ports.ErrInvalidCredentials
	}

	tenantID, err := shareddomain.ParseTenantID(claims.TenantID)
	if err != nil {
		s.warnAuthFailure(ctx, "token tenant unparseable", shareddomain.TenantID{}, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	principalID, err := shareddomain.ParsePrincipalID(claims.PrincipalID)
	if err != nil {
		s.warnAuthFailure(ctx, "token principal unparseable", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}

	var sessionID domain.SessionID
	if claims.SessionID != "" {
		if sessionID, err = domain.ParseSessionID(claims.SessionID); err != nil {
			s.warnAuthFailure(ctx, "token session unparseable", tenantID, "", err)
			return nil, ports.ErrInvalidCredentials
		}
	}

	st, err := s.liveness.LiveState(ctx, tenantID, principalID, sessionID)
	if err != nil {
		s.warnAuthFailure(ctx, "token principal or session missing", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	if err := liveError(st); err != nil {
		s.warnAuthFailure(ctx, "token account not active", tenantID, st.Principal.Email, err)
		return nil, ports.ErrInvalidCredentials
	}
	if st.Session != nil {
		if !st.Session.Live(s.now()) || st.Session.UserID != st.Principal.UserID {
			s.warnAuthFailure(ctx, "token session ended", tenantID, st.Principal.Email, nil)
			return nil, ports.ErrInvalidCredentials
		}
	}
	return tenantContextOf(st, sessionID), nil
}

// liveError reports why a live state may not act, or nil.
func liveError(st ports.LiveState) error {
	switch {
	case st.Principal.Status != domain.PrincipalActive:
		return ports.ErrPrincipalNotActive
	case st.Tenant.Status != domain.TenantActive:
		return ports.ErrTenantNotActive
	case st.UserStatus != domain.UserActive:
		return ports.ErrUserNotActive
	}
	return nil
}

func rolesOf(bindings []domain.RoleBinding) []string {
	// Bindings only — principal.Role is deliberately NOT included: it is
	// the role provisioned with, and authorizes nothing (see rolesFor).
	roles := make([]string, 0, len(bindings))
	for _, b := range bindings {
		roles = append(roles, b.Role)
	}
	return roles
}

func scopesOf(bindings []domain.RoleBinding) []string {
	seen := map[string]struct{}{}
	var scopes []string
	for _, b := range bindings {
		if _, ok := seen[b.Scope]; ok {
			continue
		}
		seen[b.Scope] = struct{}{}
		scopes = append(scopes, b.Scope)
	}
	return scopes
}

func tenantContextOf(st ports.LiveState, session domain.SessionID) *ports.TenantContext {
	return &ports.TenantContext{
		TenantID:           st.Tenant.ID,
		PrincipalID:        st.Principal.ID,
		Roles:              rolesOf(st.Bindings),
		Scopes:             scopesOf(st.Bindings),
		ResidencyZone:      st.Tenant.ResidencyZone,
		UserID:             st.Principal.UserID,
		SessionID:          session,
		PasswordChangeOnly: st.MustChange,
	}
}

// AuthorizeAction reports whether a principal may perform an action on
// a resource owned by the tenant its principal belongs to. See
// AuthorizeSystem for actions that reach beyond one tenant.
func (s *Service) AuthorizeAction(ctx context.Context, principalID shareddomain.PrincipalID, action, resource string) error {
	tenantID, err := shareddomain.ParseTenantID(resource)
	if err != nil {
		return fmt.Errorf("%w: resource must name the owning tenant: %v", ports.ErrPermissionDenied, err)
	}

	return s.authorize(ctx, principalID, tenantID, action, tenantID.String())
}

// AuthorizeSystem reports whether a principal may perform action through
// a platform (system-scope) binding, evaluated against the principal's
// home tenant (where its bindings are stored). Tenant-scoped bindings —
// however broad their role — never satisfy it.
func (s *Service) AuthorizeSystem(ctx context.Context, principalID shareddomain.PrincipalID, homeTenant shareddomain.TenantID, action string) error {
	return s.authorize(ctx, principalID, homeTenant, action, domain.ScopeSystem)
}

// authorize loads the principal's bindings and applies the rule: either
// the ordinary tenant-scoped rule against resourceTenant, or — when
// wantSystemScope is domain.ScopeSystem — the system-scope-only rule.
func (s *Service) authorize(ctx context.Context, principalID shareddomain.PrincipalID, homeTenant shareddomain.TenantID, action, mode string) error {
	bindings, err := s.bindings.ListRoleBindings(ctx, homeTenant, principalID)
	if err != nil {
		// Failing to load bindings denies rather than permits: an
		// unavailable store must never widen access.
		s.logger.Warn("identity: could not load role bindings, denying",
			"tenant_id", homeTenant.String(), "principal_id", principalID.String(), "error", err)
		return ports.ErrPermissionDenied
	}

	var ok bool
	if mode == domain.ScopeSystem {
		ok = domain.IsAuthorizedSystem(bindings, action, s.permissions)
	} else {
		resourceTenant, perr := shareddomain.ParseTenantID(mode)
		if perr != nil {
			return fmt.Errorf("%w: invalid resource tenant: %v", ports.ErrPermissionDenied, perr)
		}
		ok = domain.IsAuthorized(bindings, action, resourceTenant, s.permissions)
	}
	if !ok {
		return ports.ErrPermissionDenied
	}
	return nil
}

// LoadTenant reads a tenant by id. Read-only: handlers use it to build
// mutation responses from the authoritative store.
func (s *Service) LoadTenant(ctx context.Context, id shareddomain.TenantID) (domain.Tenant, error) {
	return s.tenants.GetTenant(ctx, id)
}

// LoadPrincipal reads a principal within its tenant. Read-only: handlers
// use it to build mutation responses from the authoritative store.
func (s *Service) LoadPrincipal(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID) (domain.Principal, error) {
	return s.principals.GetPrincipal(ctx, tenantID, id)
}

// RecordAudit writes one audit record.
func (s *Service) RecordAudit(ctx context.Context, entry domain.AuditEntry) error {
	return s.audit.AppendAudit(ctx, entry)
}

// --- helpers -------------------------------------------------------

// Actor is who is performing a mutation, for the audit trail.
type Actor struct {
	ID        uuid.UUID
	Type      domain.ActorType
	IPAddress net.IP
}

// SystemActor is the actor for mutations no person performed.
func SystemActor() Actor {
	return Actor{ID: domain.SystemActorID, Type: domain.ActorSystem}
}

// PrincipalActor is the actor for a mutation made by a signed-in user.
func PrincipalActor(id shareddomain.PrincipalID, ip net.IP) Actor {
	return Actor{ID: uuid.UUID(id), Type: domain.ActorPrincipal, IPAddress: ip}
}

// record writes an audit entry for a completed mutation.
//
// An audit failure is logged, not returned: the mutation already
// happened, and reporting it as failed would be a lie that invites the
// caller to retry a change that is already applied. A missing audit row
// is a serious operational fault, which is why it is logged at error.
func (s *Service) record(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, action, resourceType, resourceID string, before, after map[string]any) {
	entry := domain.NewAuditEntry(
		tenantID, actor.ID, actor.Type,
		action, resourceType, resourceID,
		before, after, actor.IPAddress, s.now().UTC(),
	)
	if err := s.audit.AppendAudit(ctx, entry); err != nil {
		s.logger.Error("identity: audit write failed for a mutation that succeeded",
			"action", action, "tenant_id", tenantID.String(), "resource_id", resourceID, "error", err)
	}
}

// warnAuthFailure logs a failed authentication attempt.
//
// Credential material never appears here: no password, no API key, no
// token — only who was attempted and why it failed. Without these logs,
// credential stuffing and brute force are invisible until the rate
// limiter of a later LLD arrives (LLD-02 §10.8, OWASP A09).
func (s *Service) warnAuthFailure(ctx context.Context, reason string, tenantID shareddomain.TenantID, email string, cause error) {
	attrs := []any{"reason", reason}
	if !tenantID.IsZero() {
		attrs = append(attrs, "tenant_id", tenantID.String())
	}
	if email != "" {
		attrs = append(attrs, "email", email)
	}
	if cause != nil {
		attrs = append(attrs, "error", cause)
	}
	s.logger.WarnContext(ctx, "identity: authentication failed", attrs...)
}
