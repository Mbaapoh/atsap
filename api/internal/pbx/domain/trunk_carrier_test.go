package domain_test

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
)

func prefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// D-76: what can be a carrier. Private networks can; the platform's own
// loopback, link-local, unspecified, "this network" and multicast cannot.
func TestCarrierAddress(t *testing.T) {
	for _, s := range []string{"203.0.113.10", "10.20.0.5", "192.168.10.1", "172.18.0.5", "100.64.0.1", "2001:db8::10", "fd00::1"} {
		assert.True(t, domain.CarrierAddress(netip.MustParseAddr(s)), s)
	}
	for _, s := range []string{"127.0.0.1", "127.8.9.10", "::1", "169.254.1.1", "fe80::1", "0.0.0.0", "0.1.2.3", "::", "224.0.0.1", "239.1.1.1", "ff02::1", "::ffff:127.0.0.1", "fe80::1%eth0"} {
		assert.False(t, domain.CarrierAddress(netip.MustParseAddr(s)), s)
	}
	assert.False(t, domain.CarrierAddress(netip.Addr{}), "the zero address")
}

func TestValidateHost_RefusesWhatCannotBeACarrier(t *testing.T) {
	assert.ErrorIs(t, domain.ValidateHost("localhost."), domain.ErrTrunkHostInvalid, "a trailing dot is refused as syntax first")
	for _, h := range []string{"localhost", "LOCALHOST", "pbx.localhost", "127.0.0.1", "::1", "169.254.1.1", "0.0.0.0", "224.0.0.1", "::ffff:127.0.0.1"} {
		err := domain.ValidateHost(h)
		require.Error(t, err, h)
		assert.ErrorIs(t, err, domain.ErrTrunkHostNotCarrier, h)
	}
	for _, h := range []string{"10.20.0.5", "sip.carrier.example", "localhostname.example", "mylocalhost", "2001:db8::10"} {
		assert.NoError(t, domain.ValidateHost(h), h)
	}
}

func TestParseInboundAddresses(t *testing.T) {
	got, err := domain.ParseInboundAddresses([]string{" 203.0.113.9 ", "", "203.0.113.0/28", "203.0.113.5/28", "2001:db8::/48", "::ffff:198.51.100.1", "203.0.113.9"})
	require.NoError(t, err)
	assert.Equal(t, prefixes("198.51.100.1/32", "203.0.113.0/28", "203.0.113.9/32", "2001:db8::/48"), got,
		"addresses become /32, ranges are masked, blanks dropped, duplicates removed, mapped addresses unmapped")
	assert.Equal(t, []string{"198.51.100.1", "203.0.113.0/28", "203.0.113.9", "2001:db8::/48"}, domain.InboundStrings(got),
		"a single address is shown without its /32")

	for i, bad := range []string{"sip.carrier.example", "203.0.113.0/33", "203.0.113", "fe80::1%eth0", "1.2.3.4/abc"} {
		_, err := domain.ParseInboundAddresses([]string{"203.0.113.1", bad})
		assert.ErrorIs(t, err, domain.ErrInboundAddressInvalid, "%d %s", i, bad)
		assert.ErrorContains(t, err, "entry 2", "the refusal says which entry")
	}

	none, err := domain.ParseInboundAddresses(nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestValidateInboundAddresses(t *testing.T) {
	assert.NoError(t, domain.ValidateInboundAddresses(prefixes("192.168.10.0/24", "10.0.0.0/16", "203.0.113.9/32", "2001:db8::/32")),
		"private networks and the floors themselves are accepted")
	assert.NoError(t, domain.ValidateInboundAddresses(nil))

	cases := map[string]error{
		"10.0.0.0/15":    domain.ErrInboundAddressTooBroad,
		"0.0.0.0/0":      domain.ErrInboundAddressTooBroad,
		"2001:db8::/31":  domain.ErrInboundAddressTooBroad,
		"127.0.0.0/16":   domain.ErrInboundAddressNotCarrier,
		"127.0.0.1/32":   domain.ErrInboundAddressNotCarrier,
		"169.254.0.0/16": domain.ErrInboundAddressNotCarrier,
		"224.0.0.0/24":   domain.ErrInboundAddressNotCarrier,
		"0.1.0.0/16":     domain.ErrInboundAddressNotCarrier,
		"::1/128":        domain.ErrInboundAddressNotCarrier,
		"::/32":          domain.ErrInboundAddressNotCarrier,
		"fe80::/32":      domain.ErrInboundAddressNotCarrier,
		"ff02::/32":      domain.ErrInboundAddressNotCarrier,
		"::ffff:0:0/112": domain.ErrInboundAddressInvalid,
	}
	for s, want := range cases {
		err := domain.ValidateInboundAddresses(prefixes("203.0.113.0/28", s))
		assert.ErrorIs(t, err, want, s)
		if err != nil {
			assert.ErrorContains(t, err, "entry 2", s)
		}
	}
	assert.ErrorIs(t, domain.ValidateInboundAddresses([]netip.Prefix{{}}), domain.ErrInboundAddressInvalid)

	twentyOne := make([]netip.Prefix, 21)
	for i := range twentyOne {
		twentyOne[i] = netip.MustParsePrefix(fmt.Sprintf("203.0.113.%d/32", i))
	}
	assert.NoError(t, domain.ValidateInboundAddresses(twentyOne[:20]), "twenty is the limit")
	assert.ErrorIs(t, domain.ValidateInboundAddresses(twentyOne), domain.ErrTooManyInboundAddresses)
}

func TestTrunkValidate_ChecksTheHostAndInboundAddresses(t *testing.T) {
	tr := validTrunk()
	tr.InboundAddresses = prefixes("203.0.113.0/28")
	require.NoError(t, tr.Validate())
	tr.Host = "localhost"
	assert.ErrorIs(t, tr.Validate(), domain.ErrTrunkHostNotCarrier)
	tr.Host = "sip.carrier.example"
	tr.InboundAddresses = prefixes("127.0.0.1/32")
	assert.ErrorIs(t, tr.Validate(), domain.ErrInboundAddressNotCarrier)
}

// The identify match is the union of the host's carrier addresses and
// the inbound ranges, normalised; non-carrier addresses recorded before
// D-76 are never projected.
func TestIdentifyMatch_IsTheUnion(t *testing.T) {
	match, ok := domain.IdentifyMatch(addrs("203.0.113.10", "127.0.0.1", "::1"), prefixes("2001:db8::/48", "198.51.100.0/24", "203.0.113.10/32"))
	require.True(t, ok)
	assert.Equal(t, "198.51.100.0/24,203.0.113.10,2001:db8::/48", match)

	match, ok = domain.IdentifyMatch(nil, prefixes("198.51.100.0/24"))
	require.True(t, ok)
	assert.Equal(t, "198.51.100.0/24", match, "a name not resolved yet is still recognised from its ranges")

	_, ok = domain.IdentifyMatch(addrs("127.0.0.1", "::1"), nil)
	assert.False(t, ok, "a localhost trunk recorded before D-76 recognises nothing")

	_, ok = domain.IdentifyMatch(nil, []netip.Prefix{{}, netip.MustParsePrefix("127.0.0.0/8")})
	assert.False(t, ok, "an invalid or non-carrier range is never projected")
}

func TestAddressesForHost_ANonCarrierIPIsNothing(t *testing.T) {
	got := domain.AddressesForHost("127.0.0.1")
	assert.NotNil(t, got, "still an IP host: never looked up")
	assert.Empty(t, got)
}

func TestObserveAddresses_DropsWhatCannotBeACarrier(t *testing.T) {
	current := addrs("127.0.0.1", "::1")
	assert.Empty(t, domain.ObserveAddresses(current, "localhost.example", nil, false), "recorded loopback addresses are dropped on a failure")
	assert.Empty(t, domain.ObserveAddresses(nil, "loop.example", addrs("127.0.0.1"), true), "a lookup giving only loopback records nothing")
	assert.Equal(t, addrs("203.0.113.10"), domain.ObserveAddresses(nil, "mixed.example", addrs("127.0.0.1", "203.0.113.10"), true))
	assert.Equal(t, addrs("203.0.113.10"), domain.ObserveAddresses(addrs("203.0.113.10"), "loop.example", addrs("127.0.0.1"), true),
		"a lookup giving only loopback keeps the last carrier addresses, as a failure does")
}

func TestObserveProbe_AddressNotAllowed(t *testing.T) {
	h, r := domain.ObserveProbe(domain.ProbeAddressNotAllowed, 0, 0)
	assert.Equal(t, domain.HealthUnhealthy, h)
	assert.Equal(t, domain.ReasonAddressNotAllowed, r)
	assert.True(t, r.Valid())
}

func TestNormaliseInboundAddresses(t *testing.T) {
	assert.Nil(t, domain.NormaliseInboundAddresses(nil))
	assert.Equal(t, prefixes("198.51.100.0/24", "203.0.113.0/28"),
		domain.NormaliseInboundAddresses(prefixes("203.0.113.5/28", "198.51.100.7/24", "203.0.113.0/28")))
}

func TestValidateHostSyntax_SaysNothingAboutPurpose(t *testing.T) {
	for _, h := range []string{"localhost", "127.0.0.1", "::1", "pbx.example.com"} {
		assert.NoError(t, domain.ValidateHostSyntax(h), h)
	}
	for _, h := range []string{"", "sip:pbx.example.com", "pbx.example.com:5060", "user@pbx", "a..b", "-a.example"} {
		assert.ErrorIs(t, domain.ValidateHostSyntax(h), domain.ErrTrunkHostInvalid, h)
	}
}
