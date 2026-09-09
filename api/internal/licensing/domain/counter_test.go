package domain

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func licensed(channels int) Entitlement {
	return Entitlement{State: StateValid, Edition: "core", Channels: channels}
}

func TestReserveWithinEntitlement(t *testing.T) {
	c := NewCounter()
	e := licensed(4)

	for i := 0; i < 4; i++ {
		v := c.Reserve(fmt.Sprintf("call-%d", i), e, t0)
		assert.True(t, v.Permitted, "call %d is within the entitlement", i)
		assert.Equal(t, ReasonPermitted, v.Reason)
	}
	assert.Equal(t, 4, c.InUse())
}

// TestReserveIsIdempotentPerCall: a retried setup for the same call must
// not consume a second channel, for the same reason release is keyed.
func TestReserveIsIdempotentPerCall(t *testing.T) {
	c := NewCounter()
	e := licensed(1)

	require.True(t, c.Reserve("call-a", e, t0).Permitted)
	require.True(t, c.Reserve("call-a", e, t0).Permitted, "the same call reserving twice is not a second channel")

	assert.Equal(t, 1, c.InUse())
	assert.False(t, c.Reserve("call-b", e, t0).Permitted, "a different call still hits the limit")
}

// TestReleaseIsIdempotent is D-58's central assertion. A blind decrement
// running twice under-counts, which permits calls that should be
// refused — licence leakage nothing surfaces, because everything keeps
// working. Keyed release makes the repeat a no-op.
func TestReleaseIsIdempotent(t *testing.T) {
	c := NewCounter()
	e := licensed(2)
	require.True(t, c.Reserve("call-a", e, t0).Permitted)
	require.True(t, c.Reserve("call-b", e, t0).Permitted)

	c.Release("call-a")
	c.Release("call-a")
	c.Release("call-a")

	assert.Equal(t, 1, c.InUse(), "releasing one call three times frees exactly one channel")
}

func TestReleaseOfAnUnknownCallDoesNothing(t *testing.T) {
	c := NewCounter()
	e := licensed(2)
	require.True(t, c.Reserve("call-a", e, t0).Permitted)

	// A screened-out call never reserved anything, so its termination
	// must not free someone else's channel.
	c.Release("call-never-reserved")

	assert.Equal(t, 1, c.InUse())
}

func TestRefusedCallConsumesNothing(t *testing.T) {
	c := NewCounter()
	e := licensed(1)
	require.True(t, c.Reserve("call-a", e, t0).Permitted)

	v := c.Reserve("call-b", e, t0)

	require.False(t, v.Permitted)
	assert.Equal(t, 1, c.InUse(), "a refusal must not consume a channel")
}

// TestSetupRefusesWithItsOwnReason keeps the Setup/Degraded distinction
// visible at the point of refusal: an unactivated system must not report
// over-capacity, which would send an administrator hunting for a channel
// to free instead of applying a key (D-52).
func TestSetupRefusesWithItsOwnReason(t *testing.T) {
	c := NewCounter()

	v := c.Reserve("call-a", SetupEntitlement(), t0)

	assert.False(t, v.Permitted)
	assert.Equal(t, ReasonNotActivated, v.Reason)
	assert.NotEqual(t, ReasonOverCapacity, v.Reason)
	assert.Zero(t, c.InUse())
}

func TestDegradedStillPermitsTheFloor(t *testing.T) {
	c := NewCounter()
	e := DegradedEntitlement(licensed(64), ReasonExpired)

	for i := 0; i < FreeChannels; i++ {
		assert.True(t, c.Reserve(fmt.Sprintf("call-%d", i), e, t0).Permitted,
			"a degraded installation still makes calls (D-12)")
	}
}

func TestBurstAllowance(t *testing.T) {
	t.Run("absorbs a short overage", func(t *testing.T) {
		c := NewCounter()
		e := licensed(32) // burst allowance 3
		for i := 0; i < 32; i++ {
			require.True(t, c.Reserve(fmt.Sprintf("call-%d", i), e, t0).Permitted)
		}

		v := c.Reserve("call-over-1", e, t0)

		assert.True(t, v.Permitted, "the first channel above entitlement is absorbed")
	})

	t.Run("refuses a sustained overage with its own reason", func(t *testing.T) {
		c := NewCounter()
		e := licensed(32)
		for i := 0; i < 32; i++ {
			require.True(t, c.Reserve(fmt.Sprintf("call-%d", i), e, t0).Permitted)
		}
		require.True(t, c.Reserve("call-over-1", e, t0).Permitted) // opens the window

		v := c.Reserve("call-over-2", e, t0.Add(BurstWindow))

		assert.False(t, v.Permitted)
		assert.Equal(t, ReasonOverCapacity, v.Reason,
			"over-capacity must be separable from every other refusal in telemetry (AC-06.6)")
	})

	t.Run("refuses immediately beyond the full allowance", func(t *testing.T) {
		c := NewCounter()
		e := licensed(32)
		for i := 0; i < 35; i++ { // 32 + 3 burst
			require.True(t, c.Reserve(fmt.Sprintf("call-%d", i), e, t0).Permitted)
		}

		v := c.Reserve("call-36", e, t0) // same instant, no window elapsed

		assert.False(t, v.Permitted, "beyond entitlement plus the full allowance, no window is waited for")
	})

	t.Run("a small entitlement gets no burst", func(t *testing.T) {
		c := NewCounter()
		e := licensed(FreeChannels) // 4/10 == 0
		for i := 0; i < FreeChannels; i++ {
			require.True(t, c.Reserve(fmt.Sprintf("call-%d", i), e, t0).Permitted)
		}

		assert.False(t, c.Reserve("call-5", e, t0).Permitted,
			"10% of 4 is not a burst; the free tier is 4 flat, as BRD §12.2 lists it")
	})

	t.Run("dropping back under entitlement closes the window", func(t *testing.T) {
		c := NewCounter()
		e := licensed(32)
		for i := 0; i < 32; i++ {
			require.True(t, c.Reserve(fmt.Sprintf("call-%d", i), e, t0).Permitted)
		}
		require.True(t, c.Reserve("call-over", e, t0).Permitted)
		c.Release("call-over")
		c.Release("call-0")

		// Back to 31 in use; a later burst gets a fresh window rather
		// than inheriting the stale one.
		require.True(t, c.Reserve("call-a", e, t0.Add(time.Hour)).Permitted)
		v := c.Reserve("call-b", e, t0.Add(time.Hour))

		assert.True(t, v.Permitted, "a new busy period is absorbed, not refused by a stale window")
	})
}

// TestCounterUnderRace is AC-06.10: exact under simultaneous setup at
// ten times the expected rate, verified with -race. The assertion that
// matters is the return to zero — a counter that leaked would drift
// upward and eventually refuse everything.
func TestCounterUnderRace(t *testing.T) {
	const (
		capacity = 64
		workers  = 640 // 10x
	)
	c := NewCounter()
	e := licensed(capacity)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		permitted int
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("call-%d", i)
			if c.Reserve(id, e, t0).Permitted {
				mu.Lock()
				permitted++
				mu.Unlock()
				c.Release(id)
			}
		}(i)
	}
	wg.Wait()

	assert.Zero(t, c.InUse(), "every reserved channel must be returned — a leak here is licence drift")
	assert.Positive(t, permitted, "the run must actually have exercised the counter")
	assert.LessOrEqual(t, permitted, workers)
}

// TestCounterNeverExceedsTheCeiling holds the invariant under
// concurrency: no interleaving may admit more than entitlement plus the
// burst allowance simultaneously. Reservations are held rather than
// released so the peak is observable.
func TestCounterNeverExceedsTheCeiling(t *testing.T) {
	const capacity = 20
	ceiling := capacity + burstAllowance(capacity)

	c := NewCounter()
	e := licensed(capacity)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Reserve(fmt.Sprintf("call-%d", i), e, t0)
		}(i)
	}
	wg.Wait()

	assert.LessOrEqual(t, c.InUse(), ceiling,
		"concurrent setups must never admit more than entitlement plus burst")
}

func TestBurstAllowanceSizing(t *testing.T) {
	for capacity, want := range map[int]int{
		0: 0, 1: 0, 4: 0, 9: 0, 10: 1, 32: 3, 64: 6, 128: 12, 512: 51,
	} {
		assert.Equal(t, want, burstAllowance(capacity), "capacity %d", capacity)
	}
}
