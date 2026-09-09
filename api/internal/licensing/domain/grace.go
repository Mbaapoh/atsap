package domain

import "time"

// Offline grace timings (D-14, AC-06.4, AC-06.5).
const (
	// GracePeriod is how long an installation runs with no functional
	// restriction after the last successful entitlement confirmation.
	//
	// Seven days is a business decision with a stated cost: D-14 records
	// that it makes our entitlement service critical infrastructure,
	// because an outage lasting longer degrades every partner in the
	// field at once. It is not a number to tune casually.
	GracePeriod = 7 * 24 * time.Hour

	// WarningAfter is when administrator warnings begin. Deliberately
	// later than the first day: a transient failure that resolves itself
	// overnight must not alarm anyone (AC-06.4).
	WarningAfter = 24 * time.Hour
)

// GraceState reports the licence state implied by the time elapsed since
// the last successful entitlement confirmation.
//
// It is a pure function of (now, lastConfirmedAt) and nothing else. Not
// of how many confirmations have failed, not of process uptime, not of
// the order in which components observe it. That is what makes the same
// elapsed time always produce the same state, and what makes a restart
// unable to reset the grace period — the clock is the licence's, not the
// process's (spec `entitlement-grace`).
//
// It never returns StateSetup: absence of an entitlement is not a point
// on this timeline, it is the absence of one (D-52).
func GraceState(now, lastConfirmedAt time.Time) State {
	elapsed := now.Sub(lastConfirmedAt)
	switch {
	case elapsed >= GracePeriod:
		return StateDegraded
	case elapsed >= WarningAfter:
		return StateUnverified
	default:
		return StateValid
	}
}

// ShouldWarn reports whether an administrator-visible warning is due.
//
// True from day two onward, and true while degraded as well — a degraded
// installation needs the warning more than an unverified one, not less.
func ShouldWarn(now, lastConfirmedAt time.Time) bool {
	return now.Sub(lastConfirmedAt) >= WarningAfter
}

// ApplyGrace folds the grace state into an entitlement.
//
// Valid and Unverified both keep the full licensed capacity: the grace
// period carries no functional restriction at all (AC-06.5, PRD §11.2).
// Only elapsing it degrades, and degrading goes to the floor rather than
// to zero.
//
// Grace can only ever make things worse, never better. Two states are
// therefore returned untouched:
//
//   - StateSetup, because nothing about a system that was never
//     activated is a function of when it last confirmed, and routing it
//     through here is the first step toward the Setup/Degraded collapse
//     D-52 forbids.
//   - StateDegraded, because it is already at the floor. An expired
//     licence read two days after expiry is still expired; letting the
//     grace window promote it back to Unverified would restore full
//     capacity to a licence that has run out, and would report the wrong
//     cause to the administrator besides.
//
// That second case is not hypothetical: an earlier version of this
// function overwrote the state unconditionally, so an expired licence
// inside the grace window came back as Unverified with its capacity
// restored. Caught on 2026-09-09 by TestExpiryIsReEvaluatedAsTimePasses.
func ApplyGrace(e Entitlement, now, lastConfirmedAt time.Time) Entitlement {
	if e.State == StateSetup || e.State == StateDegraded {
		return e
	}
	switch GraceState(now, lastConfirmedAt) {
	case StateDegraded:
		return DegradedEntitlement(e, ReasonExpired)
	case StateUnverified:
		e.State = StateUnverified
		return e
	default:
		return e
	}
}
