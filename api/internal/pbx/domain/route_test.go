package domain_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestValidatePrefixPattern(t *testing.T) {
	for _, ok := range []string{"4", "44", "+44", "0044", "44X", "1XXX", strings.Repeat("9", 50)} {
		assert.NoError(t, domain.ValidatePrefixPattern(ok), ok)
	}
	// Anything with expression syntax is refused rather than interpreted
	// (LLD-03 §10.5).
	for _, bad := range []string{"", "+", "4[0-9]", "44.", "^44", "44*", "4|5", "(44)", "44 ", "x44", "N44", "++44",
		strings.Repeat("9", 51)} {
		assert.ErrorIs(t, domain.ValidatePrefixPattern(bad), domain.ErrPrefixPatternInvalid, bad)
	}
}

func TestMatchesPrefix(t *testing.T) {
	for _, tc := range []struct {
		pattern, dialled string
		want             bool
	}{
		{"44", "442071234567", true},
		{"+44", "442071234567", true},
		{"44", "+442071234567", true},
		{"447", "442071234567", false},
		{"44X", "442071234567", true},
		{"44X", "44*", false}, // X is a digit, never * or #
		{"4420", "442", false},
		{"1", "", false},
	} {
		assert.Equal(t, tc.want, domain.MatchesPrefix(tc.pattern, tc.dialled), "%s vs %s", tc.pattern, tc.dialled)
	}
}

func trunk(h domain.TrunkHealth) domain.Trunk {
	tr := validTrunk()
	tr.Health = h
	return tr
}

func route(pattern string, tr domain.Trunk, priority int, cost int64) domain.CarrierRoute {
	return domain.CarrierRoute{
		ID: shareddomain.NewRouteID(), TenantID: tr.TenantID, PrefixPattern: pattern,
		TrunkID: tr.ID, Priority: priority, CostPerMinute: cost,
	}
}

func TestSelectRoutes_LongestPrefixWinsRegardlessOfPriority(t *testing.T) {
	a, b := trunk(domain.HealthHealthy), trunk(domain.HealthHealthy)
	general := route("44", a, 0, 0)
	mobile := route("447", b, 9, 900)

	got := domain.SelectRoutes("447700900123", []domain.CarrierRoute{general, mobile}, []domain.Trunk{a, b})
	require.Len(t, got, 2)
	assert.Equal(t, mobile.ID, got[0].Route.ID)
	assert.Equal(t, b.ID, got[0].Trunk.ID, "the selection names the matched route AND its trunk")
	assert.Equal(t, general.ID, got[1].Route.ID, "the general rule remains as the fallback")
}

func TestSelectRoutes_TiesBreakOnPriorityThenCost(t *testing.T) {
	a, b, c := trunk(domain.HealthHealthy), trunk(domain.HealthHealthy), trunk(domain.HealthHealthy)
	expensive := route("44", a, 1, 500)
	cheap := route("44", b, 1, 100)
	preferred := route("44", c, 0, 900)

	got := domain.SelectRoutes("4420", []domain.CarrierRoute{expensive, cheap, preferred}, []domain.Trunk{a, b, c})
	require.Len(t, got, 3)
	assert.Equal(t, []shareddomain.RouteID{preferred.ID, cheap.ID, expensive.ID},
		[]shareddomain.RouteID{got[0].Route.ID, got[1].Route.ID, got[2].Route.ID})
}

func TestSelectRoutes_SkipsUnhealthyAndDemotesDegraded(t *testing.T) {
	healthy, degraded, unhealthy := trunk(domain.HealthHealthy), trunk(domain.HealthDegraded), trunk(domain.HealthUnhealthy)
	viaDegraded := route("447", degraded, 0, 0)
	viaHealthy := route("44", healthy, 5, 0)
	viaUnhealthy := route("4477", unhealthy, 0, 0)

	got := domain.SelectRoutes("447700", []domain.CarrierRoute{viaDegraded, viaHealthy, viaUnhealthy},
		[]domain.Trunk{healthy, degraded, unhealthy})
	require.Len(t, got, 2, "an unhealthy trunk is never a candidate")
	assert.Equal(t, viaHealthy.ID, got[0].Route.ID, "healthy before degraded, even against a longer prefix")
	assert.Equal(t, viaDegraded.ID, got[1].Route.ID, "degraded stays available as a fallback")
}

func TestSelectRoutes_IgnoresRoutesToUnknownTrunks(t *testing.T) {
	known := trunk(domain.HealthHealthy)
	orphan := route("44", trunk(domain.HealthHealthy), 0, 0)
	got := domain.SelectRoutes("4420", []domain.CarrierRoute{orphan}, []domain.Trunk{known})
	assert.Empty(t, got)
}

// --- property-based (LLD-03 DoD 14) ------------------------------------

// genCase builds a random configured set with deliberately heavy overlap:
// a tiny digit alphabet and short patterns guarantee shared prefixes,
// equal lengths, equal priorities and equal costs, which is exactly where
// a tie-break bug would hide.
func genCase(r *rand.Rand) (string, []domain.CarrierRoute, []domain.Trunk) {
	healths := []domain.TrunkHealth{domain.HealthHealthy, domain.HealthDegraded, domain.HealthUnhealthy}
	trunks := make([]domain.Trunk, 1+r.IntN(4))
	for i := range trunks {
		trunks[i] = trunk(healths[r.IntN(len(healths))])
	}
	digits := "12X"
	routes := make([]domain.CarrierRoute, r.IntN(12))
	for i := range routes {
		var b strings.Builder
		for range 1 + r.IntN(3) {
			b.WriteByte(digits[r.IntN(len(digits))])
		}
		routes[i] = route(b.String(), trunks[r.IntN(len(trunks))], r.IntN(2), int64(r.IntN(2)))
	}
	var dialled strings.Builder
	for range 3 + r.IntN(3) {
		dialled.WriteByte("12"[r.IntN(2)])
	}
	return dialled.String(), routes, trunks
}

func ids(sel []domain.TrunkRoute) []shareddomain.RouteID {
	out := make([]shareddomain.RouteID, len(sel))
	for i, s := range sel {
		out[i] = s.Route.ID
	}
	return out
}

func TestSelectRoutes_Properties(t *testing.T) {
	r := rand.New(rand.NewPCG(20261011, 3))
	for i := range 2000 {
		dialled, routes, trunks := genCase(r)
		got := domain.SelectRoutes(dialled, routes, trunks)
		label := fmt.Sprintf("case %d dialled=%s", i, dialled)

		// Same input in any order yields the same winner and the same order.
		shuffled := slices.Clone(routes)
		r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		shuffledTrunks := slices.Clone(trunks)
		r.Shuffle(len(shuffledTrunks), func(a, b int) { shuffledTrunks[a], shuffledTrunks[b] = shuffledTrunks[b], shuffledTrunks[a] })
		require.Equal(t, ids(got), ids(domain.SelectRoutes(dialled, shuffled, shuffledTrunks)), label)

		health := map[shareddomain.TrunkID]domain.TrunkHealth{}
		for _, tr := range trunks {
			health[tr.ID] = tr.Health
		}
		want := 0
		for _, rt := range routes {
			if domain.MatchesPrefix(rt.PrefixPattern, dialled) && health[rt.TrunkID] != domain.HealthUnhealthy {
				want++
			}
		}
		require.Len(t, got, want, "every usable match is a candidate, nothing else is: %s", label)

		for j, c := range got {
			require.True(t, domain.MatchesPrefix(c.Route.PrefixPattern, dialled), label)
			require.NotEqual(t, domain.HealthUnhealthy, c.Trunk.Health, label)
			require.Equal(t, c.Route.TrunkID, c.Trunk.ID, "the trunk named is the route's own: %s", label)
			if j == 0 {
				continue
			}
			prev := got[j-1]
			pd, cd := prev.Trunk.Health == domain.HealthDegraded, c.Trunk.Health == domain.HealthDegraded
			require.False(t, pd && !cd, "healthy never follows degraded: %s", label)
			if pd == cd {
				pl, cl := len(prev.Route.PrefixPattern), len(c.Route.PrefixPattern)
				require.GreaterOrEqual(t, pl, cl, "longer prefix first within a health class: %s", label)
				if pl == cl && prev.Route.Priority == c.Route.Priority {
					require.LessOrEqual(t, prev.Route.CostPerMinute, c.Route.CostPerMinute, label)
				} else if pl == cl {
					require.Less(t, prev.Route.Priority, c.Route.Priority, label)
				}
			}
		}
	}
}
