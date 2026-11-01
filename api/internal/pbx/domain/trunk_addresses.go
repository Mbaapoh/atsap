package domain

import (
	"net/netip"
	"slices"
	"strings"
)

// A trunk's carrier is recognised by address only (D-75): the engine's
// identify rows never hold a name, because the engine would resolve it
// while identifying every carrier request, for every tenant. The platform
// resolves a name itself, in the trunk check, and records what it found
// on the trunk; the rules for what it records live here.

// AddressesForHost is what a trunk is recognised by the moment its host
// is set: an IP host is its own address; a name has none until the
// platform has resolved it.
func AddressesForHost(host string) []netip.Addr {
	if a, ok := hostAddress(host); ok {
		return []netip.Addr{a}
	}
	return nil
}

// ObserveAddresses is the set a trunk is recognised by after a check
// resolved its host: an IP host is always its own address; a name that
// resolved is recognised by exactly what it resolved to, replacing the
// old set; a name that failed keeps its current set, so a brief outage
// of the carrier's DNS does not refuse its calls (its health reports the
// failure).
func ObserveAddresses(current []netip.Addr, host string, resolved []netip.Addr, ok bool) []netip.Addr {
	if fixed := AddressesForHost(host); fixed != nil {
		return fixed
	}
	if !ok || len(resolved) == 0 {
		return current
	}
	return NormaliseAddresses(resolved)
}

// NormaliseAddresses sorts and deduplicates addrs, unmapping IPv4-mapped
// IPv6 addresses, so equal sets compare equal.
func NormaliseAddresses(addrs []netip.Addr) []netip.Addr {
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if a.IsValid() {
			out = append(out, a.Unmap().WithZone(""))
		}
	}
	slices.SortFunc(out, func(a, b netip.Addr) int { return a.Compare(b) })
	return slices.Compact(out)
}

// SameAddresses reports whether a and b are the same set.
func SameAddresses(a, b []netip.Addr) bool {
	return slices.Equal(NormaliseAddresses(a), NormaliseAddresses(b))
}

// IdentifyMatch is the engine's identify match for addrs: the addresses
// as comma-separated literals. With no address there is nothing to
// match, and false says the trunk must have no identify row at all.
func IdentifyMatch(addrs []netip.Addr) (string, bool) {
	addrs = NormaliseAddresses(addrs)
	if len(addrs) == 0 {
		return "", false
	}
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ","), true
}

func hostAddress(host string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(host)
	if err != nil || a.Zone() != "" {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}
