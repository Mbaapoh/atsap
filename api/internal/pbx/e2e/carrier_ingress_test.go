//go:build e2e

// pbx-carrier-ingress (D-77), on the running app and live Asterisk: two
// tenants whose trunks both recognise one carrier each receive their own
// numbers' calls, attributed by the transport source address and the
// number dialled; a number both have is refused; a header naming the
// other tenant changes nothing.
package e2e_test

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedCarrierTenant is a tenant whose trunk recognises the carrier and
// which has numbers assigned to an extension.
func sharedCarrierTenant(t *testing.T, carrier namedCarrier, numbers ...string) *rig {
	t.Helper()
	r := newRig(t)
	ext, _ := r.createExtension(t, uniqueNumber())
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "Shared carrier", "host": "sip.shared-carrier.invalid", "port": 5060,
		"channelLimit": 4, "inboundAddresses": []string{carrier.addr},
	}, nil, http.StatusOK)
	for _, n := range numbers {
		r.call(t, "PbxService/CreateInboundNumber", r.token, map[string]any{
			"tenantId": r.tenant.String(), "number": n, "extensionId": ext.ExtensionID,
		}, nil, http.StatusOK)
	}
	t.Cleanup(func() {
		_ = r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			for _, stmt := range []string{
				`DELETE FROM placed_call_legs WHERE tenant_id = $1`,
				`DELETE FROM inbound_numbers WHERE tenant_id = $1`,
			} {
				if _, err := tx.Exec(ctx, stmt, r.tenant.String()); err != nil {
					return err
				}
			}
			return nil
		})
	})
	return r
}

// callsTo counts the calls recorded in r's tenant to number.
func (r *rig) callsTo(t *testing.T, number string) int {
	t.Helper()
	var n int
	require.NoError(t, r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM calls WHERE dest_number = $1`, number).Scan(&n)
	}))
	return n
}

func TestCarrierIngress_TwoTenantsOnOneCarrierEachReceiveTheirOwnNumbers(t *testing.T) {
	carrier := startNamedCarrier(t)
	const numberA, numberB, both = "+442071237001", "+442071237002", "+442071237009"
	a := sharedCarrierTenant(t, carrier, numberA, both)
	b := sharedCarrierTenant(t, carrier, numberB, both)

	carrier.dial(t, numberA)
	require.Eventually(t, func() bool { return a.callsTo(t, numberA) == 1 }, 5*time.Second, 100*time.Millisecond, "A's number is A's call")
	assert.Zero(t, b.callsTo(t, numberA), "and never B's")

	// A header naming the other tenant is ignored: attribution is the
	// source address and the number, never what the carrier sends.
	carrier.dialWith(t, numberB, "X-Tenant: "+a.tenant.String())
	require.Eventually(t, func() bool { return b.callsTo(t, numberB) == 1 }, 5*time.Second, 100*time.Millisecond, "B's number is B's call")
	assert.Zero(t, a.callsTo(t, numberB))

	since := time.Now().Add(-time.Second)
	assert.Equal(t, "SIP/2.0 404 Not Found", carrier.dial(t, both), "a number both have is refused, never guessed")
	assert.Zero(t, a.callsTo(t, both))
	assert.Zero(t, b.callsTo(t, both))
	logs, err := exec.Command("docker", "logs", "--since", since.UTC().Format(time.RFC3339Nano), appContainer()).CombinedOutput() // #nosec G204 -- fixed test command
	require.NoError(t, err)
	assert.Contains(t, string(logs), "configured in several tenants", "the operator is told")
	assert.Equal(t, "SIP/2.0 404 Not Found", carrier.dial(t, "+442071237999"), "a number neither has")
	assert.False(t, strings.Contains(string(logs), a.tenant.String()), "no tenant id in the operator log either")
}
