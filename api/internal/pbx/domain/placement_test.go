package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestDefaultEmergencyNumbers_ArePinned(t *testing.T) {
	for _, n := range []string{"112", "911", "999"} {
		assert.True(t, domain.IsEmergency(n, domain.DefaultEmergencyNumbers), "%s must stay in the built-in set (INV-01)", n)
	}
	assert.Len(t, domain.DefaultEmergencyNumbers, 3, "a member added or removed must be a deliberate edit to this test")
}

func TestValidateDestination(t *testing.T) {
	for _, ok := range []string{"1", "+442071234567", "112", strings.Repeat("9", 32)} {
		assert.NoError(t, domain.ValidateDestination(ok), ok)
	}
	for _, bad := range []string{"", "+", "*112", "112#", "44 20", "abc", "4420@t_x", strings.Repeat("9", 33), "++44"} {
		assert.ErrorIs(t, domain.ValidateDestination(bad), domain.ErrDestinationInvalid, bad)
	}
}

func TestResolveDestination(t *testing.T) {
	tr := trunk(domain.HealthHealthy)
	ext := shareddomain.NewExtensionID()
	numbers := map[string]shareddomain.ExtensionID{"1005": ext, "112": shareddomain.NewExtensionID()}
	lookup := func(n string) (shareddomain.ExtensionID, bool) { id, ok := numbers[n]; return id, ok }
	all := route("1", tr, 0, 0)    // a route that would shadow internal 1005
	uk := route("+44", tr, 0, 0)   // ordinary outbound
	emerg := route("11", tr, 0, 0) // covers 112

	t.Run("internal wins over a shadowing route", func(t *testing.T) {
		res, err := domain.ResolveDestination("1005", domain.DefaultEmergencyNumbers, lookup,
			[]domain.CarrierRoute{all}, []domain.Trunk{tr})
		require.NoError(t, err)
		assert.Equal(t, domain.DestinationExtension, res.Destination.Kind)
		assert.Equal(t, ext, *res.Destination.ExtensionID)
		assert.False(t, res.Emergency)
		assert.Empty(t, res.Candidates, "an internal call uses no trunk")
	})

	t.Run("outbound over a route", func(t *testing.T) {
		res, err := domain.ResolveDestination("+442071234567", domain.DefaultEmergencyNumbers, lookup,
			[]domain.CarrierRoute{uk}, []domain.Trunk{tr})
		require.NoError(t, err)
		assert.Equal(t, domain.DestinationTrunk, res.Destination.Kind)
		require.Len(t, res.Candidates, 1)
		assert.Equal(t, uk.ID, res.Candidates[0].Route.ID)
	})

	t.Run("emergency never reaches an extension numbered like it", func(t *testing.T) {
		res, err := domain.ResolveDestination("112", domain.DefaultEmergencyNumbers, lookup,
			[]domain.CarrierRoute{emerg}, []domain.Trunk{tr})
		require.NoError(t, err)
		assert.True(t, res.Emergency)
		assert.Equal(t, domain.DestinationTrunk, res.Destination.Kind, "112 leaves the building even if extension 112 exists")
	})

	t.Run("emergency with no route is unroutable but still flagged", func(t *testing.T) {
		res, err := domain.ResolveDestination("911", domain.DefaultEmergencyNumbers, lookup, nil, []domain.Trunk{tr})
		require.ErrorIs(t, err, domain.ErrUnroutable)
		assert.True(t, res.Emergency, "the caller can still tell it was an emergency it could not route")
	})

	t.Run("near-emergency is ordinary", func(t *testing.T) {
		res, err := domain.ResolveDestination("1120", domain.DefaultEmergencyNumbers, lookup,
			[]domain.CarrierRoute{emerg}, []domain.Trunk{tr})
		require.NoError(t, err)
		assert.False(t, res.Emergency)
	})

	t.Run("unroutable", func(t *testing.T) {
		_, err := domain.ResolveDestination("3312345678", domain.DefaultEmergencyNumbers, lookup,
			[]domain.CarrierRoute{uk}, []domain.Trunk{tr})
		require.ErrorIs(t, err, domain.ErrUnroutable)
	})

	t.Run("invalid destination never reaches the dial plan", func(t *testing.T) {
		_, err := domain.ResolveDestination("44@x", domain.DefaultEmergencyNumbers, lookup, nil, nil)
		require.ErrorIs(t, err, domain.ErrDestinationInvalid)
	})
}
