package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetupIsNotDegraded is task 2.6a and LLD-08 DoD 11. Setup and
// Degraded must stay distinct in both directions, because collapsing
// them is how an expired licence would come to disable a working phone
// system — the one outcome the product promises never to produce
// (D-52, D-12, INV-03).
//
// Asserted through behaviour, not by comparing state strings: a later
// refactor could rename the states and this test would still hold.
func TestSetupIsNotDegraded(t *testing.T) {
	setup := SetupEntitlement()
	degraded := DegradedEntitlement(FreeEntitlement(), ReasonExpired)

	t.Run("setup permits no calls at all", func(t *testing.T) {
		assert.False(t, setup.PermitsCalls())
		assert.Zero(t, setup.Channels)
	})

	t.Run("degraded permits the floor, never zero", func(t *testing.T) {
		assert.True(t, degraded.PermitsCalls())
		assert.Equal(t, FreeChannels, degraded.Channels)
	})

	t.Run("the two states are not equal", func(t *testing.T) {
		assert.NotEqual(t, setup.State, degraded.State)
		assert.NotEqual(t, setup, degraded)
	})

	t.Run("their refusal reasons are distinguishable", func(t *testing.T) {
		assert.Equal(t, ReasonNotActivated, setup.Reason)
		assert.Equal(t, ReasonExpired, degraded.Reason)
	})
}

// TestDegradedNeverDisables covers D-12 directly: whatever the cause,
// degrading leaves a usable system. Table-driven over every cause so a
// new one cannot be added that degrades to zero.
func TestDegradedNeverDisables(t *testing.T) {
	for _, reason := range []Reason{ReasonExpired, ReasonTampered, ReasonOverCapacity} {
		t.Run(string(reason), func(t *testing.T) {
			e := DegradedEntitlement(FreeEntitlement(), reason)
			assert.True(t, e.PermitsCalls(), "degrading must never disable (D-12)")
			assert.Equal(t, FreeChannels, e.Channels)
			assert.Equal(t, reason, e.Reason, "the cause must survive into telemetry")
		})
	}
}

// TestDegradingDoesNotShrinkTheEstate is AC-06.13: reducing capacity
// constrains new calls only. An Operator with fifty tenants whose
// licence lapses must not lose forty-nine of them.
func TestDegradingDoesNotShrinkTheEstate(t *testing.T) {
	licensed := Entitlement{
		State: StateValid, Edition: "operator",
		Channels: 128, MaxExtensions: Unlimited, MaxTenants: Unlimited,
	}

	degraded := DegradedEntitlement(licensed, ReasonExpired)

	assert.Equal(t, FreeChannels, degraded.Channels, "call capacity is capped")
	assert.Equal(t, Unlimited, degraded.MaxExtensions, "extensions are not taken away (AC-06.13)")
	assert.Equal(t, Unlimited, degraded.MaxTenants, "tenants are not taken away (AC-06.13)")
	assert.Equal(t, "operator", degraded.Edition, "the edition is still what they bought")
}

// TestZeroEntitlementPermitsNothing closes the zero-value hazard that
// Unlimited == 0 creates. A struct nobody constructed must not read as
// an unlimited entitlement; it must read as Setup, which permits no
// calls. This is the licensing analogue of LLD-08 §3's rule that a
// zero-valued LicenseToken is never an entitlement.
func TestZeroEntitlementPermitsNothing(t *testing.T) {
	var zero Entitlement

	require.False(t, zero.PermitsCalls(),
		"a zero-valued Entitlement must never permit a call — Unlimited is 0, so an unconstructed struct must not read as unlimited capacity")
	assert.NotEqual(t, StateValid, zero.State)
}

// TestFreeTierValues pins the numbers BR-LIC-01 fixes. They are business
// commitments published in the BRD's entitlement matrix, not
// implementation details, so changing one should fail a test and force
// the conversation.
func TestFreeTierValues(t *testing.T) {
	free := FreeEntitlement()

	assert.Equal(t, 4, free.Channels, "BR-LIC-01: four simultaneous calls")
	assert.Equal(t, 10, free.MaxExtensions, "BR-LIC-01: ten extensions")
	assert.Equal(t, 1, free.MaxTenants, "BR-LIC-01/D-51: single tenant")
	assert.True(t, free.PermitsCalls(), "the free tier is a working phone system")
	assert.Equal(t, StateValid, free.State,
		"free is an activated entitlement, not a degraded or unactivated one (D-52)")
}

// TestRefusalReasonsAreDistinct is task 2.9. Telemetry must separate
// over-capacity on a valid licence from an unactivated system, an
// expired one, and a tampered one (BR-05, AC-06.6). A duplicated
// constant would silently merge two causes in a partner's dashboards.
func TestRefusalReasonsAreDistinct(t *testing.T) {
	reasons := []Reason{
		ReasonPermitted, ReasonOverCapacity,
		ReasonNotActivated, ReasonExpired, ReasonTampered,
	}

	seen := map[Reason]bool{}
	for _, r := range reasons {
		require.NotEmpty(t, r, "a reason code must never be the empty string")
		require.False(t, seen[r], "reason %q is duplicated — two causes would be indistinguishable in telemetry", r)
		seen[r] = true
	}
	assert.Len(t, seen, len(reasons))
}
