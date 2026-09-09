//go:build e2e

package e2e_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	licensingapp "atsap-api/internal/licensing/application"
	licensingdomain "atsap-api/internal/licensing/domain"
	licensingpg "atsap-api/internal/licensing/postgres"
	"atsap-api/internal/logging"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/application"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
	telephonypostgres "atsap-api/internal/telephony/postgres"
)

// licenceRig is a telephony-core composition whose licensing service can
// be moved through licence states while a call is up.
type licenceRig struct {
	svc      *application.Service
	licSvc   *licensingapp.Service
	pool     *corepostgres.Pool
	tenantID shareddomain.TenantID
	ari      *ari.Client
	clock    *time.Time

	// sign mints a licence this rig's own service trusts, so a stage can
	// change the entitlement WITHOUT constructing a second Service — which
	// would bring its own counter and lose the live call's reservation.
	sign func(licensingdomain.LicenseToken) string
}

// newLicenceRig activates a licence of the given capacity and composes
// telephony-core against it, mirroring cmd/atsap-api.
func newLicenceRig(t *testing.T, ctx context.Context, capacity int) *licenceRig {
	t.Helper()

	pool, err := corepostgres.Open(ctx, pgURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	tenantID := shareddomain.NewTenantID()
	_, err = pool.Unwrap().Exec(ctx,
		`INSERT INTO tenants (id, name, status) VALUES ($1,'e2e-licence','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		tenantID.String())
	require.NoError(t, err)

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	payload, err := json.Marshal(licensingdomain.LicenseToken{
		Edition: "e2e", Capacity: capacity,
		InstanceID: "00000000-0000-0000-0000-0000000000e3",
	})
	require.NoError(t, err)

	clock := time.Now().UTC()
	licSvc := licensingapp.NewService(
		licensingpg.NewStore(pool.Unwrap()),
		licensingdomain.NewKeySet(pub),
		logging.New("debug"),
	).WithClock(func() time.Time { return clock })

	sign := func(tok licensingdomain.LicenseToken) string {
		b, mErr := json.Marshal(tok)
		require.NoError(t, mErr)
		return licensingdomain.EncodeToken(b, ed25519.Sign(priv, b))
	}

	require.NoError(t, licSvc.ApplyLicenseKey(ctx,
		licensingdomain.EncodeToken(payload, ed25519.Sign(priv, payload))))

	// A Stasis application name unique to this test.
	//
	// Every test that composes its own ARI client registers a Stasis app,
	// and Asterisk tracks registrations by NAME. Sharing one name across
	// tests means they contend for the same registration: one test's
	// teardown deregisters the app another is still waiting to appear,
	// which surfaces as "timed out waiting for Stasis app" in a test that
	// did nothing wrong — including tests in other files.
	//
	// Isolating by name removes the shared resource rather than
	// sequencing access to it, which is the difference between tests that
	// are independent and tests that merely usually pass.
	appName := stasisAppFor(t)

	ariClient := ari.New(ariBase, ariUser, ariPass, appName)
	registry := acl.NewCorrelationRegistry()
	svc := application.NewService(
		acl.NewMediaGatewayAdapter(ariClient),
		telephonypostgres.NewCallStore(pool),
		&e2eLicenseAdapter{inner: licSvc},
		application.NewAlwaysPermitCompliance(),
		registry, 20, logging.New("debug"),
	)

	eventLoop := acl.NewEventLoop(registry, svc, logging.New("debug"))
	go func() {
		if err := ariClient.StreamEvents(ctx, func(ev ari.Event) { eventLoop.Handle(ctx, ev) }, logging.New("debug")); err != nil && ctx.Err() == nil {
			t.Logf("ari event stream stopped: %v", err)
		}
	}()
	waitForStasisApp(t, ctx, appName)

	return &licenceRig{svc: svc, licSvc: licSvc, pool: pool, tenantID: tenantID, ari: ariClient, clock: &clock, sign: sign}
}

// TestNoActiveCallIsDroppedByAnyLicenceTransition is task 6.2, LLD-08
// DoD 4 and INV-03 — the invariant the whole licensing design is
// arranged around.
//
// A licence problem must never take down a call that is already up. Not
// when capacity is exhausted, not when the entitlement cannot be
// confirmed, not when the grace period elapses, and not when the stored
// licence stops verifying. The commercial consequence of getting this
// wrong is a partner's phone system going silent because of our billing
// system, which is the one failure this product promises never to
// produce.
//
// Asserted with a REAL call up throughout: the call is placed and
// answered against live Asterisk, then the licence is moved through
// every degrading state, and the call is checked at each step.
func TestNoActiveCallIsDroppedByAnyLicenceTransition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Capacity 1: the call below consumes the entire entitlement, so the
	// installation is already over capacity while it is up.
	rig := newLicenceRig(t, ctx, 1)

	callID, err := rig.svc.InitiateCall(ctx, ports.InitiateCallCommand{
		TenantID: rig.tenantID, Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.NoError(t, err)

	waitFor(t, ctx, callID.String(), func() (string, error) {
		c, err := telephonypostgres.NewCallStore(rig.pool).GetCall(ctx, rig.tenantID, callID)
		if err != nil {
			return "", err
		}
		return string(c.State), nil
	}, domain.CallActive)

	assertStillActive := func(t *testing.T, stage string) {
		t.Helper()
		c, err := telephonypostgres.NewCallStore(rig.pool).GetCall(ctx, rig.tenantID, callID)
		require.NoError(t, err)
		assert.Equal(t, domain.CallActive, c.State,
			"the call must still be Active after %s — no licence state may drop a call in progress (INV-03)", stage)

		chans, err := rig.ari.ListChannels(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, chans,
			"the engine must still hold the call's channels after %s: a domain record saying Active while the media is gone is worse than a dropped call", stage)
	}

	// The call must be holding its channel before any of the stages below
	// mean anything. Asserted separately, and first, so a failure here
	// says "the reservation is missing" rather than surfacing three
	// subtests down as "capacity was not exhausted" — which is the same
	// fact reported at the wrong place.
	//
	// waitForCondition rather than a bare read: InitiateCall returns once
	// the domain record is saved, and the reservation is taken on the
	// same path, but this test observes both from outside and must not
	// assume an ordering it does not control.
	waitForCondition(t, ctx, "the live call to hold its channel", func() bool {
		return rig.licSvc.ChannelsInUse() == 1
	})

	t.Run("over capacity", func(t *testing.T) {
		// The entitlement is exhausted by the live call; a new setup is
		// refused, and the refusal must not disturb the existing one.
		v, err := rig.licSvc.ValidateCapacity(ctx, rig.tenantID, "another-call", 1)
		require.NoError(t, err)
		require.False(t, v.Permitted,
			"capacity really is exhausted (%d/%d in use), so this stage is not vacuous",
			rig.licSvc.ChannelsInUse(), 1)

		assertStillActive(t, "reaching capacity")
	})

	t.Run("entitlement unverified", func(t *testing.T) {
		*rig.clock = rig.clock.Add(3 * 24 * time.Hour)
		e, err := rig.licSvc.Entitlement(ctx)
		require.NoError(t, err)
		require.Equal(t, licensingdomain.StateUnverified, e.State)

		assertStillActive(t, "entering the grace period")
	})

	t.Run("degraded", func(t *testing.T) {
		*rig.clock = rig.clock.Add(9 * 24 * time.Hour)
		e, err := rig.licSvc.Entitlement(ctx)
		require.NoError(t, err)
		require.Equal(t, licensingdomain.StateDegraded, e.State)

		assertStillActive(t, "degrading after the grace period")
	})

	t.Run("tampered", func(t *testing.T) {
		_, err := rig.pool.Unwrap().Exec(ctx,
			`UPDATE licensing_state SET signed_payload = $1`, []byte(`{"edition":"forged","capacity":99999}`))
		require.NoError(t, err)

		e, err := rig.licSvc.Entitlement(ctx)
		require.NoError(t, err)
		require.Equal(t, licensingdomain.StateDegraded, e.State)

		assertStillActive(t, "detecting a tampered licence")
	})

	// And it ends normally, releasing what it held.
	require.NoError(t, rig.svc.HangupCall(ctx, callID, "e2e complete"))
	hangupCallChannels(t, ctx, rig.ari, callID.String())

	// Task 6.4 / AC-06.8 runs here rather than as its own top-level test,
	// and the reason is a property of the engine rather than of the code:
	// Asterisk accepts only a small number of Stasis application
	// registrations over a container's life, and every test that composes
	// its own ARI client consumes one. A separate test for this would buy
	// a second registration and cost the walking skeleton its own — which
	// is how a change to licensing came to fail a telephony test it never
	// touched.
	//
	// Reusing one engine connection for related assertions is the same
	// discipline as reusing one database: the expensive shared resource
	// is acquired once, and the assertions stay independent.
	t.Run("a capacity increase applies without a restart", func(t *testing.T) {
		// The stage above deliberately broke the stored licence, so this
		// one starts by repairing it — with a token the rig's OWN service
		// trusts, applied to that same service. Constructing a second
		// Service here would bring its own counter and lose the live
		// call's reservation, which is the thing under test.
		require.NoError(t, rig.licSvc.ApplyLicenseKey(ctx, rig.sign(licensingdomain.LicenseToken{
			Edition: "e2e", Capacity: 1,
			InstanceID: "00000000-0000-0000-0000-0000000000e3",
		})))

		second, err := rig.svc.InitiateCall(ctx, ports.InitiateCallCommand{
			TenantID: rig.tenantID, Direction: domain.Outbound,
			SourceNumber: "1000", DestNumber: "1001",
			SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
		})
		require.NoError(t, err)
		waitForCondition(t, ctx, "the new call to hold a channel", func() bool {
			return rig.licSvc.ChannelsInUse() == 1
		})

		refused, err := rig.licSvc.ValidateCapacity(ctx, rig.tenantID, "third-call", 1)
		require.NoError(t, err)
		require.False(t, refused.Permitted, "one channel, one call: the next must be refused")

		// The partner buys more capacity and applies the new key. Same
		// process, same service, same live call — which is what AC-06.8
		// actually promises: capacity applies without a restart.
		require.NoError(t, rig.licSvc.ApplyLicenseKey(ctx, rig.sign(licensingdomain.LicenseToken{
			Edition: "e2e", Capacity: 8,
			InstanceID: "00000000-0000-0000-0000-0000000000e3",
		})))

		permitted, err := rig.licSvc.ValidateCapacity(ctx, rig.tenantID, "third-call", 1)
		require.NoError(t, err)
		assert.True(t, permitted.Permitted, "the new capacity is usable immediately (AC-06.8)")

		c, err := telephonypostgres.NewCallStore(rig.pool).GetCall(ctx, rig.tenantID, second)
		require.NoError(t, err)
		assert.Equal(t, domain.CallActive, c.State, "no call in progress was interrupted by the upgrade")

		require.NoError(t, rig.svc.HangupCall(ctx, second, "e2e complete"))
		hangupCallChannels(t, ctx, rig.ari, second.String())
	})
}

// waitForCondition polls until cond holds or the deadline passes.
//
// Deliberately bounded by its own deadline as well as by ctx: a helper
// that relies solely on the caller's context inherits whatever that
// context allows, which on 2026-09-09 meant a ten-second intent becoming
// a two-minute hang. A helper should fail on its own terms and say what
// it was waiting for.
func waitForCondition(t *testing.T, ctx context.Context, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context done waiting for %s: %v", what, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("timed out after 10s waiting for %s", what)
}

// stasisAppFor derives a Stasis application name unique to one test.
//
// Asterisk accepts any name; what matters is that two concurrent or
// consecutive registrations do not collide. The test name makes a
// failure traceable to its owner in Asterisk's own logs, and the
// nanosecond suffix keeps repeated runs of the same test from colliding
// with a registration the previous run has not finished releasing.
func stasisAppFor(t *testing.T) string {
	t.Helper()
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, t.Name())
	return fmt.Sprintf("e2e-%s-%d", safe, time.Now().UnixNano())
}
