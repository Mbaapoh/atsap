package domain

import (
	"errors"
	"fmt"
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
		if !CarrierAddress(a) {
			return []netip.Addr{} // an IP host, but not a carrier's: nothing
		}
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
	resolved = CarrierAddresses(resolved)
	if !ok || len(resolved) == 0 {
		return CarrierAddresses(current)
	}
	return resolved
}

// CarrierAddresses is addrs without those that cannot be a carrier
// (D-76), normalised. Addresses recorded before D-76 — a "localhost"
// trunk's 127.0.0.1 — are dropped wherever they are read.
func CarrierAddresses(addrs []netip.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range NormaliseAddresses(addrs) {
		if CarrierAddress(a) {
			out = append(out, a)
		}
	}
	return out
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

// IdentifyMatch is the engine's identify match for a trunk: its host's
// carrier addresses and its inbound signalling ranges (D-76), as
// comma-separated literals and CIDR ranges, normalised. With nothing to
// match, false says the trunk must have no identify row at all.
func IdentifyMatch(addrs []netip.Addr, inbound []netip.Prefix) (string, bool) {
	all := make([]netip.Prefix, 0, len(addrs)+len(inbound))
	for _, a := range CarrierAddresses(addrs) {
		all = append(all, netip.PrefixFrom(a, a.BitLen()))
	}
	for _, p := range inbound {
		if p.IsValid() && CarrierAddress(p.Masked().Addr()) {
			all = append(all, p)
		}
	}
	all = normalisePrefixes(all)
	if len(all) == 0 {
		return "", false
	}
	parts := make([]string, len(all))
	for i, p := range all {
		parts[i] = prefixString(p)
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

// Inbound signalling address bounds (D-76).
const (
	MaxInboundAddresses = 20
	// MinInboundPrefixIPv4 and MinInboundPrefixIPv6 are the broadest
	// ranges accepted: a carrier's published ranges are /30 to /24; a
	// /0 would capture every unidentified request for one tenant.
	MinInboundPrefixIPv4 = 16
	MinInboundPrefixIPv6 = 32
)

// Inbound signalling address errors. Each names the entry's position, so a
// refusal says which line to fix.
var (
	ErrInboundAddressInvalid    = errors.New("inbound signalling address is not an IP address or CIDR range")
	ErrInboundAddressTooBroad   = errors.New("inbound signalling range is broader than /16 (IPv4) or /32 (IPv6)")
	ErrInboundAddressNotCarrier = errors.New("inbound signalling address cannot be a carrier: a loopback, link-local, unspecified or multicast address")
	ErrTooManyInboundAddresses  = errors.New("a trunk takes at most 20 inbound signalling addresses")
)

// thisNetwork is 0.0.0.0/8 (RFC 1122 §3.2.1.3): never a source.
var thisNetwork = netip.MustParsePrefix("0.0.0.0/8")

// CarrierAddress reports whether a can be a carrier's address (D-76):
// not loopback, link-local, unspecified, "this network" or multicast.
// Private and CGNAT ranges can — partners run carriers and SBCs on their
// own networks.
func CarrierAddress(a netip.Addr) bool {
	a = a.Unmap()
	switch {
	case !a.IsValid(), a.Zone() != "",
		a.IsLoopback(), a.IsUnspecified(),
		a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast(),
		a.IsInterfaceLocalMulticast(), a.IsMulticast():
		return false
	}
	return !thisNetwork.Contains(a)
}

// ParseInboundAddresses turns the lines a caller entered into normalised
// ranges: an address becomes a /32 or /128, a range is masked, blank
// entries are dropped and duplicates removed. It checks the syntax only;
// Trunk.Validate checks the rest.
func ParseInboundAddresses(entries []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for i, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		p, err := parseInbound(entry)
		if err != nil {
			return nil, fmt.Errorf("%w: entry %d %q", ErrInboundAddressInvalid, i+1, entry)
		}
		out = append(out, p)
	}
	return normalisePrefixes(out), nil
}

func parseInbound(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(entry)
	if err != nil || a.Zone() != "" {
		return netip.Prefix{}, errors.New("not an address")
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// ValidateInboundAddresses checks a trunk's inbound signalling ranges: at
// most MaxInboundAddresses, none broader than the floors, none that
// cannot be a carrier, and no IPv4-mapped IPv6 range (write it as IPv4).
func ValidateInboundAddresses(ranges []netip.Prefix) error {
	if len(ranges) > MaxInboundAddresses {
		return fmt.Errorf("%w: %d", ErrTooManyInboundAddresses, len(ranges))
	}
	for i, p := range ranges {
		switch {
		case !p.IsValid():
			return fmt.Errorf("%w: entry %d", ErrInboundAddressInvalid, i+1)
		case p.Addr().Is4In6():
			return fmt.Errorf("%w: entry %d %s", ErrInboundAddressInvalid, i+1, p)
		case p.Addr().Is4() && p.Bits() < MinInboundPrefixIPv4,
			p.Addr().Is6() && p.Bits() < MinInboundPrefixIPv6:
			return fmt.Errorf("%w: entry %d %s", ErrInboundAddressTooBroad, i+1, p)
		case !CarrierAddress(p.Masked().Addr()), p.Addr().Is4() && p.Overlaps(thisNetwork):
			return fmt.Errorf("%w: entry %d %s", ErrInboundAddressNotCarrier, i+1, p)
		}
	}
	return nil
}

// normalisePrefixes masks, sorts and deduplicates, so equal
// configurations compare and project identically.
func normalisePrefixes(ps []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ps))
	for _, p := range ps {
		if p.IsValid() {
			out = append(out, p.Masked())
		}
	}
	slices.SortFunc(out, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	return slices.Compact(out)
}

// NormaliseInboundAddresses is normalisePrefixes for callers outside the
// domain (the store), so stored and projected forms agree.
func NormaliseInboundAddresses(ps []netip.Prefix) []netip.Prefix {
	if len(ps) == 0 {
		return nil
	}
	return normalisePrefixes(ps)
}

// InboundStrings is ranges as a caller sees them: an address alone for a
// single host, CIDR otherwise.
func InboundStrings(ranges []netip.Prefix) []string {
	out := make([]string, 0, len(ranges))
	for _, p := range normalisePrefixes(ranges) {
		out = append(out, prefixString(p))
	}
	return out
}

func prefixString(p netip.Prefix) string {
	if p.IsSingleIP() {
		return p.Addr().String()
	}
	return p.String()
}
