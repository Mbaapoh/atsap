package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
var DefaultPermissions = map[string][]string{
	"AGENT":          {"call.read"},
	"SUPERVISOR":     {"call.read", "call.terminate", "principal.read"},
	"TENANT_ADMIN":   {domain.Wildcard},
	"PLATFORM_ADMIN": {domain.Wildcard},
}

// Service implements ports.IdentityService.
type Service struct {
	tenants     ports.TenantStore
	principals  ports.PrincipalStore
	bindings    ports.RoleBindingStore
	apiKeys     ports.ApiKeyStore
	audit       ports.AuditStore
	hasher      *PasswordHasher
	tokens      *TokenIssuer
	permissions map[string][]string
	logger      *slog.Logger
	now         func() time.Time
}

var _ ports.IdentityService = (*Service)(nil)

// NewService wires the identity orchestrator.
func NewService(
	tenants ports.TenantStore,
	principals ports.PrincipalStore,
	bindings ports.RoleBindingStore,
	apiKeys ports.ApiKeyStore,
	audit ports.AuditStore,
	hasher *PasswordHasher,
	tokens *TokenIssuer,
	logger *slog.Logger,
) *Service {
	return &Service{
		tenants: tenants, principals: principals, bindings: bindings,
		apiKeys: apiKeys, audit: audit, hasher: hasher, tokens: tokens,
		permissions: DefaultPermissions, logger: logger, now: time.Now,
	}
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
func (s *Service) ProvisionTenant(ctx context.Context, actor Actor, name, residencyZone string) (domain.Tenant, error) {
	if residencyZone == "" {
		return domain.Tenant{}, errors.New("residency zone is required and cannot be blank")
	}

	tenant := domain.Tenant{
		ID: shareddomain.NewTenantID(), Name: name,
		Status: domain.TenantActive, ResidencyZone: residencyZone,
	}
	if err := s.tenants.CreateTenant(ctx, tenant); err != nil {
		return domain.Tenant{}, fmt.Errorf("provision tenant: %w", err)
	}

	s.record(ctx, actor, tenant.ID, "tenant.create", "tenant", tenant.ID.String(), nil, map[string]any{
		"name": tenant.Name, "status": string(tenant.Status), "residency_zone": tenant.ResidencyZone,
	})
	return tenant, nil
}

// ProvisionPrincipal creates a user account within a tenant.
func (s *Service) ProvisionPrincipal(ctx context.Context, actor Actor, tenantID shareddomain.TenantID, username, email, password, role string) (domain.Principal, error) {
	if err := domain.ValidatePassword(password); err != nil {
		return domain.Principal{}, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return domain.Principal{}, fmt.Errorf("hash password: %w", err)
	}

	principal := domain.Principal{
		ID: shareddomain.NewPrincipalID(), TenantID: tenantID,
		Username: username, Email: email, PasswordHash: hash,
		Role: role, Status: domain.PrincipalActive,
	}
	if err := s.principals.CreatePrincipal(ctx, principal); err != nil {
		return domain.Principal{}, err
	}

	// The audit record names the account and its role. It carries no
	// password and no hash — NewAuditEntry would strip them anyway, but
	// they are not passed in the first place.
	s.record(ctx, actor, tenantID, "principal.create", "principal", principal.ID.String(), nil, map[string]any{
		"username": username, "email": email, "role": role, "status": string(principal.Status),
	})
	return principal, nil
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

// AuthenticateUser verifies credentials and issues a token.
//
// Every failure — unknown username, wrong password, disabled principal,
// suspended tenant — returns ports.ErrInvalidCredentials. The specific
// cause is logged, never returned: telling a caller which of those it
// was is free reconnaissance (INV-10).
func (s *Service) AuthenticateUser(ctx context.Context, tenantID shareddomain.TenantID, username, password string) (*ports.AuthToken, error) {
	principal, err := s.principals.GetPrincipalByUsername(ctx, tenantID, username)
	if err != nil {
		s.warnAuthFailure(ctx, "unknown username", tenantID, username, err)
		return nil, ports.ErrInvalidCredentials
	}

	ok, err := s.hasher.Verify(password, principal.PasswordHash)
	if err != nil {
		// A corrupt stored hash is an operational fault, not a wrong
		// password — it is logged as such, though the caller still sees
		// only "invalid credentials".
		s.logger.Error("identity: stored password hash is unusable",
			"tenant_id", tenantID.String(), "principal_id", principal.ID.String(), "error", err)
		return nil, ports.ErrInvalidCredentials
	}
	if !ok {
		s.warnAuthFailure(ctx, "wrong password", tenantID, username, nil)
		return nil, ports.ErrInvalidCredentials
	}

	if err := s.checkLiveStatus(ctx, tenantID, principal); err != nil {
		s.warnAuthFailure(ctx, "account not active", tenantID, username, err)
		return nil, ports.ErrInvalidCredentials
	}

	roles, err := s.rolesFor(ctx, tenantID, principal)
	if err != nil {
		return nil, err
	}

	token, err := s.tokens.Issue(tenantID, principal.ID, roles)
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}

	return &ports.AuthToken{
		Token:       token,
		ExpiresAt:   s.now().UTC().Add(s.tokens.lifetime),
		TenantID:    tenantID,
		PrincipalID: principal.ID,
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

	principal, err := s.principals.GetPrincipal(ctx, tenantID, key.PrincipalID)
	if err != nil {
		s.warnAuthFailure(ctx, "api key principal missing", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	if err := s.checkLiveStatus(ctx, tenantID, principal); err != nil {
		s.warnAuthFailure(ctx, "api key account not active", tenantID, principal.Username, err)
		return nil, ports.ErrInvalidCredentials
	}

	return s.tenantContext(ctx, tenantID, principal)
}

// ValidateToken checks a token's signature and expiry, then the live
// status of its principal and tenant.
//
// The live-status check is what makes disabling an account take effect
// on the next request rather than whenever the token happens to expire
// (LLD-02 §10.2). It costs a read per request, deliberately: the
// alternative is a window in which a disabled account keeps working.
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

	principal, err := s.principals.GetPrincipal(ctx, tenantID, principalID)
	if err != nil {
		s.warnAuthFailure(ctx, "token principal missing", tenantID, "", err)
		return nil, ports.ErrInvalidCredentials
	}
	if err := s.checkLiveStatus(ctx, tenantID, principal); err != nil {
		s.warnAuthFailure(ctx, "token account not active", tenantID, principal.Username, err)
		return nil, ports.ErrInvalidCredentials
	}

	return s.tenantContext(ctx, tenantID, principal)
}

// AuthorizeAction reports whether a principal may perform an action.
func (s *Service) AuthorizeAction(ctx context.Context, principalID shareddomain.PrincipalID, action, resource string) error {
	tenantID, err := shareddomain.ParseTenantID(resource)
	if err != nil {
		return fmt.Errorf("%w: resource must name the owning tenant: %v", ports.ErrPermissionDenied, err)
	}

	bindings, err := s.bindings.ListRoleBindings(ctx, tenantID, principalID)
	if err != nil {
		// Failing to load bindings denies rather than permits: an
		// unavailable store must never widen access.
		s.logger.Warn("identity: could not load role bindings, denying",
			"tenant_id", tenantID.String(), "principal_id", principalID.String(), "error", err)
		return ports.ErrPermissionDenied
	}

	if !domain.IsAuthorized(bindings, action, tenantID, s.permissions) {
		return ports.ErrPermissionDenied
	}
	return nil
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

// checkLiveStatus reports whether a principal and its tenant are both
// active right now.
func (s *Service) checkLiveStatus(ctx context.Context, tenantID shareddomain.TenantID, principal domain.Principal) error {
	if principal.Status != domain.PrincipalActive {
		return ports.ErrPrincipalNotActive
	}
	tenant, err := s.tenants.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	if tenant.Status != domain.TenantActive {
		return ports.ErrTenantNotActive
	}
	return nil
}

func (s *Service) rolesFor(ctx context.Context, tenantID shareddomain.TenantID, principal domain.Principal) ([]string, error) {
	bindings, err := s.bindings.ListRoleBindings(ctx, tenantID, principal.ID)
	if err != nil {
		return nil, fmt.Errorf("load role bindings: %w", err)
	}
	roles := make([]string, 0, len(bindings)+1)
	if principal.Role != "" {
		roles = append(roles, principal.Role)
	}
	for _, b := range bindings {
		roles = append(roles, b.Role)
	}
	return roles, nil
}

func (s *Service) tenantContext(ctx context.Context, tenantID shareddomain.TenantID, principal domain.Principal) (*ports.TenantContext, error) {
	roles, err := s.rolesFor(ctx, tenantID, principal)
	if err != nil {
		return nil, err
	}
	tenant, err := s.tenants.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &ports.TenantContext{
		TenantID:      tenantID,
		PrincipalID:   principal.ID,
		Roles:         roles,
		ResidencyZone: tenant.ResidencyZone,
	}, nil
}

// warnAuthFailure logs a failed authentication attempt.
//
// Credential material never appears here: no password, no API key, no
// token — only who was attempted and why it failed. Without these logs,
// credential stuffing and brute force are invisible until the rate
// limiter of a later LLD arrives (LLD-02 §10.8, OWASP A09).
func (s *Service) warnAuthFailure(ctx context.Context, reason string, tenantID shareddomain.TenantID, username string, cause error) {
	attrs := []any{"reason", reason}
	if !tenantID.IsZero() {
		attrs = append(attrs, "tenant_id", tenantID.String())
	}
	if username != "" {
		attrs = append(attrs, "username", username)
	}
	if cause != nil {
		attrs = append(attrs, "error", cause)
	}
	s.logger.WarnContext(ctx, "identity: authentication failed", attrs...)
}
