// Package domain holds licensing's pure logic: what an entitlement is,
// how a licence token is verified, how the offline grace period is
// evaluated, and how concurrent channels are counted.
//
// Nothing here performs I/O, reads a clock, or touches a database. Times
// are passed in. That is what makes the grace state machine and the
// capacity rules exhaustively testable, and it is why this package
// imports nothing from another bounded context (HLD 04 §10.1, enforced
// by .golangci.yml's licensing-imports-nothing rule and internal/
// archtest's isolatedContexts).
package domain

// State is the licence state of the installation as a whole. It mirrors
// PRD §11.2, with one addition that §11.2 originally lacked: Setup.
type State string

const (
	// StateSetup is a freshly installed system with no entitlement ever
	// applied. It has an administration surface and NO call path
	// (D-52, BR-LIC-01).
	StateSetup State = "Setup"

	// StateValid is an entitlement in force and confirmed.
	StateValid State = "Valid"

	// StateUnverified is an entitlement in force whose daily
	// confirmation has not succeeded recently, still within the grace
	// period. No functional restriction whatsoever (D-12, AC-06.4).
	StateUnverified State = "EntitlementUnverified"

	// StateDegraded is an entitlement that expired, whose grace period
	// elapsed, or that failed verification. Capacity falls to
	// FreeChannels — never to zero, and never to Setup (BR-LIC-02).
	StateDegraded State = "Degraded"
)

// Reason is the distinct telemetry code on a refused call setup. They
// are separate constants because BR-05 and AC-06.6 require over-capacity
// to be separable from every other refusal in telemetry, and because
// they send an administrator to different places: buy more channels,
// apply a key, renew a licence, or investigate a tampered row.
type Reason string

const (
	ReasonPermitted    Reason = "permitted"
	ReasonOverCapacity Reason = "over_capacity"
	ReasonNotActivated Reason = "not_activated"
	ReasonExpired      Reason = "expired"
	ReasonTampered     Reason = "tampered"
)

// Free tier limits (BR-LIC-01, D-49, D-51). These are the values of the
// Free Community entitlement, and FreeChannels is separately the floor
// that every degraded cause falls back to (BR-LIC-02).
//
// They are NOT a fallback for the absence of an entitlement: an
// installation with no token is StateSetup with no capacity at all
// (D-52). Reaching four channels always means an entitlement existed.
const (
	// FreeChannels is both the Free Community allowance and the
	// degradation floor. Four is enough to prove an install works
	// (AC-09.1) and obviously not enough to run a business on, which is
	// what keeps the free tier an evaluation.
	FreeChannels = 4

	// FreeMaxExtensions caps the free tier at ten extensions. Published
	// by this context, enforced where extensions are created (pbx-core).
	FreeMaxExtensions = 10

	// FreeMaxTenants makes the free tier single-tenant (D-51).
	// Published here, enforced at tenant provisioning (identity).
	FreeMaxTenants = 1

	// Unlimited is the value meaning "no cap" in MaxExtensions and
	// MaxTenants. Zero rather than -1 is deliberate for wire and
	// database compactness; the hazard that creates — an absent field
	// reading as unlimited — is closed by construction, since an
	// Entitlement is only ever produced by the constructors below and a
	// zero-valued Entitlement is StateSetup, which permits nothing.
	Unlimited = 0
)

// Entitlement is what the installation may currently do. It is a value:
// copying it is safe, and nothing mutates one after construction.
type Entitlement struct {
	State         State
	Edition       string
	Channels      int
	MaxExtensions int
	MaxTenants    int

	// Reason explains a non-Valid state — why the installation is
	// degraded, or that it was never activated. Empty when Valid.
	Reason Reason
}

// SetupEntitlement is a system that has never been activated: no
// channels, no edition, and the not-activated reason on every refusal.
//
// It is deliberately NOT expressible as a degraded entitlement with zero
// channels, and there is no function converting one into the other. The
// distinction is load-bearing: degradation protects a running phone
// system and must never disable it (D-12, INV-03, BR-09), while Setup
// protects nothing because there is no system yet. Collapsing them is
// how an expired licence would come to disable production (D-52).
func SetupEntitlement() Entitlement {
	return Entitlement{
		State:         StateSetup,
		Channels:      0,
		MaxExtensions: 0,
		MaxTenants:    0,
		Reason:        ReasonNotActivated,
	}
}

// FreeEntitlement is the Free Community tier: a real, signed, perpetual
// entitlement that happens to cost nothing (D-52).
func FreeEntitlement() Entitlement {
	return Entitlement{
		State:         StateValid,
		Edition:       "free",
		Channels:      FreeChannels,
		MaxExtensions: FreeMaxExtensions,
		MaxTenants:    FreeMaxTenants,
	}
}

// DegradedEntitlement is the floor an expired, grace-elapsed or tampered
// licence falls back to: FreeChannels, never zero, with the cause
// carried in reason rather than in a separate state per cause
// (BR-LIC-02).
//
// The extension and tenant caps of the previous entitlement are NOT
// reduced here. Degrading constrains new call capacity only; it never
// removes or disables an already-provisioned extension or tenant
// (AC-06.13).
func DegradedEntitlement(prev Entitlement, reason Reason) Entitlement {
	return Entitlement{
		State:         StateDegraded,
		Edition:       prev.Edition,
		Channels:      FreeChannels,
		MaxExtensions: prev.MaxExtensions,
		MaxTenants:    prev.MaxTenants,
		Reason:        reason,
	}
}

// PermitsCalls reports whether any call may be set up in this state. It
// exists so the Setup/Degraded distinction is asserted through behaviour
// rather than by comparing state strings.
func (e Entitlement) PermitsCalls() bool { return e.Channels > 0 }
