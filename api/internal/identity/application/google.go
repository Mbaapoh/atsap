package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
)

// GoogleIssuer is the issuer the installation-wide Google client is
// built for. A test or a compatible provider may use another.
const GoogleIssuer = "https://accounts.google.com"

// googleStateLifetime bounds how long an authorization request may take.
const googleStateLifetime = 10 * time.Minute

// WithGoogle enables "Sign in with Google" (D-67) behind the given
// protocol adapter. Without it, discovery reports Google unavailable and
// every Google RPC refuses.
func (s *Service) WithGoogle(a ports.GoogleAuthenticator, issuer string) *Service {
	clone := *s
	clone.google = a
	clone.googleIssuer = issuer
	if clone.googleIssuer == "" {
		clone.googleIssuer = GoogleIssuer
	}
	return &clone
}

// GoogleEnabled reports whether the installation has a Google client.
func (s *Service) GoogleEnabled() bool { return s.google != nil }

// GoogleRedirect is where the browser must be sent, and the state Google
// will return. State is not secret; the verifier and nonce stay server-
// side.
type GoogleRedirect struct {
	URL   string
	State string
}

// BeginGoogleSignIn starts an installation-wide Google sign-in. When
// inviteSecret is set the callback may instead accept an invitation, but
// only if Google's verified email equals the invited email (D-67).
func (s *Service) BeginGoogleSignIn(ctx context.Context, returnTo, inviteSecret string) (GoogleRedirect, error) {
	if !s.GoogleEnabled() {
		return GoogleRedirect{}, ports.ErrNotConfigured
	}
	purpose, inviteDigest := ports.OAuthStateSignIn, ""
	if inviteSecret != "" {
		purpose, inviteDigest = ports.OAuthStateInvite, DigestSecret(inviteSecret)
	}
	return s.beginGoogle(ctx, purpose, nil, inviteDigest, returnTo)
}

// BeginGoogleLink starts linking Google to the signed-in person, after
// re-entering the current password so a stolen session cannot attach an
// attacker's account (D-67).
func (s *Service) BeginGoogleLink(ctx context.Context, tc *ports.TenantContext, currentPassword, returnTo string) (GoogleRedirect, error) {
	if !s.GoogleEnabled() {
		return GoogleRedirect{}, ports.ErrNotConfigured
	}
	if tc == nil || tc.UserID.IsZero() {
		return GoogleRedirect{}, ports.ErrPermissionDenied
	}
	user, err := s.users.GetUser(ctx, tc.UserID)
	if err != nil {
		return GoogleRedirect{}, err
	}
	if user.Status != domain.UserActive || user.Password == nil {
		return GoogleRedirect{}, ports.ErrInvalidCredentials
	}
	ok, err := s.hasher.Verify(currentPassword, user.Password.Hash)
	if err != nil || !ok {
		return GoogleRedirect{}, ports.ErrInvalidCredentials
	}
	return s.beginGoogle(ctx, ports.OAuthStateLink, &user.ID, "", returnTo)
}

func (s *Service) beginGoogle(ctx context.Context, purpose ports.OAuthStatePurpose, userID *domain.UserID, inviteDigest, returnTo string) (GoogleRedirect, error) {
	state, stateDigest, err := NewSecret()
	if err != nil {
		return GoogleRedirect{}, err
	}
	verifier, _, err := NewSecret()
	if err != nil {
		return GoogleRedirect{}, err
	}
	nonce, _, err := NewSecret()
	if err != nil {
		return GoogleRedirect{}, err
	}
	if returnTo == "" {
		returnTo = "/"
	}
	if !safeReturnTo(returnTo) {
		return GoogleRedirect{}, fmt.Errorf("%w: return_to must be a local path", ports.ErrInvalidInput)
	}
	now := s.now().UTC()
	if err := s.oauth.CreateOAuthState(ctx, ports.OAuthState{
		StateDigest: stateDigest, Purpose: purpose, UserID: userID,
		InviteDigest: inviteDigest, ReturnTo: returnTo,
		Verifier: verifier, Nonce: nonce, ExpiresAt: now.Add(googleStateLifetime),
	}); err != nil {
		return GoogleRedirect{}, err
	}
	return GoogleRedirect{URL: s.google.AuthCodeURL(state, nonce, verifier, returnTo), State: state}, nil
}

// GoogleResult is the outcome of an external sign-in. Linked reports
// whether this callback created a new link (an invitation), which the
// console can use to explain the outcome.
type GoogleResult struct {
	Session SessionResult
	Linked  bool
}

// CompleteGoogleSignIn validates the provider callback and either signs
// the linked person in or accepts an invitation. An unlinked account is
// refused exactly like a wrong password (D-67).
func (s *Service) CompleteGoogleSignIn(ctx context.Context, state, code string) (GoogleResult, error) {
	if !s.GoogleEnabled() {
		return GoogleResult{}, ports.ErrNotConfigured
	}
	st, err := s.oauth.ConsumeOAuthState(ctx, DigestSecret(state), s.now().UTC())
	if errors.Is(err, ports.ErrNotFound) {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	if err != nil {
		return GoogleResult{}, err
	}
	if st.Purpose != ports.OAuthStateSignIn && st.Purpose != ports.OAuthStateInvite {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	assertion, err := s.google.Exchange(ctx, code, st.Verifier, st.Nonce)
	if err != nil {
		return GoogleResult{}, fmt.Errorf("%w: google exchange: %v", ports.ErrInvalidCredentials, err)
	}
	if !assertion.EmailVerified || assertion.Subject == "" {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}

	switch st.Purpose {
	case ports.OAuthStateInvite:
		return s.completeGoogleInvite(ctx, st, assertion)
	default:
		userID, err := s.federated.UserForFederatedIdentity(ctx, assertion.Issuer, assertion.Subject)
		if errors.Is(err, ports.ErrNotFound) {
			return GoogleResult{}, ports.ErrInvalidCredentials
		}
		if err != nil {
			return GoogleResult{}, err
		}
		return s.googleSession(ctx, userID)
	}
}

func (s *Service) completeGoogleInvite(ctx context.Context, st ports.OAuthState, assertion ports.FederatedAssertion) (GoogleResult, error) {
	now := s.now().UTC()
	userID, purpose, err := s.tickets.TicketUser(ctx, st.InviteDigest, now)
	if errors.Is(err, ports.ErrNotFound) || purpose != domain.TicketInvite {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	if err != nil {
		return GoogleResult{}, err
	}
	user, err := s.users.GetUser(ctx, userID)
	if err != nil {
		return GoogleResult{}, err
	}
	if user.Status != domain.UserActive || !strings.EqualFold(user.Email, assertion.Email) {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	// Consume only now: a mismatched Google account must not burn the
	// invitation. The UPDATE is still atomic against a concurrent redeem.
	if _, _, err := s.tickets.ConsumeTicket(ctx, st.InviteDigest, now); err != nil {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	if err := s.linkGoogle(ctx, user.ID, assertion); err != nil {
		return GoogleResult{}, err
	}
	s.event(ctx, &user.ID, domain.EventInvitationAccepted)
	res, err := s.googleSession(ctx, user.ID)
	if err != nil {
		return GoogleResult{}, err
	}
	res.Linked = true
	return res, nil
}

// CompleteGoogleLink attaches Google to the signed-in person. The state
// must have been issued to the same person by BeginGoogleLink.
func (s *Service) CompleteGoogleLink(ctx context.Context, tc *ports.TenantContext, state, code string) (string, error) {
	if !s.GoogleEnabled() {
		return "", ports.ErrNotConfigured
	}
	if tc == nil || tc.UserID.IsZero() {
		return "", ports.ErrPermissionDenied
	}
	st, err := s.oauth.ConsumeOAuthState(ctx, DigestSecret(state), s.now().UTC())
	if errors.Is(err, ports.ErrNotFound) {
		return "", ports.ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if st.Purpose != ports.OAuthStateLink || st.UserID == nil || *st.UserID != tc.UserID {
		return "", ports.ErrInvalidCredentials
	}
	assertion, err := s.google.Exchange(ctx, code, st.Verifier, st.Nonce)
	if err != nil {
		return "", fmt.Errorf("%w: google exchange: %v", ports.ErrInvalidCredentials, err)
	}
	if !assertion.EmailVerified || assertion.Subject == "" {
		return "", ports.ErrInvalidCredentials
	}
	if err := s.linkGoogle(ctx, tc.UserID, assertion); err != nil {
		return "", err
	}
	s.event(ctx, &tc.UserID, domain.EventGoogleLinked)
	return assertion.Email, nil
}

func (s *Service) linkGoogle(ctx context.Context, user domain.UserID, assertion ports.FederatedAssertion) error {
	err := s.federated.LinkFederatedIdentity(ctx, ports.FederatedIdentity{
		Issuer: assertion.Issuer, Subject: assertion.Subject, Email: assertion.Email,
		UserID: user, LinkedAt: s.now().UTC(),
	})
	if errors.Is(err, ports.ErrAlreadyLinked) {
		return ports.ErrAlreadyLinked
	}
	return err
}

func (s *Service) googleSession(ctx context.Context, userID domain.UserID) (GoogleResult, error) {
	user, err := s.users.GetUser(ctx, userID)
	if err != nil {
		return GoogleResult{}, err
	}
	if user.Status != domain.UserActive {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	memberships, err := s.activeMemberships(ctx, userID)
	if err != nil {
		return GoogleResult{}, err
	}
	if len(memberships) == 0 {
		return GoogleResult{}, ports.ErrInvalidCredentials
	}
	res, err := s.startSession(ctx, user, memberships)
	if err != nil {
		return GoogleResult{}, err
	}
	return GoogleResult{Session: res}, nil
}

// GoogleLinked returns the person's linked Google email, or ErrNotFound.
func (s *Service) GoogleLinked(ctx context.Context, tc *ports.TenantContext) (string, error) {
	if tc == nil || tc.UserID.IsZero() {
		return "", ports.ErrPermissionDenied
	}
	link, err := s.federated.FederatedIdentityForUser(ctx, s.googleIssuer, tc.UserID)
	if err != nil {
		return "", err
	}
	return link.Email, nil
}

// safeReturnTo keeps the post-sign-in redirect inside the console: an
// absolute URL would turn the callback into an open redirect.
func safeReturnTo(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == "" && strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//")
}
