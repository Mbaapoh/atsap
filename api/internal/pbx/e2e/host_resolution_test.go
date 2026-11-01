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
	"context"
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

// --- a carrier configured by name (task 4.1) ----------------------------

// namedCarrier is a throwaway container on the engine's network, reachable
// by a unique name the app resolves through Docker's DNS: a carrier
// configured by host name, as most are. It sends raw SIP from its own
// address — what the trunk's identify row must match once resolved.
type namedCarrier struct {
	name, addr string
}

func startNamedCarrier(t *testing.T) namedCarrier {
	t.Helper()
	network := os.Getenv("ENGINE_NETWORK")
	if network == "" {
		network = "deploy_voip"
	}
	image := os.Getenv("CARRIER_IMAGE")
	if image == "" {
		image = "atsap-sipua:dev" // any image with bash; this one is already on the rig
	}
	name := fmt.Sprintf("carrier-e2e-%d", time.Now().UnixNano()%1_000_000_000)
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name, "--network", network, // #nosec G204 -- fixed test command
		"--network-alias", name, "--entrypoint", "sleep", image, "300").CombinedOutput()
	require.NoError(t, err, "start the named carrier: %s", out)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() }) // #nosec G204 -- fixed test command
	addr, err := exec.Command("docker", "inspect", name,                     // #nosec G204 -- fixed test command
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}").Output()
	require.NoError(t, err)
	return namedCarrier{name: name, addr: strings.TrimSpace(string(addr))}
}

// inviteScript sends one INVITE to the engine as a single datagram (bash
// would otherwise write it in pieces) and prints the response lines.
const inviteScript = `ip=$(hostname -i | awk '{print $1}'); r=$RANDOM$RANDOM
sdp="v=0\r\no=- 1 1 IN IP4 $ip\r\ns=-\r\nc=IN IP4 $ip\r\nt=0 0\r\nm=audio 18100 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"
len=$(printf "$sdp" | wc -c)
printf "INVITE sip:$1@asterisk:5060 SIP/2.0\r\nVia: SIP/2.0/UDP $ip:5060;branch=z9hG4bK$r;rport\r\nMax-Forwards: 70\r\nFrom: <sip:carrier@$ip>;tag=$r\r\nTo: <sip:$1@asterisk>\r\nCall-ID: $r@$ip\r\nCSeq: 1 INVITE\r\nContact: <sip:carrier@$ip:5060>\r\nContent-Type: application/sdp\r\nContent-Length: $len\r\n\r\n$sdp" > /tmp/invite
exec 3<>/dev/udp/asterisk/5060
cat /tmp/invite >&3
timeout 3 cat <&3 | grep -a '^SIP/2.0' | tr -d '\r'`

// dial sends an INVITE for number and returns the final status line.
func (c namedCarrier) dial(t *testing.T, number string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", c.name, "bash", "-c", inviteScript, "invite", number).CombinedOutput() // #nosec G204 -- fixed test command
	require.NoError(t, err, "%s", out)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(line) >= 11 && strings.HasPrefix(line, "SIP/2.0 ") && line[8] >= '2' {
			return line
		}
	}
	t.Fatalf("no final response to the INVITE: %q", out)
	return ""
}

// identifyMatch reads the engine row that recognises a trunk's carrier.
func (r *rig) identifyMatch(t *testing.T, trunkID string) string {
	t.Helper()
	var match string
	err := r.pool.Unwrap().QueryRow(context.Background(),
		`SELECT "match" FROM ps_endpoint_id_ips WHERE id = 't_' || replace($1, '-', '')`, trunkID).Scan(&match)
	if err != nil {
		return ""
	}
	return match
}

// A carrier configured by its name is recognised once the platform has
// resolved it — by its address, which is all the engine is given — and
// changing the trunk's host stops recognising it. Reconcile then finds
// nothing to report for the trunk.
func TestHostResolution_ACarrierConfiguredByNameIsRecognised(t *testing.T) {
	r := newRig(t)
	carrier := startNamedCarrier(t)

	assert.Equal(t, "SIP/2.0 401 Unauthorized", carrier.dial(t, "+442071234599"), "an unknown source is challenged, not routed")

	var out struct {
		Trunk trunkMsg `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "By name", "host": carrier.name, "port": 5060, "channelLimit": 2,
	}, &out, http.StatusOK)
	trunkID := out.Trunk.TrunkID

	require.Eventually(t, func() bool { return r.identifyMatch(t, trunkID) == carrier.addr },
		10*time.Second, 100*time.Millisecond, "the check that follows creation resolves the name to %s", carrier.addr)
	assert.Equal(t, "SIP/2.0 404 Not Found", carrier.dial(t, "+442071234599"),
		"the carrier is recognised as the trunk's, and its unassigned DID is refused by the tenant's plan")

	report, err := exec.Command("docker", "exec", appContainer(), appBinary(t), "pbx", "reconcile").CombinedOutput() // #nosec G204 -- fixed test command
	// Reconcile exits non-zero for anything it reports, including engine
	// rows other tooling left on the rig; what matters here is this trunk.
	require.Contains(t, string(report), "Checked", "reconcile ran: %v %s", err, report)
	assert.NotContains(t, string(report), trunkID, "reconcile agrees with what the check recorded")

	r.call(t, "PbxService/UpdateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "trunkId": trunkID, "name": "Moved", "host": "192.0.2.1", "port": 5060, "channelLimit": 2,
	}, nil, http.StatusOK)
	assert.Equal(t, "192.0.2.1", r.identifyMatch(t, trunkID), "a new IP host is recognised at once")
	assert.Equal(t, "SIP/2.0 401 Unauthorized", carrier.dial(t, "+442071234599"), "the old carrier is no longer recognised")
}

func appContainer() string {
	if c := os.Getenv("APP_CONTAINER"); c != "" {
		return c
	}
	return "deploy-app-1"
}

// appBinary is the app's executable in its container: /out/app under
// Tilt, /usr/local/bin/atsap-api in the compose image.
func appBinary(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", appContainer(), // #nosec G204 -- fixed test command
		"--format", "{{range .Config.Entrypoint}}{{.}} {{end}}{{range .Config.Cmd}}{{.}} {{end}}").Output()
	require.NoError(t, err)
	fields := strings.Fields(string(out))
	require.NotEmpty(t, fields, "the app container has no command")
	return fields[0]
}
