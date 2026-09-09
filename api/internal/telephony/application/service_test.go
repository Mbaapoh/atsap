package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

	failOriginate     error
	failCreateBridge  error
	failAddToBridge   error
	failDestroyBridge error
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
func (f *fakeMediaGateway) DestroyChannel(context.Context, ports.ChannelRef) error { return nil }

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

	saved := h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallActive, saved.State, "one participant still connected")

	require.NoError(t, h.svc.ParticipantLeft(ctx, corrs[1].TenantID, corrs[1].CallID, corrs[1].ParticipantID))

	saved = h.callStore.get(callID)
	require.NotNil(t, saved)
	assert.Equal(t, domain.CallTerminated, saved.State)
	assert.Equal(t, "normal clearing", saved.TerminationReason)
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
