package domain

import (
	"net/netip"
	"strings"

	shareddomain "atsap-api/internal/shared/domain"
)

// CarrierIngressEndpoint is the engine endpoint every carrier's calls
// arrive on (D-77). It carries no tenant: the platform attributes each
// call from its transport source address and the dialled number.
const CarrierIngressEndpoint = "carrier-ingress"

// IngressMatch is one trunk that recognises a carrier call's source
// address and whose tenant has the dialled number.
type IngressMatch struct {
	TenantID shareddomain.TenantID
	TrunkID  shareddomain.TrunkID
}

// ParseRemoteAddr reads the engine's CHANNEL(pjsip,remote_addr): an
// address with or without a port, IPv6 bracketed when it has one.
func ParseRemoteAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if ap, err := netip.ParseAddrPort(s); err == nil {
		if ap.Addr().Zone() != "" {
			return netip.Addr{}, false
		}
		return ap.Addr().Unmap(), true
	}
	if a, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil && a.Zone() == "" {
		return a.Unmap(), true
	}
	return netip.Addr{}, false
}

// AttributeIngress is the rule (D-77): exactly one match routes; none, or
// several, refuse — the platform never guesses between tenants.
func AttributeIngress(matches []IngressMatch) (IngressMatch, bool) {
	if len(matches) != 1 {
		return IngressMatch{}, false
	}
	return matches[0], true
}
