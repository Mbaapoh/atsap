package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	shareddomain "atsap-api/internal/shared/domain"
)

// MaxPrefixPatternLength bounds a pattern's digits, excluding a leading
// "+".
const MaxPrefixPatternLength = 50

// Route validation errors.
var (
	ErrPrefixPatternInvalid = errors.New("prefix pattern must be an optional + followed by 1-50 digits or X")
	ErrCostInvalid          = errors.New("cost per minute must not be negative")
)

// CarrierRoute sends dialled numbers matching PrefixPattern to TrunkID.
//
// Overlapping patterns are permitted on purpose: overlap is how
// least-cost preference is expressed (HLD-18 §5).
type CarrierRoute struct {
	ID            shareddomain.RouteID
	TenantID      shareddomain.TenantID
	PrefixPattern string
	TrunkID       shareddomain.TrunkID
	CostPerMinute int64 // micros; stored, never computed (LLD-03 §9)
	Priority      int   // lower wins
	CreatedAt     time.Time
}

// Validate checks every caller-supplied field of a route.
func (r CarrierRoute) Validate() error {
	if err := ValidatePrefixPattern(r.PrefixPattern); err != nil {
		return err
	}
	if r.CostPerMinute < 0 {
		return fmt.Errorf("%w: %d", ErrCostInvalid, r.CostPerMinute)
	}
	if r.Priority < 0 {
		return fmt.Errorf("%w: %d", ErrPriorityInvalid, r.Priority)
	}
	return nil
}

// ValidatePrefixPattern accepts an optional leading "+" then 1-50
// characters of 0-9 or X, where X stands for any one digit (design D9).
//
// Deliberately not an expression language: FreePBX's X without its
// ranges, dots or bangs, so there is nothing in a pattern to abuse and
// nothing to evaluate (LLD-03 §10.5).
func ValidatePrefixPattern(pattern string) error {
	body := strings.TrimPrefix(pattern, "+")
	if body == "" || len(body) > MaxPrefixPatternLength {
		return fmt.Errorf("%w: %q", ErrPrefixPatternInvalid, pattern)
	}
	for _, r := range body {
		if (r < '0' || r > '9') && r != 'X' {
			return fmt.Errorf("%w: %q", ErrPrefixPatternInvalid, pattern)
		}
	}
	return nil
}

// MatchesPrefix reports whether dialled begins with pattern. A leading
// "+" is ignored on both sides, so "+44" and "44" are the same rule and
// match "+4420…" and "4420…" alike. X matches one digit, never * or #.
func MatchesPrefix(pattern, dialled string) bool {
	p := strings.TrimPrefix(pattern, "+")
	d := strings.TrimPrefix(dialled, "+")
	if p == "" || len(d) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		switch {
		case p[i] == 'X':
			if d[i] < '0' || d[i] > '9' {
				return false
			}
		case p[i] != d[i]:
			return false
		}
	}
	return true
}

// TrunkRoute is one selected candidate: the rule that matched and the
// trunk it sends to. It is what a call record needs to say which rule
// matched and why (AC-02.3, AC-02.6).
type TrunkRoute struct {
	Route CarrierRoute
	Trunk Trunk
}

// SelectRoutes returns every usable candidate for dialled, best first.
//
// A candidate is a route whose pattern matches and whose trunk exists
// and is not Unhealthy. The order is total, so the same input set always
// yields the same order whatever order it arrived in (LLD-03 DoD 14):
//
//  1. Healthy trunks before Degraded ones — degraded carriers are a
//     fallback, not a peer (design D8).
//  2. Longer pattern before shorter — the more specific rule wins
//     regardless of priority.
//  3. Lower priority, then lower stored cost.
//  4. Route id, so that two otherwise identical rules still order the
//     same way every time.
func SelectRoutes(dialled string, routes []CarrierRoute, trunks []Trunk) []TrunkRoute {
	byID := make(map[shareddomain.TrunkID]Trunk, len(trunks))
	for _, t := range trunks {
		byID[t.ID] = t
	}

	var out []TrunkRoute
	for _, r := range routes {
		t, ok := byID[r.TrunkID]
		if !ok || t.Health == HealthUnhealthy || !MatchesPrefix(r.PrefixPattern, dialled) {
			continue
		}
		out = append(out, TrunkRoute{Route: r, Trunk: t})
	}

	slices.SortFunc(out, compareCandidates)
	return out
}

func compareCandidates(a, b TrunkRoute) int {
	if da, db := a.Trunk.Health == HealthDegraded, b.Trunk.Health == HealthDegraded; da != db {
		if da {
			return 1
		}
		return -1
	}
	if la, lb := patternLength(a.Route.PrefixPattern), patternLength(b.Route.PrefixPattern); la != lb {
		return lb - la
	}
	if a.Route.Priority != b.Route.Priority {
		return a.Route.Priority - b.Route.Priority
	}
	if a.Route.CostPerMinute != b.Route.CostPerMinute {
		if a.Route.CostPerMinute < b.Route.CostPerMinute {
			return -1
		}
		return 1
	}
	return strings.Compare(a.Route.ID.String(), b.Route.ID.String())
}

func patternLength(p string) int { return len(strings.TrimPrefix(p, "+")) }

// DestinationKind says what a routing answer points at.
type DestinationKind string

const (
	// DestinationExtension is a call to a local extension.
	DestinationExtension DestinationKind = "EXTENSION"
	// DestinationTrunk is a call leaving through a carrier.
	DestinationTrunk DestinationKind = "TRUNK"
)

// Destination is what routing returns: a domain answer, never an engine
// endpoint name. The ACL turns it into one (LLD-03 §3).
type Destination struct {
	Kind        DestinationKind
	ExtensionID *shareddomain.ExtensionID
	TrunkID     *shareddomain.TrunkID
	DialledE164 string
}
