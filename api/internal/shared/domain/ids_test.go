package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/shared/domain"
)

func TestTenantID(t *testing.T) {
	id := domain.NewTenantID()
	assert.False(t, id.IsZero(), "NewTenantID() should not be zero")

	parsed, err := domain.ParseTenantID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)

	_, err = domain.ParseTenantID("not-a-uuid")
	assert.Error(t, err)

	var zero domain.TenantID
	assert.True(t, zero.IsZero())
}

func TestCallID(t *testing.T) {
	id := domain.NewCallID()
	assert.False(t, id.IsZero(), "NewCallID() should not be zero")

	parsed, err := domain.ParseCallID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)

	_, err = domain.ParseCallID("not-a-uuid")
	assert.Error(t, err)

	var zero domain.CallID
	assert.True(t, zero.IsZero())
}

func TestParticipantID(t *testing.T) {
	id := domain.NewParticipantID()
	assert.False(t, id.IsZero(), "NewParticipantID() should not be zero")

	parsed, err := domain.ParseParticipantID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)

	_, err = domain.ParseParticipantID("not-a-uuid")
	assert.Error(t, err)

	var zero domain.ParticipantID
	assert.True(t, zero.IsZero())
}
