package domain

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
)

func TestNormalizeEmail(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"Ana@Example.com", "ana@example.com"},
		{"  ana@example.COM ", "ana@example.com"},
		{"a.b+c@sub.example.co.uk", "a.b+c@sub.example.co.uk"},
	} {
		got, err := NormalizeEmail(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got)
	}
	for _, bad := range []string{"", "ana", "@example.com", "ana@", "ana@example", "a@b@example.com",
		"ana@.example.com", "ana@example.com.", "an a@example.com"} {
		_, err := NormalizeEmail(bad)
		assert.True(t, errors.Is(err, ErrInvalidEmail), bad)
	}
	assert.Equal(t, "example.com", EmailDomain("ana@example.com"))
}

func TestSessionRefresh(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	s := NewSession(NewUserID(), "d1", t0)

	t.Run("current secret rotates and extends idle", func(t *testing.T) {
		next, out := s.Refresh("d1", "d2", t0.Add(10*time.Minute))
		require.Equal(t, RefreshRotated, out)
		assert.Equal(t, "d2", next.RefreshDigest)
		assert.Equal(t, "d1", next.PreviousDigest)
		assert.Equal(t, t0.Add(40*time.Minute), next.IdleExpiresAt)
		assert.Equal(t, s.AbsoluteExpiresAt, next.AbsoluteExpiresAt, "absolute expiry never moves")
	})

	t.Run("the previous generation is reuse and revokes", func(t *testing.T) {
		rotated, _ := s.Refresh("d1", "d2", t0.Add(time.Minute))
		revoked, out := rotated.Refresh("d1", "d3", t0.Add(2*time.Minute))
		require.Equal(t, RefreshReused, out)
		require.NotNil(t, revoked.RevokedAt)
		assert.Equal(t, RevokeReuse, revoked.RevokeReason)
		_, out = revoked.Refresh("d2", "d4", t0.Add(3*time.Minute))
		assert.Equal(t, RefreshRevoked, out, "after reuse even the current secret is dead")
	})

	t.Run("a rotation starts the previous secret's grace window", func(t *testing.T) {
		at := t0.Add(time.Minute)
		rotated, _ := s.Refresh("d1", "d2", at)
		require.NotNil(t, rotated.PreviousRotatedAt)
		assert.Equal(t, at, *rotated.PreviousRotatedAt)
	})

	t.Run("the previous secret within the window is a retry", func(t *testing.T) {
		at := t0.Add(time.Minute)
		rotated, _ := s.Refresh("d1", "d2", at)
		retried, out := rotated.Refresh("d1", "d3", at.Add(RefreshGraceWindow-time.Second))
		require.Equal(t, RefreshRetried, out)
		assert.Nil(t, retried.RevokedAt)
		assert.Equal(t, "d3", retried.RefreshDigest)
		assert.Equal(t, "d1", retried.PreviousDigest, "the retried secret stays the previous generation")
		assert.Equal(t, at, *retried.PreviousRotatedAt, "a retry does not move the window")
		assert.Equal(t, at.Add(RefreshGraceWindow-time.Second).Add(SessionIdleTimeout), retried.IdleExpiresAt)

		_, out = retried.Refresh("d2", "d4", at.Add(RefreshGraceWindow-time.Second))
		assert.Equal(t, RefreshUnknown, out, "the secret from the lost response no longer works")
		_, out = retried.Refresh("d3", "d4", at.Add(2*time.Minute))
		assert.Equal(t, RefreshRotated, out, "the retry's secret is the current one")
	})

	t.Run("the window closes exactly at its end", func(t *testing.T) {
		at := t0.Add(time.Minute)
		rotated, _ := s.Refresh("d1", "d2", at)
		revoked, out := rotated.Refresh("d1", "d3", at.Add(RefreshGraceWindow))
		require.Equal(t, RefreshReused, out)
		assert.Equal(t, RevokeReuse, revoked.RevokeReason)
	})

	t.Run("retries do not extend the window", func(t *testing.T) {
		at := t0.Add(time.Minute)
		rotated, _ := s.Refresh("d1", "d2", at)
		retried, out := rotated.Refresh("d1", "d3", at.Add(10*time.Second))
		require.Equal(t, RefreshRetried, out)
		retried, out = retried.Refresh("d1", "d4", at.Add(29*time.Second))
		require.Equal(t, RefreshRetried, out)
		revoked, out := retried.Refresh("d1", "d5", at.Add(31*time.Second))
		require.Equal(t, RefreshReused, out, "31 s after the rotation, whatever the retries")
		require.NotNil(t, revoked.RevokedAt)
	})

	t.Run("a session rotated before the window existed has none", func(t *testing.T) {
		legacy := s
		legacy.RefreshDigest, legacy.PreviousDigest, legacy.PreviousRotatedAt = "d2", "d1", nil
		_, out := legacy.Refresh("d1", "d3", t0.Add(time.Second))
		assert.Equal(t, RefreshReused, out)
	})

	t.Run("a retry still respects expiry", func(t *testing.T) {
		at := t0.Add(SessionAbsoluteTimeout - 10*time.Second)
		near := s
		near.IdleExpiresAt = s.AbsoluteExpiresAt
		rotated, out := near.Refresh("d1", "d2", at)
		require.Equal(t, RefreshRotated, out)
		_, out = rotated.Refresh("d1", "d3", t0.Add(SessionAbsoluteTimeout))
		assert.Equal(t, RefreshExpired, out)
	})

	t.Run("an older secret is unknown, not reuse", func(t *testing.T) {
		at := t0.Add(time.Minute)
		r1, _ := s.Refresh("d1", "d2", at)
		r2, _ := r1.Refresh("d2", "d3", at.Add(time.Minute))
		next, out := r2.Refresh("d1", "d4", at.Add(time.Minute+time.Second))
		assert.Equal(t, RefreshUnknown, out)
		assert.Nil(t, next.RevokedAt)
	})

	t.Run("idle expiry boundary", func(t *testing.T) {
		_, out := s.Refresh("d1", "d2", t0.Add(SessionIdleTimeout-time.Second))
		assert.Equal(t, RefreshRotated, out)
		_, out = s.Refresh("d1", "d2", t0.Add(SessionIdleTimeout))
		assert.Equal(t, RefreshExpired, out, "exactly 30 minutes idle is expired")
	})

	t.Run("absolute expiry at 12 hours despite activity", func(t *testing.T) {
		cur, digest := s, "d1"
		at := t0
		for i := 0; ; i++ {
			at = at.Add(25 * time.Minute)
			fresh := fmt.Sprintf("n%d", i)
			next, out := cur.Refresh(digest, fresh, at)
			if at.Before(t0.Add(SessionAbsoluteTimeout)) {
				require.Equal(t, RefreshRotated, out, "refresh %d at %s", i, at)
				assert.False(t, next.IdleExpiresAt.After(next.AbsoluteExpiresAt), "idle never passes absolute")
				cur, digest = next, fresh
				continue
			}
			assert.Equal(t, RefreshExpired, out)
			break
		}
		_, out := s.Refresh("d1", "d2", t0.Add(SessionAbsoluteTimeout))
		assert.Equal(t, RefreshExpired, out, "exactly 12 hours is expired")
	})

	t.Run("unknown and empty digests", func(t *testing.T) {
		_, out := s.Refresh("nope", "d2", t0)
		assert.Equal(t, RefreshUnknown, out)
		_, out = s.Refresh("", "d2", t0)
		assert.Equal(t, RefreshUnknown, out, "an empty previous digest is never a match")
	})

	t.Run("live and revoke", func(t *testing.T) {
		assert.True(t, s.Live(t0))
		r := s.Revoke(t0, RevokeSignOut)
		assert.False(t, r.Live(t0))
		assert.Equal(t, RevokeSignOut, r.Revoke(t0, RevokeReuse).RevokeReason, "the first reason is kept")
	})
}

func TestThrottlePolicy(t *testing.T) {
	p := DefaultThrottlePolicy
	for failures, want := range map[int]time.Duration{
		0: 0, 4: 0,
		5:  30 * time.Second,
		6:  time.Minute,
		7:  2 * time.Minute,
		9:  8 * time.Minute,
		10: 15 * time.Minute, // 16 min capped
		50: 15 * time.Minute,
	} {
		assert.Equal(t, want, p.Delay(failures), "failures=%d", failures)
	}
}

func TestThrottlePolicy_WindowStartsAFreshRun(t *testing.T) {
	p := DefaultThrottlePolicy
	t0 := time.Now()
	assert.True(t, p.Continues(t0, t0.Add(15*time.Minute)))
	assert.False(t, p.Continues(t0, t0.Add(15*time.Minute+time.Second)), "a failure after the window starts again")
	assert.True(t, ThrottlePolicy{}.Continues(t0, t0.Add(48*time.Hour)), "no window: every failure counts")
}

func TestCanResetPassword(t *testing.T) {
	a, b, c := shareddomain.NewTenantID(), shareddomain.NewTenantID(), shareddomain.NewTenantID()
	assert.True(t, CanResetPassword([]shareddomain.TenantID{a}, []shareddomain.TenantID{a}, false))
	assert.True(t, CanResetPassword([]shareddomain.TenantID{a, b}, []shareddomain.TenantID{a, b}, false))
	assert.False(t, CanResetPassword([]shareddomain.TenantID{a}, []shareddomain.TenantID{a, c}, false),
		"a shared person cannot be reset from one tenant")
	assert.False(t, CanResetPassword([]shareddomain.TenantID{a}, nil, false), "nobody but platform resets a member of nothing")
	assert.True(t, CanResetPassword(nil, []shareddomain.TenantID{a, c}, true), "platform scope always may")
}

func TestEnforcesSSO(t *testing.T) {
	now := time.Now()
	p := &IdentityProvider{ID: NewIdentityProviderID(), Mode: ProviderEnforced}
	verified := &DomainClaim{Domain: "acme.com", ProviderID: p.ID, VerifiedAt: &now}
	assert.True(t, EnforcesSSO(verified, p))
	assert.False(t, EnforcesSSO(&DomainClaim{Domain: "acme.com", ProviderID: p.ID}, p), "unverified routes nothing")
	optional := *p
	optional.Mode = ProviderOptional
	assert.False(t, EnforcesSSO(verified, &optional))
	assert.False(t, EnforcesSSO(nil, p))
	other := &IdentityProvider{ID: NewIdentityProviderID(), Mode: ProviderEnforced}
	assert.False(t, EnforcesSSO(verified, other), "the claim must be this provider's")
}

func TestTicketLifetimes(t *testing.T) {
	assert.Equal(t, 30*time.Minute, TicketReset.Lifetime())
	assert.Equal(t, 72*time.Hour, TicketInvite.Lifetime())
}
