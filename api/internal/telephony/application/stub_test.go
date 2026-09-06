package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/application"
)

func TestAlwaysPermitLicense(t *testing.T) {
	lic := application.NewAlwaysPermitLicense()
	verdict, err := lic.ValidateCapacity(context.Background(), shareddomain.NewTenantID(), 1)
	require.NoError(t, err)
	assert.True(t, verdict.Permitted)
}

func TestAlwaysPermitCompliance(t *testing.T) {
	comp := application.NewAlwaysPermitCompliance()
	verdict, err := comp.Evaluate(context.Background(), shareddomain.NewTenantID(), "+15551234567")
	require.NoError(t, err)
	assert.True(t, verdict.Permitted)
}
