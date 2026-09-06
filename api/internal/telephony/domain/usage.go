package domain

import (
	"time"

	shareddomain "atsap-api/internal/shared/domain"
)

// UsageTick is one participant's usage for one elapsed second, ready to
// persist as a usage_seconds row (docs/hld/03-domain-model.md §5).
type UsageTick struct {
	ParticipantID shareddomain.ParticipantID
	CallID        shareddomain.CallID
	TenantID      shareddomain.TenantID
	SecondTS      time.Time
	Direction     Direction
}

// GenerateUsageTicks produces exactly one UsageTick per whole second a
// participant was connected in [connectedAt, until), with no duplicate
// and no missing second (D-24 extensibility seam #4; spec: "Per-
// participant, per-second usage while connected"). connectedAt is
// truncated to the second first so tick boundaries stay stable
// regardless of sub-second precision in the input.
func GenerateUsageTicks(p *CallParticipant, callID shareddomain.CallID, tenantID shareddomain.TenantID, direction Direction, connectedAt, until time.Time) []UsageTick {
	start := connectedAt.Truncate(time.Second)
	elapsed := until.Sub(start)
	if elapsed <= 0 {
		return nil
	}
	n := int(elapsed / time.Second)

	ticks := make([]UsageTick, 0, n)
	for i := range n {
		ticks = append(ticks, UsageTick{
			ParticipantID: p.ID,
			CallID:        callID,
			TenantID:      tenantID,
			SecondTS:      start.Add(time.Duration(i) * time.Second),
			Direction:     direction,
		})
	}
	return ticks
}
