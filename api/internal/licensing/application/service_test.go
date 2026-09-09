package application_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/licensing/application"
	"atsap-api/internal/licensing/domain"
	"atsap-api/internal/licensing/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

var now = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

// fakeStore records what it was asked to do, so tests can assert that a
// write happened — or did not — rather than only that no error came back.
type fakeStore struct {
	lic     *ports.StoredLicense
	saves   int
	touches int

	failLoad  error
	failSave  error
	failTouch error
}

func (f *fakeStore) Load(context.Context) (ports.StoredLicense, error) {
	if f.failLoad != nil {
		return ports.StoredLicense{}, f.failLoad
	}
	if f.lic == nil {
		return ports.StoredLicense{}, ports.ErrNoLicense
	}
	// Mirrors the real store: Load never returns the display cache, so a
	// test cannot accidentally prove something the production path
	// could not do (D-53).
	return ports.StoredLicense{
		InstanceID:      f.lic.InstanceID,
		SignedPayload:   f.lic.SignedPayload,
		Signature:       f.lic.Signature,
		LastConfirmedAt: f.lic.LastConfirmedAt,
		GraceStartedAt:  f.lic.GraceStartedAt,
	}, nil
}

func (f *fakeStore) Save(_ context.Context, lic ports.StoredLicense) error {
	if f.failSave != nil {
		return f.failSave
	}
	f.saves++
	stored := lic
	f.lic = &stored
	return nil
}

func (f *fakeStore) TouchConfirmed(_ context.Context, at time.Time) error {
	if f.failTouch != nil {
		return f.failTouch
	}
	f.touches++
	if f.lic != nil {
		f.lic.LastConfirmedAt = at
	}
	return nil
}

type rig struct {
	svc   *application.Service
	store *fakeStore
	keys  domain.KeySet
	sign  func(domain.LicenseToken) string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	store := &fakeStore{}
	svc := application.NewService(store, domain.NewKeySet(pub),
		slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now })

	return &rig{
		svc:   svc,
		store: store,
		keys:  domain.NewKeySet(pub),
		sign: func(tok domain.LicenseToken) string {
			payload, err := json.Marshal(tok)
			require.NoError(t, err)
			return domain.EncodeToken(payload, ed25519.Sign(priv, payload))
		},
	}
}

func licensed(channels int) domain.LicenseToken {
	return domain.LicenseToken{
		Edition: "core", Capacity: channels,
		MaxTenants: domain.Unlimited, MaxExtensions: domain.Unlimited,
		InstanceID: "11111111-1111-1111-1111-111111111111",
	}
}

// TestNoLicenceIsSetupNotAFloor is design D5a and D-52. The absence of a
// licence must produce a system with no call path — not four channels,
// which is what an earlier draft of this change assumed.
func TestNoLicenceIsSetupNotAFloor(t *testing.T) {
	r := newRig(t)

	e, err := r.svc.Entitlement(context.Background())

	require.NoError(t, err, "having no licence is a normal state, not a failure")
	assert.Equal(t, domain.StateSetup, e.State)
	assert.False(t, e.PermitsCalls(), "a never-activated installation has no call path")
	assert.NotEqual(t, domain.FreeChannels, e.Channels, "absence is not the degradation floor")
}

func TestSetupRefusesCallsWithTheNotActivatedReason(t *testing.T) {
	r := newRig(t)

	v, err := r.svc.ValidateCapacity(context.Background(), shareddomain.NewTenantID(), "call-1", 1)

	require.NoError(t, err)
	assert.False(t, v.Permitted)
	assert.Equal(t, domain.ReasonNotActivated, v.Reason,
		"an unactivated system must not report over-capacity — that sends an administrator hunting for a channel to free")
}

// TestApplyLicenseKey covers 3.5a: a token supplied by configuration
// activates the installation with no restart.
func TestApplyLicenseKey(t *testing.T) {
	ctx := context.Background()

	t.Run("a valid token activates the installation", func(t *testing.T) {
		r := newRig(t)

		require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(32))))

		e, err := r.svc.Entitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, domain.StateValid, e.State)
		assert.Equal(t, 32, e.Channels)
	})

	t.Run("it takes effect without a restart", func(t *testing.T) {
		r := newRig(t)
		// Establish the Setup entitlement in the cache first, so this
		// proves invalidation rather than a cold read.
		before, err := r.svc.Entitlement(ctx)
		require.NoError(t, err)
		require.False(t, before.PermitsCalls())

		require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(64))))

		after, err := r.svc.Entitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, 64, after.Channels, "the same Service instance must see the new entitlement")
	})

	t.Run("an untrusted token leaves the entitlement in force untouched", func(t *testing.T) {
		r := newRig(t)
		require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(32))))

		_, otherPriv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		payload, err := json.Marshal(licensed(9999))
		require.NoError(t, err)
		forged := domain.EncodeToken(payload, ed25519.Sign(otherPriv, payload))

		err = r.svc.ApplyLicenseKey(ctx, forged)

		assert.ErrorIs(t, err, domain.ErrUntrusted)
		e, entErr := r.svc.Entitlement(ctx)
		require.NoError(t, entErr)
		assert.Equal(t, 32, e.Channels, "a rejected token must not disturb the licence already in force")
	})

	t.Run("a malformed envelope is refused before anything is stored", func(t *testing.T) {
		r := newRig(t)

		err := r.svc.ApplyLicenseKey(ctx, "not-a-token")

		assert.ErrorIs(t, err, domain.ErrUntrusted)
		assert.Zero(t, r.store.saves)
	})
}

// TestApplyLicenseKeyIsIdempotent is task 3.5b and D-58.
//
// The grace-clock half is the one that matters. Startup intake runs on
// every boot, so if re-applying the same token refreshed
// last_confirmed_at, an installation could restart daily and never
// degrade however long it stayed disconnected — the 7-day grace would
// silently become unbounded.
func TestApplyLicenseKeyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	token := r.sign(licensed(32))

	require.NoError(t, r.svc.ApplyLicenseKey(ctx, token))
	firstConfirmed := r.store.lic.LastConfirmedAt
	savesAfterFirst := r.store.saves

	// Six days pass with no confirmation, then the process restarts and
	// applies the same token from ATSAPBX_LICENSE_TOKEN.
	sixDaysLater := now.Add(6 * 24 * time.Hour)
	r.svc.WithClock(func() time.Time { return sixDaysLater })

	require.NoError(t, r.svc.ApplyLicenseKey(ctx, token), "re-applying the same token succeeds")

	assert.Equal(t, savesAfterFirst, r.store.saves, "nothing is rewritten")
	assert.Equal(t, firstConfirmed, r.store.lic.LastConfirmedAt,
		"re-application must not restart the grace clock — startup intake runs on every boot (D-58)")
}

// TestStoredLicenceThatFailsVerificationDegrades is D-53 at the service
// level: a row that does not verify is never honoured, and never
// disables.
func TestStoredLicenceThatFailsVerificationDegrades(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(512))))

	// Someone edits the row: the payload now claims more, so the
	// signature no longer matches it.
	tampered, err := json.Marshal(licensed(99999))
	require.NoError(t, err)
	r.store.lic.SignedPayload = tampered

	e, err := application.NewService(r.store, domain.NewKeySet(), slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now }).
		Entitlement(ctx)

	require.NoError(t, err)
	assert.Equal(t, domain.StateDegraded, e.State)
	assert.Equal(t, domain.ReasonTampered, e.Reason)
	assert.Equal(t, domain.FreeChannels, e.Channels,
		"a tampered row grants the floor, never what it claimed")
	assert.True(t, e.PermitsCalls(), "tampering degrades, it never disables (BRD §10.2)")
}

// TestStoreFailureFailsClosed: an installation that cannot read its
// licence must not assume one.
func TestStoreFailureFailsClosed(t *testing.T) {
	r := newRig(t)
	r.store.failLoad = errors.New("database unavailable")

	e, err := r.svc.Entitlement(context.Background())

	require.Error(t, err)
	assert.False(t, e.PermitsCalls(), "an unreadable licence grants nothing")
}

// TestVerifyDailyEntitlement is task 3.6.
func TestVerifyDailyEntitlement(t *testing.T) {
	ctx := context.Background()

	t.Run("a successful confirmation returns to valid and moves the clock", func(t *testing.T) {
		r := newRig(t)
		require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(32))))

		later := now.Add(3 * 24 * time.Hour)
		r.svc.WithClock(func() time.Time { return later })

		state, err := r.svc.VerifyDailyEntitlement(ctx)

		require.NoError(t, err)
		assert.Equal(t, domain.StateValid, state)
		assert.Equal(t, later, r.store.lic.LastConfirmedAt)
	})

	t.Run("a failing confirmation is not an error — that is what grace is for", func(t *testing.T) {
		r := newRig(t)
		require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(32))))
		r.store.failTouch = errors.New("entitlement service unreachable")

		state, err := r.svc.VerifyDailyEntitlement(ctx)

		require.NoError(t, err, "an unreachable entitlement service is survivable by design (D-14)")
		assert.Equal(t, domain.StateValid, state, "still inside the grace window")
	})

	t.Run("an unactivated installation has nothing to confirm", func(t *testing.T) {
		r := newRig(t)

		state, err := r.svc.VerifyDailyEntitlement(ctx)

		require.NoError(t, err)
		assert.Equal(t, domain.StateSetup, state)
		assert.Zero(t, r.store.touches, "there is no licence to confirm")
	})
}

// TestConfirmationFailureDoesNotAffectCallSetup is AC-06.3: no
// entitlement work sits in the call path. Asserted by breaking
// confirmation entirely and showing capacity is unaffected.
func TestConfirmationFailureDoesNotAffectCallSetup(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(4))))
	r.store.failTouch = errors.New("entitlement service hanging")

	_, _ = r.svc.VerifyDailyEntitlement(ctx)

	for i := 0; i < 4; i++ {
		v, err := r.svc.ValidateCapacity(ctx, shareddomain.NewTenantID(), string(rune('a'+i)), 1)
		require.NoError(t, err)
		assert.True(t, v.Permitted, "call setup is unaffected by a failing confirmation")
	}
}

// TestGraceElapsesToTheFloor drives the full window with an injected
// clock, through the service rather than the pure function.
func TestGraceElapsesToTheFloor(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(64))))

	for name, tc := range map[string]struct {
		elapsed time.Duration
		state   domain.State
		channel int
	}{
		"day 0": {0, domain.StateValid, 64},
		"day 3": {3 * 24 * time.Hour, domain.StateUnverified, 64},
		"day 8": {8 * 24 * time.Hour, domain.StateDegraded, domain.FreeChannels},
	} {
		t.Run(name, func(t *testing.T) {
			at := now.Add(tc.elapsed)
			svc := application.NewService(r.store, r.keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
				WithClock(func() time.Time { return at })

			e, err := svc.Entitlement(ctx)

			require.NoError(t, err)
			assert.Equal(t, tc.state, e.State)
			assert.Equal(t, tc.channel, e.Channels)
		})
	}
}

// TestGraceIsReEvaluatedAsTimePassesWithoutInvalidation covers a defect
// that shipped past every other test in this change and was caught by an
// e2e licence-transition test on 2026-09-09.
//
// The first implementation cached the COMPUTED entitlement. Once a Valid
// one was cached, nothing re-evaluated it: the only invalidations were
// applying a key and a successful confirmation, neither of which happens
// while an installation sits disconnected. So a running process whose
// grace period elapsed would keep serving full capacity indefinitely —
// the seven-day window silently unbounded, and D-12's degradation
// unreachable in exactly the situation it exists for.
//
// The fix is to cache what is expensive and unchanging (the verified
// signature) and recompute what is cheap and time-dependent (expiry and
// grace). This asserts the consequence: the SAME Service instance, with
// no invalidation and no restart, must degrade when time passes.
func TestGraceIsReEvaluatedAsTimePassesWithoutInvalidation(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(64))))

	// Read once, so the licence is cached and any staleness is real.
	first, err := r.svc.Entitlement(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.StateValid, first.State)
	require.Equal(t, 64, first.Channels)

	for name, tc := range map[string]struct {
		elapsed  time.Duration
		state    domain.State
		channels int
	}{
		"day 3 — unverified, unrestricted": {3 * 24 * time.Hour, domain.StateUnverified, 64},
		"day 8 — degraded to the floor":    {8 * 24 * time.Hour, domain.StateDegraded, domain.FreeChannels},
		"day 90 — still degraded":          {90 * 24 * time.Hour, domain.StateDegraded, domain.FreeChannels},
	} {
		t.Run(name, func(t *testing.T) {
			r.svc.WithClock(func() time.Time { return now.Add(tc.elapsed) })

			got, err := r.svc.Entitlement(ctx)

			require.NoError(t, err)
			assert.Equal(t, tc.state, got.State,
				"the same Service instance must notice time passing, with nothing invalidating a cache")
			assert.Equal(t, tc.channels, got.Channels)
		})
	}
}

// TestExpiryIsReEvaluatedAsTimePasses is the same property for the other
// time-dependent field. A licence that expires while the process runs
// must degrade without anything prompting a re-read.
func TestExpiryIsReEvaluatedAsTimePasses(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)

	tok := licensed(64)
	tok.ExpiresAt = now.Add(24 * time.Hour)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(tok)))

	before, err := r.svc.Entitlement(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.StateValid, before.State)

	r.svc.WithClock(func() time.Time { return now.Add(25 * time.Hour) })

	after, err := r.svc.Entitlement(ctx)

	require.NoError(t, err)
	assert.Equal(t, domain.StateDegraded, after.State)
	assert.Equal(t, domain.ReasonExpired, after.Reason)
	assert.True(t, after.PermitsCalls(), "expiry degrades, it never disables")
}

// TestSetupIsNotCached: an installation in Setup must notice the moment a
// licence is applied. Caching "no licence" would mean a freshly
// activated system kept refusing calls until something restarted it.
func TestSetupIsNotCached(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)

	setup, err := r.svc.Entitlement(ctx)
	require.NoError(t, err)
	require.False(t, setup.PermitsCalls())

	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(8))))

	after, err := r.svc.Entitlement(ctx)
	require.NoError(t, err)
	assert.Equal(t, 8, after.Channels, "activation takes effect on the next read, not the next restart")
}

// TestTamperedLicenceIsNotCached: restoring a good licence must take
// effect without a restart, so a row that failed verification is
// re-checked rather than remembered.
func TestTamperedLicenceIsNotCached(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(64))))

	// Break it behind the service's back.
	good := r.store.lic.SignedPayload
	r.store.lic.SignedPayload = []byte(`{"edition":"forged","capacity":99999}`)

	svc := application.NewService(r.store, r.keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now })
	broken, err := svc.Entitlement(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonTampered, broken.Reason)

	// Restore it. The same instance must recover.
	r.store.lic.SignedPayload = good

	fixed, err := svc.Entitlement(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.StateValid, fixed.State,
		"a repaired licence takes effect on the next read, not the next restart")
	assert.Equal(t, 64, fixed.Channels)
}
