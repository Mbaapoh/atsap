package asterisk

import (
	"strings"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// pjsipTech is the channel technology every projected endpoint is
// dialled through.
const pjsipTech = "PJSIP/"

// Addresser implements ports.EndpointAddresser: the engine dial strings
// an origination needs, built here and nowhere else (design D2).
type Addresser struct{}

var _ ports.EndpointAddresser = Addresser{}

// ExtensionAddress dials a projected extension's endpoint.
func (Addresser) ExtensionAddress(id shareddomain.ExtensionID) string {
	return pjsipTech + domain.EndpointIdentifier(id)
}

// TrunkAddress dials number through a projected trunk:
// PJSIP/<number>@<endpoint>, the engine's form for "this user, via that
// endpoint". A leading "+" is dropped — carriers expect digits in the
// Request-URI user part. number is already domain-validated as digits
// with an optional "+", so nothing in it can alter the dial string's
// shape (LLD-03 §10.5).
func (Addresser) TrunkAddress(id shareddomain.TrunkID, number string) string {
	return pjsipTech + strings.TrimPrefix(number, "+") + "@" + domain.TrunkEndpointIdentifier(id)
}

// Identify attributes a projected endpoint name back to its domain
// identity: e_… to its extension, t_… to its trunk, anything else
// refused (design D4).
func (Addresser) Identify(endpoint string) (ports.EndpointIdentity, bool) {
	if id, ok := domain.ExtensionIDFromIdentifier(endpoint); ok {
		return ports.EndpointIdentity{ExtensionID: id}, true
	}
	if id, ok := domain.TrunkIDFromIdentifier(endpoint); ok {
		return ports.EndpointIdentity{TrunkID: id, IsTrunk: true}, true
	}
	return ports.EndpointIdentity{}, false
}
