package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/application"
)

// TestAlwaysPermitCompliance covers the one stub still standing.
//
// Its sibling, TestAlwaysPermitLicense, was removed with
// stub_license.go when licensing-capacity-grace wired the real adapter
// (LLD-08 DoD 7). stub_compliance.go stays until LLD-04 lands the real
// compliance context, so this test stays with it.
func TestAlwaysPermitCompliance(t *testing.T) {
	comp := application.NewAlwaysPermitCompliance()

	verdict, err := comp.Evaluate(context.Background(), shareddomain.NewTenantID(), "+15551234567")

	require.NoError(t, err)
	assert.True(t, verdict.Permitted)
}
