//go:build integration

package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpg "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

// fakeCalls stands in for telephony-core: it records every origination
// and keeps an outcome per call that a test can move along. Placement is
// pbx-core's logic; telephony's is proven by its own suites and by the
// e2e tier, where the real one runs.
type fakeCalls struct {
	mu        sync.Mutex
	initiated []telephonyports.InitiateCallCommand
	outcomes  map[shareddomain.CallID]ports.CallOutcome
	hangups   []shareddomain.CallID
	failNext  error
	screenOut string // non-empty: the next call is refused by Screening with this reason
}

func newFakeCalls() *fakeCalls {
	return &fakeCalls{outcomes: map[shareddomain.CallID]ports.CallOutcome{}}
}

func (f *fakeCalls) InitiateCall(_ context.Context, cmd telephonyports.InitiateCallCommand) (shareddomain.CallID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return shareddomain.CallID{}, err
	}
	f.initiated = append(f.initiated, cmd)
	id := shareddomain.NewCallID()
	out := ports.CallOutcome{StartedAt: time.Now().UTC()}
	if f.screenOut != "" {
		out.Ended, out.Reason, f.screenOut = true, f.screenOut, ""
	}
	f.outcomes[id] = out
	return id, nil
}

func (f *fakeCalls) HangupCall(_ context.Context, id shareddomain.CallID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hangups = append(f.hangups, id)
	out := f.outcomes[id]
	out.Ended = true
	f.outcomes[id] = out
	return nil
}

func (f *fakeCalls) AnswerParticipant(context.Context, shareddomain.CallID, shareddomain.ParticipantID) error {
	return nil
}
func (f *fakeCalls) HoldParticipant(context.Context, shareddomain.CallID, shareddomain.ParticipantID) error {
	return nil
}
func (f *fakeCalls) TransferParticipant(context.Context, telephonyports.TransferCommand) error {
	return nil
}

func (f *fakeCalls) Status(_ context.Context, _ shareddomain.TenantID, id shareddomain.CallID) (ports.CallOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, ok := f.outcomes[id]
	if !ok {
		return ports.CallOutcome{}, ports.ErrNotFound
	}
	return out, nil
}

func (f *fakeCalls) set(id shareddomain.CallID, mutate func(*ports.CallOutcome)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.outcomes[id]
	mutate(&out)
	f.outcomes[id] = out
}

func (f *fakeCalls) last() telephonyports.InitiateCallCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initiated[len(f.initiated)-1]
}

type placer struct {
	*harness
	svc   *application.Service
	calls *fakeCalls
	from  ports.CreatedExtension
}

func newPlacer(t *testing.T) *placer {
	t.Helper()
	h := newHarness(t)
	calls := newFakeCalls()
	p := &placer{harness: h, calls: calls, svc: h.svc.WithPlacement(application.Placement{
		Calls: calls, Status: calls, Addresser: asterisk.Addresser{}, Legs: pbxpg.NewCallLegStore(),
		WatchInterval: 10 * time.Millisecond, WatchTimeout: 3 * time.Second,
	})}
	p.from = h.create(t, "1001")
	p.register(t, p.from.Extension.ID)
	return p
}

// register stands in for a device registering: Asterisk writes the
// contact row itself in production.
func (p *placer) register(t *testing.T, id shareddomain.ExtensionID) {
	t.Helper()
	_, err := p.pool.Unwrap().Exec(context.Background(),
		`INSERT INTO ps_contacts (id, endpoint, uri, expiration_time) VALUES ($1, $2, $3, $4)`,
		"c-"+id.String(), domain.EndpointIdentifier(id), "sip:x@10.0.0.1:5060", time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
}

func (p *placer) trunk(t *testing.T, name string, limit, priority int) ports.TrunkView {
	t.Helper()
	cmd := p.trunkCmd(name)
	cmd.ChannelLimit, cmd.Priority = limit, priority
	v, err := p.svc.CreateTrunk(p.ctx, cmd)
	require.NoError(t, err)
	return v
}

func (p *placer) route(t *testing.T, pattern string, trunk ports.TrunkView, priority int) {
	t.Helper()
	_, err := p.svc.CreateRoute(p.ctx, ports.CreateRouteCommand{TenantID: p.tenant, PrefixPattern: pattern,
		TrunkID: trunk.ID, Priority: priority})
	require.NoError(t, err)
}

func (p *placer) place(dest string) (ports.PlacedCall, error) {
	return p.svc.PlaceCall(p.ctx, ports.PlaceCallCommand{TenantID: p.tenant, ExtensionID: p.from.Extension.ID, Destination: dest})
}

func (p *placer) health(t *testing.T, id shareddomain.TrunkID) domain.TrunkHealth {
	t.Helper()
	listed, err := p.svc.ListTrunks(p.ctx, p.tenant, ports.Page{Limit: 50})
	require.NoError(t, err)
	for _, v := range listed {
		if v.ID == id {
			return v.Health
		}
	}
	t.Fatalf("trunk %s not listed", id)
	return ""
}

// --- 4.1 -----------------------------------------------------------------

func TestPlaceCall_OutboundOverATrunk(t *testing.T) {
	p := newPlacer(t)
	tr := p.trunk(t, "Primary", 5, 0)
	p.route(t, "+44", tr, 0)

	placed, err := p.place("+442071234567")
	require.NoError(t, err)

	cmd := p.calls.last()
	assert.Equal(t, "OUTBOUND", string(cmd.Direction))
	assert.Equal(t, "PJSIP/"+domain.EndpointIdentifier(p.from.Extension.ID), cmd.SourceEndpointURI, "the caller's phone is one leg")
	assert.Equal(t, "PJSIP/442071234567@"+domain.TrunkEndpointIdentifier(tr.ID), cmd.DestEndpointURI, "the other leaves via the trunk")
	assert.False(t, cmd.BypassScreening, "an ordinary call is screened")
	assert.Equal(t, "1001", cmd.SourceNumber)

	assert.Equal(t, 1, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs WHERE call_id = $1
		AND trunk_id = $2 AND route_id IS NOT NULL AND released_at IS NULL`, placed.CallID.String(), tr.ID.String()),
		"the matched route and trunk are on record, and the channel is held")
}

func TestPlaceCall_InternalCallUsesNoTrunk(t *testing.T) {
	p := newPlacer(t)
	callee := p.create(t, "1005")
	p.route(t, "1", p.trunk(t, "Catch-all", 5, 0), 0) // would shadow 1005 if routes came first

	_, err := p.place("1005")
	require.NoError(t, err)
	cmd := p.calls.last()
	assert.Equal(t, "INTERNAL", string(cmd.Direction))
	assert.Equal(t, "PJSIP/"+domain.EndpointIdentifier(callee.Extension.ID), cmd.DestEndpointURI)
	assert.Zero(t, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs WHERE trunk_id IS NOT NULL`))
}

func TestPlaceCall_Refusals(t *testing.T) {
	p := newPlacer(t)
	p.route(t, "+44", p.trunk(t, "Primary", 5, 0), 0)

	_, err := p.place("+33123456789")
	assert.ErrorIs(t, err, domain.ErrUnroutable)

	_, err = p.place("44*1")
	assert.ErrorIs(t, err, ports.ErrInvalidInput)

	silent := p.create(t, "1002") // never registered
	_, err = p.svc.PlaceCall(p.ctx, ports.PlaceCallCommand{TenantID: p.tenant, ExtensionID: silent.Extension.ID, Destination: "+4420"})
	assert.ErrorIs(t, err, ports.ErrCallerNotRegistered)

	_, err = p.svc.PlaceCall(p.ctx, ports.PlaceCallCommand{TenantID: p.tenant, ExtensionID: shareddomain.NewExtensionID(), Destination: "+4420"})
	assert.ErrorIs(t, err, ports.ErrNotFound)

	p.authz.deny = identityports.ErrPermissionDenied
	_, err = p.place("+4420")
	assert.ErrorIs(t, err, identityports.ErrPermissionDenied)

	assert.Empty(t, p.calls.initiated, "no refusal sets anything up")
	assert.Zero(t, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs`))
}

func TestPlaceCall_ChannelLimitPassesOverAndFrees(t *testing.T) {
	p := newPlacer(t)
	a := p.trunk(t, "A", 1, 0)
	b := p.trunk(t, "B", 1, 1)
	p.route(t, "44", a, 0)
	p.route(t, "44", b, 1)

	first, err := p.place("4420")
	require.NoError(t, err)
	assert.Contains(t, p.calls.last().DestEndpointURI, domain.TrunkEndpointIdentifier(a.ID))

	_, err = p.place("4420")
	require.NoError(t, err)
	assert.Contains(t, p.calls.last().DestEndpointURI, domain.TrunkEndpointIdentifier(b.ID), "a full trunk is passed over")

	_, err = p.place("4420")
	require.ErrorIs(t, err, ports.ErrTrunkCapacity, "no room anywhere refuses the call")

	// The first call ends — not through HangupCall, by the far end: the
	// next count notices on its own.
	p.calls.set(first.CallID, func(o *ports.CallOutcome) { o.Ended = true })
	_, err = p.place("4420")
	require.NoError(t, err)
	assert.Contains(t, p.calls.last().DestEndpointURI, domain.TrunkEndpointIdentifier(a.ID), "an ended call frees its channel")
}

func TestPlaceCall_EmergencyBypassesAndIgnoresTheLimit(t *testing.T) {
	p := newPlacer(t)
	tr := p.trunk(t, "Only", 1, 0)
	p.route(t, "11", tr, 0)
	p.route(t, "44", tr, 0)

	_, err := p.place("4420")
	require.NoError(t, err)
	assert.False(t, p.calls.last().BypassScreening)

	_, err = p.place("112")
	require.NoError(t, err, "an emergency call is never refused by the channel limit")
	assert.True(t, p.calls.last().BypassScreening, "and is placed with Screening bypassed")
	assert.Equal(t, 1, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs WHERE emergency AND released_at IS NULL`),
		"it is still recorded: it occupies a channel others see")

	_, err = p.place("1120")
	require.Error(t, err, "a near-emergency number is ordinary, and the trunk is now full")
	assert.ErrorIs(t, err, ports.ErrTrunkCapacity)
}

func TestPlaceCall_ScreenedOutIsARefusalWithItsReason(t *testing.T) {
	p := newPlacer(t)
	p.route(t, "44", p.trunk(t, "Primary", 5, 0), 0)
	p.calls.screenOut = "over_capacity"

	_, err := p.place("4420")
	require.ErrorIs(t, err, ports.ErrCallRefused)
	assert.Contains(t, err.Error(), "over_capacity")
	assert.Zero(t, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs WHERE released_at IS NULL`),
		"a refused call holds no channel")
}

// --- 4.3: the observer -----------------------------------------------------

func TestPlaceCall_OriginateFailureTakesTheTrunkOutOfSelection(t *testing.T) {
	p := newPlacer(t)
	bad := p.trunk(t, "Bad", 5, 0)
	good := p.trunk(t, "Good", 5, 1)
	p.route(t, "44", bad, 0)
	p.route(t, "44", good, 1)

	p.calls.failNext = errors.New("allocation failed")
	_, err := p.place("4420")
	require.Error(t, err)
	assert.Equal(t, domain.HealthUnhealthy, p.health(t, bad.ID))
	assert.Zero(t, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs WHERE released_at IS NULL`))

	_, err = p.place("4420")
	require.NoError(t, err)
	assert.Contains(t, p.calls.last().DestEndpointURI, domain.TrunkEndpointIdentifier(good.ID), "skipped by the next placement")
}

func TestPlaceCall_AnsweredCallReportsHealthyAndUnansweredReportsNothing(t *testing.T) {
	p := newPlacer(t)
	tr := p.trunk(t, "Primary", 5, 0)
	p.route(t, "44", tr, 0)
	require.NoError(t, p.svc.ReportRouteHealth(p.ctx, tr.ID, 200, 3*time.Second)) // degraded, still selectable
	require.Equal(t, domain.HealthDegraded, p.health(t, tr.ID))

	unanswered, err := p.place("4420")
	require.NoError(t, err)
	p.calls.set(unanswered.CallID, func(o *ports.CallOutcome) { o.Ended = true })
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, domain.HealthDegraded, p.health(t, tr.ID), "an unanswered call does not blame or clear the carrier")

	answered, err := p.place("4420")
	require.NoError(t, err)
	now := time.Now().UTC().Add(15 * time.Second) // rang for 15 s: a person, not the carrier
	p.calls.set(answered.CallID, func(o *ports.CallOutcome) { o.AnsweredAt = &now })
	assert.Eventually(t, func() bool { return p.health(t, tr.ID) == domain.HealthHealthy },
		2*time.Second, 20*time.Millisecond, "an answered leg is reported healthy, however long it rang")
}

// --- 4.4 -----------------------------------------------------------------

func TestHangupCall(t *testing.T) {
	p := newPlacer(t)
	tr := p.trunk(t, "Primary", 1, 0)
	p.route(t, "44", tr, 0)
	placed, err := p.place("4420")
	require.NoError(t, err)

	require.NoError(t, p.svc.HangupCall(p.ctx, p.tenant, placed.CallID))
	assert.Equal(t, []shareddomain.CallID{placed.CallID}, p.calls.hangups)
	assert.Zero(t, p.countTenantRow(t, `SELECT count(*) FROM placed_call_legs WHERE released_at IS NULL`), "the channel is freed")

	require.NoError(t, p.svc.HangupCall(p.ctx, p.tenant, placed.CallID), "hanging up twice is harmless")
	assert.Len(t, p.calls.hangups, 1, "and does nothing the second time")

	_, err = p.place("4420")
	require.NoError(t, err, "the freed channel is usable at once")

	otherID, otherCtx := p.otherTenant(t)
	otherSvc := p.svc
	assert.ErrorIs(t, otherSvc.HangupCall(otherCtx, otherID, placed.CallID), ports.ErrNotFound,
		"another tenant's call reads as nonexistent")
	assert.ErrorIs(t, p.svc.HangupCall(p.ctx, p.tenant, shareddomain.NewCallID()), ports.ErrNotFound,
		"a call never placed here cannot be hung up here")
}

// --- 4.2: the limit under concurrency, deterministically -------------------

// A placement holding the trunk's lock takes the last channel; a second
// placement must wait for it and then count afresh and refuse. Proved by
// holding the lock rather than racing goroutines, which pass by luck.
func TestPlaceCall_RacingPlacementsCannotShareTheLastChannel(t *testing.T) {
	p := newPlacer(t)
	tr := p.trunk(t, "Only", 1, 0)
	p.route(t, "44", tr, 0)
	legStore := pbxpg.NewCallLegStore()

	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- p.pool.WithTenant(context.Background(), p.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if err := legStore.LockTrunk(ctx, tx, tr.ID); err != nil {
				return err
			}
			trunkID := tr.ID
			if err := legStore.Insert(ctx, tx, domain.PlacedCallLeg{ID: newUUID(), TenantID: p.tenant,
				ExtensionID: p.from.Extension.ID, TrunkID: &trunkID, Destination: "4400", CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	done := make(chan error, 1)
	go func() { _, err := p.place("4420"); done <- err }()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("PlaceCall finished (%v) while another placement held the trunk's lock: it counted without serializing", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-holder)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ports.ErrTrunkCapacity, "the waiter counts after the holder commits and finds no room")
	case <-time.After(5 * time.Second):
		t.Fatal("PlaceCall never resumed")
	}
}

func newUUID() uuid.UUID { return uuid.New() }
