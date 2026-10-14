//go:build e2e

// Live proof for pbx-trunks-routes task 4.4: a trunk and route created
// through the public API become engine state Asterisk itself loads, and a
// deleted trunk is gone from both the engine and route selection.
//
// A placed outbound call is NOT proven here: that needs PlaceCall and a
// carrier to receive it, and is pbx-call-placement's acceptance (LLD-03
// DoD 3 split).
package e2e_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxapp "atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// carrierHost is TEST-NET-3 (RFC 5737): routable nowhere, so nothing is
// ever sent to a real carrier from this rig.
const carrierHost = "203.0.113.10"

// engineCLI runs one Asterisk CLI command in the dev rig's engine
// container — the same access `make rig-restore` uses. ARI cannot answer
// this question: it only knows endpoints already loaded, and returns 404
// for a realtime endpoint until something looks it up.
func engineCLI(t *testing.T, command string) string {
	t.Helper()
	container := os.Getenv("ASTERISK_CONTAINER")
	if container == "" {
		container = "deploy-asterisk-1"
	}
	out, err := exec.Command("docker", "exec", container, "asterisk", "-rx", command).CombinedOutput() // #nosec G204 -- fixed test command
	require.NoError(t, err, "the engine container must be up: %s", out)
	return string(out)
}

type trunkMsg struct {
	TrunkID   string `json:"trunkId"`
	HasSecret bool   `json:"hasSecret"`
	Health    string `json:"health"`
}

func TestTrunks_ProjectionIsLiveAndDeletionIsImmediate(t *testing.T) {
	r := newRig(t)
	t.Cleanup(func() {
		_ = r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, _ = tx.Exec(ctx, `DELETE FROM carrier_routes WHERE tenant_id = $1`, r.tenant.String())
			_, err := tx.Exec(ctx, `DELETE FROM carrier_trunks WHERE tenant_id = $1`, r.tenant.String())
			return err
		})
	})

	var created struct {
		Trunk trunkMsg `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "e2e carrier", "host": carrierHost, "port": 5060,
		"authUsername": "e2e-acct", "secret": "e2e-carrier-secret", "channelLimit": 4,
	}, &created, http.StatusOK)
	require.True(t, created.Trunk.HasSecret)
	assert.Equal(t, "TRUNK_HEALTH_UNKNOWN", created.Trunk.Health, "not checked yet, never healthy by default")

	r.call(t, "PbxService/CreateRoute", r.token, map[string]any{
		"tenantId": r.tenant.String(), "prefixPattern": "+44", "trunkId": created.Trunk.TrunkID,
	}, nil, http.StatusOK)

	trunkID, err := shareddomain.ParseTrunkID(created.Trunk.TrunkID)
	require.NoError(t, err)
	identifier := domain.TrunkEndpointIdentifier(trunkID)

	// The engine loads it from realtime with no reload: endpoint, static
	// contact at the carrier, and outbound auth presenting our username.
	endpoint := engineCLI(t, "pjsip show endpoint "+identifier)
	require.Contains(t, endpoint, "Endpoint:  "+identifier, "Asterisk must load the projected trunk: %s", endpoint)
	// The endpoint summary truncates the contact URI; the AoR's own
	// parameter list carries it whole.
	assert.Regexp(t, `contact\s+:\s+sip:`+carrierHost+`:5060`, engineCLI(t, "pjsip show aor "+identifier),
		"calls go to the carrier's contact")
	assert.Regexp(t, `outbound_auth\s+:\s+`+identifier, endpoint)
	assert.Regexp(t, `from_user\s+:\s+e2e-acct`, endpoint)
	assert.Regexp(t, `from_domain\s+:\s+`+carrierHost, endpoint)
	assert.Regexp(t, `username\s+:\s+e2e-acct`, engineCLI(t, "pjsip show auth "+identifier))

	planner := pbxapp.NewService(r.pool, pbxpostgres.NewStore(), pbxpostgres.NewTrunkStore(), pbxpostgres.NewRouteStore(),
		ports.ExtensionCapFunc(func(context.Context) (int, error) { return 0, nil }), nil, nil, nil, "")
	selected, err := planner.SelectOutboundRoute(context.Background(), r.tenant, "+442071234567")
	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, trunkID, selected[0].Trunk.ID, "the live route selects the live trunk")

	// Delete through the API: gone from the engine and from selection at once.
	r.call(t, "PbxService/DeleteTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "trunkId": created.Trunk.TrunkID,
	}, nil, http.StatusOK)

	var n int
	require.NoError(t, r.pool.Unwrap().QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM ps_endpoints WHERE id = $1) + (SELECT count(*) FROM ps_auths WHERE id = $1)
		      + (SELECT count(*) FROM ps_aors WHERE id = $1)`, identifier).Scan(&n))
	assert.Zero(t, n, "no engine row survives the delete")
	gone := engineCLI(t, "pjsip show endpoint "+identifier)
	assert.False(t, strings.Contains(gone, "Endpoint:  "+identifier), "Asterisk no longer finds it: %s", gone)

	selected, err = planner.SelectOutboundRoute(context.Background(), r.tenant, "+442071234567")
	require.NoError(t, err)
	assert.Empty(t, selected, "a deleted trunk is never selected again")

	var routes struct {
		Routes []any `json:"routes"`
	}
	r.call(t, "PbxService/ListRoutes", r.token, map[string]any{"tenantId": r.tenant.String()}, &routes, http.StatusOK)
	assert.Empty(t, routes.Routes, "its route went with it")
}
