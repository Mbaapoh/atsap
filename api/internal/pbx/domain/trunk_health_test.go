package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
)

func at(m int) time.Time         { return time.Date(2026, 11, 2, 10, m, 0, 0, time.UTC) }
func ptr(t time.Time) *time.Time { return &t }

func healthyProbe(m int) domain.HealthSample {
	return domain.HealthSample{Source: domain.HealthFromProbe, Healthy: true, Reason: domain.ReasonAnswering, At: at(m)}
}
func failedProbe(m int) domain.HealthSample {
	return domain.HealthSample{Source: domain.HealthFromProbe, Healthy: false, Reason: domain.ReasonNoResponse, At: at(m)}
}

// pbx-trunk-health-thresholds: HLD T-6 as a pure decision (D-80).

func TestDecideHealth_ConsecutiveFailures(t *testing.T) {
	state := domain.HealthState{Health: domain.HealthHealthy, Reason: domain.ReasonAnswering}
	// Two failures keep a healthy trunk healthy.
	for i := 1; i <= 2; i++ {
		state.ConsecutiveProbeFailures = i
		d := domain.DecideHealth(state, failedProbe(i))
		assert.Equal(t, domain.HealthHealthy, d.Health, "failure %d must not degrade", i)
		assert.False(t, d.Changed)
	}
	state.ConsecutiveProbeFailures = 3
	d := domain.DecideHealth(state, failedProbe(3))
	assert.Equal(t, domain.HealthDegraded, d.Health)
	assert.Equal(t, domain.ReasonNoResponse, d.Reason)
	assert.True(t, d.Changed)
}

func TestDecideHealth_HealthyProbe(t *testing.T) {
	d := domain.DecideHealth(domain.HealthState{Health: domain.HealthUnknown}, healthyProbe(1))
	assert.Equal(t, domain.HealthHealthy, d.Health)
	assert.True(t, d.Changed)

	d = domain.DecideHealth(domain.HealthState{Health: domain.HealthHealthy, Reason: domain.ReasonAnswering}, healthyProbe(2))
	assert.Equal(t, domain.HealthHealthy, d.Health)
	assert.False(t, d.Changed)
}

func TestDecideHealth_ConfigurationAndSlow(t *testing.T) {
	d := domain.DecideHealth(domain.HealthState{Health: domain.HealthHealthy},
		domain.HealthSample{Source: domain.HealthFromProbe, Reason: domain.ReasonHostNotFound, At: at(1)})
	assert.Equal(t, domain.HealthUnhealthy, d.Health)
	assert.Equal(t, domain.ReasonHostNotFound, d.Reason)

	d = domain.DecideHealth(domain.HealthState{Health: domain.HealthHealthy},
		domain.HealthSample{Source: domain.HealthFromProbe, Reason: domain.ReasonSlowResponse, At: at(1)})
	assert.Equal(t, domain.HealthDegraded, d.Health)
	assert.Equal(t, domain.ReasonSlowResponse, d.Reason)
}

func TestDecideHealth_Recovery(t *testing.T) {
	start := at(0)
	state := domain.HealthState{Health: domain.HealthDegraded, Reason: domain.ReasonNoResponse, HealthySince: ptr(start)}
	d := domain.DecideHealth(state, healthyProbe(4))
	assert.Equal(t, domain.HealthDegraded, d.Health, "less than five minutes is not recovery")
	assert.NotNil(t, d.HealthySince)
	assert.Equal(t, start, *d.HealthySince)

	d = domain.DecideHealth(state, healthyProbe(5))
	assert.Equal(t, domain.HealthHealthy, d.Health)
	assert.Nil(t, d.HealthySince)
	assert.True(t, d.Changed)

	// A failure resets the window.
	state.HealthySince = ptr(start)
	d = domain.DecideHealth(state, failedProbe(5))
	assert.Nil(t, d.HealthySince)
	assert.Equal(t, domain.HealthDegraded, d.Health)
}

func TestDecideHealth_CallWindow(t *testing.T) {
	call := domain.HealthSample{Source: domain.HealthFromCall, Healthy: false, Reason: domain.ReasonNoResponse, At: at(1)}
	state := domain.HealthState{Health: domain.HealthHealthy, Reason: domain.ReasonAnswering, CallTotal: 5, CallFailures: 2}
	d := domain.DecideHealth(state, call)
	assert.Equal(t, domain.HealthDegraded, d.Health)
	assert.Equal(t, domain.ReasonCallSetupFailures, d.Reason)

	state.CallFailures = 1 // exactly 20%: no degradation
	d = domain.DecideHealth(state, call)
	assert.Equal(t, domain.HealthHealthy, d.Health)

	state.CallTotal, state.CallFailures = 4, 4 // too small a window
	d = domain.DecideHealth(state, call)
	assert.Equal(t, domain.HealthHealthy, d.Health)

	// A configuration failure is stronger than a call window.
	state = domain.HealthState{Health: domain.HealthUnhealthy, Reason: domain.ReasonHostNotFound, CallTotal: 5, CallFailures: 5}
	d = domain.DecideHealth(state, call)
	assert.Equal(t, domain.HealthUnhealthy, d.Health)
	assert.Equal(t, domain.ReasonHostNotFound, d.Reason)
}

func TestDecideHealth_CallSuccessDoesNotChange(t *testing.T) {
	call := domain.HealthSample{Source: domain.HealthFromCall, Healthy: true, Reason: domain.ReasonAnswering, At: at(1)}
	d := domain.DecideHealth(domain.HealthState{Health: domain.HealthHealthy, CallTotal: 5, CallFailures: 0}, call)
	assert.Equal(t, domain.HealthHealthy, d.Health)
	assert.False(t, d.Changed)
}

func TestTrunkHealth_Failed(t *testing.T) {
	assert.False(t, domain.HealthHealthy.Failed())
	assert.False(t, domain.HealthUnknown.Failed())
	assert.True(t, domain.HealthDegraded.Failed())
	assert.True(t, domain.HealthUnhealthy.Failed())
}

func TestHealthSource_Valid(t *testing.T) {
	assert.True(t, domain.HealthFromProbe.Valid())
	assert.True(t, domain.HealthFromCall.Valid())
	assert.False(t, domain.HealthSource("OTHER").Valid())
}

func TestDecideHealth_UnknownAndEmptyState(t *testing.T) {
	// An empty state is Unknown; a sub-threshold failure keeps it there.
	d := domain.DecideHealth(domain.HealthState{}, failedProbe(1))
	assert.Equal(t, domain.HealthUnknown, d.Health)
	assert.False(t, d.Changed)

	// A call-only window degrades even an Unknown trunk.
	call := domain.HealthSample{Source: domain.HealthFromCall, Reason: domain.ReasonNoResponse, At: at(1)}
	d = domain.DecideHealth(domain.HealthState{CallTotal: 5, CallFailures: 5}, call)
	assert.Equal(t, domain.HealthDegraded, d.Health)
	assert.Equal(t, domain.ReasonCallSetupFailures, d.Reason)
}
