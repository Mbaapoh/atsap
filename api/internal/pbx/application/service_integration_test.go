//go:build integration

package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityapp "atsap-api/internal/identity/application"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	identitypg "atsap-api/internal/identity/postgres"
	"atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpg "atsap-api/internal/pbx/postgres"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

const testRealm = "asterisk"

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
}

// fakeAuthorizer records what was asked and can be told to refuse, so a
// test can prove authorization happens BEFORE anything is written rather
// than inferring it from a happy path.
type fakeAuthorizer struct {
	deny  error
	calls []string
}

func (f *fakeAuthorizer) AuthorizeAction(_ context.Context, _ shareddomain.PrincipalID, action, resource string) error {
	f.calls = append(f.calls, action+" on "+resource)
	return f.deny
}

// failingProjector wraps the real one and fails on demand, so atomicity
// can be proven rather than assumed.
type failingProjector struct {
	ports.EndpointProjector
	failProject error
	failRemove  error
}

func (f *failingProjector) ProjectExtension(ctx context.Context, tx pgx.Tx, e domain.Extension) error {
	if f.failProject != nil {
		return f.failProject
	}
	return f.EndpointProjector.ProjectExtension(ctx, tx, e)
}

func (f *failingProjector) RemoveExtension(ctx context.Context, tx pgx.Tx, id shareddomain.ExtensionID) error {
	if f.failRemove != nil {
		return f.failRemove
	}
	return f.EndpointProjector.RemoveExtension(ctx, tx, id)
}

type harness struct {
	pool      *corepostgres.Pool
	svc       *application.Service
	authz     *fakeAuthorizer
	projector *failingProjector
	tenant    shareddomain.TenantID
	ctx       context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	url := testDatabaseURL(t)
	_ = corepostgres.MigrateDownAll(url, migrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(url, migrationsDir(t)))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(url, migrationsDir(t)) })

	pool, err := corepostgres.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	identityStore := identitypg.NewStore(pool)
	tenant := identitydomain.Tenant{
		ID:            shareddomain.NewTenantID(),
		Name:          "Acme",
		Status:        identitydomain.TenantActive,
		ResidencyZone: "EU",
	}
	require.NoError(t, identityStore.CreateTenant(context.Background(), tenant))

	authz := &fakeAuthorizer{}
	projector := &failingProjector{EndpointProjector: asterisk.NewProjector(asterisk.Config{
		Realm:           testRealm,
		SIPTransport:    "transport-udp",
		WebRTCTransport: "transport-wss",
	}, pool)}

	svc := application.NewService(pool, pbxpg.NewStore(), projector, authz, identityStore, testRealm)

	ctx := identityapp.WithTenantContext(context.Background(), &identityports.TenantContext{
		TenantID:    tenant.ID,
		PrincipalID: shareddomain.NewPrincipalID(),
	})

	return &harness{pool: pool, svc: svc, authz: authz, projector: projector, tenant: tenant.ID, ctx: ctx}
}

func (h *harness) create(t *testing.T, number string) ports.CreatedExtension {
	t.Helper()
	got, err := h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: number, DisplayName: "Ext " + number, DeviceType: domain.DeviceSIP,
	})
	require.NoError(t, err)
	return got
}

// countRow counts rows on tables with NO row-level security — the ps_*
// projection. It uses the raw pool deliberately.
func (h *harness) countRow(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// countTenantRow counts rows on RLS-protected tables (extensions,
// audit_logs). Those are invisible without a tenant context — which is
// RLS doing its job, and was worth discovering here rather than in
// production: an unscoped read simply returns nothing rather than
// failing, so a test that forgets the context quietly asserts against an
// empty set.
func (h *harness) countTenantRow(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, args...).Scan(&n)
	}))
	return n
}

// --- 5.1: domain row, projection and audit in one transaction --------

func TestCreateExtension_WritesDomainProjectionAndAudit(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")

	id := domain.EndpointIdentifier(created.Extension.ID)

	assert.Equal(t, 1, h.countTenantRow(t, `SELECT count(*) FROM extensions WHERE id = $1`, created.Extension.ID.String()))
	assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`, id))
	assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_auths WHERE id = $1`, id))
	assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_aors WHERE id = $1`, id))
	assert.Equal(t, 1, h.countTenantRow(t,
		`SELECT count(*) FROM audit_logs WHERE resource_id = $1 AND action = 'extension.created'`,
		created.Extension.ID.String()))

	// The generated secret is returned once and is a real credential for
	// the digest that was stored.
	require.NotEmpty(t, created.Secret)
	var digest string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT md5_cred FROM ps_auths WHERE id = $1`, id).Scan(&digest))
	assert.Equal(t, domain.HA1(created.Extension.AuthUsername, testRealm, created.Secret), digest,
		"the projected digest must be the HA1 of the secret handed to the caller, or the device cannot register")

	assert.Equal(t, domain.StatusNotRegistered, created.Extension.Registration)
	assert.Contains(t, h.authz.calls[0], "extension.create", "authorization must precede the write")
}

func TestCreateExtension_RefusedWithoutPermission(t *testing.T) {
	h := newHarness(t)
	h.authz.deny = identityports.ErrPermissionDenied

	_, err := h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: "1000", DisplayName: "Nope", DeviceType: domain.DeviceSIP,
	})
	require.ErrorIs(t, err, identityports.ErrPermissionDenied)
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM extensions`), "a refused call must write nothing")
	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_endpoints`))
}

func TestCreateExtension_RejectsInvalidNumber(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: "10*", DisplayName: "Bad", DeviceType: domain.DeviceSIP,
	})
	require.ErrorIs(t, err, ports.ErrInvalidInput)
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM extensions`))
}

// --- 5.2: atomicity, both directions ---------------------------------

func TestCreateExtension_ProjectionFailureLeavesNoDomainRow(t *testing.T) {
	h := newHarness(t)
	h.projector.failProject = errors.New("engine unavailable")

	_, err := h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: "1000", DisplayName: "Ext", DeviceType: domain.DeviceSIP,
	})
	require.Error(t, err)

	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM extensions`),
		"a failed projection must leave no extension: the console must never show one no phone can register to")
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM audit_logs`),
		"and no audit record of something that did not happen")
}

func TestCreateExtension_DomainFailureLeavesNoProjection(t *testing.T) {
	h := newHarness(t)
	first := h.create(t, "1000")

	// A duplicate number fails the domain write; the projection for it
	// must not survive. Any extra ps_* row would be an orphan the engine
	// would honour and the platform would not know about.
	before := h.countRow(t, `SELECT count(*) FROM ps_endpoints`)
	_, err := h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: "1000", DisplayName: "Duplicate", DeviceType: domain.DeviceSIP,
	})
	require.ErrorIs(t, err, ports.ErrNumberTaken)

	assert.Equal(t, before, h.countRow(t, `SELECT count(*) FROM ps_endpoints`),
		"a failed domain write must leave no engine state behind")
	assert.Equal(t, 1, h.countTenantRow(t, `SELECT count(*) FROM extensions`))
	_ = first
}

// --- 5.3: update, delete, rotation -----------------------------------

func TestUpdateExtension_ReprojectsAndAudits(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	require.NoError(t, h.svc.UpdateExtension(h.ctx, ports.UpdateExtensionCommand{
		TenantID: h.tenant, ID: created.Extension.ID,
		DisplayName: "Renamed", DeviceType: domain.DeviceWebRTC,
	}))

	got, err := h.svc.GetExtension(h.ctx, h.tenant, created.Extension.ID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.DisplayName)

	var transport string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT transport FROM ps_endpoints WHERE id = $1`, id).Scan(&transport))
	assert.Equal(t, "transport-wss", transport,
		"a device-type change the engine never learns about is a change that did not happen")

	assert.Equal(t, 1, h.countTenantRow(t,
		`SELECT count(*) FROM audit_logs WHERE action = 'extension.updated'`))
}

func TestDeleteExtension_RemovesDomainAndProjection(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	require.NoError(t, h.svc.DeleteExtension(h.ctx, h.tenant, created.Extension.ID))

	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM extensions WHERE id = $1`, created.Extension.ID.String()))
	for _, table := range []string{"ps_endpoints", "ps_auths", "ps_aors"} {
		assert.Zero(t, h.countRow(t, `SELECT count(*) FROM `+table+` WHERE id = $1`, id),
			"%s must be deprovisioned with the extension", table)
	}
	assert.Equal(t, 1, h.countTenantRow(t, `SELECT count(*) FROM audit_logs WHERE action = 'extension.deleted'`))
}

// The projection is only reached after RLS authorized the domain delete
// (design D9). Deleting an id the tenant cannot see must therefore leave
// the engine untouched — this is the cross-tenant deprovisioning hole,
// asserted at the layer that closes it.
func TestDeleteExtension_UnknownIdLeavesProjectionIntact(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	err := h.svc.DeleteExtension(h.ctx, h.tenant, shareddomain.NewExtensionID())
	require.ErrorIs(t, err, ports.ErrNotFound)

	assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`, id),
		"a delete that removed no domain row must never reach the unguarded projection")
}

func TestRegenerateSecret_ReplacesTheCredential(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")
	id := domain.EndpointIdentifier(created.Extension.ID)

	rotated, err := h.svc.RegenerateSecret(h.ctx, h.tenant, created.Extension.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rotated.Secret)
	assert.NotEqual(t, created.Secret, rotated.Secret)

	var digest string
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT md5_cred FROM ps_auths WHERE id = $1`, id).Scan(&digest))
	assert.Equal(t, domain.HA1(created.Extension.AuthUsername, testRealm, rotated.Secret), digest,
		"the new secret must authenticate")
	assert.NotEqual(t, domain.HA1(created.Extension.AuthUsername, testRealm, created.Secret), digest,
		"and the old one must not — a rotation exists to end two live credentials, not create them")
}

// --- 5.4: listing carries registration state -------------------------

func TestListExtensions_AttachesRegistrationStatus(t *testing.T) {
	h := newHarness(t)
	registered := h.create(t, "1000")
	_ = h.create(t, "1001")

	// Standing in for Asterisk, which writes contacts itself.
	_, err := h.pool.Unwrap().Exec(context.Background(),
		`INSERT INTO ps_contacts (id, endpoint, uri, expiration_time)
		 VALUES ($1, $2, $3, extract(epoch from now())::bigint + 3600)`,
		"c1", domain.EndpointIdentifier(registered.Extension.ID), "sip:x@10.0.0.1:5060")
	require.NoError(t, err)

	views, err := h.svc.ListExtensions(h.ctx, h.tenant, ports.Page{Limit: 10})
	require.NoError(t, err)
	require.Len(t, views, 2)

	byNumber := map[string]ports.ExtensionView{}
	for _, v := range views {
		byNumber[v.Number] = v
	}
	assert.Equal(t, domain.StatusRegistered, byNumber["1000"].Registration)
	assert.Equal(t, domain.StatusNotRegistered, byNumber["1001"].Registration,
		"an extension with no contact defaults to not registered rather than being omitted")
}

// --- 5.5: no audit row carries credential material -------------------

func TestAuditRows_NeverContainCredentialMaterial(t *testing.T) {
	h := newHarness(t)
	created := h.create(t, "1000")

	require.NoError(t, h.svc.UpdateExtension(h.ctx, ports.UpdateExtensionCommand{
		TenantID: h.tenant, ID: created.Extension.ID,
		DisplayName: "Renamed", DeviceType: domain.DeviceSIP,
	}))
	rotated, err := h.svc.RegenerateSecret(h.ctx, h.tenant, created.Extension.ID)
	require.NoError(t, err)
	require.NoError(t, h.svc.DeleteExtension(h.ctx, h.tenant, created.Extension.ID))

	var states []string
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT coalesce(before_state::text, '') || coalesce(after_state::text, '') FROM audit_logs`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			states = append(states, s)
		}
		return rows.Err()
	}))

	var digest string
	n := 0
	for _, state := range states {
		n++

		assert.NotContains(t, state, created.Secret, "the generated plaintext must never be audited")
		assert.NotContains(t, state, rotated.Secret, "nor a rotated one")
		digest = domain.HA1(created.Extension.AuthUsername, testRealm, created.Secret)
		assert.NotContains(t, state, digest, "nor the stored digest, which is password-equivalent")
		assert.NotContains(t, strings.ToLower(state), "secret_digest",
			"the field must be absent by construction, not merely empty")
	}
	assert.GreaterOrEqual(t, n, 4, "create, update, rotate and delete must each be audited")
}
