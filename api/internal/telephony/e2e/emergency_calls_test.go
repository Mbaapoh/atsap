//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	licensingdomain "atsap-api/internal/licensing/domain"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
	telephonypostgres "atsap-api/internal/telephony/postgres"
)

// TestEmergencyCallsConnectInEveryLicenceState is task 6.3 (INV-01,
// AC-06.7, spec capacity-enforcement): an emergency destination connects
// with capacity exhausted, while degraded, with an expired licence, and
// with no licence at all.
//
// What this proves, precisely: telephony-core's Screening consults
// neither capacity nor compliance for a bypass-marked call, so no licence
// state can stand between the caller and the number.
//
// What it does NOT prove: number identification. Nothing here decides
// that "112" is an emergency destination — that is pbx/domain.IsEmergency
// (unit-proven, LLD-03 §3), and the flag is set explicitly here rather
// than derived because HLD 04 §10.1 forbids telephony-core from importing
// pbx-core, tests included. Deriving it at the live ingress is
// pbx-inbound-and-emergency's work. A test asserting an ordinary call
// connects would prove nothing about the invariant (coverage.md G2's
// objection) — so every stage below first proves the ordinary call is
// refused, and none of them is vacuous.
func TestEmergencyCallsConnectInEveryLicenceState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	rig := newLicenceRig(t, ctx, 1)

	// placeEmergency drives one emergency call to Active with media up,
	// asserting licensing never saw it, and tears it down.
	placeEmergency := func(t *testing.T, stage string) {
		t.Helper()
		before := rig.licSvc.ChannelsInUse()
		callID, err := rig.svc.InitiateCall(ctx, ports.InitiateCallCommand{
			TenantID: rig.tenantID, Direction: domain.Outbound,
			SourceNumber: "1000", DestNumber: "112", BypassScreening: true,
			SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
		})
		require.NoError(t, err)
		waitFor(t, ctx, "emergency call Active ("+stage+")", func() (string, error) {
			c, err := telephonypostgres.NewCallStore(rig.pool).GetCall(ctx, rig.tenantID, callID)
			if err != nil {
				return "", err
			}
			return string(c.State), nil
		}, domain.CallActive)

		chans, err := rig.ari.ListChannels(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, chans,
			"media must be up for the emergency call (%s)", stage)
		assert.Equal(t, before, rig.licSvc.ChannelsInUse(),
			"emergency must consume no capacity (%s)", stage)

		require.NoError(t, rig.svc.HangupCall(ctx, callID, "e2e complete"))
		hangupCallChannels(t, ctx, rig.ari, callID.String())
	}

	// assertOrdinaryRefused proves the stage is not vacuous: without the
	// flag, the same rig refuses the same endpoints with a verdict.
	assertOrdinaryRefused := func(t *testing.T, stage string) {
		t.Helper()
		refused, err := rig.svc.InitiateCall(ctx, ports.InitiateCallCommand{
			TenantID: rig.tenantID, Direction: domain.Outbound,
			SourceNumber: "1000", DestNumber: "1001",
			SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
		})
		require.NoError(t, err, "screened-out is a verdict, not an error (%s)", stage)
		c, err := telephonypostgres.NewCallStore(rig.pool).GetCall(ctx, rig.tenantID, refused)
		require.NoError(t, err)
		assert.Equal(t, domain.CallTerminated, c.State,
			"ordinary call must be refused in stage %s or the emergency proof is vacuous", stage)
	}

	t.Run("exhausted", func(t *testing.T) {
		v, err := rig.licSvc.ValidateCapacity(ctx, rig.tenantID, "fill-1", 1)
		require.NoError(t, err)
		require.True(t, v.Permitted)
		require.Equal(t, 1, rig.licSvc.ChannelsInUse())

		assertOrdinaryRefused(t, "exhausted")
		placeEmergency(t, "exhausted")
	})

	t.Run("degraded", func(t *testing.T) {
		*rig.clock = rig.clock.Add(9 * 24 * time.Hour)
		e, err := rig.licSvc.Entitlement(ctx)
		require.NoError(t, err)
		require.Equal(t, licensingdomain.StateDegraded, e.State)

		// The degraded floor is 4; fill-1 still holds one, so three more
		// exhaust it.
		for _, id := range []string{"fill-2", "fill-3", "fill-4"} {
			v, err := rig.licSvc.ValidateCapacity(ctx, rig.tenantID, id, 1)
			require.NoError(t, err)
			require.True(t, v.Permitted)
		}
		require.Equal(t, 4, rig.licSvc.ChannelsInUse())

		assertOrdinaryRefused(t, "degraded")
		placeEmergency(t, "degraded")
	})

	t.Run("expired", func(t *testing.T) {
		past := rig.clock.Add(-time.Hour)
		require.NoError(t, rig.licSvc.ApplyLicenseKey(ctx, rig.sign(licensingdomain.LicenseToken{
			Edition: "e2e", Capacity: 1, ExpiresAt: past,
			InstanceID: "00000000-0000-0000-0000-0000000000e3",
		})))
		e, err := rig.licSvc.Entitlement(ctx)
		require.NoError(t, err)
		require.Equal(t, licensingdomain.StateDegraded, e.State)
		// Re-applying touches the row, never the counter: the four
		// fillers from the degraded stage still hold the floor.
		require.Equal(t, 4, rig.licSvc.ChannelsInUse())

		assertOrdinaryRefused(t, "expired")
		placeEmergency(t, "expired")
	})

	t.Run("no licence at all", func(t *testing.T) {
		_, err := rig.pool.Unwrap().Exec(ctx, `DELETE FROM licensing_state`)
		require.NoError(t, err)

		// The row is gone but the service holds its last verified
		// entitlement cached — and that cache still refuses (the four
		// fillers hold the degraded floor), which is fail-closed and
		// correct: deleting a row must not open the gates. A truly fresh
		// boot with no row fails closed with an error instead; that path
		// is covered at unit level, since the bypass sits before either
		// consult and never distinguishes them.
		refused, err := rig.svc.InitiateCall(ctx, ports.InitiateCallCommand{
			TenantID: rig.tenantID, Direction: domain.Outbound,
			SourceNumber: "1000", DestNumber: "1001",
			SourceEndpointURI: "PJSIP/1000", DestEndpointURI: "PJSIP/1001",
		})
		require.NoError(t, err, "screened-out is a verdict, not an error")
		c, err := telephonypostgres.NewCallStore(rig.pool).GetCall(ctx, rig.tenantID, refused)
		require.NoError(t, err)
		assert.Equal(t, domain.CallTerminated, c.State,
			"ordinary call must be refused with no licence or the emergency proof is vacuous")

		placeEmergency(t, "no licence")
	})
}
