//go:build integration

package sipprobe

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
)

// TestProbe_RealPeers checks the probe against things it did not write:
// the rig's Asterisk (a real SIP stack, published on this host's 5060), a
// name that cannot exist (RFC 6761 .invalid, through the real resolver),
// and an address nothing answers from (TEST-NET-1, RFC 5737).
func TestProbe_RealPeers(t *testing.T) {
	p := &Prober{}
	assert.Equal(t, domain.ProbeAddressNotAllowed, p.Probe(context.Background(), "127.0.0.1", 5060).Outcome,
		"by default the platform never checks loopback (D-76)")

	// The rig publishes Asterisk on this host's loopback; only this test
	// widens Permit to reach it there.
	loop := &Prober{Permit: func(netip.Addr) bool { return true }}
	got := loop.Probe(context.Background(), "127.0.0.1", 5060)
	assert.Equal(t, domain.ProbeAnswered, got.Outcome, "Asterisk answers OPTIONS")
	assert.GreaterOrEqual(t, got.Status, 200)
	assert.Less(t, got.RTT, time.Second)
	t.Logf("Asterisk answered %d in %s", got.Status, got.RTT)

	assert.Equal(t, domain.ProbeHostNotFound, p.Probe(context.Background(), "no-such-carrier.invalid", 5060).Outcome)

	start := time.Now()
	assert.Equal(t, domain.ProbeNoResponse, p.Probe(context.Background(), "192.0.2.1", 5060).Outcome)
	assert.InDelta(t, DefaultTimeout.Seconds(), time.Since(start).Seconds(), 0.5, "silence costs the whole budget, no more")
}
