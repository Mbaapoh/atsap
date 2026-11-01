//go:build e2e

// pbx-carrier-addresses (D-76), on the running app and live Asterisk: a
// carrier is recognised from its published signalling ranges, not only
// from its host; a host that cannot be a carrier is refused, and a name
// that resolves only to loopback is never checked.
package e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A carrier whose calls come from an address its host never resolves to
// is recognised once that address is configured as an inbound signalling
// address, and no longer once it is removed.
func TestCarrierAddresses_RecognisedFromAPublishedRange(t *testing.T) {
	r := newRig(t)
	carrier := startNamedCarrier(t)

	var out struct {
		Trunk trunkMsg `json:"trunk"`
	}
	// The host is a name that never resolves: only the range can match.
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "Ranges", "host": "sip.elsewhere.invalid", "port": 5060, "channelLimit": 2,
	}, &out, http.StatusOK)
	trunkID := out.Trunk.TrunkID
	assert.Equal(t, "SIP/2.0 401 Unauthorized", carrier.dial(t, "+442071234599"), "no range yet: an unknown source")

	setInbound := func(inbound []string) {
		t.Helper()
		r.call(t, "PbxService/UpdateTrunk", r.token, map[string]any{
			"tenantId": r.tenant.String(), "trunkId": trunkID, "name": "Ranges", "host": "sip.elsewhere.invalid", "port": 5060, "channelLimit": 2,
			"inboundAddresses": inbound,
		}, nil, http.StatusOK)
	}
	setInbound([]string{carrier.addr + "/32", "203.0.113.0/28"})
	assert.Equal(t, carrier.addr+",203.0.113.0/28", r.identifyMatch(t, trunkID), "projected at once, with no lookup")
	assert.Equal(t, "SIP/2.0 404 Not Found", carrier.dial(t, "+442071234599"), "recognised from its range, routed by the tenant's plan")

	setInbound(nil)
	assert.Empty(t, r.identifyMatch(t, trunkID), "no range and no resolved address: no row")
	assert.Equal(t, "SIP/2.0 401 Unauthorized", carrier.dial(t, "+442071234599"), "a removed range stops being recognised at once")
}

// A host that cannot be a carrier is refused with the field named, and
// nothing is created.
func TestCarrierAddresses_ANonCarrierHostIsRefused(t *testing.T) {
	r := newRig(t)
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "pbx.localhost"} {
		raw := r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
			"tenantId": r.tenant.String(), "name": "Loop", "host": host, "channelLimit": 1,
		}, nil, http.StatusBadRequest)
		var body struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(raw, &body))
		assert.Equal(t, "invalid_argument", body.Code, host)
		assert.Contains(t, body.Message, "trunk host cannot be a carrier", host)
	}
	raw := r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "Broad", "host": "sip.carrier.example", "channelLimit": 1, "inboundAddresses": []string{"10.0.0.0/8"},
	}, nil, http.StatusBadRequest)
	assert.Contains(t, string(raw), "broader than /16")
	var listed struct {
		Trunks []trunkMsg `json:"trunks"`
	}
	r.call(t, "PbxService/ListTrunks", r.token, map[string]any{"tenantId": r.tenant.String()}, &listed, http.StatusOK)
	assert.Empty(t, listed.Trunks, "nothing was created")
}

// A name that resolves only to loopback — Docker's ip6-localhost, ::1 —
// is accepted (it is a name) but never checked: it reads "Address not
// allowed", not "No response".
func TestCarrierAddresses_ANameResolvingToLoopbackIsNotChecked(t *testing.T) {
	r := newRig(t)
	var out struct {
		Trunk trunkMsg `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "Loops back", "host": "ip6-localhost", "port": 5060, "channelLimit": 1,
	}, &out, http.StatusOK)

	var last checkedTrunk
	require.Eventually(t, func() bool {
		var listed struct {
			Trunks []checkedTrunk `json:"trunks"`
		}
		r.call(t, "PbxService/ListTrunks", r.token, map[string]any{"tenantId": r.tenant.String()}, &listed, http.StatusOK)
		for _, tr := range listed.Trunks {
			if tr.TrunkID == out.Trunk.TrunkID {
				last = tr
			}
		}
		return last.HealthReason == "TRUNK_HEALTH_REASON_ADDRESS_NOT_ALLOWED"
	}, 15*time.Second, 200*time.Millisecond, "last seen: %+v", last)
	assert.Equal(t, "TRUNK_HEALTH_UNHEALTHY", last.Health)
	assert.Empty(t, r.identifyMatch(t, out.Trunk.TrunkID), "loopback is never recognised")
}
