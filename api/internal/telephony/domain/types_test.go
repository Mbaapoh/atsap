package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/domain"
)

func TestCallStates_AllValuesDistinct(t *testing.T) {
	states := []domain.CallState{
		domain.CallInitiated, domain.CallScreening, domain.CallRouting,
		domain.CallPresenting, domain.CallActive, domain.CallDegraded,
		domain.CallTerminating, domain.CallTerminated,
	}
	seen := make(map[domain.CallState]bool, len(states))
	for _, s := range states {
		assert.False(t, seen[s], "duplicate CallState value: %s", s)
		seen[s] = true
	}
}

func TestParticipantStates_AllValuesDistinct(t *testing.T) {
	states := []domain.ParticipantState{
		domain.ParticipantInvited, domain.ParticipantRinging, domain.ParticipantConnected,
		domain.ParticipantOnHold, domain.ParticipantTransferring, domain.ParticipantDisconnected,
	}
	seen := make(map[domain.ParticipantState]bool, len(states))
	for _, s := range states {
		assert.False(t, seen[s], "duplicate ParticipantState value: %s", s)
		seen[s] = true
	}
}

func TestParticipantRoles_AllValuesDistinct(t *testing.T) {
	roles := []domain.ParticipantRole{
		domain.RoleCaller, domain.RoleAgent, domain.RoleIVR, domain.RoleQueue,
		domain.RoleSupervisor, domain.RoleSpecialist, domain.RoleAI,
	}
	seen := make(map[domain.ParticipantRole]bool, len(roles))
	for _, r := range roles {
		assert.False(t, seen[r], "duplicate ParticipantRole value: %s", r)
		seen[r] = true
	}
}

// TestCallIsAggregateOfParticipants covers the spec requirement "Call is
// an aggregate of participants, not fixed roles": a two-party call has
// exactly two independently identifiable CallParticipant records, with
// no fixed caller/agent struct fields anywhere on Call.
func TestCallIsAggregateOfParticipants(t *testing.T) {
	call := domain.NewCall(shareddomain.NewCallID(), shareddomain.NewTenantID(), domain.Outbound, "1000", "1001", fixedTime)

	caller := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), Role: domain.RoleCaller}
	callee := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), Role: domain.RoleAgent}

	require.NoError(t, call.AddParticipant(caller))
	require.NoError(t, call.AddParticipant(callee))

	require.Len(t, call.Participants, 2)
	assert.NotEqual(t, call.Participants[0].ID, call.Participants[1].ID)
}
