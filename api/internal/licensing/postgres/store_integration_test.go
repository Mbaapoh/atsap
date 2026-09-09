//go:build integration

package postgres_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/licensing/application"
	"atsap-api/internal/licensing/domain"
	"atsap-api/internal/licensing/ports"
	licensingpg "atsap-api/internal/licensing/postgres"
	corepostgres "atsap-api/internal/postgres"
)

var now = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func databaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return wd + "/../../../migrations"
}

// newStore resets the schema and returns a store over a clean database.
func newStore(t *testing.T) (*licensingpg.Store, *pgxpool.Pool) {
	t.Helper()
	url := databaseURL(t)
	_ = corepostgres.MigrateDownAll(url, migrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(url, migrationsDir(t)))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(url, migrationsDir(t)) })

	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return licensingpg.NewStore(pool), pool
}

// signedLicence mints a licence and returns it with the key that
// verifies it. Keys are generated per test: no private key material
// exists in the repository (D-39).
func signedLicence(t *testing.T, tok domain.LicenseToken) (ports.StoredLicense, domain.KeySet) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	payload, err := json.Marshal(tok)
	require.NoError(t, err)

	return ports.StoredLicense{
		InstanceID:      tok.InstanceID,
		SignedPayload:   payload,
		Signature:       ed25519.Sign(priv, payload),
		LastConfirmedAt: now,
		Display: ports.DisplayClaims{
			Edition: tok.Edition, Capacity: tok.Capacity,
			MaxTenants: tok.MaxTenants, MaxExtensions: tok.MaxExtensions,
			Fingerprint: tok.Fingerprint[:], Status: "VALID",
		},
	}, domain.NewKeySet(pub)
}

func coreLicence() domain.LicenseToken {
	return domain.LicenseToken{
		Edition: "core", Capacity: 64,
		MaxTenants: domain.Unlimited, MaxExtensions: domain.Unlimited,
		InstanceID: "11111111-1111-1111-1111-111111111111",
	}
}

func TestStore_NoLicenceIsNotAnError(t *testing.T) {
	store, _ := newStore(t)

	_, err := store.Load(context.Background())

	assert.ErrorIs(t, err, ports.ErrNoLicense,
		"a fresh installation reports Setup, which is a state rather than a failure (D-52)")
}

func TestStore_SaveAndLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	lic, _ := signedLicence(t, coreLicence())

	require.NoError(t, store.Save(ctx, lic))
	got, err := store.Load(ctx)

	require.NoError(t, err)
	assert.Equal(t, lic.SignedPayload, got.SignedPayload)
	assert.Equal(t, lic.Signature, got.Signature)
	assert.WithinDuration(t, lic.LastConfirmedAt, got.LastConfirmedAt, time.Second)
}

func TestStore_SaveIsAnUpsert(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)
	first, _ := signedLicence(t, coreLicence())
	require.NoError(t, store.Save(ctx, first))

	upgraded := coreLicence()
	upgraded.Capacity = 256
	second, _ := signedLicence(t, upgraded)
	second.InstanceID = first.InstanceID
	require.NoError(t, store.Save(ctx, second))

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM licensing_state`).Scan(&rows))
	assert.Equal(t, 1, rows, "an installation holds one licence, not a history")

	got, err := store.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, second.SignedPayload, got.SignedPayload)
}

// TestStore_LoadNeverReturnsTheDisplayCache is D-53 made structural.
//
// The claim columns exist in the table for support and display. Load
// does not select them, and StoredLicense does not carry them back — so
// there is no path from a claim column into an entitlement decision that
// somebody has to remember not to take.
func TestStore_LoadNeverReturnsTheDisplayCache(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	lic, _ := signedLicence(t, coreLicence())
	require.NoError(t, store.Save(ctx, lic))

	got, err := store.Load(ctx)

	require.NoError(t, err)
	assert.Equal(t, ports.DisplayClaims{}, got.Display,
		"Load must not populate Display: the cache is write-only by construction (D-53)")
}

// TestEditingClaimColumnsChangesNoEntitlementDecision is task 3.4a,
// LLD-08 DoD 10 and AC-06.16 — the test whose absence made the exposure
// D-53 closed possible in the first place.
//
// The platform runs on the partner's hardware, so they hold this
// database. Before D-53 the entitlement was a row of parsed claims and a
// single UPDATE granted any capacity, edition or tenant count, without
// forging a signature, patching a binary, or defeating the fingerprint.
func TestEditingClaimColumnsChangesNoEntitlementDecision(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)
	lic, keys := signedLicence(t, coreLicence())
	require.NoError(t, store.Save(ctx, lic))

	svc := application.NewService(store, keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now })

	before, err := svc.Entitlement(ctx)
	require.NoError(t, err)
	require.Equal(t, 64, before.Channels)

	// The edit a partner would actually make.
	_, err = pool.Exec(ctx, `
		UPDATE licensing_state
		   SET capacity = 100000, edition = 'operator', max_tenants = 0, max_extensions = 0`)
	require.NoError(t, err)

	// A fresh Service, so nothing is answered from a cache populated
	// before the edit — this must re-read and re-verify.
	after, err := application.NewService(store, keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now }).
		Entitlement(ctx)

	require.NoError(t, err)
	assert.Equal(t, 64, after.Channels,
		"capacity comes from the signed payload; editing the column grants nothing (D-53)")
	assert.Equal(t, "core", after.Edition, "nor does editing the edition")
	assert.Equal(t, domain.StateValid, after.State,
		"the payload still verifies, so the licence is still valid — only the display cache is now wrong")
}

// TestBreakingTheSignatureDegradesRatherThanHonours is the other half:
// an edit that does invalidate the signature must degrade to the floor
// and report tampering, never honour what the row claimed and never
// disable the installation (BRD §10.2).
func TestBreakingTheSignatureDegradesRatherThanHonours(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)
	lic, keys := signedLicence(t, coreLicence())
	require.NoError(t, store.Save(ctx, lic))

	// Rewrite the payload itself to claim more. The signature no longer
	// matches it, which is exactly what makes this detectable.
	forged, err := json.Marshal(domain.LicenseToken{Edition: "operator", Capacity: 100000})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE licensing_state SET signed_payload = $1`, forged)
	require.NoError(t, err)

	got, err := application.NewService(store, keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now }).
		Entitlement(ctx)

	require.NoError(t, err)
	assert.Equal(t, domain.StateDegraded, got.State)
	assert.Equal(t, domain.ReasonTampered, got.Reason)
	assert.Equal(t, domain.FreeChannels, got.Channels,
		"a tampered licence grants the floor, never the 100000 channels it claimed")
	assert.True(t, got.PermitsCalls(),
		"tampering degrades and never disables — a working phone system is not switched off (D-12)")
}

func TestStore_TouchConfirmedMovesOnlyTheClock(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	lic, _ := signedLicence(t, coreLicence())
	require.NoError(t, store.Save(ctx, lic))

	later := now.Add(72 * time.Hour)
	require.NoError(t, store.TouchConfirmed(ctx, later))

	got, err := store.Load(ctx)
	require.NoError(t, err)
	assert.WithinDuration(t, later, got.LastConfirmedAt, time.Second)
	assert.Equal(t, lic.SignedPayload, got.SignedPayload,
		"confirmation says the licence is still ours; it never changes what the licence grants")
	assert.Equal(t, lic.Signature, got.Signature)
}

// TestStore_HoldsExactlyOneLicence covers a defect found on 2026-09-09.
//
// An installation has one licence (T-1). instance_id being the primary
// key stops the same instance being recorded twice, but not a SECOND
// instance being added alongside the first — and Load reads "the
// licence" with LIMIT 1, so with two rows it picks arbitrarily.
//
// The consequence is nastier than a wrong number: a service holding one
// key reads a row signed by another, fails to verify it, and reports a
// perfectly good licence as TAMPERED. Silent, and it presents as a
// security event rather than a data-model mistake. That is exactly what
// happened when two e2e fixtures each activated their own instance.
func TestStore_HoldsExactlyOneLicence(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)

	first := coreLicence()
	first.InstanceID = "11111111-1111-1111-1111-111111111111"
	licA, keysA := signedLicence(t, first)
	require.NoError(t, store.Save(ctx, licA))

	// A different instance, signed by a different key — the shape that
	// used to produce a second row.
	second := coreLicence()
	second.InstanceID = "22222222-2222-2222-2222-222222222222"
	second.Capacity = 128
	licB, keysB := signedLicence(t, second)
	require.NoError(t, store.Save(ctx, licB))

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM licensing_state`).Scan(&rows))
	assert.Equal(t, 1, rows, "applying a licence supersedes the previous one; it never joins it")

	got, err := store.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, licB.SignedPayload, got.SignedPayload, "the licence in force is the one most recently applied")

	// And it verifies against the key that signed it, rather than
	// reporting as tampered.
	tok, err := domain.VerifyToken(got.SignedPayload, got.Signature, keysB)
	require.NoError(t, err, "the stored licence must verify against its own key")
	assert.Equal(t, 128, tok.Capacity)

	_, err = domain.VerifyToken(got.SignedPayload, got.Signature, keysA)
	assert.Error(t, err, "and must not verify against the superseded licence's key")
}

// TestStore_SingletonConstraintRejectsASecondRow proves the database
// enforces it, not only the write path. A future writer that inserts
// directly must fail rather than quietly create the ambiguity Load
// cannot resolve.
func TestStore_SingletonConstraintRejectsASecondRow(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)
	lic, _ := signedLicence(t, coreLicence())
	require.NoError(t, store.Save(ctx, lic))

	_, err := pool.Exec(ctx, `
		INSERT INTO licensing_state (
			instance_id, signed_payload, signature, fingerprint,
			edition, capacity, max_tenants, max_extensions,
			entitlement_status, last_confirmed_at)
		VALUES ('33333333-3333-3333-3333-333333333333', '\x00', '\x00', '[]',
			'core', 1, 0, 0, 'VALID', NOW())`)

	require.Error(t, err, "the schema must refuse a second licence row")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code,
		"expected unique_violation from licensing_state_singleton, got %s (%s)", pgErr.Code, pgErr.Message)
}
