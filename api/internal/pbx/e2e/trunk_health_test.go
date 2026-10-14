//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type checkedTrunk struct {
	TrunkID          string `json:"trunkId"`
	Health           string `json:"health"`
	HealthReason     string `json:"healthReason"`
	HealthObservedAt string `json:"healthObservedAt"`
}

// TestTrunkHealth_IsWhatTheCarrierSays is pbx-trunk-health-probing task
// 2.2 on the running app: trunks are created through the public API, and
// each reports, within seconds, what the platform's own check of its
// carrier found — against real peers, not fakes. The engine answers SIP
// on its compose name; a .invalid name cannot exist (RFC 6761); nothing
// answers from TEST-NET-1 (RFC 5737).
func TestTrunkHealth_IsWhatTheCarrierSays(t *testing.T) {
	r := newRig(t)
	t.Cleanup(func() {
		_ = r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM carrier_trunks WHERE tenant_id = $1`, r.tenant.String())
			return err
		})
	})

	create := func(name, host string) checkedTrunk {
		var out struct {
			Trunk checkedTrunk `json:"trunk"`
		}
		r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
			"tenantId": r.tenant.String(), "name": name, "host": host, "port": 5060, "channelLimit": 2,
		}, &out, http.StatusOK)
		assert.Equal(t, "TRUNK_HEALTH_UNKNOWN", out.Trunk.Health, "%s: not checked yet when created", name)
		assert.Empty(t, out.Trunk.HealthObservedAt, "%s: no observation yet", name)
		return out.Trunk
	}
	health := func(id string) checkedTrunk {
		var out struct {
			Trunks []checkedTrunk `json:"trunks"`
		}
		r.call(t, "PbxService/ListTrunks", r.token, map[string]any{"tenantId": r.tenant.String(), "limit": 50}, &out, http.StatusOK)
		for _, tr := range out.Trunks {
			if tr.TrunkID == id {
				return tr
			}
		}
		t.Fatalf("trunk %s not listed", id)
		return checkedTrunk{}
	}
	await := func(id, wantHealth, wantReason string) {
		t.Helper()
		var last checkedTrunk
		require.Eventually(t, func() bool {
			last = health(id)
			return last.Health == wantHealth && last.HealthReason == wantReason
		}, 15*time.Second, 200*time.Millisecond, "want %s/%s", wantHealth, wantReason)
		observed, err := time.Parse(time.RFC3339Nano, last.HealthObservedAt)
		require.NoError(t, err, "an observation carries its time")
		assert.WithinDuration(t, time.Now(), observed, time.Minute)
	}

	engine := create("The engine", "asterisk")
	missing := create("A typo", "no-such-carrier.invalid")
	unreachable := create("Nobody home", "192.0.2.1")

	await(engine.TrunkID, "TRUNK_HEALTH_HEALTHY", "TRUNK_HEALTH_REASON_ANSWERING")
	await(missing.TrunkID, "TRUNK_HEALTH_UNHEALTHY", "TRUNK_HEALTH_REASON_HOST_NOT_FOUND")
	await(unreachable.TrunkID, "TRUNK_HEALTH_UNHEALTHY", "TRUNK_HEALTH_REASON_NO_RESPONSE")

	// Fixing the typo is checked at once, not at the next round.
	var updated struct {
		Trunk checkedTrunk `json:"trunk"`
	}
	r.call(t, "PbxService/UpdateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "trunkId": missing.TrunkID, "name": "A typo, fixed", "host": "asterisk", "port": 5060, "channelLimit": 2,
	}, &updated, http.StatusOK)
	assert.Equal(t, "TRUNK_HEALTH_UNKNOWN", updated.Trunk.Health, "the old observation was of another address")
	await(missing.TrunkID, "TRUNK_HEALTH_HEALTHY", "TRUNK_HEALTH_REASON_ANSWERING")
}
