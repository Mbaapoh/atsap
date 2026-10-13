package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityports "atsap-api/internal/identity/ports"
	licensingdomain "atsap-api/internal/licensing/domain"
	pbxports "atsap-api/internal/pbx/ports"
)

// TestCaps_NotLicensedBeforeActivation covers phase-a-hardening task 3.1
// (D-72): an installation with no licence applied provisions nothing —
// its zero caps must not read as "unlimited" — while a free, licensed or
// degraded installation passes its caps through unchanged.
func TestCaps_NotLicensedBeforeActivation(t *testing.T) {
	_, err := extensionCap(licensingdomain.SetupEntitlement())
	assert.True(t, errors.Is(err, pbxports.ErrNotLicensed), "Setup refuses extensions: %v", err)
	_, err = tenantCap(licensingdomain.SetupEntitlement())
	assert.True(t, errors.Is(err, identityports.ErrNotLicensed), "Setup refuses tenants: %v", err)

	free := licensingdomain.FreeEntitlement()
	licensed := licensingdomain.Entitlement{State: licensingdomain.StateValid, Edition: "licensed", Channels: 30}
	degraded := licensingdomain.DegradedEntitlement(free, licensingdomain.ReasonExpired)
	for _, tc := range []struct {
		name               string
		e                  licensingdomain.Entitlement
		extensions, tenant int
	}{
		{"free", free, licensingdomain.FreeMaxExtensions, licensingdomain.FreeMaxTenants},
		{"licensed, unlimited", licensed, licensingdomain.Unlimited, licensingdomain.Unlimited},
		{"degraded keeps its caps", degraded, licensingdomain.FreeMaxExtensions, licensingdomain.FreeMaxTenants},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := extensionCap(tc.e)
			require.NoError(t, err)
			assert.Equal(t, tc.extensions, n)
			n, err = tenantCap(tc.e)
			require.NoError(t, err)
			assert.Equal(t, tc.tenant, n)
		})
	}
}
