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

func TestPrincipalID(t *testing.T) {
	id := domain.NewPrincipalID()
	assert.False(t, id.IsZero(), "NewPrincipalID() should not be zero")

	parsed, err := domain.ParsePrincipalID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)

	_, err = domain.ParsePrincipalID("not-a-uuid")
	assert.Error(t, err)

	var zero domain.PrincipalID
	assert.True(t, zero.IsZero())
}

func TestApiKeyID(t *testing.T) {
	id := domain.NewApiKeyID()
	assert.False(t, id.IsZero(), "NewApiKeyID() should not be zero")

	parsed, err := domain.ParseApiKeyID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)

	_, err = domain.ParseApiKeyID("not-a-uuid")
	assert.Error(t, err)

	var zero domain.ApiKeyID
	assert.True(t, zero.IsZero())
}

// TestIDTypesAreDistinct guards the reason these are separate named types
// rather than a shared alias: a PrincipalID must not be assignable to a
// TenantID parameter, so a mixed-up argument is a compile error rather
// than a cross-tenant bug found at runtime.
func TestIDTypesAreDistinct(t *testing.T) {
	principal := domain.NewPrincipalID()
	apiKey := domain.NewApiKeyID()

	assert.NotEqual(t, principal.String(), apiKey.String())
	assert.NotEqual(t, domain.PrincipalID{}, principal)
}

func TestExtensionID(t *testing.T) {
	id := domain.NewExtensionID()
	assert.False(t, id.IsZero(), "NewExtensionID() should not be zero")

	parsed, err := domain.ParseExtensionID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)

	_, err = domain.ParseExtensionID("not-a-uuid")
	assert.Error(t, err)

	var zero domain.ExtensionID
	assert.True(t, zero.IsZero())
}
