//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mailMessages returns the subjects and bodies of mail sent to to.
func mailMessages(t *testing.T, to string) []struct{ subject, body string } {
	t.Helper()
	search := "http://localhost:8025/api/v1/search?query=" + url.QueryEscape(fmt.Sprintf("to:%q", to))
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(search) // #nosec G107 -- fixed test endpoint
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	var list struct {
		Messages []struct {
			ID      string `json:"ID"`
			Subject string `json:"Subject"`
		} `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&list))
	out := make([]struct{ subject, body string }, 0, len(list.Messages))
	for _, m := range list.Messages {
		msgRes, err := client.Get("http://localhost:8025/api/v1/message/" + m.ID) // #nosec G107 -- fixed test endpoint
		require.NoError(t, err)
		var msg struct {
			Subject string `json:"Subject"`
			Text    string `json:"Text"`
		}
		require.NoError(t, json.NewDecoder(msgRes.Body).Decode(&msg))
		_ = msgRes.Body.Close()
		out = append(out, struct{ subject, body string }{msg.Subject, msg.Text})
	}
	return out
}

// BR-08 / D-80: a carrier failure emails its tenant administrator once,
// with no engine identifier, and further failures do not repeat it.
func TestCarrierAlert_FailureReachesTheOperatorOnce(t *testing.T) {
	r := newRig(t)
	const name = "Alerting carrier"
	var out struct {
		Trunk checkedTrunk `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": name, "host": "no-such-carrier.invalid", "port": 5060, "channelLimit": 2,
	}, &out, http.StatusOK)

	// A host that cannot resolve is Unhealthy on its first check.
	require.Eventually(t, func() bool {
		var listed struct {
			Trunks []checkedTrunk `json:"trunks"`
		}
		r.call(t, "PbxService/ListTrunks", r.token, map[string]any{"tenantId": r.tenant.String(), "limit": 50}, &listed, http.StatusOK)
		for _, tr := range listed.Trunks {
			if tr.TrunkID == out.Trunk.TrunkID {
				return tr.Health == "TRUNK_HEALTH_UNHEALTHY" && tr.HealthReason == "TRUNK_HEALTH_REASON_HOST_NOT_FOUND"
			}
		}
		return false
	}, 15*time.Second, 200*time.Millisecond)

	var alerts []struct{ subject, body string }
	require.Eventually(t, func() bool {
		alerts = nil
		for _, m := range mailMessages(t, r.email) {
			if strings.Contains(m.subject, name) {
				alerts = append(alerts, m)
			}
		}
		return len(alerts) == 1
	}, 15*time.Second, 300*time.Millisecond, "one alert to the tenant administrator")
	assert.Contains(t, alerts[0].body, "host not found")
	assert.NotContains(t, alerts[0].body, "PJSIP/")
	assert.NotContains(t, alerts[0].body, "t_")

	// Further failures are silent.
	time.Sleep(12 * time.Second)
	count := 0
	for _, m := range mailMessages(t, r.email) {
		if strings.Contains(m.subject, name) {
			count++
		}
	}
	assert.Equal(t, 1, count, "no repeated alert while the trunk stays failed")
}
