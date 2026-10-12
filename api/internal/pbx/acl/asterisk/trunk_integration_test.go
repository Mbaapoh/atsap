//go:build integration

package asterisk_test

import (
	"context"
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
	}
}

func (h *harness) projectTrunk(t *testing.T, tr domain.Trunk, secret []byte) {
	t.Helper()
	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
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
