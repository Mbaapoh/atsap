package domain_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
)

func addrs(ss ...string) []netip.Addr {
	out := make([]netip.Addr, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

func TestAddressesForHost(t *testing.T) {
	assert.Equal(t, addrs("203.0.113.10"), domain.AddressesForHost("203.0.113.10"), "an IPv4 host is its own address")
	assert.Equal(t, addrs("2001:db8::10"), domain.AddressesForHost("2001:db8::10"), "an IPv6 host is its own address")
	assert.Equal(t, addrs("203.0.113.10"), domain.AddressesForHost("::ffff:203.0.113.10"), "an IPv4-mapped host is unmapped")
	assert.Nil(t, domain.AddressesForHost("sip.carrier.example"), "a name has no address until resolved")
	assert.Nil(t, domain.AddressesForHost("locslhodt"), "a typo is a name like any other")
	assert.Nil(t, domain.AddressesForHost("fe80::1%eth0"), "a zoned address is not a carrier address")
}

func TestObserveAddresses(t *testing.T) {
	current := addrs("198.51.100.1")

	t.Run("an IP host is always its own address", func(t *testing.T) {
		got := domain.ObserveAddresses(current, "203.0.113.10", nil, false)
		assert.Equal(t, addrs("203.0.113.10"), got)
		got = domain.ObserveAddresses(current, "2001:db8::10", addrs("198.51.100.9"), true)
		assert.Equal(t, addrs("2001:db8::10"), got, "a lookup never overrides a literal")
	})

	t.Run("a resolved name replaces the set, sorted and deduplicated", func(t *testing.T) {
		got := domain.ObserveAddresses(current, "sip.carrier.example",
			addrs("2001:db8::2", "203.0.113.20", "203.0.113.10", "203.0.113.20", "::ffff:203.0.113.10"), true)
		assert.Equal(t, addrs("203.0.113.10", "203.0.113.20", "2001:db8::2"), got)
	})

	t.Run("a failed name keeps its current set", func(t *testing.T) {
		assert.Equal(t, current, domain.ObserveAddresses(current, "sip.carrier.example", nil, false))
		assert.Nil(t, domain.ObserveAddresses(nil, "locslhodt", nil, false), "a name never resolved stays unrecognised")
	})

	t.Run("a lookup that succeeded with no address is a failure", func(t *testing.T) {
		assert.Equal(t, current, domain.ObserveAddresses(current, "sip.carrier.example", nil, true))
	})
}

func TestSameAddresses(t *testing.T) {
	assert.True(t, domain.SameAddresses(addrs("203.0.113.20", "203.0.113.10"), addrs("203.0.113.10", "203.0.113.20", "203.0.113.10")))
	assert.True(t, domain.SameAddresses(nil, []netip.Addr{}))
	assert.False(t, domain.SameAddresses(addrs("203.0.113.10"), addrs("203.0.113.11")))
	assert.False(t, domain.SameAddresses(nil, addrs("203.0.113.10")))
}

func TestIdentifyMatch(t *testing.T) {
	match, ok := domain.IdentifyMatch(addrs("2001:db8::2", "203.0.113.10"), nil)
	assert.True(t, ok)
	assert.Equal(t, "203.0.113.10,2001:db8::2", match, "literals only, IPv4 first, comma-separated")

	match, ok = domain.IdentifyMatch([]netip.Addr{{}, netip.MustParseAddr("203.0.113.10")}, nil)
	assert.True(t, ok)
	assert.Equal(t, "203.0.113.10", match, "an invalid address is never projected")

	_, ok = domain.IdentifyMatch(nil, nil)
	assert.False(t, ok, "no address means no identify row at all")
}
