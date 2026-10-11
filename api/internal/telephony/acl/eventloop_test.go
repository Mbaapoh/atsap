package acl_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/domain"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// domainSink is an EventSink backed by a real *domain.Call, so the test
// proves the event loop drives real domain transitions (task 5.6's
// "asserts the resulting Call/Participant states match the expected
// happy-path progression"), not just that some callback fired.
type domainSink struct {
	call *domain.Call
}

func (s *domainSink) ParticipantAnswered(_ context.Context, _, _, participantID, _ string) error {
	pid, err := shareddomain.ParseParticipantID(participantID)
	if err != nil {
		return err
	}
	_, err = s.call.ConnectParticipant(pid, fixedEventTime)
	return err
}

func (s *domainSink) ParticipantLeft(_ context.Context, _, _, participantID string) error {
	pid, err := shareddomain.ParseParticipantID(participantID)
	if err != nil {
		return err
	}
	_, err = s.call.DisconnectParticipant(pid, fixedEventTime)
	return err
}

func (s *domainSink) CallArrived(context.Context, string, string, string, string, string) error {
	return errors.New("domainSink: arriving calls are not part of the originated happy path")
}

var fixedEventTime = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func stasisStartEvent(t *testing.T, channelID string) ari.Event {
	t.Helper()
	raw := fmt.Sprintf(`{"type":"StasisStart","channel":{"id":%q,"state":"Up"}}`, channelID)
	return ari.Event{Type: "StasisStart", Raw: []byte(raw)}
}

func channelStateChangeEvent(t *testing.T, channelID, state string) ari.Event {
	t.Helper()
	raw := fmt.Sprintf(`{"type":"ChannelStateChange","channel":{"id":%q,"state":%q}}`, channelID, state)
	return ari.Event{Type: "ChannelStateChange", Raw: []byte(raw)}
}

func channelDestroyedEvent(t *testing.T, channelID string) ari.Event {
	t.Helper()
	raw := fmt.Sprintf(`{"type":"ChannelDestroyed","channel":{"id":%q}}`, channelID)
	return ari.Event{Type: "ChannelDestroyed", Raw: []byte(raw)}
}

// newPresentingTwoPartyCall builds a Call already advanced to Presenting
// with two Invited participants, matching the state the orchestrator
// (task 6.1) would hand off to the event loop after origination.
func newPresentingTwoPartyCall(t *testing.T) (*domain.Call, *domain.CallParticipant, *domain.CallParticipant) {
	t.Helper()
	call := domain.NewCall(shareddomain.NewCallID(), shareddomain.NewTenantID(), domain.Outbound, "1000", "1001", fixedEventTime)
	caller := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: call.TenantID, Role: domain.RoleCaller}
	callee := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: call.TenantID, Role: domain.RoleAgent}
	require.NoError(t, call.AddParticipant(caller))
	require.NoError(t, call.AddParticipant(callee))
	require.NoError(t, call.TransitionToScreening())
	_, err := call.ApplyScreeningVerdict(true, true, fixedEventTime)
	require.NoError(t, err)
	require.NoError(t, call.TransitionToPresenting())
	return call, caller, callee
}

// TestEventLoop_HappyPathProgression scripts the exact ARI event
// sequence a real two-party call produces and asserts the resulting
// domain state matches the expected happy-path progression.
func TestEventLoop_HappyPathProgression(t *testing.T) {
	call, caller, callee := newPresentingTwoPartyCall(t)

	registry := acl.NewCorrelationRegistry()
	registry.Register("chan-caller", acl.Correlation{CallID: call.ID.String(), ParticipantID: caller.ID.String()})
	registry.Register("chan-callee", acl.Correlation{CallID: call.ID.String(), ParticipantID: callee.ID.String()})

	loop := acl.NewEventLoop(registry, &domainSink{call: call}, discardLogger())
	ctx := context.Background()

	loop.Handle(ctx, stasisStartEvent(t, "chan-caller"))
	assert.Equal(t, domain.CallPresenting, call.State, "one participant answered is not enough for Active")

	loop.Handle(ctx, stasisStartEvent(t, "chan-callee"))
	assert.Equal(t, domain.CallActive, call.State, "both participants answered: Active")

	loop.Handle(ctx, channelDestroyedEvent(t, "chan-caller"))
	assert.Equal(t, domain.CallActive, call.State, "callee still connected: not yet Terminating")

	loop.Handle(ctx, channelDestroyedEvent(t, "chan-callee"))
	assert.Equal(t, domain.CallTerminating, call.State, "last connected participant left: Terminating")

	_, ok := registry.Lookup("chan-caller")
	assert.False(t, ok, "destroyed channels are removed from the registry")
	_, ok = registry.Lookup("chan-callee")
	assert.False(t, ok)
}

// TestEventLoop_ChannelStateChange_NoSinkCall covers that
// ChannelStateChange (Ringing, Up, or any other state) is not acted on
// at all: ParticipantAnswered fires on StasisStart instead (see
// EventLoop's doc comment for why).
func TestEventLoop_ChannelStateChange_NoSinkCall(t *testing.T) {
	call, caller, _ := newPresentingTwoPartyCall(t)
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan-caller", acl.Correlation{CallID: call.ID.String(), ParticipantID: caller.ID.String()})

	loop := acl.NewEventLoop(registry, &domainSink{call: call}, discardLogger())
	loop.Handle(context.Background(), channelStateChangeEvent(t, "chan-caller", "Up"))

	assert.Equal(t, domain.ParticipantInvited, caller.State, "ChannelStateChange must not connect the participant")
}

// TestEventLoop_UncorrelatedStasisStart_RoutesToCallArrived covers the
// arriving-call dispatch (task 2.3): a StasisStart no originated leg owns
// is not answered bookkeeping — it is a new call, and the sink learns
// the tenant (from the channel variable), the source endpoint (from the
// channel name), the dialled number (from the dialplan extension), and
// the caller number.
func TestEventLoop_UncorrelatedStasisStart_RoutesToCallArrived(t *testing.T) {
	for _, tc := range []struct {
		name           string
		channelID      string
		channelName    string
		dialled        string
		caller         string
		vars           map[string]string
		wantTenant     string
		wantSource     string
	}{
		{
			name: "extension channel",
			channelID: "chan-ext",
			channelName: "PJSIP/e_6f8b2c1a-4d3e-4f5a-8b1c-9d2e3f4a5b6c-00000001",
			dialled: "+442071234567", caller: "+1555000111",
			vars: map[string]string{"chan-ext": "11111111-2222-3333-4444-555555555555"},
			wantTenant: "11111111-2222-3333-4444-555555555555",
			wantSource: "e_6f8b2c1a-4d3e-4f5a-8b1c-9d2e3f4a5b6c",
		},
		{
			name: "trunk channel",
			channelID: "chan-trunk",
			channelName: "PJSIP/t_a1b2c3d4-5e6f-4a5b-8c9d-0e1f2a3b4c5d-00000002",
			dialled: "442071234567", caller: "+1555000222",
			vars: map[string]string{"chan-trunk": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
			wantTenant: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			wantSource: "t_a1b2c3d4-5e6f-4a5b-8c9d-0e1f2a3b4c5d",
		},
		{
			name: "no tenant variable",
			channelID: "chan-static",
			channelName: "PJSIP/1000-00000003",
			dialled: "2000", caller: "1000",
			vars: map[string]string{},
			wantTenant: "",
			wantSource: "1000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := acl.NewCorrelationRegistry()
			sink := &recordingSink{}
			loop := acl.NewEventLoop(registry, sink, discardLogger())
			loop.SetChannelVariableReader(mapVarReader{vars: tc.vars})

			channelID := tc.channelID
			loop.Handle(context.Background(), arrivingStasisStart(channelID, tc.channelName, tc.dialled, tc.caller))

			require.Len(t, sink.arrivedCalls, 1, "an uncorrelated StasisStart is an arriving call")
			got := sink.arrivedCalls[0]
			assert.Equal(t, tc.wantTenant, got.tenantID)
			assert.Equal(t, tc.wantSource, got.sourceEndpoint)
			assert.Equal(t, tc.dialled, got.dialled)
			assert.Equal(t, tc.caller, got.callerNumber)
			assert.Equal(t, channelID, got.channelID)
			assert.Zero(t, sink.answeredCalls, "an arriving channel is never answered by dispatch")
		})
	}
}

// TestEventLoop_UncorrelatedChannelGone_NoSinkCall covers the counterpart:
// a ChannelDestroyed for an unknown channel is still bookkeeping, not a
// call — nothing is routed anywhere.
func TestEventLoop_UncorrelatedChannelGone_NoSinkCall(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), channelDestroyedEvent(t, "unknown-chan"))

	assert.Zero(t, sink.answeredCalls)
	assert.Zero(t, sink.leftCalls)
	assert.Empty(t, sink.arrivedCalls)
}

// TestEventLoop_UnhandledEventType_NoSinkCall covers event types this
// loop deliberately does not act on (e.g. Dial, ChannelVarset — the
// many bookkeeping events real Asterisk sends alongside the ones this
// loop cares about).
func TestEventLoop_UnhandledEventType_NoSinkCall(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), ari.Event{Type: "ChannelVarset", Raw: []byte(`{"channel":{"id":"chan1"}}`)})

	assert.Zero(t, sink.answeredCalls)
	assert.Zero(t, sink.leftCalls)
}

func TestEventLoop_DecodeError_NoSinkCall(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), ari.Event{Type: "StasisStart", Raw: []byte("not json")})
	loop.Handle(context.Background(), ari.Event{Type: "ChannelDestroyed", Raw: []byte("not json")})

	assert.Zero(t, sink.answeredCalls)
	assert.Zero(t, sink.leftCalls)
}

// TestEventLoop_SinkError_Logged covers a sink returning an error: the
// loop logs it and continues (never panics, never blocks the stream).
func TestEventLoop_SinkError_Logged(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})
	sink := &recordingSink{err: errors.New("boom")}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	assert.NotPanics(t, func() {
		loop.Handle(context.Background(), stasisStartEvent(t, "chan1"))
	})
	assert.Equal(t, 1, sink.answeredCalls)

	registry.Register("chan2", acl.Correlation{CallID: "call-1", ParticipantID: "participant-2"})
	assert.NotPanics(t, func() {
		loop.Handle(context.Background(), channelDestroyedEvent(t, "chan2"))
	})
	assert.Equal(t, 1, sink.leftCalls)
}

// TestEventLoop_ParticipantAnswered_PassesChannelID covers that the
// triggering channel ID reaches the sink — needed downstream to bridge
// the newly answered leg (MediaGateway.AddChannelToBridge).
func TestEventLoop_ParticipantAnswered_PassesChannelID(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan-42", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), stasisStartEvent(t, "chan-42"))

	assert.Equal(t, "chan-42", sink.lastAnsweredChan)
}

// TestEventLoop_ChannelHangupRequest covers the second event name that
// maps to the same handler as ChannelDestroyed.
func TestEventLoop_ChannelHangupRequest(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	raw := `{"type":"ChannelHangupRequest","channel":{"id":"chan1"}}`
	loop.Handle(context.Background(), ari.Event{Type: "ChannelHangupRequest", Raw: []byte(raw)})

	assert.Equal(t, 1, sink.leftCalls)
	_, ok := registry.Lookup("chan1")
	assert.False(t, ok)
}

type recordingSink struct {
	answeredCalls    int
	leftCalls        int
	lastAnsweredChan string
	arrivedCalls     []arrivedCall
	err              error
}

type arrivedCall struct {
	tenantID       string
	sourceEndpoint string
	dialled        string
	callerNumber   string
	channelID      string
}

func (s *recordingSink) ParticipantAnswered(_ context.Context, _, _, _, channelID string) error {
	s.answeredCalls++
	s.lastAnsweredChan = channelID
	return s.err
}

func (s *recordingSink) ParticipantLeft(_ context.Context, _, _, _ string) error {
	s.leftCalls++
	return s.err
}

func (s *recordingSink) CallArrived(_ context.Context, tenantID, sourceEndpoint, dialled, callerNumber, channelID string) error {
	s.arrivedCalls = append(s.arrivedCalls, arrivedCall{tenantID, sourceEndpoint, dialled, callerNumber, channelID})
	return s.err
}

// mapVarReader stands in for the ARI client's channel-variable reads.
type mapVarReader struct {
	vars map[string]string
	err  error
}

func (m mapVarReader) GetChannelVariable(_ context.Context, channelID, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.vars[channelID], nil
}

// arrivingStasisStart builds a StasisStart payload the way real Asterisk
// sends one for a channel the platform did not originate: a tech/name
// channel id, dialplan extension, and caller ID.
func arrivingStasisStart(channelID, channelName, dialled, callerNumber string) ari.Event {
	raw := fmt.Sprintf(`{"type":"StasisStart","channel":{"id":%q,"name":%q,"state":"Ring","dialplan":{"context":"stasis-in","exten":%q,"priority":1},"caller":{"number":%q,"name":%q}}}`,
		channelID, channelName, dialled, callerNumber, callerNumber)
	return ari.Event{Type: "StasisStart", Raw: []byte(raw)}
}
