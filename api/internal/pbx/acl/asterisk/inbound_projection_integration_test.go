//go:build integration

package asterisk_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
)

// TestProjectExtension_WritesTenantVariable covers design D1: every
// projected extension endpoint carries ATSA_TENANT_ID, so an arriving
// channel carries its tenant with no SIP header to forge and no
// cross-tenant lookup to make.
func TestProjectExtension_WritesTenantVariable(t *testing.T) {
	h := newHarness(t)
	ext := newExtension("1000", domain.DeviceSIP)
	id := domain.EndpointIdentifier(ext.ID)

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))

	var setVar *string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT set_var FROM ps_endpoints WHERE id = $1`, id).Scan(&setVar))
	require.NotNil(t, setVar)
	assert.Equal(t, "ATSA_TENANT_ID="+ext.TenantID.String(), *setVar)

	drift, err := h.projector.DiffExtension(context.Background(), ext)
	require.NoError(t, err)
	assert.Empty(t, drift)
}

// TestProjectExtension_ReportsAndRepairsMissingTenantVariable covers the
// reconcile half: an endpoint projected before set_var existed is
// reported, and re-projection repairs it.
func TestProjectExtension_ReportsAndRepairsMissingTenantVariable(t *testing.T) {
	h := newHarness(t)
	ext := newExtension("1001", domain.DeviceSIP)
	id := domain.EndpointIdentifier(ext.ID)

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))
	// Simulate a pre-change projection: blank the variable by hand.
	_, err := h.pool.Unwrap().Exec(context.Background(),
		`UPDATE ps_endpoints SET set_var = NULL WHERE id = $1`, id)
	require.NoError(t, err)

	drift, err := h.projector.DiffExtension(context.Background(), ext)
	require.NoError(t, err)
	assert.Contains(t, drift, "set_var")

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))
	drift, err = h.projector.DiffExtension(context.Background(), ext)
	require.NoError(t, err)
	assert.Empty(t, drift)
}

// TestProjectTrunk_WritesTenantVariable covers design D5 as amended by
// D-77: the trunk endpoint (used for calls out) carries ATSA_TENANT_ID;
// the carrier's calls in are recognised by the shared ingress, which
// carries none, and the trunk has no identify row of its own.
func TestProjectTrunk_WritesTenantVariable(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	id := domain.TrunkEndpointIdentifier(tr.ID)
	h.projectTrunk(t, tr, []byte(carrierSecret))

	var setVar *string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT set_var FROM ps_endpoints WHERE id = $1`, id).Scan(&setVar))
	require.NotNil(t, setVar)
	assert.Equal(t, "ATSA_TENANT_ID="+tr.TenantID.String(), *setVar)
	assert.Zero(t, h.trunkIdentifyRows(t, tr.ID))
	match, ok := h.ingressMatch(t)
	require.True(t, ok)
	assert.Equal(t, sampleMatch, match, "the carrier's addresses, never its name (D-75), in the ingress (D-77)")

	drift, err := h.projector.DiffTrunk(context.Background(), tr)
	require.NoError(t, err)
	assert.Empty(t, drift)
}

// TestRemoveTrunk_StopsRecognisingItsCarrier: a deleted trunk's carrier
// must no longer be recognised — its addresses leave the ingress.
func TestRemoveTrunk_StopsRecognisingItsCarrier(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tr.TenantID.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM carrier_trunks WHERE id = $1`, tr.ID.String()); err != nil {
			return err
		}
		return h.projector.RemoveTrunk(ctx, tx, tr.ID)
	}))
	_, ok := h.ingressMatch(t)
	assert.False(t, ok, "a deleted trunk's carrier must no longer be recognized")
}
