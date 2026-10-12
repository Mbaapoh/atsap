//go:build integration

package postgres_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// Revision-2 store behaviour against real Postgres (task 3.2).

func member(t *testing.T, store *identitypostgres.Store, tenant shareddomain.TenantID, name string) domain.Principal {
	t.Helper()
	p := newPrincipal(t, store, tenant, name)
	require.NoError(t, store.CreatePrincipal(context.Background(), p))
	return p
}

func TestAccounts_UsersByEmailIgnoreCase(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")

	u, err := store.GetUserByEmail(ctx, "ANA@Example.TEST")
	require.NoError(t, err)
	assert.Equal(t, p.UserID, u.ID)
	require.NotNil(t, u.Password)
	assert.Equal(t, testHash, u.Password.Hash)

	require.NoError(t, store.SetPassword(ctx, u.ID, domain.PasswordCredential{Hash: "new-hash", MustChange: true, ChangedAt: time.Now().UTC()}))
	u, err = store.GetUser(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "new-hash", u.Password.Hash)
	assert.True(t, u.Password.MustChange)

	pwless := domain.User{ID: domain.NewUserID(), Email: "nopw@example.test", Status: domain.UserActive, CreatedAt: time.Now().UTC()}
	require.NoError(t, store.CreateUser(ctx, pwless))
	got, err := store.GetUser(ctx, pwless.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Password, "a person may have no password")
}

func TestAccounts_MembershipsAreTheirOwnAndActiveTenantsOnly(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()
	ana := member(t, store, tenantA, "ana")
	member(t, store, tenantB, "ana")
	member(t, store, tenantA, "bob")
	require.NoError(t, store.CreateRoleBinding(ctx, domain.RoleBinding{PrincipalID: ana.ID, TenantID: tenantA, Role: "SUPERVISOR", Scope: domain.ScopeTenant}))

	ms, err := store.ListMemberships(ctx, ana.UserID)
	require.NoError(t, err)
	require.Len(t, ms, 2, "Ana's two memberships, never Bob's")
	for _, m := range ms {
		if m.TenantID == tenantA {
			assert.Equal(t, []string{"SUPERVISOR"}, m.Roles)
		}
	}

	require.NoError(t, store.SetTenantStatus(ctx, tenantB, domain.TenantSuspended))
	ms, err = store.ListMemberships(ctx, ana.UserID)
	require.NoError(t, err)
	assert.Len(t, ms, 1, "a suspended tenant is not offered")
}

func TestAccounts_LiveStateInOneRead(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")
	sess := domain.NewSession(p.UserID, strings.Repeat("a", 64), time.Now().UTC())
	sess.PrincipalID = &p.ID
	require.NoError(t, store.CreateSession(ctx, sess))

	st, err := store.LiveState(ctx, tenantA, p.ID, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, st.Principal.ID)
	assert.Equal(t, domain.UserActive, st.UserStatus)
	require.NotNil(t, st.Session)
	assert.Equal(t, sess.ID, st.Session.ID)

	_, err = store.LiveState(ctx, tenantA, p.ID, domain.NewSessionID())
	assert.ErrorIs(t, err, ports.ErrNotFound, "an unknown session is not a live one")
}

// TestAccounts_ConcurrentRefreshesSerializeOnTheRow: while one refresh
// holds the session row, a second refresh of the same secret waits, then
// sees the rotation and is a replay. Fault-injected: without FOR UPDATE
// both would rotate.
func TestAccounts_ConcurrentRefreshesSerializeOnTheRow(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")
	now := time.Now().UTC()
	digest := strings.Repeat("d", 64)
	require.NoError(t, store.CreateSession(ctx, domain.NewSession(p.UserID, digest, now)))

	holding := make(chan struct{})
	release := make(chan struct{})
	outcomes := make([]domain.RefreshOutcome, 2)
	var wg sync.WaitGroup
	refresh := func(i int, newDigest string, hold bool) {
		defer wg.Done()
		_, _ = store.UpdateSessionByDigest(ctx, digest, func(cur domain.Session) (domain.Session, error) {
			next, out := cur.Refresh(digest, newDigest, now.Add(time.Second))
			outcomes[i] = out
			if hold {
				close(holding)
				<-release
			}
			return next, nil
		})
	}
	wg.Add(1)
	go refresh(0, strings.Repeat("1", 64), true)
	<-holding
	wg.Add(1)
	go refresh(1, strings.Repeat("2", 64), false)
	time.Sleep(300 * time.Millisecond)
	assert.Zero(t, outcomes[1], "the second refresh is blocked on the row lock while the first holds it")
	close(release)
	wg.Wait()

	assert.Equal(t, domain.RefreshRotated, outcomes[0])
	assert.Equal(t, domain.RefreshRetried, outcomes[1],
		"after the first commits, the same secret inside its grace window is a retry (identity-refresh-grace)")

	// The same secret once the window has closed is a replay.
	_, _ = store.UpdateSessionByDigest(ctx, digest, func(cur domain.Session) (domain.Session, error) {
		next, out := cur.Refresh(digest, strings.Repeat("3", 64), now.Add(time.Second+domain.RefreshGraceWindow))
		outcomes[0] = out
		return next, nil
	})
	assert.Equal(t, domain.RefreshReused, outcomes[0])
}

func TestAccounts_GraceWindowPersists(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")
	// Microseconds: Postgres keeps no more, so the stored time is exactly
	// the one the domain computed.
	now := time.Now().UTC().Truncate(time.Microsecond)
	d1, d2, d3 := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	sess := domain.NewSession(p.UserID, d1, now)
	require.NoError(t, store.CreateSession(ctx, sess))

	stored, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.PreviousRotatedAt, "a new session has no previous secret, so no window")

	rotate := func(presented, next string, at time.Time) domain.RefreshOutcome {
		var out domain.RefreshOutcome
		_, err := store.UpdateSessionByDigest(ctx, presented, func(cur domain.Session) (domain.Session, error) {
			n, o := cur.Refresh(presented, next, at)
			out = o
			return n, nil
		})
		require.NoError(t, err)
		return out
	}

	rotatedAt := now.Add(time.Minute)
	require.Equal(t, domain.RefreshRotated, rotate(d1, d2, rotatedAt))
	stored, err = store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.PreviousRotatedAt)
	assert.True(t, rotatedAt.Equal(*stored.PreviousRotatedAt), "the rotation time is stored")

	require.Equal(t, domain.RefreshRetried, rotate(d1, d3, rotatedAt.Add(20*time.Second)))
	stored, err = store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, rotatedAt.Equal(*stored.PreviousRotatedAt), "a retry leaves the window where it was")
	assert.Equal(t, d3, stored.RefreshDigest)
	assert.Equal(t, d1, stored.PreviousDigest)

	require.Equal(t, domain.RefreshReused, rotate(d1, strings.Repeat("e", 64), rotatedAt.Add(domain.RefreshGraceWindow)))
	stored, err = store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.NotNil(t, stored.RevokedAt, "the revocation after the window persists")
}

func TestAccounts_SessionsRevokeAndRedeemTicketAtomically(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")
	now := time.Now().UTC()
	keep := domain.NewSession(p.UserID, strings.Repeat("k", 64), now)
	other := domain.NewSession(p.UserID, strings.Repeat("o", 64), now)
	require.NoError(t, store.CreateSession(ctx, keep))
	require.NoError(t, store.CreateSession(ctx, other))

	n, err := store.RevokeUserSessions(ctx, p.UserID, keep.ID, now, domain.RevokePasswordChanged)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	got, err := store.GetSession(ctx, keep.ID)
	require.NoError(t, err)
	assert.Nil(t, got.RevokedAt, "the kept session continues")

	for _, d := range []string{"t1", "t2"} {
		require.NoError(t, store.CreateTicket(ctx, ports.ResetTicket{UserID: p.UserID, Digest: strings.Repeat(d, 32),
			Purpose: domain.TicketReset, ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now}))
	}
	count, err := store.CountTickets(ctx, p.UserID, domain.TicketReset, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	user, purpose, err := store.RedeemTicket(ctx, strings.Repeat("t1", 32), domain.PasswordCredential{Hash: "reset-hash", ChangedAt: now}, now)
	require.NoError(t, err)
	assert.Equal(t, p.UserID, user)
	assert.Equal(t, domain.TicketReset, purpose)
	u, err := store.GetUser(ctx, p.UserID)
	require.NoError(t, err)
	assert.Equal(t, "reset-hash", u.Password.Hash)
	got, err = store.GetSession(ctx, keep.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.RevokedAt, "a reset ends every session")

	_, _, err = store.RedeemTicket(ctx, strings.Repeat("t1", 32), domain.PasswordCredential{Hash: "x"}, now)
	assert.ErrorIs(t, err, ports.ErrNotFound, "single use")
	_, _, err = store.RedeemTicket(ctx, strings.Repeat("t2", 32), domain.PasswordCredential{Hash: "x"}, now)
	assert.ErrorIs(t, err, ports.ErrNotFound, "every other outstanding ticket died with it")

	require.NoError(t, store.CreateTicket(ctx, ports.ResetTicket{UserID: p.UserID, Digest: strings.Repeat("t3", 32),
		Purpose: domain.TicketReset, ExpiresAt: now.Add(time.Minute), CreatedAt: now}))
	_, _, err = store.RedeemTicket(ctx, strings.Repeat("t3", 32), domain.PasswordCredential{Hash: "x"}, now.Add(time.Minute))
	assert.ErrorIs(t, err, ports.ErrNotFound, "expired")
}

func TestAccounts_ProvidersDomainsAndLinks(t *testing.T) {
	store, tenantA, tenantB := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")
	prov := domain.IdentityProvider{ID: domain.NewIdentityProviderID(), TenantID: tenantA, Kind: domain.ProviderOIDC,
		DisplayName: "Acme Entra", Issuer: "https://login.example/acme", Mode: domain.ProviderEnforced}
	require.NoError(t, store.CreateProvider(ctx, prov))
	rival := domain.IdentityProvider{ID: domain.NewIdentityProviderID(), TenantID: tenantB, Kind: domain.ProviderSAML,
		DisplayName: "Rival", Issuer: "https://idp.example/rival", Mode: domain.ProviderEnforced}
	require.NoError(t, store.CreateProvider(ctx, rival))

	require.NoError(t, store.ClaimDomain(ctx, domain.DomainClaim{Domain: "Acme.com", ProviderID: prov.ID}))
	assert.ErrorIs(t, store.ClaimDomain(ctx, domain.DomainClaim{Domain: "acme.com", ProviderID: rival.ID}), ports.ErrDomainClaimed)

	_, _, err := store.ProviderForDomain(ctx, "acme.com")
	assert.ErrorIs(t, err, ports.ErrNotFound, "an unverified claim routes nothing")

	require.NoError(t, store.VerifyDomain(ctx, "acme.com", time.Now().UTC()))
	claim, got, err := store.ProviderForDomain(ctx, "ACME.com")
	require.NoError(t, err, "discovery reads the provider before any tenant is known")
	assert.Equal(t, prov.ID, got.ID)
	assert.Equal(t, tenantA, got.TenantID)
	assert.True(t, domain.EnforcesSSO(&claim, &got))

	link := domain.ExternalIdentity{ProviderID: prov.ID, Subject: "sub-1", UserID: p.UserID, LinkedAt: time.Now().UTC()}
	require.NoError(t, store.LinkExternalIdentity(ctx, link))
	assert.ErrorIs(t, store.LinkExternalIdentity(ctx, link), ports.ErrAlreadyLinked)
	uid, err := store.UserForExternalIdentity(ctx, prov.ID, "sub-1")
	require.NoError(t, err)
	assert.Equal(t, p.UserID, uid)
}

func TestAccounts_ThrottleBlocksAndPrunes(t *testing.T) {
	store, _, _ := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond) // Postgres precision
	key, idle := strings.Repeat("a", 64), strings.Repeat("b", 64)

	require.NoError(t, store.RecordFailure(ctx, []string{idle}, now.Add(-25*time.Hour), domain.DefaultThrottlePolicy))
	for i := 0; i < 5; i++ {
		require.NoError(t, store.RecordFailure(ctx, []string{key}, now, domain.DefaultThrottlePolicy))
	}
	wait, err := store.BlockedFor(ctx, []string{key, idle}, now)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, wait)

	require.NoError(t, store.Clear(ctx, key))
	wait, err = store.BlockedFor(ctx, []string{key}, now)
	require.NoError(t, err)
	assert.Zero(t, wait)

	// A run that stops for longer than the window starts again.
	spaced := strings.Repeat("c", 64)
	for i := 0; i < 6; i++ {
		require.NoError(t, store.RecordFailure(ctx, []string{spaced}, now.Add(time.Duration(i)*20*time.Minute), domain.DefaultThrottlePolicy))
	}
	wait, err = store.BlockedFor(ctx, []string{spaced}, now.Add(100*time.Minute))
	require.NoError(t, err)
	assert.Zero(t, wait, "six failures twenty minutes apart are never a run of six")

	// The idle key was pruned by the writes above: a fresh failure on it
	// counts from one again, so it is not blocked.
	require.NoError(t, store.RecordFailure(ctx, []string{idle}, now, domain.DefaultThrottlePolicy))
	wait, err = store.BlockedFor(ctx, []string{idle}, now)
	require.NoError(t, err)
	assert.Zero(t, wait, "rows idle for more than 24 hours are pruned on write")
}

func TestAccounts_AuthEventsAppendOnly(t *testing.T) {
	store, tenantA, _ := newStore(t)
	ctx := context.Background()
	p := member(t, store, tenantA, "ana")
	require.NoError(t, store.AppendAuthEvent(ctx, domain.AuthEvent{UserID: &p.UserID, Kind: domain.EventSignInSucceeded,
		SourceAddress: "203.0.113.5", At: time.Now().UTC()}))
	require.NoError(t, store.AppendAuthEvent(ctx, domain.AuthEvent{Kind: domain.EventSignInFailed, At: time.Now().UTC()}))
	events, err := store.ListAuthEvents(ctx, p.UserID, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "203.0.113.5", events[0].SourceAddress)

	typ := reflect.TypeOf(store)
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if strings.Contains(name, "AuthEvent") {
			assert.NotRegexp(t, `^(Update|Delete|Set|Remove)`, name, "auth events have no update or delete path")
		}
	}
}
