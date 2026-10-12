package domain

// This file turns an entitlement and live usage into what PRD §11.2 calls
// the licence and capacity state (LLD-08 §6.1, D-70). §11.2 lists eight
// states flat, but they mix three independent facts — the entitlement
// state, the tier, and the capacity level — so all three are reported,
// plus one headline the console shows, chosen here by a fixed precedence.
// The browser never derives any of it.

// Condition is the one PRD §11.2 state an installation is in right now:
// the most urgent of its facts.
type Condition string

// Conditions, in precedence order (most urgent first).
const (
	ConditionSetup           Condition = "Setup"
	ConditionDegraded        Condition = "Degraded"
	ConditionCapacityReached Condition = "CapacityReached"
	ConditionUnverified      Condition = "EntitlementUnverified"
	ConditionCapacityWarning Condition = "CapacityWarning"
	ConditionFreeCommunity   Condition = "FreeCommunity"
	ConditionValid           Condition = "Valid"
)

// Tier is which kind of entitlement is in force.
type Tier string

// Tiers. TierNone is Setup: no entitlement, so no tier.
const (
	TierNone          Tier = ""
	TierFreeCommunity Tier = "FreeCommunity"
	TierCommercial    Tier = "Commercial"
)

// CapacityLevel is live usage against the channels in force.
type CapacityLevel string

// Capacity levels. CapacityNone is Setup: there is no capacity to measure.
const (
	CapacityNone    CapacityLevel = ""
	CapacityNormal  CapacityLevel = "Normal"
	CapacityWarning CapacityLevel = "Warning"
	CapacityReached CapacityLevel = "Reached"
)

// FreeEdition is the edition string of the Free Community tier: the one
// FreeEntitlement and the licence issuer use (BR-LIC-01). The tier is
// read from it, never from a channel count — a 4-channel commercial
// licence is still commercial.
const FreeEdition = "free"

// CapacityWarningPercent is where usage becomes a warning (PRD §11.2).
const CapacityWarningPercent = 80

// StatusConditions is the reported view of an entitlement under load.
type StatusConditions struct {
	Condition Condition
	Tier      Tier
	Capacity  CapacityLevel
}

// Conditions computes the condition, tier and capacity level of
// entitlement e with inUse channels held. Pure: no clock, no I/O.
//
// Thresholds use integer arithmetic so no rounding edge exists: Warning
// when inUse×100 ≥ 80×channels, Reached when inUse ≥ channels. Usage
// above the channels in force — calls admitted into the burst allowance —
// is still Reached: the allowance absorbs a busy hour, it does not make
// the installation any less full. Degraded is measured against its floor.
//
// Precedence, most urgent first: Setup, Degraded, Capacity Reached,
// Entitlement Unverified, Capacity Warning, Free Community, Valid.
// Degraded leads Reached because it explains why capacity is low and how
// to restore it; Reached leads Unverified because calls are being declined
// now, while Unverified is a deadline.
func Conditions(e Entitlement, inUse int) StatusConditions {
	if e.State == StateSetup || !e.PermitsCalls() {
		return StatusConditions{Condition: ConditionSetup, Tier: TierNone, Capacity: CapacityNone}
	}

	out := StatusConditions{Tier: TierCommercial, Capacity: capacityLevel(e.Channels, inUse)}
	if e.Edition == FreeEdition {
		out.Tier = TierFreeCommunity
	}

	switch {
	case e.State == StateDegraded:
		out.Condition = ConditionDegraded
	case out.Capacity == CapacityReached:
		out.Condition = ConditionCapacityReached
	case e.State == StateUnverified:
		out.Condition = ConditionUnverified
	case out.Capacity == CapacityWarning:
		out.Condition = ConditionCapacityWarning
	case out.Tier == TierFreeCommunity:
		out.Condition = ConditionFreeCommunity
	default:
		out.Condition = ConditionValid
	}
	return out
}

func capacityLevel(channels, inUse int) CapacityLevel {
	switch {
	case inUse >= channels:
		return CapacityReached
	case inUse*100 >= CapacityWarningPercent*channels:
		return CapacityWarning
	default:
		return CapacityNormal
	}
}
