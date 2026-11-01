//go:build integration

package asterisk_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

const carrierSecret = "carrier-s3cret-value"

func sampleTrunk() domain.Trunk {
	id := shareddomain.NewTrunkID()
	return domain.Trunk{
		ID: id, TenantID: shareddomain.NewTenantID(), Name: "carrier",
		Host: "sip.carrier.example", Port: 5080, AuthUsername: "acct-1001",
		CredentialRef: domain.TrunkCredentialRef(id), ChannelLimit: 30, Health: domain.HealthHealthy,
		// What the platform last resolved the name to (D-75).
		ResolvedAddresses: []netip.Addr{netip.MustParseAddr("2001:db8::10"), netip.MustParseAddr("203.0.113.10")},
	}
}

// sampleMatch is sampleTrunk's part of the ingress match: its addresses,
// IPv4 first.
const sampleMatch = "203.0.113.10,2001:db8::10"

// ingressMatch reads the shared carrier-ingress identify row (D-77).
func (h *harness) ingressMatch(t *testing.T) (string, bool) {
	t.Helper()
	var match string
	err := h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT "match" FROM ps_endpoint_id_ips WHERE id = $1`, domain.CarrierIngressEndpoint).Scan(&match)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return match, true
}

// trunkIdentifyRows counts identify rows of the trunk's own: none since
// D-77.
func (h *harness) trunkIdentifyRows(t *testing.T, id shareddomain.TrunkID) int {
	t.Helper()
	var n int
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM ps_endpoint_id_ips WHERE id = $1`, domain.TrunkEndpointIdentifier(id)).Scan(&n))
	return n
}

// saveTrunk writes the trunk's domain row, as the service does before it
// projects: the ingress is built from every trunk's recorded addresses.
func (h *harness) saveTrunk(ctx context.Context, tx pgx.Tx, tr domain.Trunk) error {
	if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, 'carriers') ON CONFLICT (id) DO NOTHING`, tr.TenantID.String()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tr.TenantID.String()); err != nil {
		return err
	}
	addrs := tr.ResolvedAddresses
	if addrs == nil {
		addrs = []netip.Addr{}
	}
	ranges := tr.InboundAddresses
	if ranges == nil {
		ranges = []netip.Prefix{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO carrier_trunks (id, tenant_id, name, host, port, resolved_addresses, inbound_addresses)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET host = EXCLUDED.host, port = EXCLUDED.port,
			resolved_addresses = EXCLUDED.resolved_addresses, inbound_addresses = EXCLUDED.inbound_addresses`,
		tr.ID.String(), tr.TenantID.String(), tr.Name, tr.Host, tr.Port, addrs, ranges)
	return err
}

// D-77: a trunk projects no identify row of its own; its addresses are
// recognised by the shared ingress, rebuilt with every write. A name with
// no address adds nothing; a check that resolved it adds what it found; a
// loopback recorded before D-76 is never there.
func TestProjectTrunk_RecognitionIsTheIngress(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	tr.ResolvedAddresses = nil
	h.projectTrunk(t, tr, []byte(carrierSecret))
	assert.Zero(t, h.trunkIdentifyRows(t, tr.ID), "no identify row of its own")
	_, ok := h.ingressMatch(t)
	assert.False(t, ok, "a name not resolved yet adds nothing; with no trunk recognised, no ingress row")
	assert.True(t, h.readTrunk(t, tr.ID).endpoint, "the trunk's endpoint is still projected, for calls out")

	tr.ResolvedAddresses = []netip.Addr{netip.MustParseAddr("203.0.113.20"), netip.MustParseAddr("127.0.0.1")}
	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		if err := h.saveTrunk(ctx, tx, tr); err != nil {
			return err
		}
		return h.projector.ProjectTrunkIdentify(ctx, tx, tr)
	}))
	match, ok := h.ingressMatch(t)
	require.True(t, ok)
	assert.Equal(t, "203.0.113.20", match, "a check's result reaches the ingress; loopback never does")

	tr.Host, tr.ResolvedAddresses = "198.51.100.7", domain.AddressesForHost("198.51.100.7")
	h.projectTrunk(t, tr, nil)
	match, _ = h.ingressMatch(t)
	assert.Equal(t, "198.51.100.7", match, "an IP host is its own address")
}

// The ingress is the union across tenants; removing a trunk takes its part
// out; reconcile compares the ingress with every trunk's addresses, never
// looking a name up, and repair rebuilds it.
func TestProjectIngress_IsTheUnionAcrossTenants(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a := sampleTrunk()
	b := sampleTrunk() // another tenant
	b.ResolvedAddresses = nil
	b.InboundAddresses = []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}
	h.projectTrunk(t, a, []byte(carrierSecret))
	h.projectTrunk(t, b, []byte(carrierSecret))

	match, ok := h.ingressMatch(t)
	require.True(t, ok)
	assert.Equal(t, "198.51.100.0/24,"+sampleMatch, match)
	var setVar *string
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx, `SELECT set_var FROM ps_endpoints WHERE id = $1`, domain.CarrierIngressEndpoint).Scan(&setVar))
	assert.Nil(t, setVar, "the ingress never names a tenant")
	drift, err := h.projector.DiffIngress(ctx)
	require.NoError(t, err)
	assert.Empty(t, drift)

	for _, tampered := range []string{"sip.carrier.example", sampleMatch, "198.51.100.0/24," + sampleMatch + ",192.0.2.1"} {
		_, err := h.pool.Unwrap().Exec(ctx, `UPDATE ps_endpoint_id_ips SET "match" = $2 WHERE id = $1`, domain.CarrierIngressEndpoint, tampered)
		require.NoError(t, err)
		drift, err := h.projector.DiffIngress(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"identify"}, drift, "a match of %q is drift", tampered)
		require.NoError(t, h.projector.EnsureIngress(ctx))
		drift, err = h.projector.DiffIngress(ctx)
		require.NoError(t, err)
		assert.Empty(t, drift, "repair rebuilds the union")
	}

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.TenantID.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM carrier_trunks WHERE id = $1`, b.ID.String()); err != nil {
			return err
		}
		return h.projector.RemoveTrunk(ctx, tx, b.ID)
	}))
	match, _ = h.ingressMatch(t)
	assert.Equal(t, sampleMatch, match, "a removed trunk's ranges leave the ingress with it")
}

// A per-trunk identify row left from before D-77 is drift of that trunk;
// projecting the trunk removes it, and startup removes them all.
func TestProjectIngress_LeftoverTrunkRowsAreRemoved(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))
	insertLeftover := func() {
		_, err := h.pool.Unwrap().Exec(ctx, `INSERT INTO ps_endpoint_id_ips (id, endpoint, "match") VALUES ($1, $1, '203.0.113.10')`, domain.TrunkEndpointIdentifier(tr.ID))
		require.NoError(t, err)
	}
	insertLeftover()
	drift, err := h.projector.DiffTrunk(ctx, tr)
	require.NoError(t, err)
	assert.Equal(t, []string{"identify"}, drift)
	h.projectTrunk(t, tr, nil)
	assert.Zero(t, h.trunkIdentifyRows(t, tr.ID), "re-projection removes it")

	insertLeftover()
	require.NoError(t, h.projector.EnsureIngress(ctx))
	assert.Zero(t, h.trunkIdentifyRows(t, tr.ID), "startup removes every one")
	_, err = h.pool.Unwrap().Exec(ctx, `DELETE FROM ps_endpoint_id_ips WHERE id = $1`, domain.CarrierIngressEndpoint)
	require.NoError(t, err)
	drift, err = h.projector.DiffIngress(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"identify"}, drift, "a missing ingress row is drift")
}

func (h *harness) projectTrunk(t *testing.T, tr domain.Trunk, secret []byte) {
	t.Helper()
	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		if err := h.saveTrunk(ctx, tx, tr); err != nil {
			return err
		}
		return h.projector.ProjectTrunk(ctx, tx, tr, secret)
	}))
}

type trunkRows struct {
	endpoint, aor, auth                    bool
	transport, dialplanContext, fromDomain string
	inboundAuth, outboundAuth, fromUser    *string
	contact, authType, username, password  *string
}

func (h *harness) readTrunk(t *testing.T, id shareddomain.TrunkID) trunkRows {
	t.Helper()
	ctx := context.Background()
	identifier := domain.TrunkEndpointIdentifier(id)
	var r trunkRows
	err := h.pool.Unwrap().QueryRow(ctx, `SELECT transport, context, from_domain, auth, outbound_auth, from_user
		FROM ps_endpoints WHERE id = $1`, identifier).
		Scan(&r.transport, &r.dialplanContext, &r.fromDomain, &r.inboundAuth, &r.outboundAuth, &r.fromUser)
	r.endpoint = err == nil
	r.aor = h.pool.Unwrap().QueryRow(ctx, `SELECT contact FROM ps_aors WHERE id = $1`, identifier).Scan(&r.contact) == nil
	r.auth = h.pool.Unwrap().QueryRow(ctx, `SELECT auth_type, username, password FROM ps_auths WHERE id = $1`, identifier).
		Scan(&r.authType, &r.username, &r.password) == nil
	return r
}

func TestProjectTrunk_WritesAnOutboundCarrierEndpoint(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))

	identifier := domain.TrunkEndpointIdentifier(tr.ID)
	r := h.readTrunk(t, tr.ID)
	require.True(t, r.endpoint && r.aor && r.auth, "endpoint, aor and auth rows all written")

	assert.Equal(t, "sip:sip.carrier.example:5080", *r.contact, "calls go to the carrier's static contact")
	assert.Nil(t, r.inboundAuth, "the carrier is not challenged by us: `auth` stays empty")
	assert.Equal(t, identifier, *r.outboundAuth, "we present outbound_auth TO the carrier")
	assert.Equal(t, "acct-1001", *r.fromUser, "From user is the auth username (FreePBX default)")
	assert.Equal(t, "sip.carrier.example", r.fromDomain, "From domain is the carrier host")
	assert.Equal(t, "userpass", *r.authType)
	assert.Equal(t, "acct-1001", *r.username)
	assert.Equal(t, carrierSecret, *r.password, "the engine must be able to present the secret (LLD-03 §7.4)")
}

func TestProjectTrunk_IPAuthenticatedTrunkHasNoAuthState(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	tr.AuthUsername, tr.CredentialRef = "", ""
	h.projectTrunk(t, tr, nil)

	r := h.readTrunk(t, tr.ID)
	assert.True(t, r.endpoint && r.aor)
	assert.False(t, r.auth, "no credential, no auth row")
	assert.Nil(t, r.outboundAuth)
	assert.Nil(t, r.fromUser)
}

// An update that supplies no secret keeps the projected one; one that
// supplies a secret replaces it; clearing the username removes the auth
// state with the secret in it.
func TestProjectTrunk_SecretIsKeptReplacedAndDropped(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))

	tr.Host = "sip2.carrier.example"
	h.projectTrunk(t, tr, nil)
	r := h.readTrunk(t, tr.ID)
	assert.Equal(t, carrierSecret, *r.password, "no secret supplied: the projected one is kept")
	assert.Equal(t, "sip2.carrier.example", r.fromDomain, "while everything else follows the update")

	h.projectTrunk(t, tr, []byte("rotated-secret"))
	assert.Equal(t, "rotated-secret", *h.readTrunk(t, tr.ID).password)

	tr.AuthUsername, tr.CredentialRef = "", ""
	h.projectTrunk(t, tr, nil)
	r = h.readTrunk(t, tr.ID)
	assert.False(t, r.auth, "switching to IP auth removes the stored secret with the auth row")
	assert.Nil(t, r.outboundAuth)
}

func TestRemoveTrunk_LeavesNoEngineState(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.RemoveTrunk(ctx, tx, tr.ID)
	}))
	r := h.readTrunk(t, tr.ID)
	assert.False(t, r.endpoint || r.aor || r.auth, "a removed trunk leaves nothing for the engine to use")
}

// The §7.4 exception is for trunks only. With a trunk's secret projected,
// every extension auth row still carries no plaintext, and the secret
// appears in no engine row other than the trunk's own ps_auths.password.
func TestProjectTrunk_SecretAppearsNowhereElse(t *testing.T) {
	h := newHarness(t)
	ext := newExtension("1000", domain.DeviceSIP)
	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))

	ctx := context.Background()
	var extensionPasswords int
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT count(*) FROM ps_auths WHERE id LIKE 'e\_%' AND password IS NOT NULL`).Scan(&extensionPasswords))
	assert.Zero(t, extensionPasswords, "no extension auth row may hold a plaintext password")

	for _, q := range []string{
		`SELECT count(*) FROM ps_endpoints WHERE ps_endpoints::text LIKE '%' || $1 || '%' AND $2::text IS NOT NULL`,
		`SELECT count(*) FROM ps_aors WHERE ps_aors::text LIKE '%' || $1 || '%' AND $2::text IS NOT NULL`,
		`SELECT count(*) FROM ps_auths WHERE ps_auths::text LIKE '%' || $1 || '%' AND id <> $2`,
	} {
		var n int
		require.NoError(t, h.pool.Unwrap().QueryRow(ctx, q, carrierSecret, domain.TrunkEndpointIdentifier(tr.ID)).Scan(&n))
		assert.Zero(t, n, "secret found by %s", q)
	}
}

func TestDiffTrunk(t *testing.T) {
	h := newHarness(t)
	tr := sampleTrunk()
	h.projectTrunk(t, tr, []byte(carrierSecret))
	identifier := domain.TrunkEndpointIdentifier(tr.ID)
	ctx := context.Background()

	drift, err := h.projector.DiffTrunk(ctx, tr)
	require.NoError(t, err)
	assert.Empty(t, drift, "a fresh projection matches")

	_, err = h.pool.Unwrap().Exec(ctx, `UPDATE ps_aors SET contact = 'sip:attacker.example:5060' WHERE id = $1`, identifier)
	require.NoError(t, err)
	_, err = h.pool.Unwrap().Exec(ctx, `UPDATE ps_auths SET password = NULL WHERE id = $1`, identifier)
	require.NoError(t, err)
	drift, err = h.projector.DiffTrunk(ctx, tr)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"contact", "secret"}, drift)

	_, err = h.pool.Unwrap().Exec(ctx, `DELETE FROM ps_endpoints WHERE id = $1`, identifier)
	require.NoError(t, err)
	drift, err = h.projector.DiffTrunk(ctx, tr)
	require.NoError(t, err)
	assert.Equal(t, []string{"missing"}, drift)

	inv, err := h.projector.ListProjected(ctx)
	require.NoError(t, err)
	assert.Empty(t, inv.Unattributable, "trunk rows are attributable, not unknown")
}
