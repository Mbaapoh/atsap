//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/licensing/application"
	"atsap-api/internal/licensing/domain"
	"atsap-api/internal/licensing/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// activated returns a service over a real database, already activated
// with a licence for the given capacity, plus the log buffer so a test
// can assert what was and was not written.
func activated(t *testing.T, capacity int, at time.Time) (*application.Service, *bytes.Buffer, ports.StoredLicense) {
	t.Helper()
	store, _ := newStore(t)

	tok := coreLicence()
	tok.Capacity = capacity
	lic, keys := signedLicence(t, tok)
	lic.LastConfirmedAt = at
	require.NoError(t, store.Save(context.Background(), lic))

	logs := &bytes.Buffer{}
	svc := application.NewService(store, keys,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))).
		WithClock(func() time.Time { return at })

	return svc, logs, lic
}

// TestCapacityIsInstallationScopedNotTenantScoped is task 5.1.
//
// One licence governs one installed instance however many tenants it
// serves (T-1). A per-tenant counter would let an Operator with fifty
// tenants run fifty times the channels they bought, which is the whole
// commercial model inverted.
func TestCapacityIsInstallationScopedNotTenantScoped(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := activated(t, 2, now)

	tenantA, tenantB := shareddomain.NewTenantID(), shareddomain.NewTenantID()

	first, err := svc.ValidateCapacity(ctx, tenantA, "call-a", 1)
	require.NoError(t, err)
	require.True(t, first.Permitted)

	second, err := svc.ValidateCapacity(ctx, tenantB, "call-b", 1)
	require.NoError(t, err)
	require.True(t, second.Permitted, "a second tenant draws on the same entitlement")

	// The third is refused whichever tenant asks — including a brand-new
	// one, which a per-tenant counter would have admitted.
	for name, tenant := range map[string]shareddomain.TenantID{
		"tenant A again": tenantA,
		"tenant B again": tenantB,
		"a third tenant": shareddomain.NewTenantID(),
	} {
		t.Run(name, func(t *testing.T) {
			v, err := svc.ValidateCapacity(ctx, tenant, "call-"+name, 1)
			require.NoError(t, err)
			assert.False(t, v.Permitted, "capacity is installation-wide, not per tenant")
			assert.Equal(t, domain.ReasonOverCapacity, v.Reason)
		})
	}
}

// TestBurstBehaviourAgainstARealLicence is task 5.2.
func TestBurstBehaviourAgainstARealLicence(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := activated(t, 32, now) // burst allowance 3

	for i := 0; i < 32; i++ {
		v, err := svc.ValidateCapacity(ctx, shareddomain.NewTenantID(), fmt.Sprintf("call-%d", i), 1)
		require.NoError(t, err)
		require.True(t, v.Permitted)
	}

	t.Run("a short overage is absorbed", func(t *testing.T) {
		v, err := svc.ValidateCapacity(ctx, shareddomain.NewTenantID(), "call-over", 1)
		require.NoError(t, err)
		assert.True(t, v.Permitted, "a busy hour must not be penalised (BRD §10.2)")
	})

	t.Run("a sustained overage is refused with its own reason", func(t *testing.T) {
		// The same service, seen from beyond the burst window.
		svc.WithClock(func() time.Time { return now.Add(domain.BurstWindow) })

		v, err := svc.ValidateCapacity(ctx, shareddomain.NewTenantID(), "call-later", 1)

		require.NoError(t, err)
		assert.False(t, v.Permitted)
		assert.Equal(t, domain.ReasonOverCapacity, v.Reason)
	})

	t.Run("the over-capacity reason is distinct from every other refusal", func(t *testing.T) {
		// AC-06.6: over-capacity must be separable in telemetry from a
		// compliance refusal, an unactivated system, and an expired
		// licence. Compliance refusals never carry a licensing reason at
		// all — they come from a different port — so what is asserted
		// here is that licensing's own reasons stay distinct.
		for _, other := range []domain.Reason{
			domain.ReasonNotActivated, domain.ReasonExpired,
			domain.ReasonTampered, domain.ReasonPermitted,
		} {
			assert.NotEqual(t, domain.ReasonOverCapacity, other)
		}
	})
}

// TestRestartResetsTheInUseCount is task 5.3 — design D4's stated
// consequence, recorded rather than fixed.
//
// The count of channels in use is per process (LLD-08 §5, §7). A restart
// while calls are up therefore loses it, and the installation briefly
// permits more concurrent calls than it should. On a single node this
// self-corrects as those calls end; sharing the count across nodes is
// D-08's multi-node work.
//
// Observed here so the behaviour is documented by a test rather than by
// a sentence somebody may not believe.
func TestRestartResetsTheInUseCount(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)

	tok := coreLicence()
	tok.Capacity = 2
	lic, keys := signedLicence(t, tok)
	require.NoError(t, store.Save(ctx, lic))

	newSvc := func() *application.Service {
		return application.NewService(store, keys, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))).
			WithClock(func() time.Time { return now })
	}

	before := newSvc()
	for _, id := range []string{"call-a", "call-b"} {
		v, err := before.ValidateCapacity(ctx, shareddomain.NewTenantID(), id, 1)
		require.NoError(t, err)
		require.True(t, v.Permitted)
	}
	refused, err := before.ValidateCapacity(ctx, shareddomain.NewTenantID(), "call-c", 1)
	require.NoError(t, err)
	require.False(t, refused.Permitted, "the entitlement is full")

	// The process restarts. The two calls are still up on the engine, but
	// nothing in the database recorded that they held channels — the
	// entitlement is persisted, the usage is not.
	after := newSvc()

	v, err := after.ValidateCapacity(ctx, shareddomain.NewTenantID(), "call-d", 1)

	require.NoError(t, err)
	assert.True(t, v.Permitted,
		"OBSERVED, not desired: a restart resets the in-use count, so the installation briefly over-admits. "+
			"Single-node this self-corrects as the pre-restart calls end (design D4, LLD-08 §7). "+
			"If this ever needs fixing, it is the multi-node work D-08 defers, not a bug in this change")
}

// TestGraceEndToEndAgainstARealLicence is task 5.4: the whole window,
// driven by an injected clock through the real store.
func TestGraceEndToEndAgainstARealLicence(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)

	tok := coreLicence()
	tok.Capacity = 64
	lic, keys := signedLicence(t, tok)
	lic.LastConfirmedAt = now
	require.NoError(t, store.Save(ctx, lic))

	at := func(d time.Duration) *application.Service {
		return application.NewService(store, keys, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))).
			WithClock(func() time.Time { return now.Add(d) })
	}

	t.Run("day 0: valid, full capacity", func(t *testing.T) {
		e, err := at(0).Entitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, domain.StateValid, e.State)
		assert.Equal(t, 64, e.Channels)
	})

	t.Run("day 3: unverified, still full capacity", func(t *testing.T) {
		e, err := at(3 * 24 * time.Hour).Entitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, domain.StateUnverified, e.State)
		assert.Equal(t, 64, e.Channels, "the grace period restricts nothing (AC-06.5)")
	})

	t.Run("day 8: degraded to the floor, never disabled", func(t *testing.T) {
		e, err := at(8 * 24 * time.Hour).Entitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, domain.StateDegraded, e.State)
		assert.Equal(t, domain.FreeChannels, e.Channels)
		assert.True(t, e.PermitsCalls(), "degrading never disables (D-12)")
	})

	t.Run("confirmation restores full capacity with no restart", func(t *testing.T) {
		svc := at(9 * 24 * time.Hour)
		degraded, err := svc.Entitlement(ctx)
		require.NoError(t, err)
		require.Equal(t, domain.FreeChannels, degraded.Channels)

		state, err := svc.VerifyDailyEntitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, domain.StateValid, state)

		recovered, err := svc.Entitlement(ctx)
		require.NoError(t, err)
		assert.Equal(t, 64, recovered.Channels, "the same instance sees full capacity again")
	})
}

// TestNoLicenceMaterialInLogsOrErrors is task 5.5 and D-39.
//
// Licence payloads, signatures and fingerprints must not reach a log
// line or an error returned to a caller — on the accept path or the
// reject path. A licence in a diagnostic bundle is a licence in a
// support ticket.
func TestNoLicenceMaterialInLogsOrErrors(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)

	tok := coreLicence()
	tok.Fingerprint = [5]string{"cpu-serial-abc", "mac-de:ad:be:ef", "host-uuid-xyz", "disk-123", "board-456"}
	lic, keys := signedLicence(t, tok)

	logs := &bytes.Buffer{}
	svc := application.NewService(store, keys,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))).
		WithClock(func() time.Time { return now })

	// Accept path.
	token := domain.EncodeToken(lic.SignedPayload, lic.Signature)
	require.NoError(t, svc.ApplyLicenseKey(ctx, token))
	_, err := svc.Entitlement(ctx)
	require.NoError(t, err)

	// Reject path: a forged token, and a stored licence that no longer
	// verifies.
	_, foreignPriv, kerr := ed25519.GenerateKey(nil)
	require.NoError(t, kerr)
	forgedPayload, merr := json.Marshal(tok)
	require.NoError(t, merr)
	forged := domain.EncodeToken(forgedPayload, ed25519.Sign(foreignPriv, forgedPayload))
	applyErr := svc.ApplyLicenseKey(ctx, forged)
	require.Error(t, applyErr)

	emptyKeySvc := application.NewService(store, domain.NewKeySet(),
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))).
		WithClock(func() time.Time { return now })
	_, err = emptyKeySvc.Entitlement(ctx)
	require.NoError(t, err)

	haystack := logs.String() + "\n" + applyErr.Error()

	for name, needle := range map[string]string{
		"the licence token":  token,
		"the signature":      string(lic.Signature),
		"the raw payload":    string(lic.SignedPayload),
		"a fingerprint attr": "cpu-serial-abc",
		"another attr":       "mac-de:ad:be:ef",
	} {
		t.Run("no "+name, func(t *testing.T) {
			assert.NotContains(t, haystack, needle,
				"licence material must never reach a log line or a returned error (D-39, LLD-08 §10)")
		})
	}

	t.Run("the logs are not empty, so this test is not vacuous", func(t *testing.T) {
		assert.NotEmpty(t, strings.TrimSpace(logs.String()),
			"if nothing was logged at all, the assertions above prove nothing")
	})
}

// TestStoreIsUsableFromASecondService covers the ordinary case the
// others build on: two services over one database agree about the
// entitlement, because it is derived from the row rather than held only
// in the process that wrote it.
func TestStoreIsUsableFromASecondService(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	tok := coreLicence()
	lic, keys := signedLicence(t, tok)
	require.NoError(t, store.Save(ctx, lic))

	first, err := application.NewService(store, keys, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))).
		WithClock(func() time.Time { return now }).Entitlement(ctx)
	require.NoError(t, err)

	second, err := application.NewService(store, keys,
		slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))).
		WithClock(func() time.Time { return now }).Entitlement(ctx)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}
