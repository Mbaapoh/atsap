package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/application"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fakes ---------------------------------------------------------------

type fakeMediaGateway struct {
	mu               sync.Mutex
	originateCalls   []ports.OriginateRequest
	bridgesCreated   int
	addedToBridge    []addedChannel
	destroyedBridges []ports.BridgeID
	destroyedChans   []ports.ChannelRef
	rungChans        []ports.ChannelRef
	answeredChans    []ports.ChannelRef
	rejectedChans    []rejectedChannel

	failOriginate     error
	failCreateBridge  error
	failAddToBridge   error
	failDestroyBridge error
}

type rejectedChannel struct {
	channelRef ports.ChannelRef
	cause      ports.RefusalCause
}

type addedChannel struct {
	bridgeID   ports.BridgeID
	channelRef ports.ChannelRef
}

func (f *fakeMediaGateway) Originate(_ context.Context, req ports.OriginateRequest) (ports.ChannelRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOriginate != nil {
		return "", f.failOriginate
	}
	f.originateCalls = append(f.originateCalls, req)
	// Echoes the requested channelId, matching real ARI semantics when
	// channelId is supplied on origination (D-19).
	return ports.ChannelRef(req.ChannelID), nil
}

func (f *fakeMediaGateway) CreateBridge(_ context.Context, _ string) (ports.BridgeID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreateBridge != nil {
		return "", f.failCreateBridge
	}
	f.bridgesCreated++
	return ports.BridgeID(fmt.Sprintf("bridge-%d", f.bridgesCreated)), nil
}

func (f *fakeMediaGateway) AddChannelToBridge(_ context.Context, bridgeID ports.BridgeID, channelRef ports.ChannelRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAddToBridge != nil {
		return f.failAddToBridge
	}
	f.addedToBridge = append(f.addedToBridge, addedChannel{bridgeID, channelRef})
	return nil
}

func (f *fakeMediaGateway) DestroyBridge(_ context.Context, bridgeID ports.BridgeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDestroyBridge != nil {
		return f.failDestroyBridge
	}
	f.destroyedBridges = append(f.destroyedBridges, bridgeID)
	return nil
}

func (f *fakeMediaGateway) StartPlayback(context.Context, ports.ChannelRef, string) error { return nil }
func (f *fakeMediaGateway) StartSnoop(context.Context, ports.ChannelRef, ports.SnoopRequest) (ports.SnoopRef, error) {
	return "", errors.New("StartSnoop: not implemented in this change")
}
func (f *fakeMediaGateway) DestroyChannel(_ context.Context, ref ports.ChannelRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyedChans = append(f.destroyedChans, ref)
	return nil
}

func (f *fakeMediaGateway) RingChannel(_ context.Context, ref ports.ChannelRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rungChans = append(f.rungChans, ref)
	return nil
}

func (f *fakeMediaGateway) AnswerChannel(_ context.Context, ref ports.ChannelRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answeredChans = append(f.answeredChans, ref)
	return nil
}

func (f *fakeMediaGateway) RejectChannel(_ context.Context, ref ports.ChannelRef, cause ports.RefusalCause) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejectedChans = append(f.rejectedChans, rejectedChannel{ref, cause})
	return nil
}

type channelHistoryEntry struct {
	participantID string
	channelRef    ports.ChannelRef
	bridgeID      ports.BridgeID
	eventType     string
}

type fakeCallStore struct {
	mu             sync.Mutex
	calls          map[string]*domain.Call
	saveCount      int
	channelHistory []channelHistoryEntry
	usageTicks     []domain.UsageTick
	savedEvents    []event.DomainEvent // every event ever passed to SaveCall, across all calls

	failSave           error
	failAddChanHistory error
	failRecordUsage    error
}

func newFakeCallStore() *fakeCallStore {
	return &fakeCallStore{calls: make(map[string]*domain.Call)}
}

// SaveCall stands in for the real transactional-outbox write
// (internal/telephony/postgres.CallStore.SaveCall persists call and
// events in one DB transaction; this fake just records both, proving
// the orchestrator passes events through on every save, not that they
// commit atomically — that guarantee is proven separately, by the real
// adapter's integration test).
func (f *fakeCallStore) SaveCall(_ context.Context, call *domain.Call, events []event.DomainEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSave != nil {
		return f.failSave
	}
	f.calls[call.ID.String()] = call
	f.saveCount++
	f.savedEvents = append(f.savedEvents, events...)
	return nil
}

func (f *fakeCallStore) GetCall(_ context.Context, _ shareddomain.TenantID, id shareddomain.CallID) (*domain.Call, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.calls[id.String()]
	if !ok {
		return nil, fmt.Errorf("call %s not found", id)
	}
	return c, nil
}

func (f *fakeCallStore) AddChannelHistory(_ context.Context, _ shareddomain.TenantID, participantID shareddomain.ParticipantID, channelRef ports.ChannelRef, bridgeID ports.BridgeID, eventType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAddChanHistory != nil {
		return f.failAddChanHistory
	}
	f.channelHistory = append(f.channelHistory, channelHistoryEntry{participantID.String(), channelRef, bridgeID, eventType})
	return nil
}

func (f *fakeCallStore) RecordUsageTicks(_ context.Context, ticks []domain.UsageTick) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRecordUsage != nil {
		return f.failRecordUsage
	}
	f.usageTicks = append(f.usageTicks, ticks...)
	return nil
}

func (f *fakeCallStore) get(id shareddomain.CallID) *domain.Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id.String()]
}

// --- test harness ----------------------------------------------------------

type harness struct {
	svc       *application.Service
	mediaGW   *fakeMediaGateway
	callStore *fakeCallStore
	registry  *acl.CorrelationRegistry
	license   *recordingLicense
}

func newHarness() *harness {
	h := &harness{
		mediaGW:   &fakeMediaGateway{},
		callStore: newFakeCallStore(),
		registry:  acl.NewCorrelationRegistry(),
		// The permissive stub this replaced counted nothing, so no test
		// could observe a reservation. Recording instead means the
		// reserve/release balance is assertable — the property D-58
		// protects — without changing what any existing test expects.
		license: newRecordingLicense(),
	}
	h.svc = application.NewService(
		h.mediaGW,
		h.callStore,
		h.license,
		application.NewAlwaysPermitCompliance(),
		h.registry,
		20,
		discardLogger(),
	)
	return h
}

func initiateTestCall(t *testing.T, h *harness) shareddomain.CallID {
	t.Helper()
	callID, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID:          shareddomain.NewTenantID(),
		Direction:         domain.Outbound,
		SourceNumber:      "1000",
		DestNumber:        "1001",
		SourceEndpointURI: "PJSIP/1000",
		DestEndpointURI:   "PJSIP/1001",
	})
	require.NoError(t, err)
	return callID
}

// answerBothParticipants drives the two ParticipantAnswered calls the
// real ACL event loop would produce once both originated channels reach
// Up, using the channel IDs the fake MediaGateway echoed back.
func answerBothParticipants(t *testing.T, h *harness, callID shareddomain.CallID) {
	t.Helper()
	require.Len(t, h.mediaGW.originateCalls, 2)
	for _, req := range h.mediaGW.originateCalls {
		corr, ok := h.registry.Lookup(req.ChannelID)
		require.True(t, ok, "correlation should be registered for %s", req.ChannelID)
		err := h.svc.ParticipantAnswered(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID, req.ChannelID)
		require.NoError(t, err)
	}
	_ = callID
}

// rewindAnsweredAt sets every participant's AnsweredAt back by d,
// simulating a real multi-second connected interval deterministically
// (no sleep). The fake CallStore holds the same *domain.Call pointer the
// Service mutates (SaveCall never copies), so this reaches the live
// object the next Service call will operate on.
func rewindAnsweredAt(t *testing.T, h *harness, callID shareddomain.CallID, d time.Duration) {
	t.Helper()
	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	for _, p := range saved.Participants {
		require.NotNil(t, p.AnsweredAt)
		rewound := p.AnsweredAt.Add(-d)
		p.AnsweredAt = &rewound
	}
}

// --- tests -----------------------------------------------------------------

// TestInitiateCall_ReachesActive covers task 6.1: InitiateCall through
// to Active, using a fake MediaGateway/CallStore and the real
// always-permit stub adapters.
func TestInitiateCall_ReachesActive(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)

	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallPresenting, saved.State, "InitiateCall should leave the call Presenting, awaiting answers")
	assert.Len(t, h.mediaGW.originateCalls, 2, "both legs are originated")
	assert.Len(t, h.callStore.channelHistory, 2)

	answerBothParticipants(t, h, callID)

	saved = h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallActive, saved.State)
	assert.Equal(t, 1, h.mediaGW.bridgesCreated, "one bridge, created up front by InitiateCall")
	assert.Len(t, h.mediaGW.addedToBridge, 2, "both legs added to the same bridge")
	assert.Equal(t, h.mediaGW.addedToBridge[0].bridgeID, h.mediaGW.addedToBridge[1].bridgeID)

	var sawInitiated, sawActive bool
	for _, e := range h.callStore.savedEvents {
		switch e.(type) {
		case domain.CallInitiatedEvent:
			sawInitiated = true
		case domain.CallActiveEvent:
			sawActive = true
		}
	}
	assert.True(t, sawInitiated)
	assert.True(t, sawActive)
}

// TestInitiateCall_ScreeningRejection covers a call whose capacity or
// compliance verdict is not permitted — the stub adapters always permit,
// so this uses a rejecting fake in their place.
func TestInitiateCall_ScreeningRejection(t *testing.T) {
	h := newHarness()
	h.svc = application.NewService(
		h.mediaGW, h.callStore, rejectingLicense{}, application.NewAlwaysPermitCompliance(),
		h.registry, 20, discardLogger(),
	)

	callID, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.NoError(t, err)

	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
	assert.Contains(t, saved.TerminationReason, "capacity")
	assert.Empty(t, h.mediaGW.originateCalls, "a screened-out call must never originate")
}

type rejectingLicense struct{}

func (rejectingLicense) ValidateCapacity(context.Context, shareddomain.TenantID, string, int) (ports.CapacityVerdict, error) {
	return ports.CapacityVerdict{Permitted: false, Reason: "over capacity"}, nil
}

func (rejectingLicense) ReleaseCapacity(context.Context, string) error { return nil }

type failingLicense struct{}

func (failingLicense) ValidateCapacity(context.Context, shareddomain.TenantID, string, int) (ports.CapacityVerdict, error) {
	return ports.CapacityVerdict{}, errors.New("entitlement service unreachable")
}

func (failingLicense) ReleaseCapacity(context.Context, string) error { return nil }

// recordingLicense observes reservation and release so a test can assert
// the count returns to where it started, which is the property D-58
// exists to protect. A plain counter would hide a double release; the
// per-call map does not.
type recordingLicense struct {
	mu        sync.Mutex
	reserved  map[string]int
	released  map[string]int
	permitted bool
}

func newRecordingLicense() *recordingLicense {
	return &recordingLicense{reserved: map[string]int{}, released: map[string]int{}, permitted: true}
}

func (r *recordingLicense) ValidateCapacity(_ context.Context, _ shareddomain.TenantID, callID string, _ int) (ports.CapacityVerdict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.permitted {
		return ports.CapacityVerdict{Permitted: false, Reason: "over capacity"}, nil
	}
	r.reserved[callID]++
	return ports.CapacityVerdict{Permitted: true}, nil
}

func (r *recordingLicense) ReleaseCapacity(_ context.Context, callID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released[callID]++
	return nil
}

func (r *recordingLicense) counts(callID string) (reserved, released int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reserved[callID], r.released[callID]
}

type failingCompliance struct{}

func (failingCompliance) Evaluate(context.Context, shareddomain.TenantID, string) (ports.ComplianceVerdict, error) {
	return ports.ComplianceVerdict{}, errors.New("compliance service unreachable")
}

// TestInitiateCall_LicenseServiceFailure and
// TestInitiateCall_ComplianceServiceFailure cover the port *failing*
// (a network/db error), distinct from ApplyScreeningVerdict's own
// rejected-but-answered verdict path (TestInitiateCall_ScreeningRejection).
func TestInitiateCall_LicenseServiceFailure(t *testing.T) {
	h := newHarness()
	h.svc = application.NewService(
		h.mediaGW, h.callStore, failingLicense{}, application.NewAlwaysPermitCompliance(),
		h.registry, 20, discardLogger(),
	)
	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	assert.ErrorContains(t, err, "entitlement service unreachable")
}

func TestInitiateCall_ComplianceServiceFailure(t *testing.T) {
	h := newHarness()
	h.svc = application.NewService(
		h.mediaGW, h.callStore, h.license, failingCompliance{},
		h.registry, 20, discardLogger(),
	)
	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	assert.ErrorContains(t, err, "compliance service unreachable")
}

// denyCompliance refuses every evaluation and counts calls, so a test can
// prove the emergency path never consulted it.
type denyCompliance struct{ calls int }

func (d *denyCompliance) Evaluate(context.Context, shareddomain.TenantID, string) (ports.ComplianceVerdict, error) {
	d.calls++
	return ports.ComplianceVerdict{Permitted: false, Reason: "do-not-call"}, nil
}

// TestInitiateCall_EmergencyBypassesScreeningConsults is task 6.3 at unit
// level (INV-01, spec capacity-enforcement): with capacity refusing and
// compliance refusing, an emergency call still proceeds to Presenting,
// reserves nothing, and tears down cleanly — while the ordinary control
// is screened out. DestNumber "112" is a label only: identification is
// pbx-core's job (LLD-03); the flag stands in for it here.
func TestInitiateCall_EmergencyBypassesScreeningConsults(t *testing.T) {
	h := newHarness()
	h.license.permitted = false
	deny := &denyCompliance{}
	h.svc = application.NewService(
		h.mediaGW, h.callStore, h.license, deny,
		h.registry, 20, discardLogger(),
	)

	// Control first: without the flag, both refusals screen the call out.
	refused, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.NoError(t, err, "screened-out is a verdict, not an error")
	assert.Equal(t, domain.CallTerminated, h.callStore.get(refused).State)
	require.Equal(t, 1, deny.calls, "control must consult compliance or the test is vacuous")

	callID, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "112", BypassScreening: true,
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.CallPresenting, h.callStore.get(callID).State,
		"emergency proceeds past Screening with both consults refusing")
	assert.Len(t, h.mediaGW.originateCalls, 2, "both legs originated")
	reserved, _ := h.license.counts(callID.String())
	assert.Zero(t, reserved, "emergency takes no reservation")
	assert.Equal(t, 1, deny.calls, "compliance consulted once (control only), never for emergency")

	// Teardown lands on the documented no-op: nothing reserved, nothing freed.
	require.NoError(t, h.svc.HangupCall(context.Background(), callID, "test complete"))
	reserved, released := h.license.counts(callID.String())
	assert.Zero(t, reserved)
	assert.Equal(t, 1, released, "unconditional release fired and was a no-op")
}

// errLicense fails every check: the never-licensed installation, where
// there is no entitlement to consult at all and ValidateCapacity fails
// closed with an error rather than a refusing verdict.
type errLicense struct{}

func (errLicense) ValidateCapacity(context.Context, shareddomain.TenantID, string, int) (ports.CapacityVerdict, error) {
	return ports.CapacityVerdict{}, errors.New("no entitlement")
}

func (errLicense) ReleaseCapacity(context.Context, string) error { return nil }

// TestInitiateCall_EmergencyProceedsWhenLicensingErrors covers the
// fresh-installation path of task 6.3: with licensing erroring (not
// merely refusing), the ordinary setup fails with that error while the
// emergency call proceeds — the bypass sits before either consult and
// never distinguishes error from refusal.
func TestInitiateCall_EmergencyProceedsWhenLicensingErrors(t *testing.T) {
	h := newHarness()
	deny := &denyCompliance{}
	h.svc = application.NewService(
		h.mediaGW, h.callStore, errLicense{}, deny,
		h.registry, 20, discardLogger(),
	)

	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.Error(t, err, "no licence means no call path")

	callID, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "112", BypassScreening: true,
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.CallPresenting, h.callStore.get(callID).State)
	assert.Zero(t, deny.calls, "neither consult runs for emergency, error path included")
}

// TestParticipantAnswered_SaveFailure and
// TestHangupCall_DestroyBridgeFailure_StillTerminates cover
// persistAndPublish and finalizeTermination's own failure paths beyond
// what InitiateCall's tests already exercise.
func TestParticipantAnswered_SaveFailure(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	req := h.mediaGW.originateCalls[0]
	corr, ok := h.registry.Lookup(req.ChannelID)
	require.True(t, ok)

	h.callStore.failSave = errors.New("db unavailable")
	err := h.svc.ParticipantAnswered(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID, req.ChannelID)
	assert.ErrorContains(t, err, "db unavailable")
	_ = callID
}

func TestHangupCall_DestroyBridgeFailure_StillTerminates(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)
	h.mediaGW.failDestroyBridge = errors.New("bridge already gone")

	// DestroyBridge failing must not block the domain record from
	// reaching Terminated (finalizeTermination logs, does not return,
	// this specific error).
	err := h.svc.HangupCall(context.Background(), callID, "test")
	require.NoError(t, err)
	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
}

// TestHangupCall_TerminatesAndIsImmutable covers task 6.2.
func TestHangupCall_TerminatesAndIsImmutable(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)

	err := h.svc.HangupCall(context.Background(), callID, "caller hung up")
	require.NoError(t, err)

	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
	assert.Equal(t, "caller hung up", saved.TerminationReason)
	assert.Len(t, h.mediaGW.destroyedBridges, 1)

	err = h.svc.HangupCall(context.Background(), callID, "again")
	assert.ErrorIs(t, err, application.ErrCallNotFound, "a Terminated call is removed from the in-flight set")
}

// HangupCall must release the legs on the engine, not only the bridge:
// destroying the bridge alone left both phones on the line with media
// flowing while the call read Terminated (pbx-call-placement UAT).
func TestHangupCall_ReleasesEveryLegOnTheEngine(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)

	require.NoError(t, h.svc.HangupCall(context.Background(), callID, "api hangup"))
	assert.Len(t, h.mediaGW.destroyedChans, 2, "both legs are hung up")
	for _, ref := range h.mediaGW.destroyedChans {
		assert.True(t, strings.HasPrefix(string(ref), "atsa-part-"), "addressed by the channel each leg was originated with")
	}
	originated := map[string]bool{}
	for _, o := range h.mediaGW.originateCalls {
		originated[o.ChannelID] = true
	}
	for _, ref := range h.mediaGW.destroyedChans {
		assert.True(t, originated[string(ref)], "%s must be a channel this call originated", ref)
	}
}

// A call hung up while still ringing releases its ringing legs too.
func TestHangupCall_ReleasesRingingLegs(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)

	require.NoError(t, h.svc.HangupCall(context.Background(), callID, "abandoned"))
	assert.Len(t, h.mediaGW.destroyedChans, 2, "legs that never answered are released as well")
}

// TestParticipantLeft_LastConnectedTerminates covers the natural
// hangup path (ChannelDestroyed events, not an explicit HangupCall):
// disconnecting the last connected participant reaches Terminated.
func TestParticipantLeft_LastConnectedTerminates(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)

	var corrs []acl.Correlation
	for _, req := range h.mediaGW.originateCalls {
		corr, ok := h.registry.Lookup(req.ChannelID)
		require.True(t, ok)
		corrs = append(corrs, corr)
	}

	ctx := context.Background()
	require.NoError(t, h.svc.ParticipantLeft(ctx, corrs[0].TenantID, corrs[0].CallID, corrs[0].ParticipantID))

	// One party leaving ends a two-party call, and the OTHER party's leg
	// is released on the engine — not left on a dead bridge, in silence,
	// accruing usage.
	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
	assert.Equal(t, "normal clearing", saved.TerminationReason)
	require.Len(t, h.mediaGW.destroyedChans, 1, "the remaining party's leg is hung up")
	assert.Equal(t, "atsa-part-"+corrs[1].ParticipantID, string(h.mediaGW.destroyedChans[0]))
	for _, p := range saved.Participants {
		assert.Equal(t, domain.ParticipantDisconnected, p.State, "nobody is left connected")
	}

	// The second leg's own ChannelDestroyed then arrives for a call that
	// is already over: the expected echo, not a second ending.
	err := h.svc.ParticipantLeft(ctx, corrs[1].TenantID, corrs[1].CallID, corrs[1].ParticipantID)
	assert.ErrorIs(t, err, application.ErrCallNotFound)
}

// TestParticipantLeft_NeverAnswered_TerminatesDirectly covers the
// "Destination does not answer" path: a leg destroyed (e.g. Asterisk's
// own alerting timeout) before the call ever reached Active terminates
// the whole call directly, never via DisconnectParticipant's mid-call
// path (spec: "the call moves to Terminated without ever reaching
// Active, and no usage is recorded for that attempt").
func TestParticipantLeft_NeverAnswered_TerminatesDirectly(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)

	req := h.mediaGW.originateCalls[0]
	corr, ok := h.registry.Lookup(req.ChannelID)
	require.True(t, ok)

	err := h.svc.ParticipantLeft(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID)
	require.NoError(t, err)

	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
	assert.Empty(t, h.callStore.usageTicks, "no usage for a leg that never connected")
	_ = callID
}

// TestNotImplementedMethods covers AnswerParticipant, HoldParticipant,
// and TransferParticipant: not built in this change (LLD-01 §1), but
// must fail loudly and identifiably, never silently no-op.
func TestNotImplementedMethods(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	err := h.svc.AnswerParticipant(ctx, shareddomain.NewCallID(), shareddomain.NewParticipantID())
	assert.ErrorIs(t, err, application.ErrNotImplemented)

	err = h.svc.HoldParticipant(ctx, shareddomain.NewCallID(), shareddomain.NewParticipantID())
	assert.ErrorIs(t, err, application.ErrNotImplemented)

	err = h.svc.TransferParticipant(ctx, ports.TransferCommand{})
	assert.ErrorIs(t, err, application.ErrNotImplemented)
}

// TestInitiateCall_OriginateFailure covers a MediaGateway failure during
// origination: the error must propagate, not be swallowed.
func TestInitiateCall_OriginateFailure(t *testing.T) {
	h := newHarness()
	h.mediaGW.failOriginate = errors.New("asterisk unreachable")

	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	assert.ErrorContains(t, err, "asterisk unreachable")
}

func TestInitiateCall_SaveCallFailure(t *testing.T) {
	h := newHarness()
	h.callStore.failSave = errors.New("db unavailable")

	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	assert.ErrorContains(t, err, "db unavailable")
}

func TestInitiateCall_ChannelHistoryFailure(t *testing.T) {
	h := newHarness()
	h.callStore.failAddChanHistory = errors.New("db write failed")

	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	assert.ErrorContains(t, err, "db write failed")
}

// TestParticipantAnswered_UnknownCall / TestParticipantLeft_UnknownCall
// cover an ARI event for a call this process has no in-flight record of
// (e.g. after a restart — an accepted limitation, see LLD-01 §9).
func TestParticipantAnswered_UnknownCall(t *testing.T) {
	h := newHarness()
	err := h.svc.ParticipantAnswered(context.Background(), "tenant-1", shareddomain.NewCallID().String(), shareddomain.NewParticipantID().String(), "chan1")
	assert.ErrorIs(t, err, application.ErrCallNotFound)
}

func TestParticipantAnswered_BadCallID(t *testing.T) {
	h := newHarness()
	err := h.svc.ParticipantAnswered(context.Background(), "tenant-1", "not-a-uuid", shareddomain.NewParticipantID().String(), "chan1")
	assert.Error(t, err)
}

func TestParticipantAnswered_BadParticipantID(t *testing.T) {
	h := newHarness()
	err := h.svc.ParticipantAnswered(context.Background(), "tenant-1", shareddomain.NewCallID().String(), "not-a-uuid", "chan1")
	assert.Error(t, err)
}

// TestInitiateCall_BridgeCreateFailure: the bridge is created inside
// InitiateCall itself, before either leg is originated (real-Asterisk
// SDP timing — see EventLoop's doc comment), so a bridge-create failure
// surfaces there, not later from ParticipantAnswered.
func TestInitiateCall_BridgeCreateFailure(t *testing.T) {
	h := newHarness()
	h.mediaGW.failCreateBridge = errors.New("bridge create failed")

	_, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID:          shareddomain.NewTenantID(),
		Direction:         domain.Outbound,
		SourceNumber:      "1000",
		DestNumber:        "1001",
		SourceEndpointURI: "PJSIP/1000",
		DestEndpointURI:   "PJSIP/1001",
	})
	assert.ErrorContains(t, err, "bridge create failed")
	assert.Empty(t, h.mediaGW.originateCalls, "must not originate either leg once the bridge can't be created")
}

func TestParticipantAnswered_AddToBridgeFailure(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	h.mediaGW.failAddToBridge = errors.New("add to bridge failed")

	req := h.mediaGW.originateCalls[0]
	corr, ok := h.registry.Lookup(req.ChannelID)
	require.True(t, ok)

	err := h.svc.ParticipantAnswered(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID, req.ChannelID)
	assert.ErrorContains(t, err, "add to bridge failed")
	_ = callID
}

func TestParticipantLeft_UnknownCall(t *testing.T) {
	h := newHarness()
	err := h.svc.ParticipantLeft(context.Background(), "tenant-1", shareddomain.NewCallID().String(), shareddomain.NewParticipantID().String())
	assert.ErrorIs(t, err, application.ErrCallNotFound)
}

func TestParticipantLeft_BadCallID(t *testing.T) {
	h := newHarness()
	err := h.svc.ParticipantLeft(context.Background(), "tenant-1", "not-a-uuid", shareddomain.NewParticipantID().String())
	assert.Error(t, err)
}

func TestParticipantLeft_BadParticipantID(t *testing.T) {
	h := newHarness()
	err := h.svc.ParticipantLeft(context.Background(), "tenant-1", shareddomain.NewCallID().String(), "not-a-uuid")
	assert.Error(t, err)
}

func TestParticipantLeft_UnknownParticipantOnCall(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)

	err := h.svc.ParticipantLeft(context.Background(), "tenant-1", callID.String(), shareddomain.NewParticipantID().String())
	assert.Error(t, err)
}

func TestHangupCall_UnknownCall(t *testing.T) {
	h := newHarness()
	err := h.svc.HangupCall(context.Background(), shareddomain.NewCallID(), "reason")
	assert.ErrorIs(t, err, application.ErrCallNotFound)
}

func TestHangupCall_RecordUsageFailure_StillTerminates(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)
	rewindAnsweredAt(t, h, callID, 5*time.Second) // ensure GenerateUsageTicks actually produces ticks
	h.callStore.failRecordUsage = errors.New("usage write failed")

	// recordUsageForParticipant's error is logged, not fatal — a usage
	// write failure must never block the call from reaching Terminated.
	err := h.svc.HangupCall(context.Background(), callID, "test")
	require.NoError(t, err)

	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
}

// TestUsageTicks_RecordedOnDisconnect covers task 6.3: continuous,
// non-duplicated per-second usage ticks for a Connected participant,
// persisted via CallStore when its connected interval ends.
//
// answerBothParticipants and HangupCall below happen microseconds apart
// in a unit test, so real elapsed time would round down to zero whole
// seconds and produce no ticks — not a bug in the scheduling, just an
// unrealistic clock for a fast unit test. The fake CallStore holds the
// same *domain.Call pointer the Service mutates (SaveCall never copies),
// so rewinding each participant's AnsweredAt here simulates a real
// multi-second connected interval deterministically, with no sleep.
func TestUsageTicks_RecordedOnDisconnect(t *testing.T) {
	h := newHarness()
	callID := initiateTestCall(t, h)
	answerBothParticipants(t, h, callID)

	rewindAnsweredAt(t, h, callID, 5*time.Second)

	require.NoError(t, h.svc.HangupCall(context.Background(), callID, "test teardown"))

	require.NotEmpty(t, h.callStore.usageTicks, "two participants were connected: ticks must exist")

	seen := make(map[string]bool)
	for _, tick := range h.callStore.usageTicks {
		key := tick.ParticipantID.String() + "|" + tick.SecondTS.String()
		assert.False(t, seen[key], "duplicate usage tick: %s", key)
		seen[key] = true
		assert.Equal(t, callID, tick.CallID)
	}
}

// TestCapacityIsReleasedOnEveryTerminationRoute is task 4.2 and the
// telephony half of D-58.
//
// A call that reserved a channel must return it however it ends. The
// failure mode this guards is quiet and expensive: a route that forgets
// to release leaves the count high, so the installation refuses calls it
// should permit, and nothing indicates why.
func TestCapacityIsReleasedOnEveryTerminationRoute(t *testing.T) {
	ctx := context.Background()

	t.Run("normal hangup", func(t *testing.T) {
		h := newHarness()
		callID := initiateTestCall(t, h)

		require.NoError(t, h.svc.HangupCall(ctx, callID, "caller hung up"))

		reserved, released := h.license.counts(callID.String())
		assert.Equal(t, 1, reserved)
		assert.Equal(t, 1, released, "a completed call returns its channel")
	})

	t.Run("last participant leaves", func(t *testing.T) {
		h := newHarness()
		callID := initiateTestCall(t, h)
		call := h.callStore.get(callID)
		require.NotNil(t, call)

		for _, p := range call.Participants {
			_ = h.svc.ParticipantLeft(ctx, "", callID.String(), p.ID.String())
		}

		_, released := h.license.counts(callID.String())
		assert.Equal(t, 1, released, "a call ended by its last participant returns its channel")
	})

	t.Run("terminating twice releases once", func(t *testing.T) {
		h := newHarness()
		callID := initiateTestCall(t, h)

		require.NoError(t, h.svc.HangupCall(ctx, callID, "caller hung up"))
		// A second termination signal for the same call — a duplicate
		// StasisEnd, a retried hangup, an ACL redelivery.
		_ = h.svc.HangupCall(ctx, callID, "caller hung up again")

		_, released := h.license.counts(callID.String())
		assert.Equal(t, 1, released,
			"terminating twice must free one channel, not two — under-counting permits calls that should be refused")
	})
}

// TestScreenedOutCallReleasesNothing: a call refused at Screening never
// reserved anything, so its termination must not free someone else's
// channel.
func TestScreenedOutCallReleasesNothing(t *testing.T) {
	h := newHarness()
	h.license.permitted = false
	h.svc = application.NewService(
		h.mediaGW, h.callStore, h.license, application.NewAlwaysPermitCompliance(),
		h.registry, 20, discardLogger(),
	)

	callID, err := h.svc.InitiateCall(context.Background(), ports.InitiateCallCommand{
		TenantID: shareddomain.NewTenantID(), Direction: domain.Outbound,
		SourceNumber: "1000", DestNumber: "1001",
		SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
	})
	require.NoError(t, err)

	reserved, _ := h.license.counts(callID.String())
	assert.Zero(t, reserved, "a refused call consumes no channel")
}

// --- arriving calls (pbx-inbound-and-emergency task 2.2) --------------------

// fakeInboundRouter stands in for pbx-core's router: telephony owns the
// port, so the fake lives on this side of the seam (the same arrangement
// as BypassScreening in InitiateCallCommand).
type fakeInboundRouter struct {
	mu       sync.Mutex
	decision ports.InboundDecision
	err      error
	calls    []ports.InboundRequest
}

func (f *fakeInboundRouter) ResolveInbound(_ context.Context, req ports.InboundRequest) (ports.InboundDecision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return f.decision, f.err
}

func connectDecision() ports.InboundDecision {
	return ports.InboundDecision{DestinationURI: "PJSIP/1001", Direction: domain.Inbound}
}

// arrivedHarness wires a harness whose Service consults router for
// arriving calls.
func arrivedHarness(router ports.InboundRouter) *harness {
	h := newHarness()
	h.svc.SetInboundRouter(router)
	return h
}

// lastSavedCall returns the single call the fake store holds. Arriving
// calls allocate their own ID inside CallArrived, so tests cannot name
// it up front — but each test creates exactly one.
func lastSavedCall(t *testing.T, h *harness) *domain.Call {
	t.Helper()
	h.callStore.mu.Lock()
	defer h.callStore.mu.Unlock()
	require.Len(t, h.callStore.calls, 1, "exactly one call should be persisted")
	for _, c := range h.callStore.calls {
		return c
	}
	return nil
}

func arriveTestCall(t *testing.T, h *harness, tenant string) {
	t.Helper()
	err := h.svc.CallArrived(context.Background(), tenant, "e_caller", "2000", "+1555000111", "chan-arriving")
	require.NoError(t, err)
}

// TestCallArrived_ConnectsToActive covers the arriving-call happy path:
// router connects, caller hears ringing, destination originates, and the
// destination answering answers the caller and bridges both to Active.
func TestCallArrived_ConnectsToActive(t *testing.T) {
	router := &fakeInboundRouter{decision: connectDecision()}
	h := arrivedHarness(router)
	tenant := shareddomain.NewTenantID()
	arriveTestCall(t, h, tenant.String())

	require.Len(t, router.calls, 1, "the router decides every arriving call")
	assert.Equal(t, tenant, router.calls[0].TenantID)
	assert.Equal(t, "e_caller", router.calls[0].SourceEndpoint)
	assert.Equal(t, "2000", router.calls[0].Dialled)
	assert.Equal(t, "+1555000111", router.calls[0].CallerNumber)
	assert.NotEmpty(t, router.calls[0].CallID)

	saved := lastSavedCall(t, h)
	assert.Equal(t, domain.CallPresenting, saved.State)
	require.Len(t, saved.Participants, 2, "caller plus originated destination")
	assert.Equal(t, []ports.ChannelRef{"chan-arriving"}, h.mediaGW.rungChans, "caller hears ringing while the destination sets up")
	require.Len(t, h.mediaGW.originateCalls, 1, "only the destination leg is originated")
	assert.Equal(t, "PJSIP/1001", h.mediaGW.originateCalls[0].EndpointURI)
	assert.Empty(t, h.mediaGW.answeredChans, "caller is answered only when the far end answers, so it hears ringing, not early media")

	corr, ok := h.registry.Lookup(h.mediaGW.originateCalls[0].ChannelID)
	require.True(t, ok, "destination leg is correlated under its originated id")
	require.NoError(t, h.svc.ParticipantAnswered(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID, h.mediaGW.originateCalls[0].ChannelID))

	saved = lastSavedCall(t, h)
	assert.Equal(t, domain.CallActive, saved.State, "caller answered + both bridged: Active by the two-connected rule")
	assert.Equal(t, []ports.ChannelRef{"chan-arriving"}, h.mediaGW.answeredChans)
	require.Len(t, h.mediaGW.addedToBridge, 2, "destination and caller share one bridge")
	assert.Equal(t, h.mediaGW.addedToBridge[0].bridgeID, h.mediaGW.addedToBridge[1].bridgeID)
}

// TestCallArrived_RecordsTheRoutersDirectionAndCaller covers
// phase-a-hardening task 1.1: the router decides what an arriving call is
// — a desk phone calling another extension is INTERNAL, from that
// extension's number, not from the engine endpoint the channel carried —
// and an empty caller keeps the channel's own number (a carrier call).
func TestCallArrived_RecordsTheRoutersDirectionAndCaller(t *testing.T) {
	cases := []struct {
		name       string
		decision   ports.InboundDecision
		wantDir    domain.Direction
		wantSource string
	}{
		{
			name:       "desk phone to extension",
			decision:   ports.InboundDecision{DestinationURI: "PJSIP/1002", Direction: domain.Internal, CallerNumber: "1001", CallerName: "Reception"},
			wantDir:    domain.Internal,
			wantSource: "1001",
		},
		{
			name:       "carrier call keeps the channel's number",
			decision:   connectDecision(),
			wantDir:    domain.Inbound,
			wantSource: "+1555000111",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := arrivedHarness(&fakeInboundRouter{decision: tc.decision})
			arriveTestCall(t, h, shareddomain.NewTenantID().String())

			saved := lastSavedCall(t, h)
			assert.Equal(t, tc.wantDir, saved.Direction)
			assert.Equal(t, tc.wantSource, saved.SourceNumber)
			assert.Equal(t, "2000", saved.DestNumber, "the dialled number is unchanged")
		})
	}
}

// TestCallArrived_Refusals covers every router refusal cause: the
// channel is rejected with the mapped cause, the call terminates with
// the reason, and no other leg is ever originated.
func TestCallArrived_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  ports.RefusalCause
		reason string
	}{
		{"not found", ports.RefusalNotFound, "inbound call refused: destination not found"},
		{"unavailable", ports.RefusalUnavailable, "inbound call refused: no capacity"},
		{"forbidden", ports.RefusalForbidden, "inbound call refused: compliance rejected the call"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &fakeInboundRouter{decision: ports.InboundDecision{Refuse: tc.cause}}
			h := arrivedHarness(router)
			arriveTestCall(t, h, shareddomain.NewTenantID().String())

			require.Len(t, h.mediaGW.rejectedChans, 1)
			assert.Equal(t, ports.ChannelRef("chan-arriving"), h.mediaGW.rejectedChans[0].channelRef)
			assert.Equal(t, tc.cause, h.mediaGW.rejectedChans[0].cause)
			saved := lastSavedCall(t, h)
			assert.Equal(t, domain.CallTerminated, saved.State)
			assert.Equal(t, tc.reason, saved.TerminationReason)
			assert.Empty(t, h.mediaGW.originateCalls, "a refused call originates no other leg")
			assert.Empty(t, h.mediaGW.rungChans, "a refused call never rings")
		})
	}
}

// TestCallArrived_ScreeningRefusals covers Screening on the arrived
// path: a capacity refusal is a 503 (Unavailable) and a compliance
// refusal a 403 (Forbidden).
func TestCallArrived_ScreeningRefusals(t *testing.T) {
	t.Run("capacity refused", func(t *testing.T) {
		h := arrivedHarness(&fakeInboundRouter{decision: connectDecision()})
		h.license.permitted = false
		arriveTestCall(t, h, shareddomain.NewTenantID().String())

		require.Len(t, h.mediaGW.rejectedChans, 1)
		assert.Equal(t, ports.RefusalUnavailable, h.mediaGW.rejectedChans[0].cause)
		assert.Equal(t, domain.CallTerminated, lastSavedCall(t, h).State)
		assert.Empty(t, h.mediaGW.originateCalls)
	})

	t.Run("compliance refused", func(t *testing.T) {
		h := arrivedHarness(&fakeInboundRouter{decision: connectDecision()})
		deny := &denyCompliance{}
		h.svc = application.NewService(
			h.mediaGW, h.callStore, h.license, deny,
			h.registry, 20, discardLogger(),
		)
		h.svc.SetInboundRouter(&fakeInboundRouter{decision: connectDecision()})
		arriveTestCall(t, h, shareddomain.NewTenantID().String())

		require.Len(t, h.mediaGW.rejectedChans, 1)
		assert.Equal(t, ports.RefusalForbidden, h.mediaGW.rejectedChans[0].cause)
		assert.Equal(t, domain.CallTerminated, lastSavedCall(t, h).State)
	})
}

// TestCallArrived_BypassSkipsScreeningUnderExhaustion covers the
// emergency path: a bypassed call connects with licensed capacity
// exhausted, consulting neither capacity nor compliance.
func TestCallArrived_BypassSkipsScreeningUnderExhaustion(t *testing.T) {
	router := &fakeInboundRouter{decision: ports.InboundDecision{
		DestinationURI: "PJSIP/1001", Direction: domain.Inbound, BypassScreening: true,
	}}
	h := arrivedHarness(router)
	h.license.permitted = false
	deny := &denyCompliance{}
	h.svc = application.NewService(
		h.mediaGW, h.callStore, h.license, deny,
		h.registry, 20, discardLogger(),
	)
	h.svc.SetInboundRouter(router)

	arriveTestCall(t, h, shareddomain.NewTenantID().String())

	assert.Equal(t, domain.CallPresenting, lastSavedCall(t, h).State)
	assert.Len(t, h.mediaGW.originateCalls, 1)
	assert.Zero(t, deny.calls, "a bypassed call never consults compliance")
}

// TestCallArrived_CallerHangupWhileRingingReleasesDestination covers the
// race the design calls out: the caller leaves before the destination
// answers, so the still-ringing destination leg must be released rather
// than ringing forever for a call that no longer exists.
func TestCallArrived_CallerHangupWhileRingingReleasesDestination(t *testing.T) {
	h := arrivedHarness(&fakeInboundRouter{decision: connectDecision()})
	arriveTestCall(t, h, shareddomain.NewTenantID().String())
	require.Len(t, h.mediaGW.originateCalls, 1)
	destChannelID := h.mediaGW.originateCalls[0].ChannelID

	corr, ok := h.registry.Lookup("chan-arriving")
	require.True(t, ok, "arriving caller is correlated under its own channel id")
	require.NoError(t, h.svc.ParticipantLeft(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID))

	assert.Equal(t, domain.CallTerminated, lastSavedCall(t, h).State)
	assert.Contains(t, h.mediaGW.destroyedChans, ports.ChannelRef(destChannelID),
		"the ringing destination leg is released on caller hangup")
}

// TestCallArrived_RouterErrorRefuses covers the router failing: the call
// is refused (Unavailable — retryable, never a wrong 404) rather than
// left hanging in silence.
func TestCallArrived_RouterErrorRefuses(t *testing.T) {
	router := &fakeInboundRouter{err: errors.New("router store unreachable")}
	h := arrivedHarness(router)
	arriveTestCall(t, h, shareddomain.NewTenantID().String())

	require.Len(t, h.mediaGW.rejectedChans, 1)
	assert.Equal(t, ports.RefusalUnavailable, h.mediaGW.rejectedChans[0].cause)
	assert.Equal(t, domain.CallTerminated, lastSavedCall(t, h).State)
	assert.Empty(t, h.mediaGW.originateCalls)
}

// TestCallArrived_NoTenantVariableRefusesWithoutRouter covers design D1:
// a channel carrying no tenant never reaches the router and is refused
// NotFound (404).
func TestCallArrived_NoTenantVariableRefusesWithoutRouter(t *testing.T) {
	router := &fakeInboundRouter{decision: connectDecision()}
	h := arrivedHarness(router)

	require.NoError(t, h.svc.CallArrived(context.Background(), "", "e_caller", "2000", "+1555000111", "chan-unknown"))
	require.NoError(t, h.svc.CallArrived(context.Background(), "not-a-uuid", "e_caller", "2000", "+1555000111", "chan-garbage"))

	assert.Empty(t, router.calls, "no tenant means no routing decision to make")
	require.Len(t, h.mediaGW.rejectedChans, 2)
	for _, r := range h.mediaGW.rejectedChans {
		assert.Equal(t, ports.RefusalNotFound, r.cause)
	}
	assert.Empty(t, h.mediaGW.originateCalls)
}

// TestCallArrived_DefaultRouterRefusesNotFound covers LLD-03 DoD 15 at
// unit level: a Service wired with no pbx-core refuses every arriving
// call as not found (the e2e half — the walking-skeleton suites passing
// unmodified — is task 2.4's gate).
func TestCallArrived_DefaultRouterRefusesNotFound(t *testing.T) {
	h := newHarness()
	require.NoError(t, h.svc.CallArrived(context.Background(),
		shareddomain.NewTenantID().String(), "e_caller", "2000", "+1555000111", "chan-arriving"))

	require.Len(t, h.mediaGW.rejectedChans, 1)
	assert.Equal(t, ports.RefusalNotFound, h.mediaGW.rejectedChans[0].cause)
	assert.Empty(t, h.mediaGW.originateCalls)
}

// connectArrived drives an arriving call to Active and returns it with
// the destination leg's correlation.
func connectArrived(t *testing.T, h *harness) (*domain.Call, acl.Correlation) {
	t.Helper()
	arriveTestCall(t, h, shareddomain.NewTenantID().String())
	corr, ok := h.registry.Lookup(h.mediaGW.originateCalls[0].ChannelID)
	require.True(t, ok)
	require.NoError(t, h.svc.ParticipantAnswered(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID,
		h.mediaGW.originateCalls[0].ChannelID))
	call := lastSavedCall(t, h)
	require.Equal(t, domain.CallActive, call.State)
	return call, corr
}

// HangupCall on an arrived call releases the caller's REAL channel — the
// one Asterisk assigned — not the atsa-part-<id> name originated legs
// use. It used to resolve the caller's channel after finalizing, by which
// point the arrived record was already deleted, so the fallback named a
// channel that does not exist and the caller stayed on the line.
func TestHangupCall_ArrivedReleasesTheCallersOwnChannel(t *testing.T) {
	h := arrivedHarness(&fakeInboundRouter{decision: connectDecision()})
	call, _ := connectArrived(t, h)

	require.NoError(t, h.svc.HangupCall(context.Background(), call.ID, "api hangup"))
	assert.Contains(t, h.mediaGW.destroyedChans, ports.ChannelRef("chan-arriving"), "the arrived caller's own channel is hung up")
	assert.Contains(t, h.mediaGW.destroyedChans, ports.ChannelRef(h.mediaGW.originateCalls[0].ChannelID), "and the destination's")
}

// The destination hanging up an arrived call releases the caller.
func TestParticipantLeft_DestinationLeavingReleasesArrivedCaller(t *testing.T) {
	h := arrivedHarness(&fakeInboundRouter{decision: connectDecision()})
	call, corr := connectArrived(t, h)

	require.NoError(t, h.svc.ParticipantLeft(context.Background(), corr.TenantID, corr.CallID, corr.ParticipantID))
	assert.Equal(t, []ports.ChannelRef{"chan-arriving"}, h.mediaGW.destroyedChans,
		"the caller is released, not left on a dead bridge")
	assert.Equal(t, domain.CallTerminated, h.callStore.get(call.ID).State)
}

// The caller hanging up an arrived call releases the destination.
func TestParticipantLeft_ArrivedCallerLeavingReleasesDestination(t *testing.T) {
	h := arrivedHarness(&fakeInboundRouter{decision: connectDecision()})
	call, corr := connectArrived(t, h)

	var caller shareddomain.ParticipantID
	for _, p := range call.Participants {
		if p.ID.String() != corr.ParticipantID {
			caller = p.ID
		}
	}
	require.NoError(t, h.svc.ParticipantLeft(context.Background(), corr.TenantID, corr.CallID, caller.String()))
	assert.Equal(t, []ports.ChannelRef{ports.ChannelRef(h.mediaGW.originateCalls[0].ChannelID)}, h.mediaGW.destroyedChans)
	assert.Equal(t, domain.CallTerminated, h.callStore.get(call.ID).State)
}
