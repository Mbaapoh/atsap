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

func (s *domainSink) ParticipantAnswered(_ context.Context, corr acl.Correlation, _ string) error {
	pid, err := shareddomain.ParseParticipantID(corr.ParticipantID)
	if err != nil {
		return err
	}
	_, err = s.call.ConnectParticipant(pid, fixedEventTime)
	return err
}

func (s *domainSink) ParticipantLeft(_ context.Context, corr acl.Correlation) error {
	pid, err := shareddomain.ParseParticipantID(corr.ParticipantID)
	if err != nil {
		return err
	}
	_, err = s.call.DisconnectParticipant(pid, fixedEventTime)
	return err
}

var fixedEventTime = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

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

	loop.Handle(ctx, channelStateChangeEvent(t, "chan-caller", "Up"))
	assert.Equal(t, domain.CallPresenting, call.State, "one participant answered is not enough for Active")

	loop.Handle(ctx, channelStateChangeEvent(t, "chan-callee", "Up"))
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

// TestEventLoop_IgnoresNonUpStateChanges covers Ringing and other
// intermediate states not treated as an answer.
func TestEventLoop_IgnoresNonUpStateChanges(t *testing.T) {
	call, caller, _ := newPresentingTwoPartyCall(t)
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan-caller", acl.Correlation{CallID: call.ID.String(), ParticipantID: caller.ID.String()})

	loop := acl.NewEventLoop(registry, &domainSink{call: call}, discardLogger())
	loop.Handle(context.Background(), channelStateChangeEvent(t, "chan-caller", "Ringing"))

	assert.Equal(t, domain.ParticipantInvited, caller.State, "Ringing must not connect the participant")
}

// TestEventLoop_UnregisteredChannel_NoSinkCall covers an event for a
// channel the registry has no correlation for (e.g. already removed, or
// not ours) — must not call the sink or panic.
func TestEventLoop_UnregisteredChannel_NoSinkCall(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), channelStateChangeEvent(t, "unknown-chan", "Up"))
	loop.Handle(context.Background(), channelDestroyedEvent(t, "unknown-chan"))

	assert.Zero(t, sink.answeredCalls)
	assert.Zero(t, sink.leftCalls)
}

// TestEventLoop_UnhandledEventType_NoSinkCall covers StasisStart and any
// other event type: this loop deliberately does not act on them (see
// EventLoop's doc comment).
func TestEventLoop_UnhandledEventType_NoSinkCall(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	registry.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), ari.Event{Type: "StasisStart", Raw: []byte(`{"channel":{"id":"chan1"}}`)})

	assert.Zero(t, sink.answeredCalls)
	assert.Zero(t, sink.leftCalls)
}

func TestEventLoop_DecodeError_NoSinkCall(t *testing.T) {
	registry := acl.NewCorrelationRegistry()
	sink := &recordingSink{}
	loop := acl.NewEventLoop(registry, sink, discardLogger())

	loop.Handle(context.Background(), ari.Event{Type: "ChannelStateChange", Raw: []byte("not json")})
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
		loop.Handle(context.Background(), channelStateChangeEvent(t, "chan1", "Up"))
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

	loop.Handle(context.Background(), channelStateChangeEvent(t, "chan-42", "Up"))

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
	err              error
}

func (s *recordingSink) ParticipantAnswered(_ context.Context, _ acl.Correlation, channelID string) error {
	s.answeredCalls++
	s.lastAnsweredChan = channelID
	return s.err
}

func (s *recordingSink) ParticipantLeft(context.Context, acl.Correlation) error {
	s.leftCalls++
	return s.err
}
