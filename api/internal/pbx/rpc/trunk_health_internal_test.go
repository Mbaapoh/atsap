package rpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// TestToProtoTrunk_Health covers pbx-trunk-health-probing design D5: every
// observed state and reason reaches the wire, a trunk never observed says
// so (UNKNOWN, no reason, no time), and nothing is invented.
func TestToProtoTrunk_Health(t *testing.T) {
	at := time.Date(2026, 10, 14, 14, 2, 31, 0, time.UTC)
	for _, tc := range []struct {
		health     domain.TrunkHealth
		reason     domain.HealthReason
		observed   *time.Time
		wantHealth atsapbxv1.TrunkHealth
		wantReason atsapbxv1.TrunkHealthReason
	}{
		{domain.HealthUnknown, "", nil, atsapbxv1.TrunkHealth_TRUNK_HEALTH_UNKNOWN, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_UNSPECIFIED},
		{domain.HealthHealthy, domain.ReasonAnswering, &at, atsapbxv1.TrunkHealth_TRUNK_HEALTH_HEALTHY, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_ANSWERING},
		{domain.HealthDegraded, domain.ReasonSlowResponse, &at, atsapbxv1.TrunkHealth_TRUNK_HEALTH_DEGRADED, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_SLOW_RESPONSE},
		{domain.HealthUnhealthy, domain.ReasonHostNotFound, &at, atsapbxv1.TrunkHealth_TRUNK_HEALTH_UNHEALTHY, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_HOST_NOT_FOUND},
		{domain.HealthUnhealthy, domain.ReasonNoResponse, &at, atsapbxv1.TrunkHealth_TRUNK_HEALTH_UNHEALTHY, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_NO_RESPONSE},
		{domain.HealthUnhealthy, domain.ReasonCarrierError, &at, atsapbxv1.TrunkHealth_TRUNK_HEALTH_UNHEALTHY, atsapbxv1.TrunkHealthReason_TRUNK_HEALTH_REASON_CARRIER_ERROR},
	} {
		t.Run(string(tc.health)+"/"+string(tc.reason), func(t *testing.T) {
			got := toProtoTrunk(ports.TrunkView{ID: shareddomain.NewTrunkID(), Health: tc.health, HealthReason: tc.reason, HealthObservedAt: tc.observed})
			assert.Equal(t, tc.wantHealth, got.GetHealth())
			assert.Equal(t, tc.wantReason, got.GetHealthReason())
			if tc.observed == nil {
				assert.Nil(t, got.GetHealthObservedAt(), "never observed: no time")
			} else {
				assert.True(t, at.Equal(got.GetHealthObservedAt().AsTime()))
			}
		})
	}
}
