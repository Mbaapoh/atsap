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

// TestProjectTrunk_WritesTenantVariableAndIdentify covers design D5:
// the trunk endpoint carries ATSA_TENANT_ID, and one identify row
// matches the carrier's host so its signalling is attributed to the
// trunk.
func TestProjectTrunk_WritesTenantVariableAndIdentify(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	id := domain.TrunkEndpointIdentifier(tr.ID)
	h.projectTrunk(t, tr, []byte(carrierSecret))

	var setVar *string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT set_var FROM ps_endpoints WHERE id = $1`, id).Scan(&setVar))
	require.NotNil(t, setVar)
	assert.Equal(t, "ATSA_TENANT_ID="+tr.TenantID.String(), *setVar)

	var endpoint, match string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT endpoint, "match" FROM ps_endpoint_id_ips WHERE id = $1`, id).Scan(&endpoint, &match))
	assert.Equal(t, id, endpoint)
	assert.Equal(t, tr.Host, match)

	drift, err := h.projector.DiffTrunk(context.Background(), tr)
	require.NoError(t, err)
	assert.Empty(t, drift)
}

// TestProjectTrunk_ReportsAndRepairsMissingIdentify covers the
// reconcile half for trunks: a missing identify row is reported and
// re-projection repairs it.
func TestProjectTrunk_ReportsAndRepairsMissingIdentify(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	id := domain.TrunkEndpointIdentifier(tr.ID)
	h.projectTrunk(t, tr, []byte(carrierSecret))

	_, err := h.pool.Unwrap().Exec(context.Background(),
		`DELETE FROM ps_endpoint_id_ips WHERE id = $1`, id)
	require.NoError(t, err)

	drift, err := h.projector.DiffTrunk(context.Background(), tr)
	require.NoError(t, err)
	assert.Contains(t, drift, "identify")

	h.projectTrunk(t, tr, nil)
	drift, err = h.projector.DiffTrunk(context.Background(), tr)
	require.NoError(t, err)
	assert.Empty(t, drift)
}

// TestRemoveTrunk_RemovesIdentifyRow covers that a deleted trunk's
// carrier stops being recognized: the identify row leaves with it.
func TestRemoveTrunk_RemovesIdentifyRow(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	id := domain.TrunkEndpointIdentifier(tr.ID)
	h.projectTrunk(t, tr, []byte(carrierSecret))

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.RemoveTrunk(ctx, tx, tr.ID)
	}))

	var n int
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ps_endpoint_id_ips WHERE id = $1`, id).Scan(&n))
	assert.Zero(t, n, "a deleted trunk's carrier must no longer be recognized")
}
