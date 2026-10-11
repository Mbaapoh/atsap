package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// This file holds the revision-2 aggregates (LLD-02 §3.1, D-61–D-66):
// the person behind every membership, their sessions, their recovery
// tickets, and the SSO-ready provider model. Every rule here is a pure
// function — no clock of its own, no I/O — so it is unit-testable in
// full and the application layer only orchestrates.

// UserID identifies a person across the whole installation. It is
// identity's own type: no other context reads it (they read PrincipalID,
// the membership).
type UserID uuid.UUID

// NewUserID generates a new random UserID.
func NewUserID() UserID { return UserID(uuid.New()) }

// ParseUserID parses s as a UserID.
func ParseUserID(s string) (UserID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return UserID{}, fmt.Errorf("parse user id: %w", err)
	}
	return UserID(id), nil
}

// String returns the canonical UUID string form.
func (id UserID) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id UserID) IsZero() bool { return id == UserID{} }

// SessionID identifies one signed-in browser session.
type SessionID uuid.UUID

// NewSessionID generates a new random SessionID.
func NewSessionID() SessionID { return SessionID(uuid.New()) }

// ParseSessionID parses s as a SessionID.
func ParseSessionID(s string) (SessionID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return SessionID{}, fmt.Errorf("parse session id: %w", err)
	}
	return SessionID(id), nil
}

// String returns the canonical UUID string form.
func (id SessionID) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id SessionID) IsZero() bool { return id == SessionID{} }

// IdentityProviderID identifies a tenant's external identity provider.
type IdentityProviderID uuid.UUID

// NewIdentityProviderID generates a new random IdentityProviderID.
func NewIdentityProviderID() IdentityProviderID { return IdentityProviderID(uuid.New()) }

// String returns the canonical UUID string form.
func (id IdentityProviderID) String() string { return uuid.UUID(id).String() }

// ParseIdentityProviderID parses s as an IdentityProviderID.
func ParseIdentityProviderID(s string) (IdentityProviderID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return IdentityProviderID{}, fmt.Errorf("parse identity provider id: %w", err)
	}
	return IdentityProviderID(id), nil
}

// --- email ----------------------------------------------------------

// ErrInvalidEmail is returned by NormalizeEmail for anything that cannot
// identify a person.
var ErrInvalidEmail = errors.New("invalid email address")

// NormalizeEmail returns the form an email is stored and compared in:
// trimmed and lower-cased, with exactly one "@" separating a non-empty
// local part from a domain that contains a dot. It deliberately does not
// try to validate deliverability — that is what an invitation proves.
func NormalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	local, domainPart, ok := strings.Cut(e, "@")
	if !ok || local == "" || domainPart == "" || strings.Contains(domainPart, "@") ||
		!strings.Contains(domainPart, ".") || strings.HasPrefix(domainPart, ".") ||
		strings.HasSuffix(domainPart, ".") || strings.ContainsAny(e, " \t\r\n") || len(e) > 255 {
		return "", ErrInvalidEmail
	}
	return e, nil
}

// EmailDomain returns the domain of a normalized email.
func EmailDomain(normalized string) string {
	_, d, _ := strings.Cut(normalized, "@")
	return d
}

// --- user -----------------------------------------------------------

// UserStatus is the person's account status. Only the person themselves
// or platform scope may change it (D-61): a tenant administrator governs
// memberships, never the person.
type UserStatus string

// User statuses.
const (
	UserActive   UserStatus = "ACTIVE"
	UserDisabled UserStatus = "DISABLED"
)

// Valid reports whether s is a recognised user status.
func (s UserStatus) Valid() bool { return s == UserActive || s == UserDisabled }

// CredentialKind names a kind of credential (D-66). Only CredentialPassword
// is written today; TOTP and passkeys arrive with identity-mfa.
type CredentialKind string

// Credential kinds.
const (
	CredentialPassword CredentialKind = "password"
)

// PasswordCredential is a user's password: an Argon2id hash, never the
// plaintext (INV-11).
type PasswordCredential struct {
	Hash string
	// MustChange is set by an administrator or off-network reset: the
	// next sign-in may do nothing but change it (D-64).
	MustChange bool
	ChangedAt  time.Time
}

// User is one person across the installation (D-61).
type User struct {
	ID          UserID
	Email       string // normalized, unique installation-wide
	DisplayName string
	Status      UserStatus
	// Password is nil when the person has no password: invited and not
	// yet set, or SSO-only (D-63).
	Password  *PasswordCredential
	CreatedAt time.Time
}

// Membership is a user's membership of one tenant as the user sees it:
// which tenant, which principal acts there, and its roles. It is only
// ever shown to the person themselves (D-61).
type Membership struct {
	PrincipalID shareddomain.PrincipalID
	TenantID    shareddomain.TenantID
	TenantName  string
	Status      PrincipalStatus
	Roles       []string
}

// --- sessions -------------------------------------------------------

// Session lifetimes (D-62).
const (
	SessionIdleTimeout     = 30 * time.Minute
	SessionAbsoluteTimeout = 12 * time.Hour
)

// Session revocation reasons, recorded on the row and in auth events.
const (
	RevokeSignOut         = "sign-out"
	RevokeReuse           = "reuse"
	RevokePasswordChanged = "password-changed"
	RevokePasswordReset   = "password-reset"
	RevokeUserDisabled    = "user-disabled"
)

// Session is one signed-in browser (D-62). Secrets are held only as
// SHA-256 digests.
type Session struct {
	ID     SessionID
	UserID UserID
	// PrincipalID is the membership the current access token names; nil
	// until the person has chosen a tenant.
	PrincipalID       *shareddomain.PrincipalID
	RefreshDigest     string
	PreviousDigest    string
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	RevokeReason      string
	CreatedAt         time.Time
}

// NewSession starts a session at now holding digest.
func NewSession(user UserID, digest string, now time.Time) Session {
	return Session{
		ID: NewSessionID(), UserID: user, RefreshDigest: digest,
		IdleExpiresAt:     now.Add(SessionIdleTimeout),
		AbsoluteExpiresAt: now.Add(SessionAbsoluteTimeout),
		CreatedAt:         now,
	}
}

// RefreshOutcome is what presenting a refresh secret means.
type RefreshOutcome int

// Refresh outcomes.
const (
	// RefreshRotated: the secret was current; the session now holds
	// newDigest, and the presented one became the previous generation.
	RefreshRotated RefreshOutcome = iota + 1
	// RefreshReused: the presented secret was already rotated away. Only
	// a copy can present it, so the whole session must be revoked.
	RefreshReused
	// RefreshExpired: idle or absolute lifetime has passed.
	RefreshExpired
	// RefreshRevoked: the session had already been ended.
	RefreshRevoked
	// RefreshUnknown: the digest matches neither generation.
	RefreshUnknown
)

// Live reports whether the session may still back an access token at now.
func (s Session) Live(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.IdleExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}

// Refresh decides what presenting presentedDigest means at now, and on
// RefreshRotated returns the session advanced to newDigest with its idle
// expiry extended (never past the absolute expiry). On RefreshReused it
// returns the session revoked. Pure: the caller persists the result under
// a row lock, so two concurrent refreshes cannot both rotate.
func (s Session) Refresh(presentedDigest, newDigest string, now time.Time) (Session, RefreshOutcome) {
	switch {
	case s.RevokedAt != nil:
		return s, RefreshRevoked
	case presentedDigest == "" || (presentedDigest != s.RefreshDigest && presentedDigest != s.PreviousDigest):
		return s, RefreshUnknown
	case presentedDigest == s.PreviousDigest && s.PreviousDigest != "":
		revoked := s.revoke(now, RevokeReuse)
		return revoked, RefreshReused
	case !now.Before(s.IdleExpiresAt) || !now.Before(s.AbsoluteExpiresAt):
		return s, RefreshExpired
	}
	next := s
	next.PreviousDigest = s.RefreshDigest
	next.RefreshDigest = newDigest
	next.IdleExpiresAt = now.Add(SessionIdleTimeout)
	if next.IdleExpiresAt.After(s.AbsoluteExpiresAt) {
		next.IdleExpiresAt = s.AbsoluteExpiresAt
	}
	return next, RefreshRotated
}

// Revoke ends the session at now for reason. Revoking an ended session
// keeps the first reason.
func (s Session) Revoke(now time.Time, reason string) Session {
	if s.RevokedAt != nil {
		return s
	}
	return s.revoke(now, reason)
}

func (s Session) revoke(now time.Time, reason string) Session {
	t := now
	s.RevokedAt = &t
	s.RevokeReason = reason
	return s
}

// --- throttling -----------------------------------------------------

// ThrottlePolicy decides how long a key is blocked after a run of
// consecutive failures (D-64): nothing for the first FreeAttempts, then
// Base doubling per further failure, capped at Max. There is no hard
// lockout — a known victim's email must not be a denial-of-service lever.
//
// Failures count only while they keep coming: a failure more than Window
// after the previous one starts a fresh run. Without that, an office
// behind one address would accumulate a day's worth of typos into a block.
type ThrottlePolicy struct {
	FreeAttempts int
	Base         time.Duration
	Max          time.Duration
	Window       time.Duration
}

// DefaultThrottlePolicy is D-64's: five free attempts within 15 minutes,
// then 30 s doubling, capped at 15 minutes.
var DefaultThrottlePolicy = ThrottlePolicy{FreeAttempts: 5, Base: 30 * time.Second, Max: 15 * time.Minute, Window: 15 * time.Minute}

// Continues reports whether a failure at now continues a run whose last
// failure was at last.
func (p ThrottlePolicy) Continues(last, now time.Time) bool {
	return p.Window <= 0 || now.Sub(last) <= p.Window
}

// Delay returns how long to block after the given number of consecutive
// failures.
func (p ThrottlePolicy) Delay(failures int) time.Duration {
	if failures < p.FreeAttempts {
		return 0
	}
	d := p.Base
	for i := p.FreeAttempts; i < failures && d < p.Max; i++ {
		d *= 2
	}
	if d > p.Max {
		d = p.Max
	}
	return d
}

// --- password reset authority -----------------------------------------

// CanResetPassword reports whether an actor may reset a person's password
// (D-64). Platform scope always may. Otherwise the actor must administer
// every tenant the person belongs to: resetting a shared person's
// password from one tenant would take over their access to the others.
// A person with no memberships can be reset only by platform scope.
func CanResetPassword(actorAdministers []shareddomain.TenantID, memberOf []shareddomain.TenantID, systemScope bool) bool {
	if systemScope {
		return true
	}
	if len(memberOf) == 0 {
		return false
	}
	admin := make(map[shareddomain.TenantID]struct{}, len(actorAdministers))
	for _, t := range actorAdministers {
		admin[t] = struct{}{}
	}
	for _, t := range memberOf {
		if _, ok := admin[t]; !ok {
			return false
		}
	}
	return true
}

// --- recovery tickets -------------------------------------------------

// TicketPurpose is what an emailed secret is for.
type TicketPurpose string

// Ticket purposes and their lifetimes (D-64).
const (
	TicketReset  TicketPurpose = "reset"
	TicketInvite TicketPurpose = "invite"

	ResetTicketLifetime  = 30 * time.Minute
	InviteTicketLifetime = 72 * time.Hour
)

// Lifetime returns how long a ticket of this purpose is valid.
func (p TicketPurpose) Lifetime() time.Duration {
	if p == TicketInvite {
		return InviteTicketLifetime
	}
	return ResetTicketLifetime
}

// --- federation readiness (D-63) ---------------------------------------

// ProviderKind is the protocol a provider speaks.
type ProviderKind string

// Provider kinds.
const (
	ProviderOIDC ProviderKind = "OIDC"
	ProviderSAML ProviderKind = "SAML"
)

// ProviderMode is whether, and how strongly, a provider is in use.
type ProviderMode string

// Provider modes.
const (
	ProviderDisabled ProviderMode = "DISABLED"
	ProviderOptional ProviderMode = "OPTIONAL"
	ProviderEnforced ProviderMode = "ENFORCED"
)

// IdentityProvider is a tenant's external IdP. Nothing signs anyone in
// through one yet; the model exists so OIDC lands as an adapter.
type IdentityProvider struct {
	ID          IdentityProviderID
	TenantID    shareddomain.TenantID
	Kind        ProviderKind
	DisplayName string
	Issuer      string
	Mode        ProviderMode
}

// DomainClaim is a provider's claim on an email domain. An unverified
// claim routes nothing.
type DomainClaim struct {
	Domain     string
	ProviderID IdentityProviderID
	VerifiedAt *time.Time
}

// EnforcesSSO reports whether a claim and its provider together require
// SSO for the domain: the claim must be verified and the provider
// enforced. Anything less leaves password sign-in exactly as it was.
func EnforcesSSO(claim *DomainClaim, provider *IdentityProvider) bool {
	return claim != nil && claim.VerifiedAt != nil && provider != nil &&
		provider.ID == claim.ProviderID && provider.Mode == ProviderEnforced
}

// ExternalIdentity links a provider's subject to a user. It is keyed by
// (provider, subject) and never matched by email (D-63).
type ExternalIdentity struct {
	ProviderID IdentityProviderID
	Subject    string
	UserID     UserID
	LinkedAt   time.Time
}

// --- authentication history ---------------------------------------------

// AuthEventKind names one authentication outcome (D-64). None of them
// carries a password, secret or token.
type AuthEventKind string

// Auth event kinds.
const (
	EventSignInSucceeded    AuthEventKind = "sign_in.succeeded"
	EventSignInFailed       AuthEventKind = "sign_in.failed"
	EventThrottled          AuthEventKind = "sign_in.throttled"
	EventPasswordChanged    AuthEventKind = "password.changed"
	EventResetRequested     AuthEventKind = "password.reset_requested"
	EventResetCompleted     AuthEventKind = "password.reset_completed"
	EventAdminReset         AuthEventKind = "password.admin_reset"
	EventSessionRevoked     AuthEventKind = "session.revoked"
	EventInvitationAccepted AuthEventKind = "invitation.accepted"
)

// AuthEvent is one entry in a person's authentication history. UserID is
// nil when the attempt named an email with no account.
type AuthEvent struct {
	UserID        *UserID
	Kind          AuthEventKind
	SourceAddress string
	At            time.Time
}
