//go:build integration

package application_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	pbxpg "atsap-api/internal/pbx/postgres"
)

func (h *harness) reconciler(t *testing.T) *application.Reconciler {
	t.Helper()
	projector := asterisk.NewProjector(asterisk.Config{
		Realm:           testRealm,
		SIPTransport:    "transport-udp",
		WebRTCTransport: "transport-wss",
	}, h.pool)
	return application.NewReconciler(h.pool, pbxpg.NewStore(), projector, projector)
}

// A freshly written estate must reconcile clean. If this ever fails, the
// projector and the reconciler disagree about what a correct projection
// looks like — which would make every other assertion here meaningless.
func TestReconcile_CleanEstate(t *testing.T) {
	h := newHarness(t)
	h.create(t, "1000")
	h.create(t, "1001")

	report, err := h.reconciler(t).Reconcile(context.Background())
	require.NoError(t, err)
	assert.True(t, report.Clean(), "a freshly created estate must reconcile clean: %+v", report)
	assert.Equal(t, 2, report.Checked, "a clean report must be distinguishable from one that checked nothing")
}

// 8.1: a hand-edited projection row is detected, naming what differs.
func TestReconcile_DetectsHandEditedRow(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	// Exactly what an operator poking at the database would do.
	_, err := h.pool.Unwrap().Exec(context.Background(),
		`UPDATE ps_endpoints SET context = 'default' WHERE id = $1`, id)
	require.NoError(t, err)

	report, err := h.reconciler(t).Reconcile(context.Background())
	require.NoError(t, err)
	require.False(t, report.Clean())
	require.Len(t, report.Diverged, 1)
	assert.Equal(t, created.Extension.ID, report.Diverged[0].ExtensionID)
	assert.Contains(t, report.Diverged[0].Fields, "context",
		"the report must say WHAT differs, not merely that something does")
	assert.Equal(t, "1000", report.Diverged[0].Number, "and name the extension an operator recognises")
}

// A deleted projection is reported as missing rather than as every field
// differing — one line an operator can act on beats eight.
func TestReconcile_DetectsMissingProjection(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	_, err := h.pool.Unwrap().Exec(context.Background(), `DELETE FROM ps_endpoints WHERE id = $1`, id)
	require.NoError(t, err)

	report, err := h.reconciler(t).Reconcile(context.Background())
	require.NoError(t, err)
	require.Len(t, report.Diverged, 1)
	assert.Equal(t, []string{"missing"}, report.Diverged[0].Fields)
}

// A projected endpoint with no domain row is an endpoint a device could
// still register against that nobody is administering.
func TestReconcile_DetectsOrphanedProjection(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	// Remove the domain row directly, leaving the projection behind — the
	// state a partial failure or a manual delete would produce.
	deleteDomainRowDirectly(t, h, created.Extension.ID.String())

	report, err := h.reconciler(t).Reconcile(context.Background())
	require.NoError(t, err)
	require.Len(t, report.Orphaned, 1, "report: %+v", report)
	assert.Equal(t, created.Extension.ID, report.Orphaned[0])
	assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`, id))
}

// A row this platform did not write is reported and deliberately NOT
// repaired: deleting something nobody can account for is how a
// reconciler turns a mystery into an outage.
func TestReconcile_ReportsButNeverRepairsUnattributableRows(t *testing.T) {
	h := newHarness(t)
	_, err := h.pool.Unwrap().Exec(context.Background(),
		`INSERT INTO ps_endpoints (id, context) VALUES ('hand-written-by-someone', 'default')`)
	require.NoError(t, err)

	rec := h.reconciler(t)
	report, err := rec.Reconcile(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"hand-written-by-someone"}, report.Unattributable)

	_, err = rec.Repair(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, 1, h.countRow(t,
		`SELECT count(*) FROM ps_endpoints WHERE id = 'hand-written-by-someone'`),
		"an unattributable row must survive a repair, not be silently deleted")
}

// 8.2: --fix restores agreement, and a run without it changes nothing.
func TestRepair_RestoresAgreementAndIsOptIn(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	_, err := h.pool.Unwrap().Exec(context.Background(),
		`UPDATE ps_endpoints SET context = 'default', transport = 'nonsense' WHERE id = $1`, id)
	require.NoError(t, err)

	rec := h.reconciler(t)

	// Reconcile alone must change nothing.
	report, err := rec.Reconcile(context.Background())
	require.NoError(t, err)
	require.False(t, report.Clean())

	var context_ string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT context FROM ps_endpoints WHERE id = $1`, id).Scan(&context_))
	assert.Equal(t, "default", context_, "reporting must not repair; repair is opt-in")

	repaired, err := rec.Repair(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, 1, repaired)

	after, err := rec.Reconcile(context.Background())
	require.NoError(t, err)
	assert.True(t, after.Clean(), "after repair the estate must reconcile clean: %+v", after)

	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT context FROM ps_endpoints WHERE id = $1`, id).Scan(&context_))
	assert.Equal(t, "stasis-in", context_, "repair restores what the platform says it should be")
}

func TestRepair_RemovesOrphanedProjections(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	survivor := h.create(t, "1001")
	orphanID := domain.EndpointIdentifier(created.Extension.ID)

	deleteDomainRowDirectly(t, h, created.Extension.ID.String())

	rec := h.reconciler(t)
	report, err := rec.Reconcile(context.Background())
	require.NoError(t, err)
	require.Len(t, report.Orphaned, 1)

	repaired, err := rec.Repair(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, 1, repaired)

	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`, orphanID),
		"an orphaned endpoint must be removed: a device could otherwise still register against it")
	assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`,
		domain.EndpointIdentifier(survivor.Extension.ID)),
		"and the extension that still exists must be untouched")
}

// deleteDomainRowDirectly removes an extension row without going through
// the service, leaving its projection behind — the state a partial
// failure or a manual database edit produces. It must run under a tenant
// context, since extensions is RLS-protected.
func deleteDomainRowDirectly(t *testing.T, h *harness, extensionID string) {
	t.Helper()
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant,
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM extensions WHERE id = $1`, extensionID)
			return err
		}))
}
