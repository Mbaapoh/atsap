//go:build e2e

// pbx-trunk-host-resolution, on the running app and live Asterisk: one
// tenant's trunk host that does not resolve must cost no other tenant
// anything. On 2026-10-13 the rig's "locslhodt" trunk made Asterisk spend
// about 5 s on DNS while identifying every SIP request; whether a lookup
// is slow depends on the resolver of the day, so the deterministic proof
// is that Asterisk performs no lookup at all while identifying. Asterisk
// logs every failed lookup (netsock2.c, ast_sockaddr_resolve), so the
// lookups a request caused can be counted in its log.
package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unresolvable is the typo that found this: a single label that no
// resolver knows.
const unresolvable = "locslhodt"

// identifyBudget is how long a request may take to be identified and
// answered on an idle rig; a DNS stall is seconds.
const identifyBudget = time.Second

// engineLookups counts the engine's failed name lookups of host since.
func engineLookups(t *testing.T, host string, since time.Time) int {
	t.Helper()
	container := os.Getenv("ASTERISK_CONTAINER")
	if container == "" {
		container = "deploy-asterisk-1"
	}
	out, err := exec.Command("docker", "logs", "--since", since.UTC().Format(time.RFC3339Nano), container).CombinedOutput() // #nosec G204 -- fixed test command
	require.NoError(t, err, "the engine container must be up: %s", out)
	return strings.Count(string(out), fmt.Sprintf("getaddrinfo(%q", host))
}

// createBadTrunk gives r a trunk whose host does not resolve: accepted,
// as the owner decided (2026-11-01), and flagged by its health.
func createBadTrunk(t *testing.T, r *rig) string {
	t.Helper()
	var out struct {
		Trunk trunkMsg `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "A typo", "host": unresolvable, "port": 5060, "channelLimit": 2,
	}, &out, http.StatusOK)
	return out.Trunk.TrunkID
}

// Another tenant's carrier is identified without the engine looking up
// the typo, and promptly. Carrier requests are what reach identification
// by address: phones are identified by username first (D5 of
// pbx-inbound-and-emergency), which is why only carrier calls stalled.
// The DID is unassigned, so each INVITE is identified and refused 404
// with no dialog to clean up.
func TestHostResolution_AnUnresolvableHostIsNotLookedUpWhileIdentifying(t *testing.T) {
	careless := newRig(t)
	createBadTrunk(t, careless)

	other := newInboundRig(t, 30)

	since := time.Now().Add(-time.Second)
	for i := 0; i < 3; i++ {
		start := time.Now()
		code, _ := other.carrier.dial(t, "+442071234599", "carrier", "")
		took := time.Since(start)
		require.Equal(t, http.StatusNotFound, code, "the other tenant's carrier is identified and answered")
		t.Logf("carrier INVITE %d took %v", i+1, took)
		assert.Less(t, took, identifyBudget, "another tenant's typo must not slow this tenant's carrier")
	}
	assert.Zero(t, engineLookups(t, unresolvable, since),
		"the engine must not resolve trunk host names while identifying requests")
}

// Placing a call over an unresolvable trunk fails that call alone: while
// it is attempted, the engine answers other carriers' requests promptly. (Task 1.1 measures this; the outbound contact stays a name
// by design, see design.md Non-Goals.)
func TestHostResolution_DiallingAnUnresolvableTrunkDelaysNoOneElse(t *testing.T) {
	p := newInboundRig(t, 30)
	bad := createBadTrunk(t, p.rig)
	p.call(t, "PbxService/CreateRoute", p.token, map[string]any{
		"tenantId": p.tenant.String(), "prefixPattern": "+1", "trunkId": bad,
	}, nil, http.StatusOK)

	// The rig's SIP ports host one inbound rig at a time, so the carrier
	// that must stay prompt is this tenant's own: an engine-wide stall
	// would delay it exactly as it would delay another tenant's.
	other := p

	var wg sync.WaitGroup
	var dialled int
	var dialTook time.Duration
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		dialled, _ = p.phone.dial(t, "+15550100", p.ext.AuthUsername, p.phoneSec)
		dialTook = time.Since(start)
	}()

	time.Sleep(20 * time.Millisecond) // the dial is under way
	for i := 0; i < 3; i++ {
		start := time.Now()
		code, _ := other.carrier.dial(t, "+442071234599", "carrier", "")
		took := time.Since(start)
		require.Equal(t, http.StatusNotFound, code, "the other tenant's carrier is identified and answered")
		t.Logf("carrier INVITE %d during the dial took %v", i+1, took)
		assert.Less(t, took, identifyBudget, "a call to another tenant's unresolvable trunk must not slow this tenant's carrier")
	}
	wg.Wait()
	t.Logf("the call to the unresolvable trunk ended with SIP %d after %v", dialled, dialTook)
	assert.NotEqual(t, http.StatusOK, dialled, "nothing answers at an unresolvable host")
}
