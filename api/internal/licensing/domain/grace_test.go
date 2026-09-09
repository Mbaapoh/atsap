package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var confirmed = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// TestGraceState walks the timeline with an injected clock, including
// both boundaries. The day-6/day-7 pair is the one that matters: it is
// the difference between a partner working normally and a partner
// degraded, and an off-by-one here is a support incident.
func TestGraceState(t *testing.T) {
	for name, tc := range map[string]struct {
		elapsed time.Duration
		want    State
	}{
		"same instant":                {0, StateValid},
		"one hour":                    {time.Hour, StateValid},
		"just before the first day":   {24*time.Hour - time.Nanosecond, StateValid},
		"exactly one day":             {24 * time.Hour, StateUnverified},
		"day 2":                       {2 * 24 * time.Hour, StateUnverified},
		"day 6":                       {6 * 24 * time.Hour, StateUnverified},
		"just before the grace ends":  {7*24*time.Hour - time.Nanosecond, StateUnverified},
		"exactly the grace period":    {7 * 24 * time.Hour, StateDegraded},
		"day 30":                      {30 * 24 * time.Hour, StateDegraded},
		"a year":                      {365 * 24 * time.Hour, StateDegraded},
		"clock skew: confirmed later": {-time.Hour, StateValid},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, GraceState(confirmed.Add(tc.elapsed), confirmed))
		})
	}
}

// TestNoWarningOnTheFirstDay is AC-06.4: warnings start from day two, so
// a transient failure that clears overnight alarms nobody.
func TestWarningTiming(t *testing.T) {
	assert.False(t, ShouldWarn(confirmed.Add(time.Hour), confirmed), "no warning in the first hours")
	assert.False(t, ShouldWarn(confirmed.Add(23*time.Hour), confirmed), "no warning on the first day")
	assert.True(t, ShouldWarn(confirmed.Add(24*time.Hour), confirmed), "warning from day two (AC-06.4)")
	assert.True(t, ShouldWarn(confirmed.Add(60*24*time.Hour), confirmed), "still warning once degraded")
}

// TestGraceStateDependsOnlyOnElapsedTime is task 2.5 and the
// "repeated failures do not accelerate degradation" scenario. The
// function takes two times and nothing else — this asserts the
// consequence: identical inputs give identical results however many
// times they are evaluated, and a different absolute clock with the same
// gap gives the same answer.
func TestGraceStateDependsOnlyOnElapsedTime(t *testing.T) {
	gap := 3 * 24 * time.Hour

	t.Run("repeated evaluation is stable", func(t *testing.T) {
		first := GraceState(confirmed.Add(gap), confirmed)
		for i := 0; i < 100; i++ {
			assert.Equal(t, first, GraceState(confirmed.Add(gap), confirmed))
		}
	})

	t.Run("the same gap anywhere on the calendar gives the same state", func(t *testing.T) {
		other := time.Date(2031, 2, 28, 3, 15, 0, 0, time.UTC)
		assert.Equal(t,
			GraceState(confirmed.Add(gap), confirmed),
			GraceState(other.Add(gap), other))
	})

	t.Run("a restart does not reset the grace period", func(t *testing.T) {
		// A restart changes nothing this function can see, which is the
		// point: state is derived from the licence's last confirmation,
		// never from process uptime.
		elapsed := 10 * 24 * time.Hour
		assert.Equal(t, StateDegraded, GraceState(confirmed.Add(elapsed), confirmed),
			"an installation degraded before a restart is degraded after it")
	})
}

// TestApplyGrace_FullFunctionThroughoutTheWindow is AC-06.5: the grace
// period carries NO functional restriction. Capacity must be untouched
// on day 6, not merely "mostly working".
func TestApplyGrace_FullFunctionThroughoutTheWindow(t *testing.T) {
	licensed := Entitlement{State: StateValid, Edition: "core", Channels: 64, MaxTenants: Unlimited}

	for name, elapsed := range map[string]time.Duration{
		"day 0": 0,
		"day 2": 2 * 24 * time.Hour,
		"day 6": 6 * 24 * time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			got := ApplyGrace(licensed, confirmed.Add(elapsed), confirmed)
			assert.Equal(t, 64, got.Channels, "the grace period restricts nothing (AC-06.5)")
		})
	}
}

func TestApplyGrace_DegradesToTheFloorAfterTheWindow(t *testing.T) {
	licensed := Entitlement{State: StateValid, Edition: "core", Channels: 64, MaxTenants: Unlimited}

	got := ApplyGrace(licensed, confirmed.Add(8*24*time.Hour), confirmed)

	assert.Equal(t, StateDegraded, got.State)
	assert.Equal(t, FreeChannels, got.Channels)
	assert.True(t, got.PermitsCalls(), "degrading never disables (D-12)")
	assert.Equal(t, Unlimited, got.MaxTenants, "the provisioned estate is untouched (AC-06.13)")
}

// TestApplyGrace_SetupIsUntouched keeps the Setup/Degraded separation
// intact at the one place they could be conflated. A system that was
// never activated has no last-confirmation timeline to sit on, and
// routing it through the grace machine is the first step toward the
// collapse D-52 forbids.
func TestApplyGrace_SetupIsUntouched(t *testing.T) {
	setup := SetupEntitlement()

	for name, elapsed := range map[string]time.Duration{
		"immediately":       0,
		"after the window":  8 * 24 * time.Hour,
		"long after":        365 * 24 * time.Hour,
		"before, via clock": -time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			got := ApplyGrace(setup, confirmed.Add(elapsed), confirmed)
			assert.Equal(t, StateSetup, got.State, "Setup never becomes Degraded (D-52)")
			assert.False(t, got.PermitsCalls())
		})
	}
}

// TestApplyGrace_RecoveryRestoresFullCapacity is the AC-06.5 recovery
// half: a successful confirmation returns the installation to valid and
// to its full entitlement, with no restart.
func TestApplyGrace_RecoveryRestoresFullCapacity(t *testing.T) {
	licensed := Entitlement{State: StateValid, Edition: "core", Channels: 64}
	now := confirmed.Add(9 * 24 * time.Hour)

	degraded := ApplyGrace(licensed, now, confirmed)
	require := assert.New(t)
	require.Equal(FreeChannels, degraded.Channels)

	// Confirmation succeeds: lastConfirmedAt moves to now.
	recovered := ApplyGrace(licensed, now, now)

	require.Equal(StateValid, recovered.State)
	require.Equal(64, recovered.Channels, "full capacity returns without a restart")
}
