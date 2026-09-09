package domain

import (
	"sync"
	"time"
)

// BurstDivisor sets the overage allowance as a fraction of the licensed
// entitlement: capacity/10, i.e. 10% (T-2/T-3, BRD §10.2).
//
// Integer division, deliberately. A 32-channel licence gets 3 spare
// channels; a 4-channel free tier gets none, because 10% of 4 is not a
// burst and granting the free tier 25% more than it says would
// contradict the entitlement matrix in BRD §12.2, which lists 4 flat.
const BurstDivisor = 10

// BurstWindow is how long usage may sit above the licensed entitlement
// before the allowance stops absorbing it. Beyond this, an overage is no
// longer a busy hour — it is under-buying, and the refusals it produces
// are the signal the partner needs (BR-05).
const BurstWindow = 15 * time.Minute

// Counter tracks concurrent channels in use for the installation.
//
// Reservations are keyed by call so release is idempotent rather than a
// blind decrement (D-58). That matters because of which way the two
// options fail: a decrement that runs twice under-counts, so the
// installation permits calls it should refuse — licence leakage that no
// test notices, because everything continues to work. A keyed release
// that runs twice does nothing at all.
//
// Counts are in memory and per process (LLD-08 §5, §7). A restart resets
// them while calls are still up; on a single node that self-corrects as
// those calls end, and sharing the count across nodes is D-08's
// multi-node work, not this.
type Counter struct {
	mu sync.Mutex

	// held is the set of calls currently holding a reservation. A map
	// rather than an integer is what makes release idempotent: releasing
	// a call that holds nothing is a no-op by construction.
	held map[string]struct{}

	// overSince is when usage first exceeded the licensed entitlement,
	// zero while at or below it. The burst allowance is bounded by this
	// rather than by a token bucket so that a busy hour is absorbed but
	// a permanently under-bought installation is not.
	overSince time.Time
}

// NewCounter returns an empty counter.
func NewCounter() *Counter {
	return &Counter{held: make(map[string]struct{})}
}

// InUse reports how many calls currently hold a reservation.
func (c *Counter) InUse() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.held)
}

// Reserve attempts to take one channel for callID against e, and returns
// the verdict.
//
// Reserving for a call that already holds a channel is a no-op returning
// permitted: a retried setup for the same call must not consume a second
// channel, for the same reason release is keyed.
func (c *Counter) Reserve(callID string, e Entitlement, now time.Time) CapacityVerdict {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.held[callID]; exists {
		return CapacityVerdict{Permitted: true, Reason: ReasonPermitted}
	}

	// An installation that was never activated has no capacity to
	// reserve from, and the reason says so rather than reporting an
	// over-capacity condition that would send an administrator looking
	// for a channel to free (D-52).
	if !e.PermitsCalls() {
		return CapacityVerdict{Permitted: false, Reason: e.Reason}
	}

	want := len(c.held) + 1
	switch {
	case want <= e.Channels:
		// Within entitlement: any prior overage is over.
		c.overSince = time.Time{}

	case want <= e.Channels+burstAllowance(e.Channels):
		// Into the burst allowance. Start the window on first entry, and
		// refuse once it has been open too long.
		if c.overSince.IsZero() {
			c.overSince = now
		}
		if now.Sub(c.overSince) >= BurstWindow {
			return CapacityVerdict{Permitted: false, Reason: ReasonOverCapacity}
		}

	default:
		// Beyond entitlement plus the full allowance: refused
		// immediately, without waiting for any window.
		return CapacityVerdict{Permitted: false, Reason: ReasonOverCapacity}
	}

	c.held[callID] = struct{}{}
	return CapacityVerdict{Permitted: true, Reason: ReasonPermitted}
}

// Release returns callID's channel. Releasing a call that holds nothing
// — because it was refused, because it already released, or because a
// second termination signal arrived — is a no-op (D-58).
func (c *Counter) Release(callID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.held, callID)
	if len(c.held) == 0 {
		c.overSince = time.Time{}
	}
}

// burstAllowance is the overage permitted above capacity.
func burstAllowance(capacity int) int {
	if capacity <= 0 {
		return 0
	}
	return capacity / BurstDivisor
}

// CapacityVerdict is licensing's answer to a capacity check: whether the
// call may proceed, and a distinct telemetry reason when it may not.
//
// It carries no SIP response code and no retry interval. Shaping a
// refusal into signalling is ingress's job (pbx-core, LLD-08 §1), and a
// verdict that named a SIP code would put licensing in the signalling
// path it deliberately stays out of.
type CapacityVerdict struct {
	Permitted bool
	Reason    Reason
}
