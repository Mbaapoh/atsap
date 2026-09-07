package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/domain"
)

// TestGenerateUsageTicks_NoDuplicateNoGap covers the spec scenario
// "Continuous ticking with no duplication": exactly one tick per elapsed
// second, in order, with no duplicate and no missing second.
func TestGenerateUsageTicks_NoDuplicateNoGap(t *testing.T) {
	participant := &domain.CallParticipant{ID: shareddomain.NewParticipantID()}
	callID := shareddomain.NewCallID()
	tenantID := shareddomain.NewTenantID()
	connectedAt := time.Date(2026, 9, 6, 12, 0, 0, 500_000_000, time.UTC) // .5s into the second
	until := connectedAt.Add(5 * time.Second)

	ticks := domain.GenerateUsageTicks(participant, callID, tenantID, domain.Outbound, connectedAt, until)

	require.Len(t, ticks, 5, "5 whole seconds elapsed")

	seen := make(map[time.Time]bool, len(ticks))
	for i, tick := range ticks {
		assert.False(t, seen[tick.SecondTS], "duplicate second at index %d: %v", i, tick.SecondTS)
		seen[tick.SecondTS] = true
		assert.Equal(t, participant.ID, tick.ParticipantID)
		assert.Equal(t, callID, tick.CallID)
		assert.Equal(t, tenantID, tick.TenantID)

		if i > 0 {
			gap := tick.SecondTS.Sub(ticks[i-1].SecondTS)
			assert.Equal(t, time.Second, gap, "no gap or overlap between consecutive ticks")
		}
	}
}

func TestGenerateUsageTicks_NoTimeElapsed(t *testing.T) {
	participant := &domain.CallParticipant{ID: shareddomain.NewParticipantID()}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	ticks := domain.GenerateUsageTicks(participant, shareddomain.NewCallID(), shareddomain.NewTenantID(), domain.Outbound, now, now)
	assert.Empty(t, ticks)

	ticksBackwards := domain.GenerateUsageTicks(participant, shareddomain.NewCallID(), shareddomain.NewTenantID(), domain.Outbound, now, now.Add(-time.Second))
	assert.Empty(t, ticksBackwards)
}
