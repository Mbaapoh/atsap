package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/telephony/domain"
)

func TestEventTypes(t *testing.T) {
	assert.Equal(t, "call.initiated", domain.CallInitiatedEvent{}.EventType())
	assert.Equal(t, "call.active", domain.CallActiveEvent{}.EventType())
	assert.Equal(t, "call.terminated", domain.CallTerminatedEvent{}.EventType())
	assert.Equal(t, "participant.joined", domain.ParticipantJoinedEvent{}.EventType())
	assert.Equal(t, "participant.left", domain.ParticipantLeftEvent{}.EventType())
}

func TestCall_InitiatedEvent(t *testing.T) {
	call, _, _ := newTwoPartyCall(t)
	evt := call.InitiatedEvent()

	initiated, ok := evt.(domain.CallInitiatedEvent)
	assert.True(t, ok)
	assert.Equal(t, call.ID.String(), initiated.CallID)
}
