package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
)

type fakeGoogle struct {
	assertion ports.FederatedAssertion
	err       error
}

func (f *fakeGoogle) AuthCodeURL(state, nonce, verifier, _ string) string {
	return "https://accounts.example.test/auth?state=" + state + "&nonce=" + nonce + "&verifier=" + verifier
}
func (f *fakeGoogle) Exchange(context.Context, string, string, string) (ports.FederatedAssertion, error) {
	return f.assertion, f.err
}

func (a *acct) withGoogle(f *fakeGoogle) *application.Service {
	return a.svc.WithGoogle(f, "https://accounts.example.test")
}

func TestGoogle_SignsInOnlyALinkedAccount(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "anas-own-password", "AGENT")
	user, err := a.st.GetUserByEmail(context.Background(), "ana@example.com")
	require.NoError(t, err)
	require.NoError(t, a.st.LinkFederatedIdentity(context.Background(), ports.FederatedIdentity{
		Issuer: "https://accounts.example.test", Subject: "google-sub-1", Email: "ana@example.com", UserID: user.ID, LinkedAt: a.now,
	}))
	g := &fakeGoogle{assertion: ports.FederatedAssertion{
		Issuer: "https://accounts.example.test", Subject: "google-sub-1", Email: "ana@example.com", EmailVerified: true,
	}}
	svc := a.withGoogle(g)

	begin, err := svc.BeginGoogleSignIn(context.Background(), "/calls", "")
	require.NoError(t, err)
	require.Contains(t, begin.URL, "state=")
	got, err := svc.CompleteGoogleSignIn(context.Background(), begin.State, "code")
	require.NoError(t, err)
	require.NotNil(t, got.Session.Access, "a linked person with one membership gets a token")
	assert.Equal(t, 1, len(got.Session.Memberships))
}

func TestGoogle_UnlinkedAccountIsRefusedLikeAWrongPassword(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "anas-own-password", "AGENT")
	g := &fakeGoogle{assertion: ports.FederatedAssertion{
		Issuer: "https://accounts.example.test", Subject: "not-linked", Email: "ana@example.com", EmailVerified: true,
	}}
	svc := a.withGoogle(g)
	begin, err := svc.BeginGoogleSignIn(context.Background(), "/", "")
	require.NoError(t, err)
	_, err = svc.CompleteGoogleSignIn(context.Background(), begin.State, "code")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
}

func TestGoogle_StateIsSingleUse(t *testing.T) {
	a := newAcct(t)
	g := &fakeGoogle{assertion: ports.FederatedAssertion{Issuer: "https://accounts.example.test", Subject: "s", Email: "x@example.test", EmailVerified: true}}
	svc := a.withGoogle(g)
	begin, err := svc.BeginGoogleSignIn(context.Background(), "/", "")
	require.NoError(t, err)
	_, _ = svc.CompleteGoogleSignIn(context.Background(), begin.State, "code")
	_, err = svc.CompleteGoogleSignIn(context.Background(), begin.State, "code")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "a replayed state cannot be used")
}

func TestGoogle_InvitationAcceptsOnlyTheInvitedEmail(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "invited@example.com", "", "AGENT")
	user, err := a.st.GetUserByEmail(context.Background(), "invited@example.com")
	require.NoError(t, err)
	raw, digest, err := application.NewSecret()
	require.NoError(t, err)
	require.NoError(t, a.st.CreateTicket(context.Background(), ports.ResetTicket{
		UserID: user.ID, Digest: digest, Purpose: domain.TicketInvite, ExpiresAt: a.now.Add(time.Hour), CreatedAt: a.now,
	}))

	g := &fakeGoogle{assertion: ports.FederatedAssertion{
		Issuer: "https://accounts.example.test", Subject: "google-sub-invite", Email: "invited@example.com", EmailVerified: true,
	}}
	svc := a.withGoogle(g)
	begin, err := svc.BeginGoogleSignIn(context.Background(), "/", raw)
	require.NoError(t, err)
	got, err := svc.CompleteGoogleSignIn(context.Background(), begin.State, "code")
	require.NoError(t, err)
	assert.True(t, got.Linked)
	require.NotNil(t, got.Session.Access)
	_, err = a.st.UserForFederatedIdentity(context.Background(), "https://accounts.example.test", "google-sub-invite")
	assert.NoError(t, err)

	// A second invited person is not linked to the wrong Google account;
	// the mismatch does not burn the invitation.
	a.member(t, a.tenant, "second@example.com", "", "AGENT")
	second, err := a.st.GetUserByEmail(context.Background(), "second@example.com")
	require.NoError(t, err)
	raw2, digest2, _ := application.NewSecret()
	require.NoError(t, a.st.CreateTicket(context.Background(), ports.ResetTicket{
		UserID: second.ID, Digest: digest2, Purpose: domain.TicketInvite, ExpiresAt: a.now.Add(time.Hour), CreatedAt: a.now,
	}))
	g.assertion.Email, g.assertion.Subject = "invited@example.com", "google-sub-invite-2"
	begin2, err := svc.BeginGoogleSignIn(context.Background(), "/", raw2)
	require.NoError(t, err)
	_, err = svc.CompleteGoogleSignIn(context.Background(), begin2.State, "code")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "Google's email must equal the invited email")

	g.assertion.Email, g.assertion.Subject = "second@example.com", "google-sub-second"
	begin3, err := svc.BeginGoogleSignIn(context.Background(), "/", raw2)
	require.NoError(t, err)
	_, err = svc.CompleteGoogleSignIn(context.Background(), begin3.State, "code")
	require.NoError(t, err)
}

func TestGoogle_LinkNeedsTheCurrentPasswordAndIsUnique(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "anas-own-password", "AGENT")
	user, err := a.st.GetUserByEmail(context.Background(), "ana@example.com")
	require.NoError(t, err)
	tc := &ports.TenantContext{TenantID: a.tenant.ID, UserID: user.ID}
	g := &fakeGoogle{assertion: ports.FederatedAssertion{
		Issuer: "https://accounts.example.test", Subject: "google-sub-ana", Email: "ana@example.com", EmailVerified: true,
	}}
	svc := a.withGoogle(g)

	_, err = svc.BeginGoogleLink(context.Background(), tc, "wrong", "/account")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	begin, err := svc.BeginGoogleLink(context.Background(), tc, "anas-own-password", "/account")
	require.NoError(t, err)
	email, err := svc.CompleteGoogleLink(context.Background(), tc, begin.State, "code")
	require.NoError(t, err)
	assert.Equal(t, "ana@example.com", email)

	begin2, err := svc.BeginGoogleLink(context.Background(), tc, "anas-own-password", "/account")
	require.NoError(t, err)
	_, err = svc.CompleteGoogleLink(context.Background(), tc, begin2.State, "code")
	assert.ErrorIs(t, err, ports.ErrAlreadyLinked)
}

func TestGoogle_DisabledAndUnsafeReturn(t *testing.T) {
	a := newAcct(t)
	_, err := a.svc.BeginGoogleSignIn(context.Background(), "/", "")
	assert.ErrorIs(t, err, ports.ErrNotConfigured)

	svc := a.withGoogle(&fakeGoogle{})
	_, err = svc.BeginGoogleSignIn(context.Background(), "https://evil.example", "")
	assert.ErrorIs(t, err, ports.ErrInvalidInput)
	_, err = svc.BeginGoogleSignIn(context.Background(), "//evil.example", "")
	assert.ErrorIs(t, err, ports.ErrInvalidInput)
	_, err = svc.CompleteGoogleSignIn(context.Background(), "unknown", "code")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
}

func TestGoogle_UnverifiedEmailIsRefused(t *testing.T) {
	a := newAcct(t)
	g := &fakeGoogle{assertion: ports.FederatedAssertion{
		Issuer: "https://accounts.example.test", Subject: "s", Email: "ana@example.com", EmailVerified: false,
	}}
	svc := a.withGoogle(g)
	begin, err := svc.BeginGoogleSignIn(context.Background(), "/", "")
	require.NoError(t, err)
	_, err = svc.CompleteGoogleSignIn(context.Background(), begin.State, "code")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
}

func TestGoogle_ExchangeFailureIsRefused(t *testing.T) {
	a := newAcct(t)
	g := &fakeGoogle{err: errors.New("bad code")}
	svc := a.withGoogle(g)
	begin, err := svc.BeginGoogleSignIn(context.Background(), "/", "")
	require.NoError(t, err)
	_, err = svc.CompleteGoogleSignIn(context.Background(), begin.State, "bad")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
}

func TestGoogle_LinkedEmailAndPermissionChecks(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "anas-own-password", "AGENT")
	user, err := a.st.GetUserByEmail(context.Background(), "ana@example.com")
	require.NoError(t, err)
	g := &fakeGoogle{assertion: ports.FederatedAssertion{
		Issuer: "https://accounts.example.test", Subject: "google-sub", Email: "ana@example.com", EmailVerified: true,
	}}
	svc := a.withGoogle(g)
	tc := &ports.TenantContext{TenantID: a.tenant.ID, UserID: user.ID}
	begin, err := svc.BeginGoogleLink(context.Background(), tc, "anas-own-password", "/account")
	require.NoError(t, err)
	_, err = svc.CompleteGoogleLink(context.Background(), tc, begin.State, "code")
	require.NoError(t, err)

	email, err := svc.GoogleLinked(context.Background(), tc)
	require.NoError(t, err)
	assert.Equal(t, "ana@example.com", email)

	_, err = svc.GoogleLinked(context.Background(), nil)
	assert.ErrorIs(t, err, ports.ErrPermissionDenied)
	_, err = svc.BeginGoogleLink(context.Background(), nil, "x", "/")
	assert.ErrorIs(t, err, ports.ErrPermissionDenied)
}
