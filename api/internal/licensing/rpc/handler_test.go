package rpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/licensing/domain"
	"atsap-api/internal/licensing/ports"
	"atsap-api/internal/licensing/rpc"
)

const secretToken = "eyJzZWNyZXQiOiJwYXlsb2FkIn0.c2lnbmF0dXJlLWJ5dGVz"

type fakeLicence struct {
	status   ports.LicenseStatus
	applyErr error
	applied  []string
}

func (f *fakeLicence) Status(context.Context) (ports.LicenseStatus, error) { return f.status, nil }
func (f *fakeLicence) ApplyLicenseKey(_ context.Context, token string) error {
	f.applied = append(f.applied, token)
	if f.applyErr != nil {
		return f.applyErr
	}
	f.status.Entitlement = domain.Entitlement{State: domain.StateValid, Edition: "operator", Channels: 64}
	return nil
}

type fakeAuthority struct{ authErr, systemErr error }

func (f fakeAuthority) RequireAuthenticated(context.Context) error { return f.authErr }
func (f fakeAuthority) RequireSystemScope(context.Context, string) error {
	if f.authErr != nil {
		return f.authErr
	}
	return f.systemErr
}

type fakeAuditor struct {
	edition  string
	channels int
	calls    int
}

func (f *fakeAuditor) RecordLicenceApplied(_ context.Context, edition string, channels int) error {
	f.calls++
	f.edition, f.channels = edition, channels
	return nil
}

func valid() ports.LicenseStatus {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return ports.LicenseStatus{
		Entitlement:   domain.Entitlement{State: domain.StateValid, Edition: "core", Channels: 16, MaxTenants: 0, MaxExtensions: 0},
		ChannelsInUse: 3, ExpiresAt: &exp, LastConfirmedAt: &exp,
	}
}

func TestGetLicenseStatus_AnyAuthenticatedCaller(t *testing.T) {
	h := rpc.NewHandler(&fakeLicence{status: valid()}, fakeAuthority{systemErr: rpc.ErrPermissionDenied}, &fakeAuditor{})
	res, err := h.GetLicenseStatus(context.Background(), connect.NewRequest(&atsapbxv1.GetLicenseStatusRequest{}))
	require.NoError(t, err, "no system scope is needed to read")
	st := res.Msg.GetStatus()
	assert.Equal(t, atsapbxv1.LicenseState_LICENSE_STATE_VALID, st.GetState())
	assert.Equal(t, int32(16), st.GetChannels())
	assert.Equal(t, int32(3), st.GetChannelsInUse())
	assert.Empty(t, st.GetReason(), "no reason while valid")
	assert.NotNil(t, st.GetExpiresAt())
}

func TestGetLicenseStatus_SetupIsNotActivated(t *testing.T) {
	h := rpc.NewHandler(&fakeLicence{status: ports.LicenseStatus{Entitlement: domain.SetupEntitlement()}}, fakeAuthority{}, &fakeAuditor{})
	res, err := h.GetLicenseStatus(context.Background(), connect.NewRequest(&atsapbxv1.GetLicenseStatusRequest{}))
	require.NoError(t, err)
	assert.Equal(t, atsapbxv1.LicenseState_LICENSE_STATE_NOT_ACTIVATED, res.Msg.GetStatus().GetState())
	assert.Equal(t, "not_activated", res.Msg.GetStatus().GetReason())
}

func TestGetLicenseStatus_Unauthenticated(t *testing.T) {
	h := rpc.NewHandler(&fakeLicence{status: valid()}, fakeAuthority{authErr: rpc.ErrUnauthenticated}, &fakeAuditor{})
	_, err := h.GetLicenseStatus(context.Background(), connect.NewRequest(&atsapbxv1.GetLicenseStatusRequest{}))
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestApplyLicenseKey_Outcomes(t *testing.T) {
	ctx := context.Background()
	apply := func(h *rpc.Handler) (*connect.Response[atsapbxv1.ApplyLicenseKeyResponse], error) {
		return h.ApplyLicenseKey(ctx, connect.NewRequest(&atsapbxv1.ApplyLicenseKeyRequest{Token: secretToken}))
	}

	t.Run("system scope applies, audits outcome only", func(t *testing.T) {
		lic, aud := &fakeLicence{status: valid()}, &fakeAuditor{}
		res, err := apply(rpc.NewHandler(lic, fakeAuthority{}, aud))
		require.NoError(t, err)
		assert.Equal(t, int32(64), res.Msg.GetStatus().GetChannels(), "the new licence is returned")
		assert.Equal(t, 1, aud.calls)
		assert.Equal(t, "operator", aud.edition)
		assert.Equal(t, 64, aud.channels)
		b, _ := protojson.Marshal(res.Msg)
		assert.NotContains(t, string(b), secretToken, "the token is never echoed")
	})

	t.Run("forged token is invalid argument, licence untouched", func(t *testing.T) {
		lic, aud := &fakeLicence{status: valid(), applyErr: domain.ErrUntrusted}, &fakeAuditor{}
		_, err := apply(rpc.NewHandler(lic, fakeAuthority{}, aud))
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.NotContains(t, err.Error(), secretToken)
		assert.Equal(t, 16, lic.status.Entitlement.Channels, "the licence in force is unchanged")
		assert.Zero(t, aud.calls, "a refusal is not audited as applied")
	})

	t.Run("tenant admin is permission denied, nothing applied", func(t *testing.T) {
		lic := &fakeLicence{status: valid()}
		_, err := apply(rpc.NewHandler(lic, fakeAuthority{systemErr: rpc.ErrPermissionDenied}, &fakeAuditor{}))
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		assert.Empty(t, lic.applied, "authorization precedes any attempt to apply")
	})

	t.Run("unauthenticated", func(t *testing.T) {
		_, err := apply(rpc.NewHandler(&fakeLicence{status: valid()}, fakeAuthority{authErr: rpc.ErrUnauthenticated}, &fakeAuditor{}))
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("an internal failure does not leak its text", func(t *testing.T) {
		lic := &fakeLicence{status: valid(), applyErr: errors.New("save licence: pq: relation licensing_state, token=" + secretToken)}
		_, err := apply(rpc.NewHandler(lic, fakeAuthority{}, &fakeAuditor{}))
		assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
		assert.NotContains(t, err.Error(), secretToken)
		assert.NotContains(t, err.Error(), "licensing_state")
	})
}

// licensing-status-conditions task 2.2: the server's condition, tier and
// capacity level reach the wire as-is, for every value, and the status
// still renders no licence material.
func TestGetLicenseStatus_ConditionsReachTheWire(t *testing.T) {
	for _, tc := range []struct {
		in   domain.StatusConditions
		cond atsapbxv1.LicenseCondition
		tier atsapbxv1.LicenseTier
		cap  atsapbxv1.CapacityLevel
	}{
		{domain.StatusConditions{Condition: domain.ConditionSetup},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_SETUP, atsapbxv1.LicenseTier_LICENSE_TIER_UNSPECIFIED, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_UNSPECIFIED},
		{domain.StatusConditions{Condition: domain.ConditionDegraded, Tier: domain.TierCommercial, Capacity: domain.CapacityReached},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_DEGRADED, atsapbxv1.LicenseTier_LICENSE_TIER_COMMERCIAL, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_REACHED},
		{domain.StatusConditions{Condition: domain.ConditionCapacityReached, Tier: domain.TierFreeCommunity, Capacity: domain.CapacityReached},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_CAPACITY_REACHED, atsapbxv1.LicenseTier_LICENSE_TIER_FREE_COMMUNITY, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_REACHED},
		{domain.StatusConditions{Condition: domain.ConditionUnverified, Tier: domain.TierCommercial, Capacity: domain.CapacityWarning},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_ENTITLEMENT_UNVERIFIED, atsapbxv1.LicenseTier_LICENSE_TIER_COMMERCIAL, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_WARNING},
		{domain.StatusConditions{Condition: domain.ConditionCapacityWarning, Tier: domain.TierCommercial, Capacity: domain.CapacityWarning},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_CAPACITY_WARNING, atsapbxv1.LicenseTier_LICENSE_TIER_COMMERCIAL, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_WARNING},
		{domain.StatusConditions{Condition: domain.ConditionFreeCommunity, Tier: domain.TierFreeCommunity, Capacity: domain.CapacityNormal},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_FREE_COMMUNITY, atsapbxv1.LicenseTier_LICENSE_TIER_FREE_COMMUNITY, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_NORMAL},
		{domain.StatusConditions{Condition: domain.ConditionValid, Tier: domain.TierCommercial, Capacity: domain.CapacityNormal},
			atsapbxv1.LicenseCondition_LICENSE_CONDITION_VALID, atsapbxv1.LicenseTier_LICENSE_TIER_COMMERCIAL, atsapbxv1.CapacityLevel_CAPACITY_LEVEL_NORMAL},
	} {
		st := valid()
		st.Conditions = tc.in
		h := rpc.NewHandler(&fakeLicence{status: st}, fakeAuthority{}, &fakeAuditor{})
		res, err := h.GetLicenseStatus(context.Background(), connect.NewRequest(&atsapbxv1.GetLicenseStatusRequest{}))
		require.NoError(t, err)
		got := res.Msg.GetStatus()
		assert.Equal(t, tc.cond, got.GetCondition(), "condition for %s", tc.in.Condition)
		assert.Equal(t, tc.tier, got.GetTier())
		assert.Equal(t, tc.cap, got.GetCapacity())
		rendered := protojson.Format(got)
		assert.NotContains(t, rendered, secretToken)
	}
}
