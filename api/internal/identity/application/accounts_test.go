package application_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/memstore"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Revision 2 behaviour (identity-accounts-and-sessions tasks 4.1–4.7),
// through the real service over the in-memory store, with a clock the
// test moves and an outbox that captures mail.

type acct struct {
	svc    *application.Service
	st     *memstore.Store
	mail   *memstore.Outbox
	logs   *bytes.Buffer
	now    time.Time
	tenant domain.Tenant
}

func newAcct(t *testing.T) *acct {
	t.Helper()
	a := &acct{st: memstore.New(), mail: &memstore.Outbox{}, logs: &bytes.Buffer{}, now: svcNow}
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)
	logger := slog.New(slog.NewJSONHandler(a.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a.svc = application.NewService(application.StoresOf(a.st), a.st, application.NewPasswordHasher(), issuer, logger).
		WithMailer(a.mail, "https://console.example.test").
		WithClock(func() time.Time { return a.now })
	a.tenant = a.newTenant(t, "Acme")
	return a
}

func (a *acct) newTenant(t *testing.T, name string) domain.Tenant {
	t.Helper()
	tenant, err := a.svc.ProvisionTenant(context.Background(), application.SystemActor(), name, "EU")
	require.NoError(t, err)
	return tenant
}

func (a *acct) member(t *testing.T, tenant domain.Tenant, email, password, role string) domain.Principal {
	t.Helper()
	p, err := a.svc.ProvisionPrincipal(context.Background(), application.SystemActor(), tenant.ID, email, "", password, role)
	require.NoError(t, err)
	return p
}

func (a *acct) signIn(t *testing.T, email, password string) application.SessionResult {
	t.Helper()
	res, err := a.svc.SignIn(context.Background(), email, password)
	require.NoError(t, err)
	return res
}

func (a *acct) validate(token string) (*ports.TenantContext, error) {
	return a.svc.ValidateToken(context.Background(), token)
}

var linkSecret = regexp.MustCompile(`#([A-Za-z0-9_-]{20,})`)

func (a *acct) secretMailedTo(t *testing.T, email string) string {
	t.Helper()
	m, ok := a.mail.Last(email)
	require.True(t, ok, "a mail was sent to %s", email)
	match := linkSecret.FindStringSubmatch(m.Body)
	require.Len(t, match, 2, "the mail carries a link with a secret")
	return match[1]
}

// --- 4.1 provisioning by email ------------------------------------------

func TestProvision_ExistingAndNewPersonLookAlike(t *testing.T) {
	a := newAcct(t)
	other := a.newTenant(t, "Other")
	a.member(t, other, "ana@example.com", "anas-own-password", "AGENT")

	ctx := context.Background()
	existing, err := a.svc.ProvisionPrincipal(ctx, application.SystemActor(), a.tenant.ID, " ANA@example.com ", "Ana", "admin-chosen-pw", "AGENT")
	require.NoError(t, err)
	fresh, err := a.svc.ProvisionPrincipal(ctx, application.SystemActor(), a.tenant.ID, "bob@example.com", "Ana", "admin-chosen-pw", "AGENT")
	require.NoError(t, err)

	strip := func(p domain.Principal) domain.Principal {
		p.ID, p.UserID, p.Email = shareddomain.PrincipalID{}, domain.UserID{}, ""
		return p
	}
	assert.Equal(t, strip(fresh), strip(existing), "identical apart from identifiers and the email the admin typed")
	assert.Equal(t, "ana@example.com", existing.Email, "stored normalized")

	_, err = a.svc.AuthenticateUser(ctx, a.tenant.ID, "ana@example.com", "anas-own-password")
	assert.NoError(t, err, "the existing person keeps their own password in the new tenant")
	_, err = a.svc.AuthenticateUser(ctx, a.tenant.ID, "ana@example.com", "admin-chosen-pw")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "the administrator's password was not applied")

	for _, e := range a.st.Audit {
		if e.TenantID != a.tenant.ID {
			continue
		}
		raw, _ := json.Marshal(e)
		assert.NotContains(t, string(raw), other.ID.String(), "no audit row in this tenant mentions the other tenant")
		assert.NotContains(t, string(raw), "Other")
	}
}

func TestProvision_SecondMembershipRefused(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "anas-own-password", "AGENT")
	_, err := a.svc.ProvisionPrincipal(context.Background(), application.SystemActor(), a.tenant.ID, "Ana@Example.com", "", "", "AGENT")
	assert.ErrorIs(t, err, ports.ErrAlreadyMember)
}

func TestProvision_InvalidEmailAndWeakPassword(t *testing.T) {
	a := newAcct(t)
	_, err := a.svc.ProvisionPrincipal(context.Background(), application.SystemActor(), a.tenant.ID, "not-an-email", "", "", "AGENT")
	assert.ErrorIs(t, err, domain.ErrInvalidEmail)
	_, err = a.svc.ProvisionPrincipal(context.Background(), application.SystemActor(), a.tenant.ID, "x@example.com", "", "short", "AGENT")
	assert.ErrorIs(t, err, domain.ErrPasswordTooShort)
}

func TestInvitation_SetsFirstPasswordOnceAndExpires(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "new@example.com", "", "AGENT")
	m, ok := a.mail.Last("new@example.com")
	require.True(t, ok)
	assert.Contains(t, m.Body, "https://console.example.test/invite#")

	_, err := a.svc.SignIn(ctx, "new@example.com", "anything-at-all")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "no password until one is set")

	secret := a.secretMailedTo(t, "new@example.com")
	require.NoError(t, a.svc.CompletePasswordReset(ctx, secret, "my-first-password"))
	a.signIn(t, "new@example.com", "my-first-password")
	assert.ErrorIs(t, a.svc.CompletePasswordReset(ctx, secret, "again-password"), ports.ErrInvalidCredentials, "single use")

	a.member(t, a.tenant, "late@example.com", "", "AGENT")
	late := a.secretMailedTo(t, "late@example.com")
	a.now = a.now.Add(72*time.Hour + time.Second)
	assert.ErrorIs(t, a.svc.CompletePasswordReset(ctx, late, "too-late-password"), ports.ErrInvalidCredentials, "expired after 72 h")
}

// --- 4.2 sign-in and validation -------------------------------------------

func TestSignIn_FailuresIdenticalAndHashAlwaysRuns(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	solo := a.st // a person with no membership at all
	require.NoError(t, solo.CreateUser(ctx, domain.User{ID: domain.NewUserID(), Email: "lonely@example.com", Status: domain.UserActive}))

	time1 := func(f func() error) (time.Duration, error) {
		start := time.Now()
		err := f()
		return time.Since(start), err
	}
	wrongDur, wrongErr := time1(func() error { _, err := a.svc.SignIn(ctx, "ana@example.com", "wrong password here"); return err })
	unknownDur, unknownErr := time1(func() error { _, err := a.svc.SignIn(ctx, "ghost@example.com", "wrong password here"); return err })
	_, garbageErr := a.svc.SignIn(ctx, "not an email", "whatever-password")
	_, noPwErr := a.svc.SignIn(ctx, "lonely@example.com", "whatever-password")

	for _, err := range []error{wrongErr, unknownErr, garbageErr, noPwErr} {
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	}
	assert.Greater(t, unknownDur, wrongDur/3,
		"an unknown email does the same Argon2id work as a wrong password (wrong %s, unknown %s)", wrongDur, unknownDur)
}

func TestValidate_SessionUserAndMembershipAreLive(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	other := a.newTenant(t, "Other")
	p := a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	a.member(t, other, "ana@example.com", "", "AGENT")

	res := a.signIn(t, "ana@example.com", "correct horse battery")
	assert.Nil(t, res.Access, "two memberships: no token until one is chosen")
	require.Len(t, res.Memberships, 2)

	acme, err := a.svc.RefreshSession(ctx, res.RefreshSecret, &a.tenant.ID)
	require.NoError(t, err)
	tc, err := a.validate(acme.Access.Token)
	require.NoError(t, err)
	assert.Equal(t, a.tenant.ID, tc.TenantID)
	assert.False(t, tc.SessionID.IsZero())

	otherRes, err := a.svc.RefreshSession(ctx, acme.RefreshSecret, &other.ID)
	require.NoError(t, err)

	t.Run("a disabled membership loses only its tenant", func(t *testing.T) {
		require.NoError(t, a.svc.SetPrincipalStatus(ctx, application.SystemActor(), a.tenant.ID, p.ID, domain.PrincipalDisabled))
		_, err := a.validate(acme.Access.Token)
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
		_, err = a.validate(otherRes.Access.Token)
		assert.NoError(t, err, "the other tenant still works")
		require.NoError(t, a.svc.SetPrincipalStatus(ctx, application.SystemActor(), a.tenant.ID, p.ID, domain.PrincipalActive))
	})

	t.Run("signing out ends the session at once", func(t *testing.T) {
		tc, err := a.validate(otherRes.Access.Token)
		require.NoError(t, err)
		require.NoError(t, a.svc.SignOut(ctx, tc))
		_, err = a.validate(otherRes.Access.Token)
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "refused although unexpired")
		_, err = a.svc.RefreshSession(ctx, otherRes.RefreshSecret, nil)
		assert.ErrorIs(t, err, ports.ErrSessionNotUsable)
	})

	t.Run("a disabled person loses every tenant", func(t *testing.T) {
		res := a.signIn(t, "ana@example.com", "correct horse battery")
		r, err := a.svc.RefreshSession(ctx, res.RefreshSecret, &other.ID)
		require.NoError(t, err)
		require.NoError(t, a.svc.SetUserStatus(ctx, application.SystemActor(), a.tenant.ID, p.UserID, domain.UserDisabled))
		_, err = a.validate(r.Access.Token)
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
		_, err = a.svc.RefreshSession(ctx, r.RefreshSecret, nil)
		assert.ErrorIs(t, err, ports.ErrSessionNotUsable)
		_, err = a.svc.SignIn(ctx, "ana@example.com", "correct horse battery")
		assert.ErrorIs(t, err, ports.ErrInvalidCredentials)
	})
}

func TestAuthenticateUser_DeprecatedPathHasNoSession(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	tok, err := a.svc.AuthenticateUser(context.Background(), a.tenant.ID, "ANA@example.com", "correct horse battery")
	require.NoError(t, err)
	tc, err := a.validate(tok.Token)
	require.NoError(t, err)
	assert.True(t, tc.SessionID.IsZero())
	_, err = a.svc.AuthenticateUser(context.Background(), a.newTenant(t, "Not mine").ID, "ana@example.com", "correct horse battery")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "no membership of that tenant")
}

// --- 4.3 refresh, switch, expiry --------------------------------------------

func TestRefresh_RotatesAndReuseRevokes(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	first := a.signIn(t, "ana@example.com", "correct horse battery")
	require.NotNil(t, first.Access, "one membership: signed straight in")

	second, err := a.svc.RefreshSession(ctx, first.RefreshSecret, nil)
	require.NoError(t, err)
	assert.NotEqual(t, first.RefreshSecret, second.RefreshSecret)

	_, err = a.svc.RefreshSession(ctx, first.RefreshSecret, nil)
	assert.ErrorIs(t, err, ports.ErrSessionNotUsable, "a replayed secret is refused")
	_, err = a.svc.RefreshSession(ctx, second.RefreshSecret, nil)
	assert.ErrorIs(t, err, ports.ErrSessionNotUsable, "and the whole session is revoked")
	_, err = a.validate(second.Access.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "its access tokens too")
}

func TestRefresh_IdleAndAbsoluteExpiry(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")

	idle := a.signIn(t, "ana@example.com", "correct horse battery")
	a.now = a.now.Add(30 * time.Minute)
	_, err := a.svc.RefreshSession(ctx, idle.RefreshSecret, nil)
	assert.ErrorIs(t, err, ports.ErrSessionNotUsable, "30 minutes idle")

	active := a.signIn(t, "ana@example.com", "correct horse battery")
	secret := active.RefreshSecret
	for elapsed := time.Duration(0); elapsed < 12*time.Hour-25*time.Minute; elapsed += 25 * time.Minute {
		a.now = a.now.Add(25 * time.Minute)
		r, err := a.svc.RefreshSession(ctx, secret, nil)
		require.NoError(t, err)
		secret = r.RefreshSecret
	}
	a.now = a.now.Add(25 * time.Minute)
	_, err = a.svc.RefreshSession(ctx, secret, nil)
	assert.ErrorIs(t, err, ports.ErrSessionNotUsable, "12 hours absolute, however active")
}

func TestRefresh_SwitchingNeverWidens(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	other := a.newTenant(t, "Other")
	stranger := a.newTenant(t, "Stranger")
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	a.member(t, other, "ana@example.com", "", "AGENT")

	res := a.signIn(t, "ana@example.com", "correct horse battery")
	r1, err := a.svc.RefreshSession(ctx, res.RefreshSecret, &other.ID)
	require.NoError(t, err)
	tc, err := a.validate(r1.Access.Token)
	require.NoError(t, err)
	assert.Equal(t, other.ID, tc.TenantID, "the token names the chosen tenant only")

	_, notMember := a.svc.RefreshSession(ctx, r1.RefreshSecret, &stranger.ID)
	missing := shareddomain.NewTenantID()
	_, noTenant := a.svc.RefreshSession(ctx, r1.RefreshSecret, &missing)
	assert.ErrorIs(t, notMember, ports.ErrPermissionDenied)
	assert.Equal(t, notMember.Error(), noTenant.Error(), "not a member and no such tenant look alike")

	_, err = a.svc.RefreshSession(ctx, r1.RefreshSecret, nil)
	assert.NoError(t, err, "a refused switch leaves the session usable")
}

// --- 4.4 throttling -----------------------------------------------------------

func TestThrottle_SlowsWithoutLockingOut(t *testing.T) {
	a := newAcct(t)
	ctx := application.WithSource(context.Background(), "203.0.113.7")
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")

	for i := 0; i < 5; i++ {
		_, err := a.svc.SignIn(ctx, "ana@example.com", "wrong password here")
		require.ErrorIs(t, err, ports.ErrInvalidCredentials, "attempt %d", i+1)
	}
	_, err := a.svc.SignIn(ctx, "ana@example.com", "correct horse battery")
	var te *ports.ThrottledError
	require.ErrorAs(t, err, &te, "the 6th is refused even with the right password")
	assert.Equal(t, 30*time.Second, te.RetryAfter)

	a.now = a.now.Add(31 * time.Second)
	_, err = a.svc.SignIn(context.Background(), "ana@example.com", "correct horse battery")
	require.NoError(t, err, "after the delay the owner gets in")
	for i := 0; i < 4; i++ {
		_, err := a.svc.SignIn(context.Background(), "ana@example.com", "wrong password here")
		require.ErrorIs(t, err, ports.ErrInvalidCredentials, "the count was cleared by the success")
	}
}

func TestThrottle_UnknownAndKnownAlike(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	run := func(email string) []string {
		var out []string
		for i := 0; i < 7; i++ {
			_, err := a.svc.SignIn(context.Background(), email, "wrong password here")
			out = append(out, err.Error())
		}
		return out
	}
	assert.Equal(t, run("ana@example.com"), run("ghost@example.com"))
}

// --- 4.5 password change and recovery ----------------------------------------

func TestChangePassword_EndsOtherSessionsKeepsThisOne(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	here := a.signIn(t, "ana@example.com", "correct horse battery")
	there := a.signIn(t, "ana@example.com", "correct horse battery")
	tc, err := a.validate(here.Access.Token)
	require.NoError(t, err)

	assert.ErrorIs(t, a.svc.ChangePassword(ctx, tc, "not the password", "brand-new-password"), ports.ErrInvalidCredentials)
	require.NoError(t, a.svc.ChangePassword(ctx, tc, "correct horse battery", "brand-new-password"))

	_, err = a.validate(here.Access.Token)
	assert.NoError(t, err, "the session that changed it continues")
	_, err = a.validate(there.Access.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "the other browser is signed out")
	a.signIn(t, "ana@example.com", "brand-new-password")
}

func TestReset_ByEmail(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	before := a.signIn(t, "ana@example.com", "correct horse battery")

	require.NoError(t, a.svc.RequestPasswordReset(ctx, "Ana@Example.com"))
	require.NoError(t, a.svc.RequestPasswordReset(ctx, "ghost@example.com"), "an unknown email gets the same answer")
	_, sentToGhost := a.mail.Last("ghost@example.com")
	assert.False(t, sentToGhost, "and no mail")

	secret := a.secretMailedTo(t, "ana@example.com")
	m, _ := a.mail.Last("ana@example.com")
	assert.Contains(t, m.Body, "https://console.example.test/reset#")
	require.NoError(t, a.svc.CompletePasswordReset(ctx, secret, "reset-new-password"))

	a.signIn(t, "ana@example.com", "reset-new-password")
	_, err := a.svc.SignIn(ctx, "ana@example.com", "correct horse battery")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "the old password is gone")
	_, err = a.validate(before.Access.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "every earlier session has ended")
	assert.ErrorIs(t, a.svc.CompletePasswordReset(ctx, secret, "second-use-password"), ports.ErrInvalidCredentials, "single use")

	require.NoError(t, a.svc.RequestPasswordReset(ctx, "ana@example.com"))
	stale := a.secretMailedTo(t, "ana@example.com")
	a.now = a.now.Add(30 * time.Minute)
	assert.ErrorIs(t, a.svc.CompletePasswordReset(ctx, stale, "too-late-password"), ports.ErrInvalidCredentials, "expired at 30 minutes")
}

func TestReset_RateLimitedPerPerson(t *testing.T) {
	a := newAcct(t)
	a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")
	for i := 0; i < 5; i++ {
		require.NoError(t, a.svc.RequestPasswordReset(context.Background(), "ana@example.com"))
	}
	n := 0
	for _, m := range a.mail.Sent {
		if m.To == "ana@example.com" {
			n++
		}
	}
	assert.Equal(t, 3, n, "at most three reset mails an hour")
}

func TestAdminReset_AuthorityAndForcedChange(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	other := a.newTenant(t, "Other")
	admin := a.member(t, a.tenant, "admin@example.com", "admin-password-1", "TENANT_ADMIN")
	solo := a.member(t, a.tenant, "solo@example.com", "solo-password-1", "AGENT")
	shared := a.member(t, a.tenant, "shared@example.com", "shared-password-1", "AGENT")
	a.member(t, other, "shared@example.com", "", "AGENT")

	adminRes := a.signIn(t, "admin@example.com", "admin-password-1")
	adminTC, err := a.validate(adminRes.Access.Token)
	require.NoError(t, err)
	actor := application.PrincipalActor(admin.ID, nil)

	err = a.svc.ResetPrincipalPassword(ctx, adminTC, actor, a.tenant.ID, shared.ID, "temporary-pass-1")
	assert.ErrorIs(t, err, ports.ErrPermissionDenied, "a shared person cannot be reset from one tenant")
	a.signIn(t, "shared@example.com", "shared-password-1")

	soloBefore := a.signIn(t, "solo@example.com", "solo-password-1")
	require.NoError(t, a.svc.ResetPrincipalPassword(ctx, adminTC, actor, a.tenant.ID, solo.ID, "temporary-pass-1"))
	_, err = a.validate(soloBefore.Access.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "earlier sessions ended")

	res := a.signIn(t, "solo@example.com", "temporary-pass-1")
	assert.True(t, res.PasswordChangeOnly)
	tc, err := a.validate(res.Access.Token)
	require.NoError(t, err)
	assert.True(t, tc.PasswordChangeOnly, "the token is confined to changing the password")
	require.NoError(t, a.svc.ChangePassword(ctx, tc, "temporary-pass-1", "solo-own-password"))
	tc, err = a.validate(res.Access.Token)
	require.NoError(t, err)
	assert.False(t, tc.PasswordChangeOnly, "until it is changed")

	var resets int
	for _, e := range a.st.Audit {
		if e.Action == "principal.password.reset" {
			resets++
			raw, _ := json.Marshal(e)
			assert.NotContains(t, string(raw), "temporary-pass-1")
		}
	}
	assert.Equal(t, 1, resets, "one audit row, without the password")

	// Platform scope may reset the shared person.
	platform := &ports.TenantContext{TenantID: a.tenant.ID, Scopes: []string{domain.ScopeSystem}}
	require.NoError(t, a.svc.ResetPrincipalPassword(ctx, platform, application.SystemActor(), a.tenant.ID, shared.ID, "temporary-pass-2"))
}

func TestRecoverByEmail_OffNetwork(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	other := a.newTenant(t, "Other")
	a.member(t, a.tenant, "admin@example.com", "forgotten-password", "TENANT_ADMIN")
	a.member(t, other, "admin@example.com", "", "AGENT")
	signedIn := a.signIn(t, "admin@example.com", "forgotten-password")
	before, err := a.svc.RefreshSession(ctx, signedIn.RefreshSecret, &a.tenant.ID)
	require.NoError(t, err)

	temporary, err := a.svc.RecoverByEmail(ctx, "Admin@Example.com")
	require.NoError(t, err)
	assert.Len(t, temporary, 20)
	assert.NotContains(t, a.logs.String(), temporary, "the temporary password is never logged")
	_, err = a.validate(before.Access.Token)
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "sessions revoked")
	res := a.signIn(t, "admin@example.com", temporary)
	assert.True(t, res.PasswordChangeOnly, "must change at next sign-in")

	tenants := map[shareddomain.TenantID]bool{}
	for _, e := range a.st.Audit {
		if e.Action == "principal.password.reset" {
			assert.Equal(t, domain.ActorSystem, e.ActorType)
			tenants[e.TenantID] = true
		}
	}
	assert.Len(t, tenants, 2, "one system audit row in each of the person's tenants")

	_, err = a.svc.RecoverByEmail(ctx, "nobody@example.com")
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

// --- 4.6 auth events ----------------------------------------------------------

func TestAuthEvents_OnePerOutcomeAndNoSecrets(t *testing.T) {
	a := newAcct(t)
	ctx := application.WithSource(context.Background(), "198.51.100.9")
	p := a.member(t, a.tenant, "ana@example.com", "correct horse battery", "AGENT")

	res, err := a.svc.SignIn(ctx, "ana@example.com", "correct horse battery")
	require.NoError(t, err)
	_, _ = a.svc.SignIn(ctx, "ana@example.com", "a wrong password attempt")
	_, _ = a.svc.SignIn(ctx, "ghost@example.com", "a wrong password attempt")
	require.NoError(t, a.svc.RequestPasswordReset(ctx, "ana@example.com"))

	kinds := map[domain.AuthEventKind]int{}
	var unknownFailure bool
	for _, e := range a.st.Events {
		kinds[e.Kind]++
		if e.Kind == domain.EventSignInFailed && e.UserID == nil {
			unknownFailure = true
		}
		assert.Equal(t, "198.51.100.9", e.SourceAddress)
	}
	assert.Equal(t, 1, kinds[domain.EventSignInSucceeded])
	assert.Equal(t, 2, kinds[domain.EventSignInFailed])
	assert.Equal(t, 1, kinds[domain.EventResetRequested])
	assert.True(t, unknownFailure, "an unknown email is recorded without a person")

	raw, err := json.Marshal(a.st.Events)
	require.NoError(t, err)
	for _, secret := range []string{"correct horse battery", "a wrong password attempt", res.RefreshSecret, res.Access.Token} {
		assert.NotContains(t, string(raw), secret)
	}
	events, err := a.st.ListAuthEvents(context.Background(), p.UserID, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, events)
}

// --- 4.7 discovery and SSO readiness --------------------------------------------

func TestDiscovery_AndEnforcedSSO(t *testing.T) {
	a := newAcct(t)
	ctx := context.Background()
	a.member(t, a.tenant, "ana@acme.com", "correct horse battery", "AGENT")
	pwless := domain.User{ID: domain.NewUserID(), Email: "nopw@example.com", Status: domain.UserActive}
	require.NoError(t, a.st.CreateUser(ctx, pwless))

	m, err := a.svc.DiscoverSignIn(ctx, "ana@acme.com")
	require.NoError(t, err)
	assert.Nil(t, m.Provider, "password by default")

	provider := domain.IdentityProvider{ID: domain.NewIdentityProviderID(), TenantID: a.tenant.ID,
		Kind: domain.ProviderOIDC, DisplayName: "Acme Entra", Issuer: "https://login.example/acme", Mode: domain.ProviderEnforced}
	require.NoError(t, a.st.CreateProvider(ctx, provider))
	require.NoError(t, a.st.ClaimDomain(ctx, domain.DomainClaim{Domain: "acme.com", ProviderID: provider.ID}))

	m, err = a.svc.DiscoverSignIn(ctx, "ana@acme.com")
	require.NoError(t, err)
	assert.Nil(t, m.Provider, "an unverified claim changes nothing")
	a.signIn(t, "ana@acme.com", "correct horse battery")

	require.NoError(t, a.st.VerifyDomain(ctx, "acme.com", a.now))
	known, err := a.svc.DiscoverSignIn(ctx, "ana@acme.com")
	require.NoError(t, err)
	unknown, err := a.svc.DiscoverSignIn(ctx, "nobody@acme.com")
	require.NoError(t, err)
	require.NotNil(t, known.Provider)
	assert.Equal(t, known, unknown, "the answer depends on the domain, never on the account")

	_, err = a.svc.SignIn(ctx, "ana@acme.com", "correct horse battery")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "password sign-in refused for an enforced domain")
	require.NoError(t, a.svc.RequestPasswordReset(ctx, "ana@acme.com"))
	_, mailed := a.mail.Last("ana@acme.com")
	assert.False(t, mailed, "and no reset link")

	err = a.st.ClaimDomain(ctx, domain.DomainClaim{Domain: "ACME.com", ProviderID: domain.NewIdentityProviderID()})
	assert.ErrorIs(t, err, ports.ErrDomainClaimed, "a domain is claimed once")

	link := domain.ExternalIdentity{ProviderID: provider.ID, Subject: "sub-123", UserID: pwless.ID, LinkedAt: a.now}
	require.NoError(t, a.st.LinkExternalIdentity(ctx, link))
	link.UserID = domain.NewUserID()
	assert.ErrorIs(t, a.st.LinkExternalIdentity(ctx, link), ports.ErrAlreadyLinked)

	_, err = a.svc.SignIn(ctx, "nopw@example.com", "any-password-at-all")
	assert.ErrorIs(t, err, ports.ErrInvalidCredentials, "a password-less account fails like a wrong password")
}

// TestFederatedAuthenticatorShape pins the SSO seam (design D11): OIDC
// will implement exactly this, and changing it is a decision.
func TestFederatedAuthenticatorShape(t *testing.T) {
	var _ ports.FederatedAuthenticator = fakeFederation{}
}

type fakeFederation struct{}

func (fakeFederation) Begin(context.Context, domain.IdentityProvider, string) (string, string, error) {
	return "", "", errors.New("not built")
}

func (fakeFederation) Complete(context.Context, domain.IdentityProvider, string, map[string]string) (ports.FederatedAssertion, error) {
	return ports.FederatedAssertion{}, errors.New("not built")
}

var _ = strings.TrimSpace

// TestFallbackMailer_NeverLogsTheLink (design D10): with no SMTP server
// configured, the warning says a mail was not sent and never carries the
// body, which holds a link secret.
func TestFallbackMailer_NeverLogsTheLink(t *testing.T) {
	st := memstore.New()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	svc := application.NewService(application.StoresOf(st), st, application.NewPasswordHasher(), issuer,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))).
		WithMailer(nil, "https://console.example.test")
	tenant, err := svc.ProvisionTenant(context.Background(), application.SystemActor(), "Acme", "EU")
	require.NoError(t, err)
	_, err = svc.ProvisionPrincipal(context.Background(), application.SystemActor(), tenant.ID, "new@example.com", "", "", "AGENT")
	require.NoError(t, err)

	assert.Contains(t, logs.String(), "mail not sent")
	assert.NotContains(t, logs.String(), "/invite#", "the link never reaches a log")
	assert.NotContains(t, logs.String(), "new@example.com", "nor the full address")
}
