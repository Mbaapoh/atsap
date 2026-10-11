package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// DefaultEmergencyNumbers is the emergency set PlaceCall matches against
// until pbx-inbound-and-emergency lands the per-tenant, residency-seeded
// emergency_numbers table (LLD-03 §5.1, pbx-call-placement design D7).
//
// 112 is the EU-wide (and GSM) number, 911 North America's, 999 the UK's.
// A constant rather than configuration, so no deployment can empty it:
// INV-01 says an emergency call is never refused by a commercial control,
// and a set that could be configured away is a refusal waiting to happen.
var DefaultEmergencyNumbers = NewEmergencyNumberSet("112", "911", "999")

// MaxDestinationLength bounds a dialled destination's digits. E.164 caps
// a number at 15; the headroom covers international and carrier prefixes.
const MaxDestinationLength = 32

// Destination errors.
var (
	ErrDestinationInvalid = errors.New("destination must be an optional + followed by 1-32 digits")
	ErrUnroutable         = errors.New("destination is neither a tenant extension nor matched by a route with a usable trunk")
)

// ValidateDestination accepts an optional leading "+" then 1-32 digits.
//
// The dialled digits end up in the engine's dial string, so the alphabet
// is closed by construction: no "@", "/", "&" or other character with
// meaning there, and no "*" or "#", which are feature-code signalling,
// not destinations (LLD-03 §10.5).
func ValidateDestination(dialled string) error {
	body := dialled
	if len(body) > 0 && body[0] == '+' {
		body = body[1:]
	}
	if body == "" || len(body) > MaxDestinationLength {
		return fmt.Errorf("%w: %q", ErrDestinationInvalid, dialled)
	}
	for i := 0; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return fmt.Errorf("%w: %q", ErrDestinationInvalid, dialled)
		}
	}
	return nil
}

// Resolution is the dial plan's answer for one dialled destination.
type Resolution struct {
	// Destination is an extension (internal) or a trunk-bound number. For
	// a trunk-bound one, Candidates holds every usable trunk, best first,
	// and the caller picks the first with room.
	Destination Destination
	Candidates  []TrunkRoute
	// Emergency marks a destination in the emergency set: it is placed
	// with Screening bypassed (INV-01). Only this resolver sets it.
	Emergency bool
}

// ResolveDestination applies the dial plan in a fixed order (design D1):
//
//  1. Emergency. Matched first, on the digits as dialled. An emergency
//     destination never resolves to an extension — an extension numbered
//     112 must not answer a call to 112 — so it goes straight to routes.
//  2. Internal. An exact match on a tenant extension number connects
//     internally; a route never captures an internal number.
//  3. Routes, via SelectRoutes.
//
// Anything else is ErrUnroutable. lookupExtension answers "is this an
// extension number in the tenant", which keeps the function pure while
// the caller decides how the tenant's numbers are read.
func ResolveDestination(
	dialled string,
	emergency EmergencyNumberSet,
	lookupExtension func(number string) (shareddomain.ExtensionID, bool),
	routes []CarrierRoute,
	trunks []Trunk,
) (Resolution, error) {
	if err := ValidateDestination(dialled); err != nil {
		return Resolution{}, err
	}

	isEmergency := IsEmergency(dialled, emergency)
	if !isEmergency {
		if id, ok := lookupExtension(dialled); ok {
			return Resolution{Destination: Destination{
				Kind: DestinationExtension, ExtensionID: &id, DialledE164: dialled,
			}}, nil
		}
	}

	candidates := SelectRoutes(dialled, routes, trunks)
	if len(candidates) == 0 {
		return Resolution{Emergency: isEmergency}, fmt.Errorf("%w: %q", ErrUnroutable, dialled)
	}
	first := candidates[0].Trunk.ID
	return Resolution{
		Destination: Destination{Kind: DestinationTrunk, TrunkID: &first, DialledE164: dialled},
		Candidates:  candidates,
		Emergency:   isEmergency,
	}, nil
}

// PlacedCallLeg is pbx-core's record of a call it placed: who placed it,
// which rule matched and which trunk carried it. While ReleasedAt is nil
// it occupies a channel on TrunkID (design D4).
type PlacedCallLeg struct {
	ID          uuid.UUID
	CallID      *shareddomain.CallID // nil until origination returns
	TenantID    shareddomain.TenantID
	ExtensionID shareddomain.ExtensionID
	TrunkID     *shareddomain.TrunkID // nil for an internal call
	RouteID     *shareddomain.RouteID
	Destination string
	Emergency   bool
	CreatedAt   time.Time
	ReleasedAt  *time.Time
}
