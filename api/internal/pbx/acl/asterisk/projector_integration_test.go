//go:build integration

package asterisk_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/domain"
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
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

type harness struct {
	pool      *corepostgres.Pool
	projector *asterisk.Projector
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

	return &harness{
		pool: pool,
		projector: asterisk.NewProjector(asterisk.Config{
			Realm:           testRealm,
			SIPTransport:    "transport-udp",
			WebRTCTransport: "transport-wss",
		}, pool),
	}
}

// inTx runs fn in a plain transaction. The projection tables carry no
// RLS, so no tenant context is needed to reach them — which is exactly
// why the write path is gated upstream (design D9).
func (h *harness) inTx(t *testing.T, fn func(ctx context.Context, tx pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := h.pool.Unwrap().Begin(ctx)
	require.NoError(t, err)
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func newExtension(number string, device domain.DeviceType) domain.Extension {
	id := shareddomain.NewExtensionID()
	username := domain.EndpointIdentifier(id)
	return domain.Extension{
		ID:           id,
		TenantID:     shareddomain.NewTenantID(),
		Number:       number,
		DisplayName:  "Test " + number,
		AuthUsername: username,
		SecretDigest: domain.HA1(username, testRealm, "a-generated-secret"),
		DeviceType:   device,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

// TestProjectExtension_WritesAllThreeRows covers task 4.2.
func TestProjectExtension_WritesAllThreeRows(t *testing.T) {
	h := newHarness(t)
	ext := newExtension("1000", domain.DeviceSIP)
	id := domain.EndpointIdentifier(ext.ID)

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))

	ctx := context.Background()

	var (
		transport, aors, auth, context_, allow string
	)
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT transport, aors, auth, context, allow FROM ps_endpoints WHERE id = $1`, id,
	).Scan(&transport, &aors, &auth, &context_, &allow))
	assert.Equal(t, "transport-udp", transport)
	assert.Equal(t, id, aors, "the endpoint points at its own aor")
	assert.Equal(t, id, auth, "the endpoint points at its own auth")
	assert.Equal(t, "stasis-in", context_,
		"every endpoint enters Stasis — pbx-core generates no dialplan (D-47)")
	assert.Equal(t, "ulaw,alaw", allow)

	// The credential assertion that matters: the HA1 is stored and the
	// plaintext password column is empty. A recoverable secret here would
	// undo the whole reason for auth_type=md5 (LLD-03 §7.3).
	var (
		authType, username, digest, realm string
		password                          *string
	)
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT auth_type, username, md5_cred, realm, password FROM ps_auths WHERE id = $1`, id,
	).Scan(&authType, &username, &digest, &realm, &password))
	assert.Equal(t, "md5", authType)
	assert.Equal(t, id, username, "the SIP username is the endpoint identifier, not a second value")
	assert.Equal(t, ext.SecretDigest, digest)
	assert.Equal(t, testRealm, realm, "a realm mismatch yields a credential that can never authenticate")
	assert.Nil(t, password, "no plaintext password may ever be written")

	var maxContacts int
	var removeExisting string
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT max_contacts, remove_existing FROM ps_aors WHERE id = $1`, id,
	).Scan(&maxContacts, &removeExisting))
	assert.Equal(t, 1, maxContacts)
	assert.Equal(t, "yes", removeExisting,
		"a fresh REGISTER must replace a stale contact, not be refused")
}

// Projection is an upsert, so re-projecting a changed extension must
// leave the engine holding exactly what the domain says — not a
// duplicate, and not the stale value.
func TestProjectExtension_IsIdempotentAndUpdatesInPlace(t *testing.T) {
	h := newHarness(t)
	ext := newExtension("1000", domain.DeviceSIP)
	id := domain.EndpointIdentifier(ext.ID)

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))

	ext.DeviceType = domain.DeviceWebRTC
	ext.SecretDigest = domain.HA1(id, testRealm, "a-rotated-secret")
	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.ProjectExtension(ctx, tx, ext)
	}))

	ctx := context.Background()
	var count int
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT count(*) FROM ps_endpoints WHERE id = $1`, id).Scan(&count))
	assert.Equal(t, 1, count, "re-projecting must update in place, never duplicate")

	var transport, digest string
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT transport FROM ps_endpoints WHERE id = $1`, id).Scan(&transport))
	require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
		`SELECT md5_cred FROM ps_auths WHERE id = $1`, id).Scan(&digest))
	assert.Equal(t, "transport-wss", transport, "the device type change must reach the engine")
	assert.Equal(t, ext.SecretDigest, digest, "a rotated credential must replace the old one")
}

// TestRemoveExtension_DeletesAllThreeRows covers task 4.3.
func TestRemoveExtension_DeletesAllThreeRows(t *testing.T) {
	h := newHarness(t)
	keep := newExtension("1000", domain.DeviceSIP)
	drop := newExtension("1001", domain.DeviceSIP)

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		if err := h.projector.ProjectExtension(ctx, tx, keep); err != nil {
			return err
		}
		return h.projector.ProjectExtension(ctx, tx, drop)
	}))

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		return h.projector.RemoveExtension(ctx, tx, drop.ID)
	}))

	ctx := context.Background()
	droppedID := domain.EndpointIdentifier(drop.ID)
	keptID := domain.EndpointIdentifier(keep.ID)

	for _, table := range []string{"ps_endpoints", "ps_auths", "ps_aors"} {
		var gone, kept int
		require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE id = $1`, droppedID).Scan(&gone))
		assert.Zero(t, gone, "%s must no longer hold the removed extension", table)

		require.NoError(t, h.pool.Unwrap().QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE id = $1`, keptID).Scan(&kept))
		assert.Equal(t, 1, kept, "%s must still hold the extension that was not removed", table)
	}
}

// The projection must not be observable without the domain write it rode
// in with. A rolled-back transaction leaves the engine untouched — this
// is design D1's guarantee seen from the projector's side.
func TestProjectExtension_RollsBackWithItsTransaction(t *testing.T) {
	h := newHarness(t)
	ext := newExtension("1000", domain.DeviceSIP)

	err := h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		if err := h.projector.ProjectExtension(ctx, tx, ext); err != nil {
			return err
		}
		return assert.AnError // force a rollback the way a failing domain write would
	})
	require.ErrorIs(t, err, assert.AnError)

	var count int
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM ps_endpoints WHERE id = $1`,
		domain.EndpointIdentifier(ext.ID)).Scan(&count))
	assert.Zero(t, count, "a rolled-back transaction must leave no engine state behind")
}

// TestRegistrationStatus covers task 4.4: one query for many extensions,
// expiry respected, and unregistered ids simply absent.
func TestRegistrationStatus(t *testing.T) {
	h := newHarness(t)
	registered := newExtension("1000", domain.DeviceSIP)
	expired := newExtension("1001", domain.DeviceSIP)
	never := newExtension("1002", domain.DeviceSIP)

	require.NoError(t, h.inTx(t, func(ctx context.Context, tx pgx.Tx) error {
		for _, e := range []domain.Extension{registered, expired, never} {
			if err := h.projector.ProjectExtension(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	}))

	ctx := context.Background()
	// Contacts are written by Asterisk itself; standing in for it here.
	_, err := h.pool.Unwrap().Exec(ctx,
		`INSERT INTO ps_contacts (id, endpoint, uri, expiration_time) VALUES ($1, $2, $3, $4)`,
		"c-live", domain.EndpointIdentifier(registered.ID), "sip:x@10.0.0.1:5060",
		time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)

	_, err = h.pool.Unwrap().Exec(ctx,
		`INSERT INTO ps_contacts (id, endpoint, uri, expiration_time) VALUES ($1, $2, $3, $4)`,
		"c-stale", domain.EndpointIdentifier(expired.ID), "sip:y@10.0.0.2:5060",
		time.Now().Add(-time.Hour).Unix())
	require.NoError(t, err)

	status, err := h.projector.RegistrationStatus(ctx,
		[]shareddomain.ExtensionID{registered.ID, expired.ID, never.ID})
	require.NoError(t, err)

	assert.Equal(t, domain.StatusRegistered, status[registered.ID])

	// Asterisk prunes expired contacts lazily, so presence alone is not
	// registration — a phone unplugged a week ago must not read as live.
	_, staleReported := status[expired.ID]
	assert.False(t, staleReported, "an expired contact must not count as registered")

	_, neverReported := status[never.ID]
	assert.False(t, neverReported, "an extension with no contact is simply absent from the result")
}

func TestRegistrationStatus_EmptyInputDoesNotQuery(t *testing.T) {
	h := newHarness(t)
	status, err := h.projector.RegistrationStatus(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, status)
}
