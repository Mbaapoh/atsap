package application_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/licensing/application"
	"atsap-api/internal/licensing/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestStatus_SetupIsAStatusNotAnError(t *testing.T) {
	r := newRig(t)
	st, err := r.svc.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.StateSetup, st.Entitlement.State)
	assert.Equal(t, domain.ReasonNotActivated, st.Entitlement.Reason)
	assert.Nil(t, st.ExpiresAt)
	assert.Nil(t, st.LastConfirmedAt)
}

func TestStatus_ValidReportsEntitlementUsageAndDates(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	tok := licensed(32)
	tok.ExpiresAt = now.Add(90 * 24 * time.Hour)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(tok)))
	v, err := r.svc.ValidateCapacity(ctx, shareddomain.NewTenantID(), "call-1", 1)
	require.NoError(t, err)
	require.True(t, v.Permitted)

	st, err := r.svc.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.StateValid, st.Entitlement.State)
	assert.Equal(t, 32, st.Entitlement.Channels)
	assert.Equal(t, 1, st.ChannelsInUse, "live usage, not the licensed figure")
	require.NotNil(t, st.ExpiresAt)
	assert.True(t, st.ExpiresAt.Equal(tok.ExpiresAt))
	require.NotNil(t, st.LastConfirmedAt)
}

func TestStatus_UnverifiedAndDegradedAsTimePasses(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(64))))

	for elapsed, want := range map[time.Duration]domain.State{
		3 * 24 * time.Hour: domain.StateUnverified,
		8 * 24 * time.Hour: domain.StateDegraded,
	} {
		at := now.Add(elapsed)
		svc := application.NewService(r.store, r.keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
			WithClock(func() time.Time { return at })
		st, err := svc.Status(ctx)
		require.NoError(t, err)
		assert.Equal(t, want, st.Entitlement.State, "after %s", elapsed)
		require.NotNil(t, st.LastConfirmedAt, "the date that explains the state is reported")
	}
}

// The status cannot carry licence material: rendered in full, it contains
// neither the token nor any part of its signature.
func TestStatus_CarriesNoLicenceMaterial(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	token := r.sign(licensed(16))
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, token))

	st, err := r.svc.Status(ctx)
	require.NoError(t, err)
	rendered := fmt.Sprintf("%+v", st)
	payload, sig, _ := strings.Cut(token, ".")
	assert.NotContains(t, rendered, payload)
	assert.NotContains(t, rendered, sig)
}
