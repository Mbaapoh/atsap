package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/domain"
)

var fixedTime = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func newTwoPartyCall(t *testing.T) (*domain.Call, *domain.CallParticipant, *domain.CallParticipant) {
	t.Helper()
	call := domain.NewCall(shareddomain.NewCallID(), shareddomain.NewTenantID(), domain.Outbound, "1000", "1001", fixedTime)
	caller := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: call.TenantID, Role: domain.RoleCaller, EndpointURI: "PJSIP/1000"}
	callee := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: call.TenantID, Role: domain.RoleAgent, EndpointURI: "PJSIP/1001"}
	require.NoError(t, call.AddParticipant(caller))
	require.NoError(t, call.AddParticipant(callee))
	return call, caller, callee
}

// TestHappyPathProgression covers the spec scenario "Full happy-path
// progression": Initiated -> Screening -> Routing -> Presenting ->
// Active -> Terminating -> Terminated, in order.
func TestHappyPathProgression(t *testing.T) {
	call, caller, callee := newTwoPartyCall(t)
	assert.Equal(t, domain.CallInitiated, call.State)

	require.NoError(t, call.TransitionToScreening())
	assert.Equal(t, domain.CallScreening, call.State)

	events, err := call.ApplyScreeningVerdict(true, true, fixedTime)
	require.NoError(t, err)
	assert.Empty(t, events, "a permitted verdict does not itself emit an event")
	assert.Equal(t, domain.CallRouting, call.State)

	require.NoError(t, call.TransitionToPresenting())
	assert.Equal(t, domain.CallPresenting, call.State)

	events, err = call.ConnectParticipant(caller.ID, fixedTime)
	require.NoError(t, err)
	assert.Len(t, events, 1, "first connect: only ParticipantJoined, not yet Active")
	assert.Equal(t, domain.CallPresenting, call.State, "one connected participant is not enough for Active")

	events, err = call.ConnectParticipant(callee.ID, fixedTime.Add(2*time.Second))
	require.NoError(t, err)
	assert.Len(t, events, 2, "second connect: ParticipantJoined and CallActive")
	assert.Equal(t, domain.CallActive, call.State)

	endTime := fixedTime.Add(10 * time.Second)
	events, err = call.DisconnectParticipant(caller.ID, endTime)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, domain.CallActive, call.State, "callee is still connected: must not yet be Terminating")

	events, err = call.DisconnectParticipant(callee.ID, endTime)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, domain.CallTerminating, call.State, "last connected participant leaving moves the call to Terminating")

	events, err = call.Terminate("normal clearing", endTime)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, domain.CallTerminated, call.State)
	assert.Equal(t, &endTime, call.EndedAt)
	assert.Equal(t, "normal clearing", call.TerminationReason)
}

// TestActiveRequiresTwoConnectedParticipants covers the spec scenario
// "Active requires two or more connected participants": with only one
// participant connected, the Call must remain in Presenting.
func TestActiveRequiresTwoConnectedParticipants(t *testing.T) {
	call, caller, _ := newTwoPartyCall(t)
	require.NoError(t, call.TransitionToScreening())
	_, err := call.ApplyScreeningVerdict(true, true, fixedTime)
	require.NoError(t, err)
	require.NoError(t, call.TransitionToPresenting())

	_, err = call.ConnectParticipant(caller.ID, fixedTime)
	require.NoError(t, err)

	assert.Equal(t, domain.CallPresenting, call.State, "one connected participant must not reach Active")
}

// TestScreeningRejection covers "A verdict is not permitted": the call
// terminates without reaching Routing, and the reason names which
// verdict rejected it.
func TestScreeningRejection(t *testing.T) {
	tests := []struct {
		name                string
		capacityPermitted   bool
		compliancePermitted bool
		wantReasonContains  string
	}{
		{"capacity rejected", false, true, "capacity"},
		{"compliance rejected", true, false, "compliance"},
		{"both rejected", false, false, "capacity and compliance"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call, _, _ := newTwoPartyCall(t)
			require.NoError(t, call.TransitionToScreening())

			events, err := call.ApplyScreeningVerdict(tt.capacityPermitted, tt.compliancePermitted, fixedTime)
			require.NoError(t, err)
			require.Len(t, events, 1)

			assert.Equal(t, domain.CallTerminated, call.State)
			assert.Contains(t, call.TerminationReason, tt.wantReasonContains)

			terminated, ok := events[0].(domain.CallTerminatedEvent)
			require.True(t, ok)
			assert.Contains(t, terminated.Reason, tt.wantReasonContains)
		})
	}
}

// TestDestinationDoesNotAnswer covers "Destination does not answer": the
// call terminates from Presenting without ever reaching Active.
func TestDestinationDoesNotAnswer(t *testing.T) {
	call, _, _ := newTwoPartyCall(t)
	require.NoError(t, call.TransitionToScreening())
	_, err := call.ApplyScreeningVerdict(true, true, fixedTime)
	require.NoError(t, err)
	require.NoError(t, call.TransitionToPresenting())

	events, err := call.Terminate("no answer within alerting timeout", fixedTime.Add(20*time.Second))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, domain.CallTerminated, call.State)
	assert.NotEqual(t, domain.CallActive, call.State)
}

// TestTerminatedCallIsImmutable covers "A Terminated call's record SHALL
// become immutable": every mutating method rejects a second call once
// the Call is Terminated.
func TestTerminatedCallIsImmutable(t *testing.T) {
	call, caller, callee := newTwoPartyCall(t)
	_, err := call.Terminate("caller hung up", fixedTime)
	require.NoError(t, err)

	stateBefore := call.State
	endedAtBefore := call.EndedAt

	_, err = call.Terminate("again", fixedTime.Add(time.Second))
	assert.ErrorIs(t, err, domain.ErrCallTerminated)

	_, err = call.ConnectParticipant(caller.ID, fixedTime)
	assert.ErrorIs(t, err, domain.ErrCallTerminated)

	_, err = call.DisconnectParticipant(callee.ID, fixedTime)
	assert.ErrorIs(t, err, domain.ErrCallTerminated)

	err = call.AddParticipant(&domain.CallParticipant{ID: shareddomain.NewParticipantID()})
	assert.ErrorIs(t, err, domain.ErrCallTerminated)

	assert.Equal(t, stateBefore, call.State, "state must not change after a rejected mutation")
	assert.Equal(t, endedAtBefore, call.EndedAt, "EndedAt must not change after a rejected mutation")
}

// TestInvalidTransitionRejected covers guard rails on out-of-order calls
// (e.g. skipping Screening).
func TestInvalidTransitionRejected(t *testing.T) {
	call, _, _ := newTwoPartyCall(t)

	// Presenting requires Routing; the call is still Initiated.
	err := call.TransitionToPresenting()
	assert.ErrorIs(t, err, domain.ErrInvalidTransition)

	// A screening verdict requires Screening; the call is still Initiated.
	_, err = call.ApplyScreeningVerdict(true, true, fixedTime)
	assert.ErrorIs(t, err, domain.ErrInvalidTransition)

	// Screening requires Initiated; already past it.
	require.NoError(t, call.TransitionToScreening())
	err = call.TransitionToScreening()
	assert.ErrorIs(t, err, domain.ErrInvalidTransition)
}

func TestConnectParticipant_UnknownParticipant(t *testing.T) {
	call, _, _ := newTwoPartyCall(t)
	_, err := call.ConnectParticipant(shareddomain.NewParticipantID(), fixedTime)
	assert.ErrorIs(t, err, domain.ErrParticipantNotFound)
}

func TestDisconnectParticipant_UnknownParticipant(t *testing.T) {
	call, _, _ := newTwoPartyCall(t)
	_, err := call.DisconnectParticipant(shareddomain.NewParticipantID(), fixedTime)
	assert.ErrorIs(t, err, domain.ErrParticipantNotFound)
}

// TestTerminatedCallRejectsEveryTransitionEntryPoint exercises the
// ErrCallTerminated guard on every transition method that has one,
// beyond the subset already covered by TestTerminatedCallIsImmutable.
func TestTerminatedCallRejectsEveryTransitionEntryPoint(t *testing.T) {
	call, _, _ := newTwoPartyCall(t)
	_, err := call.Terminate("caller hung up", fixedTime)
	require.NoError(t, err)

	err = call.TransitionToScreening()
	assert.ErrorIs(t, err, domain.ErrCallTerminated)

	_, err = call.ApplyScreeningVerdict(true, true, fixedTime)
	assert.ErrorIs(t, err, domain.ErrCallTerminated)

	err = call.TransitionToPresenting()
	assert.ErrorIs(t, err, domain.ErrCallTerminated)
}
