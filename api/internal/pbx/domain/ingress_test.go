package domain_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestParseRemoteAddr(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"172.18.0.8:60217", "172.18.0.8"},
		{"203.0.113.9", "203.0.113.9"},
		{"[2001:db8::10]:5060", "2001:db8::10"},
		{"2001:db8::10", "2001:db8::10"},
		{" [::ffff:198.51.100.7]:5060 ", "198.51.100.7"}, // surrounding space trimmed, mapped address unmapped
	} {
		got, ok := domain.ParseRemoteAddr(c.in)
		assert.True(t, ok, c.in)
		assert.Equal(t, netip.MustParseAddr(c.want), got, c.in)
	}
	for _, in := range []string{"", "carrier.example:5060", "[fe80::1%eth0]:5060", "not an address"} {
		_, ok := domain.ParseRemoteAddr(in)
		assert.False(t, ok, in)
	}
}

// D-77: exactly one routes; none or several refuse.
func TestAttributeIngress(t *testing.T) {
	a := domain.IngressMatch{TenantID: shareddomain.NewTenantID(), TrunkID: shareddomain.NewTrunkID()}
	b := domain.IngressMatch{TenantID: shareddomain.NewTenantID(), TrunkID: shareddomain.NewTrunkID()}
	got, ok := domain.AttributeIngress([]domain.IngressMatch{a})
	assert.True(t, ok)
	assert.Equal(t, a, got)
	_, ok = domain.AttributeIngress(nil)
	assert.False(t, ok, "no trunk recognises it")
	_, ok = domain.AttributeIngress([]domain.IngressMatch{a, b})
	assert.False(t, ok, "two tenants have the number: refused, never guessed")
}
