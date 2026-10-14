//go:build integration

package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityapp "atsap-api/internal/identity/application"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	identitypg "atsap-api/internal/identity/postgres"
	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpg "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

const carrierSecret = "carrier-s3cret-value"

func (f *failingProjector) ProjectTrunk(ctx context.Context, tx pgx.Tx, t domain.Trunk, secret []byte) error {
	if f.failProjectTrunk != nil {
		return f.failProjectTrunk
	}
	return f.EndpointProjector.ProjectTrunk(ctx, tx, t, secret)
}

// failingAudit fails the audit write — the LAST step of a mutation, after
// the projection — so a test can prove a late failure unwinds the engine
// rows too, not only the domain row.
type failingAudit struct{}

func (failingAudit) AppendAuditTx(context.Context, pgx.Tx, identitydomain.AuditEntry) error {
	return errors.New("audit store unavailable")
}

func (h *harness) trunkCmd(name string) ports.CreateTrunkCommand {
	return ports.CreateTrunkCommand{
		TenantID: h.tenant, Name: name, Host: "sip.carrier.example", Port: 5060,
		AuthUsername: "acct-1001", Secret: carrierSecret, ChannelLimit: 30, Priority: 1,
	}
}

func (h *harness) createTrunk(t *testing.T, name string) ports.TrunkView {
	t.Helper()
	v, err := h.svc.CreateTrunk(h.ctx, h.trunkCmd(name))
	require.NoError(t, err)
	return v
}

func (h *harness) engineRows(t *testing.T, id shareddomain.TrunkID) int {
	t.Helper()
	identifier := domain.TrunkEndpointIdentifier(id)
	return h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`, identifier) +
		h.countRow(t, `SELECT count(*) FROM ps_aors WHERE id = $1`, identifier) +
		h.countRow(t, `SELECT count(*) FROM ps_auths WHERE id = $1`, identifier)
}

// otherTenant provisions a second tenant and a context authenticated as
// one of its principals.
func (h *harness) otherTenant(t *testing.T) (shareddomain.TenantID, context.Context) {
	t.Helper()
	id := shareddomain.NewTenantID()
	require.NoError(t, identitypg.NewStore(h.pool).CreateTenant(context.Background(), identitydomain.Tenant{
		ID: id, Name: "Other", Status: identitydomain.TenantActive, ResidencyZone: "EU",
	}, 0))
	return id, identityapp.WithTenantContext(context.Background(), &identityports.TenantContext{
		TenantID: id, PrincipalID: shareddomain.NewPrincipalID(),
	})
}

// --- 2.3: one transaction, both directions; the secret goes nowhere ----

func TestCreateTrunk_WritesDomainProjectionAndAudit(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")

	assert.Equal(t, domain.HealthUnknown, v.Health, "a new trunk is not yet checked, never healthy by default")
	assert.Nil(t, v.HealthObservedAt)
	assert.True(t, v.HasSecret)
	assert.Equal(t, 1, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks WHERE id = $1`, v.ID.String()))
	assert.Equal(t, 3, h.engineRows(t, v.ID), "endpoint, aor and auth projected")
	assert.Equal(t, 1, h.countTenantRow(t,
		`SELECT count(*) FROM audit_logs WHERE resource_id = $1 AND action = 'trunk.created'`, v.ID.String()))
	assert.Contains(t, h.authz.calls[0], "trunk.create", "authorization precedes the write")

	assert.NotContains(t, fmt.Sprintf("%+v", v), carrierSecret, "the response never carries the secret")
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks WHERE carrier_trunks::text LIKE '%' || $1 || '%'`,
		carrierSecret), "the trunk row holds a reference, not the secret")
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM audit_logs WHERE audit_logs::text LIKE '%' || $1 || '%'`,
		carrierSecret), "no audit row contains the secret")
}

func TestCreateTrunk_ProjectionFailureLeavesNoDomainRow(t *testing.T) {
	h := newHarness(t)
	h.projector.failProjectTrunk = errors.New("engine unavailable")

	_, err := h.svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.Error(t, err)
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks`))
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM audit_logs`))
}

func TestCreateTrunk_LateFailureLeavesNoEngineRow(t *testing.T) {
	h := newHarness(t)
	svc := application.NewService(h.pool, pbxpg.NewStore(), pbxpg.NewTrunkStore(), pbxpg.NewRouteStore(),
		ports.ExtensionCapFunc(func(context.Context) (int, error) { return 0, nil }),
		h.projector, h.authz, failingAudit{}, testRealm)

	_, err := svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.Error(t, err)
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks`))
	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id LIKE 't\_%'`),
		"a failure after projection must unwind the engine rows too")
	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_auths WHERE id LIKE 't\_%'`))
}

func TestCreateTrunk_RefusesInvalidInputAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	for name, mutate := range map[string]func(*ports.CreateTrunkCommand){
		"secret without username": func(c *ports.CreateTrunkCommand) { c.AuthUsername = "" },
		"bad host":                func(c *ports.CreateTrunkCommand) { c.Host = "sip:carrier.example" },
		"port out of range":       func(c *ports.CreateTrunkCommand) { c.Port = 70000 },
		"zero channel limit":      func(c *ports.CreateTrunkCommand) { c.ChannelLimit = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cmd := h.trunkCmd("Bad")
			mutate(&cmd)
			_, err := h.svc.CreateTrunk(h.ctx, cmd)
			require.ErrorIs(t, err, ports.ErrInvalidInput)
		})
	}
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks`))
	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id LIKE 't\_%'`))
}

func TestCreateTrunk_IPAuthenticated(t *testing.T) {
	h := newHarness(t)
	cmd := h.trunkCmd("IP auth")
	cmd.AuthUsername, cmd.Secret = "", ""
	v, err := h.svc.CreateTrunk(h.ctx, cmd)
	require.NoError(t, err)
	assert.False(t, v.HasSecret)
	assert.Equal(t, 2, h.engineRows(t, v.ID), "endpoint and aor, no auth row")
}

func TestCreateTrunk_RefusedWithoutPermission(t *testing.T) {
	h := newHarness(t)
	h.authz.deny = identityports.ErrPermissionDenied
	_, err := h.svc.CreateTrunk(h.ctx, h.trunkCmd("Nope"))
	require.ErrorIs(t, err, identityports.ErrPermissionDenied)
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks`))
}

func TestUpdateTrunk_SecretKeptReplacedAndDropped(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")
	identifier := domain.TrunkEndpointIdentifier(v.ID)
	password := func() *string {
		var p *string
		_ = h.pool.Unwrap().QueryRow(context.Background(), `SELECT password FROM ps_auths WHERE id = $1`, identifier).Scan(&p)
		return p
	}
	update := func(username, secret string) ports.TrunkView {
		got, err := h.svc.UpdateTrunk(h.ctx, ports.UpdateTrunkCommand{
			TenantID: h.tenant, ID: v.ID, Name: "Primary", Host: "sip2.carrier.example", Port: 5060,
			AuthUsername: username, Secret: secret, ChannelLimit: 10, Priority: 2,
		})
		require.NoError(t, err)
		return got
	}

	got := update("acct-1001", "")
	assert.True(t, got.HasSecret, "an update without a secret keeps it")
	assert.Equal(t, carrierSecret, *password())

	got = update("acct-1001", "rotated-secret")
	assert.True(t, got.HasSecret)
	assert.Equal(t, "rotated-secret", *password(), "a supplied secret replaces the stored one")

	got = update("", "")
	assert.False(t, got.HasSecret, "an empty username makes the trunk IP-authenticated")
	assert.Nil(t, password(), "and its secret leaves the engine with it")

	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM audit_logs WHERE audit_logs::text LIKE '%' || $1 || '%'`,
		"rotated-secret"), "no audit row contains a replacement secret")
	assert.Equal(t, 3, h.countTenantRow(t, `SELECT count(*) FROM audit_logs WHERE action = 'trunk.updated'`))
}

func TestDeleteTrunk_TakesItsRoutesAndEngineState(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")
	for _, p := range []string{"44", "33"} {
		_, err := h.svc.CreateRoute(h.ctx, ports.CreateRouteCommand{TenantID: h.tenant, PrefixPattern: p, TrunkID: v.ID})
		require.NoError(t, err)
	}

	require.NoError(t, h.svc.DeleteTrunk(h.ctx, h.tenant, v.ID))
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_trunks`))
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM carrier_routes`))
	assert.Zero(t, h.engineRows(t, v.ID))
	assert.Equal(t, 2, h.countTenantRow(t, `SELECT count(*) FROM audit_logs WHERE action = 'route.deleted'`),
		"the deletion records the routes it took")

	got, err := h.svc.SelectOutboundRoute(h.ctx, h.tenant, "4420")
	require.NoError(t, err)
	assert.Empty(t, got, "a deleted trunk is never selected again")
}

// Another tenant naming this trunk gets the nonexistent answer from every
// path, and this tenant's engine rows survive — the gate in front of
// tables with no RLS.
func TestTrunks_CrossTenantIsIndistinguishableFromNonexistent(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")
	otherID, otherCtx := h.otherTenant(t)

	for name, id := range map[string]shareddomain.TrunkID{"foreign": v.ID, "nonexistent": shareddomain.NewTrunkID()} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, h.svc.DeleteTrunk(otherCtx, otherID, id), ports.ErrNotFound)
			_, err := h.svc.UpdateTrunk(otherCtx, ports.UpdateTrunkCommand{TenantID: otherID, ID: id, Name: "x",
				Host: "evil.example", Port: 5060, ChannelLimit: 1})
			assert.ErrorIs(t, err, ports.ErrNotFound)
			_, err = h.svc.CreateRoute(otherCtx, ports.CreateRouteCommand{TenantID: otherID, PrefixPattern: "44", TrunkID: id})
			assert.ErrorIs(t, err, ports.ErrNotFound, "a route cannot point at another tenant's trunk")
		})
	}
	listed, err := h.svc.ListTrunks(otherCtx, otherID, ports.Page{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, listed)
	assert.Equal(t, 3, h.engineRows(t, v.ID), "the owner's engine state is untouched")
}

// --- 2.4: the planner; health is observed, never set ---------------------

func TestRoutePlanner_SelectionAndObservedHealth(t *testing.T) {
	h := newHarness(t)
	primary := h.createTrunk(t, "Primary")
	backup := h.createTrunk(t, "Backup")
	for _, c := range []ports.CreateRouteCommand{
		{TenantID: h.tenant, PrefixPattern: "44", TrunkID: primary.ID, Priority: 0},
		{TenantID: h.tenant, PrefixPattern: "44", TrunkID: backup.ID, Priority: 1},
	} {
		_, err := h.svc.CreateRoute(h.ctx, c)
		require.NoError(t, err)
	}
	selectFirst := func() shareddomain.TrunkID {
		got, err := h.svc.SelectOutboundRoute(h.ctx, h.tenant, "+442071234567")
		require.NoError(t, err)
		require.NotEmpty(t, got)
		return got[0].Trunk.ID
	}
	assert.Equal(t, primary.ID, selectFirst())

	engineBefore := h.countRow(t, `SELECT count(*) FROM ps_endpoints`)

	require.NoError(t, h.svc.ReportRouteHealth(h.ctx, primary.ID, 486, time.Second))
	assert.Equal(t, primary.ID, selectFirst(), "a busy callee is not the carrier's fault")

	require.NoError(t, h.svc.ReportRouteHealth(h.ctx, primary.ID, 503, 0))
	assert.Equal(t, backup.ID, selectFirst(), "an unhealthy trunk leaves selection immediately")
	got, err := h.svc.SelectOutboundRoute(h.ctx, h.tenant, "+442071234567")
	require.NoError(t, err)
	assert.Len(t, got, 1, "and is skipped, not merely demoted")
	assert.Equal(t, engineBefore, h.countRow(t, `SELECT count(*) FROM ps_endpoints`),
		"health is selection-time state only: engine state, and so any call in progress, is untouched")

	// Configuration cannot set health: an update leaves the observation.
	_, err = h.svc.UpdateTrunk(h.ctx, ports.UpdateTrunkCommand{TenantID: h.tenant, ID: primary.ID, Name: "Primary",
		Host: "sip.carrier.example", Port: 5060, AuthUsername: "acct-1001", ChannelLimit: 30, Priority: 1})
	require.NoError(t, err)
	listed, err := h.svc.ListTrunks(h.ctx, h.tenant, ports.Page{Limit: 10})
	require.NoError(t, err)
	for _, tv := range listed {
		if tv.ID == primary.ID {
			assert.Equal(t, domain.HealthUnhealthy, tv.Health)
		}
	}

	require.NoError(t, h.svc.ReportRouteHealth(h.ctx, primary.ID, 200, 3*time.Second))
	got, err = h.svc.SelectOutboundRoute(h.ctx, h.tenant, "+442071234567")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, backup.ID, got[0].Trunk.ID, "a slow answer degrades: back in, behind the healthy trunk")

	require.NoError(t, h.svc.ReportRouteHealth(h.ctx, primary.ID, 200, 100*time.Millisecond))
	assert.Equal(t, primary.ID, selectFirst(), "a good answer restores it")
}

func TestReportRouteHealth_RequiresATenantAndStaysInIt(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")

	err := h.svc.ReportRouteHealth(context.Background(), v.ID, 503, 0)
	require.ErrorIs(t, err, identityports.ErrPermissionDenied, "no tenant in context, no report")

	_, otherCtx := h.otherTenant(t)
	err = h.svc.ReportRouteHealth(otherCtx, v.ID, 503, 0)
	require.ErrorIs(t, err, ports.ErrNotFound, "another tenant cannot mark this trunk unhealthy")
	listed, err := h.svc.ListTrunks(h.ctx, h.tenant, ports.Page{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, domain.HealthUnknown, listed[0].Health, "refused reports changed nothing")

	// The tenant's own report is recorded with its reason and time.
	require.NoError(t, h.svc.ReportRouteHealth(h.ctx, v.ID, 503, 0))
	listed, err = h.svc.ListTrunks(h.ctx, h.tenant, ports.Page{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, domain.HealthUnhealthy, listed[0].Health)
	assert.Equal(t, domain.ReasonCarrierError, listed[0].HealthReason)
	assert.NotNil(t, listed[0].HealthObservedAt)
}

// --- 2.5: reconcile covers trunks ----------------------------------------

func TestReconcile_TrunkDriftDetectedAndRepaired(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")
	identifier := domain.TrunkEndpointIdentifier(v.ID)
	ctx := context.Background()

	clean, err := h.reconciler(t).Reconcile(ctx)
	require.NoError(t, err)
	assert.True(t, clean.Clean(), "%+v", clean)
	assert.Equal(t, 1, clean.TrunksChecked)

	_, err = h.pool.Unwrap().Exec(ctx, `UPDATE ps_aors SET contact = 'sip:attacker.example:5060' WHERE id = $1`, identifier)
	require.NoError(t, err)

	report, err := h.reconciler(t).Reconcile(ctx)
	require.NoError(t, err)
	require.Len(t, report.TrunksDiverged, 1)
	assert.Equal(t, v.ID, report.TrunksDiverged[0].TrunkID)
	assert.Contains(t, report.TrunksDiverged[0].Fields, "contact")

	repaired, err := h.reconciler(t).Repair(ctx, report)
	require.NoError(t, err)
	assert.Equal(t, 1, repaired)

	after, err := h.reconciler(t).Reconcile(ctx)
	require.NoError(t, err)
	assert.True(t, after.Clean(), "%+v", after)
	var password string
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx, `SELECT password FROM ps_auths WHERE id = $1`, identifier).Scan(&password))
	assert.Equal(t, carrierSecret, password, "repair keeps the secret the engine holds")
}

func TestReconcile_OrphanedTrunkRemoved(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")
	ctx := context.Background()
	require.NoError(t, h.pool.WithTenant(ctx, h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM carrier_trunks WHERE id = $1`, v.ID.String())
		return err
	}))

	report, err := h.reconciler(t).Reconcile(ctx)
	require.NoError(t, err)
	assert.Equal(t, []shareddomain.TrunkID{v.ID}, report.OrphanedTrunks)
	assert.Empty(t, report.Unattributable, "a trunk row is attributable, not unknown")

	_, err = h.reconciler(t).Repair(ctx, report)
	require.NoError(t, err)
	assert.Zero(t, h.engineRows(t, v.ID))
}

// A lost secret is the one drift repair cannot fix: the platform holds no
// copy. It is reported apart, and repair does not pretend otherwise.
func TestReconcile_LostSecretIsReportedNotRepaired(t *testing.T) {
	h := newHarness(t)
	v := h.createTrunk(t, "Primary")
	ctx := context.Background()
	_, err := h.pool.Unwrap().Exec(ctx, `UPDATE ps_auths SET password = NULL WHERE id = $1`,
		domain.TrunkEndpointIdentifier(v.ID))
	require.NoError(t, err)

	report, err := h.reconciler(t).Reconcile(ctx)
	require.NoError(t, err)
	assert.False(t, report.Clean())
	assert.False(t, report.Repairable(), "nothing repair could do")
	require.Len(t, report.SecretsMissing, 1)
	assert.Equal(t, v.ID, report.SecretsMissing[0].TrunkID)

	// Supplying the secret again through the API is the fix.
	_, err = h.svc.UpdateTrunk(h.ctx, ports.UpdateTrunkCommand{TenantID: h.tenant, ID: v.ID, Name: "Primary",
		Host: "sip.carrier.example", Port: 5060, AuthUsername: "acct-1001", Secret: "re-supplied", ChannelLimit: 30, Priority: 1})
	require.NoError(t, err)
	after, err := h.reconciler(t).Reconcile(ctx)
	require.NoError(t, err)
	assert.True(t, after.Clean(), "%+v", after)
}
