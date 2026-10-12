package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// This file holds the console account use cases of revision 2 (LLD-02
// §6, D-61–D-66): signing in, sessions, password change and recovery.
// The rules are the domain's (account.go); this layer only orchestrates.

// resetRequestsPerHour bounds reset mails per person (design D7), so the
// reset form cannot be used to flood someone's inbox.
const resetRequestsPerHour = 3

// --- password check --------------------------------------------------------

// checkPassword is the one place a password is verified. In order:
//  1. the throttle, keyed by account and by source, before anything is
//     looked up — a blocked key costs no Argon2id and reveals nothing;
//  2. Argon2id, always: against the account's hash, or against a fixed
//     dummy hash when there is no account or no password, so response
//     time does not reveal whether the account exists (LLD-02 §10.10);
//  3. SSO enforcement for the email's domain (D-63);
//  4. the person's status.
//
// Every refusal is ports.ErrInvalidCredentials, except a throttled
// attempt, which is a *ports.ThrottledError carrying the delay. Failures
// and successes are recorded in the throttle and in auth events.
func (s *Service) checkPassword(ctx context.Context, rawEmail, password string) (domain.User, error) {
	now := s.now().UTC()
	email, emailErr := domain.NormalizeEmail(rawEmail)
	if emailErr != nil {
		email = rawEmail // still throttled and still hashed: no shortcut for garbage
	}
	accountKey := throttleKey("account", email)
	keys := []string{accountKey}
	if src := sourceFrom(ctx); src != "" {
		keys = append(keys, throttleKey("source", src))
	}

	wait, err := s.throttle.BlockedFor(ctx, keys, now)
	if err != nil {
		// An unreadable throttle fails closed: refusing is safer than an
		// unthrottled guess.
		return domain.User{}, fmt.Errorf("read throttle: %w", err)
	}
	if wait > 0 {
		s.event(ctx, nil, domain.EventThrottled)
		s.warnAuthFailure(ctx, "throttled", shareddomain.TenantID{}, email, nil)
		return domain.User{}, &ports.ThrottledError{RetryAfter: wait}
	}

	user, lookupErr := s.users.GetUserByEmail(ctx, email)
	hash := s.dummyHash
	if lookupErr == nil && user.Password != nil {
		hash = user.Password.Hash
	}
	ok, verr := s.hasher.Verify(password, hash)
	if verr != nil {
		s.logger.Error("identity: stored password hash is unusable", "user_id", user.ID.String(), "error", verr)
		ok = false
	}

	fail := func(reason string, cause error) (domain.User, error) {
		if rerr := s.throttle.RecordFailure(ctx, keys, now, s.policy); rerr != nil {
			s.logger.Error("identity: could not record a failed attempt", "error", rerr)
		}
		var uid *domain.UserID
		if lookupErr == nil {
			uid = &user.ID
		}
		s.event(ctx, uid, domain.EventSignInFailed)
		s.warnAuthFailure(ctx, reason, shareddomain.TenantID{}, email, cause)
		return domain.User{}, ports.ErrInvalidCredentials
	}

	switch {
	case emailErr != nil:
		return fail("malformed email", emailErr)
	case lookupErr != nil:
		return fail("unknown email", lookupErr)
	case user.Password == nil:
		return fail("account has no password", nil)
	case !ok:
		return fail("wrong password", nil)
	}
	if s.ssoEnforced(ctx, email) {
		return fail("domain requires SSO", nil)
	}
	if user.Status != domain.UserActive {
		return fail("account not active", ports.ErrUserNotActive)
	}

	if err := s.throttle.Clear(ctx, accountKey); err != nil {
		s.logger.Error("identity: could not clear the throttle after a success", "error", err)
	}
	return user, nil
}

// ssoEnforced reports whether a verified, enforced provider owns the
// email's domain (D-63). A lookup failure other than "none" fails closed.
func (s *Service) ssoEnforced(ctx context.Context, email string) bool {
	claim, provider, err := s.providers.ProviderForDomain(ctx, domain.EmailDomain(email))
	if errors.Is(err, ports.ErrNotFound) {
		return false
	}
	if err != nil {
		s.logger.Error("identity: could not read the domain's identity provider; refusing password sign-in", "error", err)
		return true
	}
	return domain.EnforcesSSO(&claim, &provider)
}

// --- discovery -------------------------------------------------------------

// SignInMethod is how an email signs in.
type SignInMethod struct {
	// Provider is set when the email's domain requires SSO through it;
	// nil means password sign-in.
	Provider *domain.IdentityProvider
}

// DiscoverSignIn answers how an email signs in (D-63). The answer depends
// only on the email's domain, never on whether an account exists, so it is
// not an account-existence oracle.
func (s *Service) DiscoverSignIn(ctx context.Context, rawEmail string) (SignInMethod, error) {
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return SignInMethod{}, nil // garbage gets the default answer, like any unknown email
	}
	claim, provider, err := s.providers.ProviderForDomain(ctx, domain.EmailDomain(email))
	if errors.Is(err, ports.ErrNotFound) {
		return SignInMethod{}, nil
	}
	if err != nil {
		return SignInMethod{}, err
	}
	if domain.EnforcesSSO(&claim, &provider) {
		return SignInMethod{Provider: &provider}, nil
	}
	return SignInMethod{}, nil
}

// --- sessions --------------------------------------------------------------

// SessionResult is what signing in or refreshing yields. RefreshSecret is
// credential material: it travels only in an HttpOnly cookie, never in a
// response body, a log, or an audit record (D-62).
type SessionResult struct {
	RefreshSecret string
	ExpiresAt     time.Time // absolute session expiry, for the cookie
	// Access is nil while the person has more than one membership and has
	// not chosen one yet.
	Access      *ports.AuthToken
	Memberships []domain.Membership
	// PasswordChangeOnly: the credential must be changed before anything
	// else is allowed.
	PasswordChangeOnly bool
}

// SignIn checks an email and password and starts a session (D-62).
func (s *Service) SignIn(ctx context.Context, email, password string) (SessionResult, error) {
	user, err := s.checkPassword(ctx, email, password)
	if err != nil {
		return SessionResult{}, err
	}
	memberships, err := s.activeMemberships(ctx, user.ID)
	if err != nil {
		return SessionResult{}, err
	}
	if len(memberships) == 0 {
		// A person with no tenant can do nothing; reporting it differently
		// would distinguish "exists" from "does not".
		s.warnAuthFailure(ctx, "no active membership", shareddomain.TenantID{}, user.Email, nil)
		return SessionResult{}, ports.ErrInvalidCredentials
	}

	now := s.now().UTC()
	raw, digest, err := NewSecret()
	if err != nil {
		return SessionResult{}, err
	}
	sess := domain.NewSession(user.ID, digest, now)
	if len(memberships) == 1 {
		pid := memberships[0].PrincipalID
		sess.PrincipalID = &pid
	}
	if err := s.sessions.CreateSession(ctx, sess); err != nil {
		return SessionResult{}, fmt.Errorf("create session: %w", err)
	}
	s.event(ctx, &user.ID, domain.EventSignInSucceeded)

	res := SessionResult{
		RefreshSecret: raw, ExpiresAt: sess.AbsoluteExpiresAt, Memberships: memberships,
		PasswordChangeOnly: user.Password != nil && user.Password.MustChange,
	}
	if sess.PrincipalID != nil {
		if res.Access, err = s.issueAccess(ctx, memberships[0].TenantID, *sess.PrincipalID, sess.ID); err != nil {
			return SessionResult{}, err
		}
	}
	return res, nil
}

// RefreshSession rotates a session's secret and issues an access token,
// for the named tenant when one is given (switching tenant is a refresh,
// never a wider token). Any secret that cannot be used — unknown,
// expired, revoked, replayed — is ports.ErrSessionNotUsable; a replay
// also revokes the session (D-62).
func (s *Service) RefreshSession(ctx context.Context, rawSecret string, tenant *shareddomain.TenantID) (SessionResult, error) {
	now := s.now().UTC()
	newRaw, newDigest, err := NewSecret()
	if err != nil {
		return SessionResult{}, err
	}

	var memberships []domain.Membership
	var chosen *domain.Membership
	retried := false
	sess, err := s.sessions.UpdateSessionByDigest(ctx, DigestSecret(rawSecret), func(cur domain.Session) (domain.Session, error) {
		next, outcome := cur.Refresh(DigestSecret(rawSecret), newDigest, now)
		switch outcome {
		case domain.RefreshRotated:
		case domain.RefreshRetried:
			retried = true
		case domain.RefreshReused:
			return next, errReuse
		default:
			return cur, ports.ErrSessionNotUsable
		}
		ms, err := s.activeMemberships(ctx, cur.UserID)
		if err != nil {
			return cur, err
		}
		memberships = ms
		chosen = chooseMembership(ms, cur.PrincipalID, tenant)
		if tenant != nil && chosen == nil {
			// Not a member, or no such tenant: one answer for both, and
			// the session is left exactly as it was.
			return cur, ports.ErrPermissionDenied
		}
		if chosen != nil {
			pid := chosen.PrincipalID
			next.PrincipalID = &pid
		}
		return next, nil
	})
	switch {
	case errors.Is(err, errReuse):
		s.event(ctx, &sess.UserID, domain.EventSessionRevoked)
		s.logger.WarnContext(ctx, "identity: a rotated refresh secret was replayed; session revoked",
			"session_id", sess.ID.String(), "user_id", sess.UserID.String())
		return SessionResult{}, ports.ErrSessionNotUsable
	case errors.Is(err, ports.ErrNotFound):
		return SessionResult{}, ports.ErrSessionNotUsable
	case err != nil:
		return SessionResult{}, err
	}
	if retried {
		// A lost response, retried within its grace window: recorded so an
		// operator can see retries, but not a security warning (D-62).
		s.event(ctx, &sess.UserID, domain.EventSessionRefreshRetried)
		s.logger.InfoContext(ctx, "identity: refresh retried with the previous secret inside its grace window",
			"session_id", sess.ID.String(), "user_id", sess.UserID.String())
	}

	user, err := s.users.GetUser(ctx, sess.UserID)
	if err != nil {
		return SessionResult{}, err
	}
	if user.Status != domain.UserActive {
		return SessionResult{}, ports.ErrSessionNotUsable
	}
	res := SessionResult{
		RefreshSecret: newRaw, ExpiresAt: sess.AbsoluteExpiresAt, Memberships: memberships,
		PasswordChangeOnly: user.Password != nil && user.Password.MustChange,
	}
	if chosen != nil {
		if res.Access, err = s.issueAccess(ctx, chosen.TenantID, chosen.PrincipalID, sess.ID); err != nil {
			return SessionResult{}, err
		}
	}
	return res, nil
}

// errReuse marks a replayed secret inside the store callback, so the
// revocation is persisted while the caller is refused.
var errReuse = errors.New("refresh secret replayed")

// chooseMembership picks the membership a refresh issues for: the named
// tenant's if given, else the session's current one if still active, else
// the only one.
func chooseMembership(ms []domain.Membership, current *shareddomain.PrincipalID, tenant *shareddomain.TenantID) *domain.Membership {
	for i := range ms {
		if tenant != nil && ms[i].TenantID == *tenant {
			return &ms[i]
		}
		if tenant == nil && current != nil && ms[i].PrincipalID == *current {
			return &ms[i]
		}
	}
	if tenant == nil && len(ms) == 1 {
		return &ms[0]
	}
	return nil
}

// activeMemberships lists a person's active memberships of active tenants.
func (s *Service) activeMemberships(ctx context.Context, user domain.UserID) ([]domain.Membership, error) {
	all, err := s.users.ListMemberships(ctx, user)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, m := range all {
		if m.Status == domain.PrincipalActive {
			out = append(out, m)
		}
	}
	return out, nil
}

// SignOut ends a session. Its access tokens are refused from their next
// use (ValidateToken checks the session).
func (s *Service) SignOut(ctx context.Context, tc *ports.TenantContext) error {
	if tc == nil || tc.SessionID.IsZero() {
		return nil // nothing to end: an API-key or legacy token has no session
	}
	if err := s.sessions.RevokeSession(ctx, tc.SessionID, s.now().UTC(), domain.RevokeSignOut); err != nil {
		return err
	}
	s.event(ctx, &tc.UserID, domain.EventSessionRevoked)
	return nil
}

// SignOutSecret ends the session a refresh secret belongs to, for a
// sign-out that arrives with only the cookie.
func (s *Service) SignOutSecret(ctx context.Context, rawSecret string) error {
	if rawSecret == "" {
		return nil
	}
	now := s.now().UTC()
	sess, err := s.sessions.UpdateSessionByDigest(ctx, DigestSecret(rawSecret), func(cur domain.Session) (domain.Session, error) {
		return cur.Revoke(now, domain.RevokeSignOut), nil
	})
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.event(ctx, &sess.UserID, domain.EventSessionRevoked)
	return nil
}

// --- password change and recovery -------------------------------------------

// ChangePassword sets a new password for the signed-in person, after
// checking the current one (throttled like sign-in). Every other session
// of theirs ends; the one that made the change continues (D-64).
func (s *Service) ChangePassword(ctx context.Context, tc *ports.TenantContext, current, next string) error {
	if tc == nil || tc.UserID.IsZero() {
		return ports.ErrPermissionDenied
	}
	if err := domain.ValidatePassword(next); err != nil {
		return err
	}
	user, err := s.users.GetUser(ctx, tc.UserID)
	if err != nil {
		return err
	}
	if _, err := s.checkPassword(ctx, user.Email, current); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := s.now().UTC()
	if err := s.users.SetPassword(ctx, user.ID, domain.PasswordCredential{Hash: hash, ChangedAt: now}); err != nil {
		return err
	}
	if _, err := s.sessions.RevokeUserSessions(ctx, user.ID, tc.SessionID, now, domain.RevokePasswordChanged); err != nil {
		return err
	}
	s.event(ctx, &user.ID, domain.EventPasswordChanged)
	return nil
}

// RequestPasswordReset emails a single-use reset link when the email
// belongs to a person who may sign in with a password. It returns nil in
// every case — known, unknown, SSO-enforced, rate-limited — so its answer
// discloses nothing (D-64). Only an internal fault is an error.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string) error {
	now := s.now().UTC()
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return nil
	}
	if src := sourceFrom(ctx); src != "" {
		key := []string{throttleKey("reset-source", src)}
		if wait, err := s.throttle.BlockedFor(ctx, key, now); err == nil && wait > 0 {
			return &ports.ThrottledError{RetryAfter: wait}
		}
		// Every request counts against the source: the throttle bounds
		// requests, not only failures.
		_ = s.throttle.RecordFailure(ctx, key, now, s.policy)
	}
	user, err := s.users.GetUserByEmail(ctx, email)
	if errors.Is(err, ports.ErrNotFound) {
		s.event(ctx, nil, domain.EventResetRequested)
		return nil
	}
	if err != nil {
		return err
	}
	s.event(ctx, &user.ID, domain.EventResetRequested)
	if user.Status != domain.UserActive || s.ssoEnforced(ctx, email) {
		return nil
	}
	n, err := s.tickets.CountTickets(ctx, user.ID, domain.TicketReset, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if n >= resetRequestsPerHour {
		return nil
	}
	s.sendTicket(ctx, user, domain.TicketReset)
	return nil
}

// CompletePasswordReset redeems a reset or invitation secret: the new
// password is set, the secret and every other outstanding one die, and
// every session ends — atomically (D-64). Any secret that cannot be
// redeemed is ports.ErrInvalidCredentials, whatever the reason.
func (s *Service) CompletePasswordReset(ctx context.Context, rawSecret, next string) error {
	if err := domain.ValidatePassword(next); err != nil {
		return err
	}
	now := s.now().UTC()
	if src := sourceFrom(ctx); src != "" {
		key := []string{throttleKey("redeem-source", src)}
		if wait, err := s.throttle.BlockedFor(ctx, key, now); err == nil && wait > 0 {
			return &ports.ThrottledError{RetryAfter: wait}
		}
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	user, purpose, err := s.tickets.RedeemTicket(ctx, DigestSecret(rawSecret), domain.PasswordCredential{Hash: hash, ChangedAt: now}, now)
	if errors.Is(err, ports.ErrNotFound) {
		if src := sourceFrom(ctx); src != "" {
			_ = s.throttle.RecordFailure(ctx, []string{throttleKey("redeem-source", src)}, now, s.policy)
		}
		return ports.ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	kind := domain.EventResetCompleted
	if purpose == domain.TicketInvite {
		kind = domain.EventInvitationAccepted
	}
	s.event(ctx, &user, kind)
	return nil
}

// ResetPrincipalPassword is an administrator's reset (D-64): the member
// gets a temporary password they must change at next sign-in, and their
// sessions end. A tenant administrator may reset only a person who belongs
// to no tenant they do not administer; otherwise platform scope is needed.
func (s *Service) ResetPrincipalPassword(ctx context.Context, tc *ports.TenantContext, actor Actor, tenantID shareddomain.TenantID,
	principalID shareddomain.PrincipalID, temporary string) error {
	if err := domain.ValidatePassword(temporary); err != nil {
		return err
	}
	target, err := s.principals.GetPrincipal(ctx, tenantID, principalID)
	if err != nil {
		return err
	}
	memberOf, err := s.users.ListMemberships(ctx, target.UserID)
	if err != nil {
		return err
	}
	var actorAdmin []shareddomain.TenantID
	if !tc.UserID.IsZero() {
		mine, err := s.activeMemberships(ctx, tc.UserID)
		if err != nil {
			return err
		}
		for _, m := range mine {
			for _, r := range m.Roles {
				if r == "TENANT_ADMIN" || r == "PLATFORM_ADMIN" {
					actorAdmin = append(actorAdmin, m.TenantID)
					break
				}
			}
		}
	} else {
		actorAdmin = []shareddomain.TenantID{tc.TenantID}
	}
	tenants := make([]shareddomain.TenantID, len(memberOf))
	for i, m := range memberOf {
		tenants[i] = m.TenantID
	}
	if !domain.CanResetPassword(actorAdmin, tenants, tc.HasScope(domain.ScopeSystem)) {
		return ports.ErrPermissionDenied
	}
	if err := s.setTemporaryPassword(ctx, target.UserID, temporary); err != nil {
		return err
	}
	s.record(ctx, actor, tenantID, "principal.password.reset", "principal", principalID.String(), nil,
		map[string]any{"must_change": true})
	s.event(ctx, &target.UserID, domain.EventAdminReset)
	return nil
}

// RecoverByEmail is the off-network recovery (D-66): run on the server by
// an operator, whose shell access is the authority, so no permission is
// checked. It returns a generated temporary password, to be shown once;
// it is never logged. ErrNotFound for an unknown email.
func (s *Service) RecoverByEmail(ctx context.Context, rawEmail string) (string, error) {
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return "", ports.ErrNotFound
	}
	user, err := s.users.GetUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	temporary, err := temporaryPassword()
	if err != nil {
		return "", err
	}
	if err := s.setTemporaryPassword(ctx, user.ID, temporary); err != nil {
		return "", err
	}
	memberships, err := s.users.ListMemberships(ctx, user.ID)
	if err != nil {
		return "", err
	}
	for _, m := range memberships {
		s.record(ctx, SystemActor(), m.TenantID, "principal.password.reset", "principal", m.PrincipalID.String(), nil,
			map[string]any{"must_change": true, "via": "off-network recovery"})
	}
	s.event(ctx, &user.ID, domain.EventAdminReset)
	return temporary, nil
}

func (s *Service) setTemporaryPassword(ctx context.Context, user domain.UserID, temporary string) error {
	hash, err := s.hasher.Hash(temporary)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := s.now().UTC()
	if err := s.users.SetPassword(ctx, user, domain.PasswordCredential{Hash: hash, MustChange: true, ChangedAt: now}); err != nil {
		return err
	}
	if _, err := s.sessions.RevokeUserSessions(ctx, user, domain.SessionID{}, now, domain.RevokePasswordReset); err != nil {
		return err
	}
	return nil
}

// temporaryPassword returns 20 characters from an unambiguous alphabet.
func temporaryPassword() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 20)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("generate temporary password: %w", err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}

// SetUserStatus disables or restores a person everywhere. Platform scope
// only — the handler checks it; a tenant administrator governs memberships,
// never the person (D-61). Disabling ends every session at once.
func (s *Service) SetUserStatus(ctx context.Context, actor Actor, homeTenant shareddomain.TenantID, user domain.UserID, status domain.UserStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid user status %q", status)
	}
	if err := s.users.SetUserStatus(ctx, user, status); err != nil {
		return err
	}
	if status == domain.UserDisabled {
		if _, err := s.sessions.RevokeUserSessions(ctx, user, domain.SessionID{}, s.now().UTC(), domain.RevokeUserDisabled); err != nil {
			return err
		}
		s.event(ctx, &user, domain.EventSessionRevoked)
	}
	s.record(ctx, actor, homeTenant, "user.status.set", "user", user.String(), nil, map[string]any{"status": string(status)})
	return nil
}

// --- mail --------------------------------------------------------------------

// sendTicket creates a reset or invitation ticket and mails its link. A
// send failure is logged with the person's id and never changes the
// caller's response (design D8).
func (s *Service) sendTicket(ctx context.Context, user domain.User, purpose domain.TicketPurpose) {
	now := s.now().UTC()
	raw, digest, err := NewSecret()
	if err != nil {
		s.logger.Error("identity: could not create a ticket", "user_id", user.ID.String(), "error", err)
		return
	}
	if err := s.tickets.CreateTicket(ctx, ports.ResetTicket{
		UserID: user.ID, Digest: digest, Purpose: purpose,
		ExpiresAt: now.Add(purpose.Lifetime()), CreatedAt: now,
	}); err != nil {
		s.logger.Error("identity: could not store a ticket", "user_id", user.ID.String(), "error", err)
		return
	}
	// The secret travels in the fragment, which browsers never send to a
	// server — so it stays out of access logs and Referer headers.
	link := s.consoleURL + "/reset#" + raw
	subject, body := "Reset your AtsaPBX password",
		"Someone asked to reset the password for this address.\n\n"+
			"To choose a new password, open this link within 30 minutes:\n\n"+link+"\n\n"+
			"If you did not ask for this, ignore this message: your password is unchanged.\n"
	if purpose == domain.TicketInvite {
		link = s.consoleURL + "/invite#" + raw
		subject, body = "You have been invited to AtsaPBX",
			"An administrator has added you to AtsaPBX.\n\n"+
				"To set your password and sign in, open this link within 72 hours:\n\n"+link+"\n"
	}
	if err := s.mailer.Send(ctx, user.Email, subject, body); err != nil {
		s.logger.Error("identity: could not send mail", "user_id", user.ID.String(), "purpose", string(purpose), "error", err)
	}
}

// logMailer is the fallback when no SMTP server is configured: it records
// that a mail would have been sent, and never the body — the body holds a
// link secret (design D10).
type logMailer struct{ logger *slog.Logger }

func (m logMailer) Send(ctx context.Context, to, subject, _ string) error {
	m.logger.WarnContext(ctx, "identity: mail not sent, no SMTP server configured", "subject", subject, "to_domain", domain.EmailDomain(to))
	return nil
}

// --- history -------------------------------------------------------------------

// event appends one auth event. A failure is logged, never returned: the
// outcome being recorded has already happened.
func (s *Service) event(ctx context.Context, user *domain.UserID, kind domain.AuthEventKind) {
	if err := s.events.AppendAuthEvent(ctx, domain.AuthEvent{
		UserID: user, Kind: kind, SourceAddress: sourceFrom(ctx), At: s.now().UTC(),
	}); err != nil {
		s.logger.Error("identity: auth event write failed", "kind", string(kind), "error", err)
	}
}
