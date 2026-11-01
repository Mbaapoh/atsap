//go:build e2e

// pbx-carrier-ingress task 1.1 (D-25 spike): before the ingress is built,
// prove on the live engine that (a) one realtime endpoint whose identify
// row covers a carrier's address takes that carrier's INVITE, and (c) an
// identify row of 500 CIDR entries still answers carrier requests
// promptly. The rows are written and removed by the test itself.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const spikeIngress = "carrier-ingress-spike"

func (r *rig) projectSpikeIngress(t *testing.T, match string) {
	t.Helper()
	r.projectSpikeIngressIn(t, match, "stasis-in")
}

// projectSpikeIngressIn writes the spike ingress with the given dialplan
// context.
func (r *rig) projectSpikeIngressIn(t *testing.T, match, dialplanContext string) {
	t.Helper()
	ctx := context.Background()
	db := r.pool.Unwrap()
	cleanup := func() {
		for _, table := range []string{"ps_endpoint_id_ips", "ps_endpoints", "ps_aors"} {
			_, _ = db.Exec(context.Background(), `DELETE FROM `+table+` WHERE id = $1`, spikeIngress) // #nosec G202 -- fixed table names
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	_, err := db.Exec(ctx, `INSERT INTO ps_aors (id) VALUES ($1)`, spikeIngress)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO ps_endpoints (id, transport, aors, context, disallow, allow, direct_media)
		VALUES ($1, 'transport-udp', $1, $2, 'all', 'ulaw,alaw', 'no')`, spikeIngress, dialplanContext)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO ps_endpoint_id_ips (id, endpoint, "match") VALUES ($1, $1, $2)`, spikeIngress, match)
	require.NoError(t, err)
}

// appSawIngress reports whether the app's event loop received an arriving
// channel from the spike endpoint since the given time: with no tenant
// on that endpoint, it logs the refusal naming the source endpoint.
func appSawIngress(t *testing.T, since time.Time) bool {
	t.Helper()
	out, err := exec.Command("docker", "logs", "--since", since.UTC().Format(time.RFC3339Nano), appContainer()).CombinedOutput() // #nosec G204 -- fixed test command
	require.NoError(t, err, "%s", out)
	return strings.Contains(string(out), `"source_endpoint":"`+spikeIngress+`"`)
}

func TestIngressSpike_OneEndpointTakesTheCarriersCalls(t *testing.T) {
	r := newRig(t)
	carrier := startNamedCarrier(t)
	assert.Equal(t, "SIP/2.0 401 Unauthorized", carrier.dial(t, "+442071234599"), "before: an unknown source")

	r.projectSpikeIngress(t, "198.51.100.0/24,"+carrier.addr+",203.0.113.0/28")
	since := time.Now().Add(-time.Second)
	start := time.Now()
	got := carrier.dial(t, "+442071234599")
	took := time.Since(start)
	t.Logf("with the ingress: %s in %v", got, took)
	assert.NotEqual(t, "SIP/2.0 401 Unauthorized", got, "the ingress recognised the carrier: no challenge")
	assert.True(t, appSawIngress(t, since), "the INVITE reached the app's event loop as a channel on the ingress endpoint, through Stasis")
}

func TestIngressSpike_FiveHundredRangesStayPrompt(t *testing.T) {
	r := newRig(t)
	carrier := startNamedCarrier(t)
	// The harness reads replies for a fixed window, so absolute times are
	// the window; what matters is the difference from an unknown source's
	// dial, which never reaches identify by address.
	var baseline time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		carrier.dial(t, "+442071234599")
		baseline = max(baseline, time.Since(start))
	}
	entries := make([]string, 0, 500)
	for i := 0; i < 499; i++ {
		entries = append(entries, fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
	}
	entries = append(entries, carrier.addr)
	r.projectSpikeIngress(t, strings.Join(entries, ","))

	var worst time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		got := carrier.dial(t, "+442071234599")
		took := time.Since(start)
		worst = max(worst, took)
		assert.NotEqual(t, "SIP/2.0 401 Unauthorized", got, "the last of 500 entries still matches")
	}
	t.Logf("worst of five carrier INVITEs against a 500-entry identify row: %v (baseline, unknown source: %v)", worst, baseline)
	assert.Less(t, worst-baseline, 500*time.Millisecond, "500 ranges add no meaningful time")
}

// (b): the transport source address of a carrier's channel is readable
// through ARI as CHANNEL(pjsip,remote_addr) — the fact the platform will
// attribute shared carrier traffic by. The channel must live long enough
// to ask, so the spike endpoint sends it to the engine's echo test
// (ael-demo, 600) instead of the app.
func TestIngressSpike_TheSourceAddressIsReadableThroughARI(t *testing.T) {
	r := newRig(t)
	carrier := startNamedCarrier(t)
	r.projectSpikeIngressIn(t, carrier.addr, "ael-demo")

	done := make(chan string, 1)
	go func() { done <- carrier.dial(t, "600") }()

	var channelID string
	require.Eventually(t, func() bool {
		var channels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if ariGet(t, "/channels", &channels) != http.StatusOK {
			return false
		}
		for _, c := range channels {
			if strings.HasPrefix(c.Name, "PJSIP/"+spikeIngress+"-") {
				channelID = c.ID
				return true
			}
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "the carrier's channel on the spike ingress")
	t.Cleanup(func() { ariDelete(t, "/channels/"+channelID) })

	var v struct {
		Value string `json:"value"`
	}
	status := ariGet(t, "/channels/"+channelID+"/variable?variable="+url.QueryEscape("CHANNEL(pjsip,remote_addr)"), &v)
	require.Equal(t, http.StatusOK, status)
	t.Logf("CHANNEL(pjsip,remote_addr) = %q (carrier container %s)", v.Value, carrier.addr)
	assert.True(t, strings.HasPrefix(v.Value, carrier.addr+":"), "the transport source address and port: %q", v.Value)
	<-done
}

const ariBase = "http://localhost:8088/ari"

func ariGet(t *testing.T, path string, out any) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ariBase+path, nil)
	require.NoError(t, err)
	req.SetBasicAuth(ariCreds())
	resp, err := e2eHTTP.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK && out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp.StatusCode
}

func ariDelete(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, ariBase+path, nil)
	require.NoError(t, err)
	req.SetBasicAuth(ariCreds())
	if resp, err := e2eHTTP.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

// ariCreds are the dev rig's ARI credentials (.env, synthetic, D-39).
func ariCreds() (string, string) {
	user, pass := os.Getenv("ARI_USERNAME"), os.Getenv("ARI_PASSWORD")
	if user == "" {
		user, pass = "voipapp", "devpassword123"
	}
	return user, pass
}
