package domain

import (
	"fmt"
	"testing"
)

// The condition rule (LLD-08 §6.1, design D1–D3, D5) over every
// entitlement state × tier × usage, at each threshold's exact boundary.

func commercial(state State, channels int) Entitlement {
	return Entitlement{State: state, Edition: "core", Channels: channels}
}

func TestConditions_SetupHasNoTierAndNoCapacity(t *testing.T) {
	got := Conditions(SetupEntitlement(), 0)
	want := StatusConditions{Condition: ConditionSetup, Tier: TierNone, Capacity: CapacityNone}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestConditions_CapacityBoundaries(t *testing.T) {
	// 10 channels: 7 = 70%, 8 = 80% (warning), 9 = 90%, 10 = 100%
	// (reached), 11 = inside the 10% burst allowance (still reached).
	for _, tc := range []struct {
		inUse int
		want  CapacityLevel
	}{
		{0, CapacityNormal}, {7, CapacityNormal}, {8, CapacityWarning}, {9, CapacityWarning},
		{10, CapacityReached}, {11, CapacityReached},
	} {
		got := Conditions(commercial(StateValid, 10), tc.inUse).Capacity
		if got != tc.want {
			t.Errorf("10 channels, %d in use: got %s, want %s", tc.inUse, got, tc.want)
		}
	}
	// 5 channels: 79% cannot occur; 3 = 60% normal, 4 = 80% warning, 5 reached.
	// 100 channels: 79 normal, 80 warning, 99 warning, 100 reached.
	for _, tc := range []struct {
		channels, inUse int
		want            CapacityLevel
	}{
		{5, 3, CapacityNormal}, {5, 4, CapacityWarning}, {5, 5, CapacityReached},
		{100, 79, CapacityNormal}, {100, 80, CapacityWarning}, {100, 99, CapacityWarning}, {100, 100, CapacityReached},
		{1, 0, CapacityNormal}, {1, 1, CapacityReached},
	} {
		got := Conditions(commercial(StateValid, tc.channels), tc.inUse).Capacity
		if got != tc.want {
			t.Errorf("%d channels, %d in use: got %s, want %s", tc.channels, tc.inUse, got, tc.want)
		}
	}
}

func TestConditions_TierFromEditionNotChannels(t *testing.T) {
	if got := Conditions(FreeEntitlement(), 0).Tier; got != TierFreeCommunity {
		t.Errorf("free edition: got %s", got)
	}
	fourChannelCommercial := commercial(StateValid, FreeChannels)
	if got := Conditions(fourChannelCommercial, 0).Tier; got != TierCommercial {
		t.Errorf("a 4-channel commercial licence is commercial: got %s", got)
	}
}

func TestConditions_Precedence(t *testing.T) {
	free := FreeEntitlement() // 4 channels: 2 is 50%, 4 is reached
	degradedFree := DegradedEntitlement(FreeEntitlement(), ReasonExpired)
	unverified := commercial(StateUnverified, 10)
	degraded := DegradedEntitlement(commercial(StateValid, 32), ReasonExpired)

	for _, tc := range []struct {
		name  string
		e     Entitlement
		inUse int
		want  StatusConditions
	}{
		{"valid, quiet", commercial(StateValid, 10), 1,
			StatusConditions{ConditionValid, TierCommercial, CapacityNormal}},
		{"valid, warning", commercial(StateValid, 10), 8,
			StatusConditions{ConditionCapacityWarning, TierCommercial, CapacityWarning}},
		{"valid, reached", commercial(StateValid, 10), 10,
			StatusConditions{ConditionCapacityReached, TierCommercial, CapacityReached}},
		{"free, quiet", free, 2,
			StatusConditions{ConditionFreeCommunity, TierFreeCommunity, CapacityNormal}},
		{"free, reached", free, 4,
			StatusConditions{ConditionCapacityReached, TierFreeCommunity, CapacityReached}},
		{"unverified, quiet", unverified, 1,
			StatusConditions{ConditionUnverified, TierCommercial, CapacityNormal}},
		{"unverified beats warning", unverified, 8,
			StatusConditions{ConditionUnverified, TierCommercial, CapacityWarning}},
		{"reached beats unverified", unverified, 10,
			StatusConditions{ConditionCapacityReached, TierCommercial, CapacityReached}},
		{"degraded, quiet, measured against its floor", degraded, 1,
			StatusConditions{ConditionDegraded, TierCommercial, CapacityNormal}},
		{"degraded beats reached; capacity still says reached", degraded, FreeChannels,
			StatusConditions{ConditionDegraded, TierCommercial, CapacityReached}},
		{"degraded free tier keeps its tier", degradedFree, 0,
			StatusConditions{ConditionDegraded, TierFreeCommunity, CapacityNormal}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Conditions(tc.e, tc.inUse); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Every state × tier × level combination yields a condition, and the
// condition always agrees with the facts it is built from.
func TestConditions_HeadlineAgreesWithFacts(t *testing.T) {
	for _, state := range []State{StateValid, StateUnverified, StateDegraded} {
		for _, edition := range []string{FreeEdition, "core"} {
			for inUse := 0; inUse <= 12; inUse++ {
				e := Entitlement{State: state, Edition: edition, Channels: 10}
				if state == StateDegraded {
					e = DegradedEntitlement(Entitlement{Edition: edition}, ReasonExpired)
				}
				got := Conditions(e, inUse)
				name := fmt.Sprintf("%s/%s/%d", state, edition, inUse)
				switch {
				case state == StateDegraded && got.Condition != ConditionDegraded:
					t.Errorf("%s: degraded must headline, got %s", name, got.Condition)
				case got.Capacity == CapacityReached && state != StateDegraded && got.Condition != ConditionCapacityReached:
					t.Errorf("%s: reached must headline, got %s", name, got.Condition)
				case got.Condition == ConditionValid && (got.Tier != TierCommercial || got.Capacity != CapacityNormal || state != StateValid):
					t.Errorf("%s: Valid only when nothing else applies, got %+v", name, got)
				case got.Condition == ConditionFreeCommunity && (got.Tier != TierFreeCommunity || got.Capacity != CapacityNormal || state != StateValid):
					t.Errorf("%s: Free Community only when quiet and valid, got %+v", name, got)
				}
			}
		}
	}
}
