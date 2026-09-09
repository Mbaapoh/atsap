package application_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/licensing/application"
	"atsap-api/internal/licensing/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// TestNoConfigurationCanSkipVerification is task 3.7 and D-54.
//
// The requirement is negative — "there is no setting that turns this
// off" — so it is asserted two ways, because neither alone is enough.
//
// Behaviourally: with no trusted key, everything is refused rather than
// permitted. A design that failed open would light up here.
//
// Structurally: no identifier anywhere in licensing may be named like a
// bypass. That catches the change this test really exists to prevent —
// somebody adding SkipVerify, InsecureSkipVerify, AllowUnsigned or
// DisableLicensing next year, which no behavioural test would notice
// until it was already wired to a flag.
func TestNoConfigurationCanSkipVerification(t *testing.T) {
	ctx := context.Background()

	t.Run("an empty key set refuses rather than permits", func(t *testing.T) {
		// The pathological configuration: nothing is trusted. Fail-open
		// would mean unlimited capacity; fail-closed means Setup.
		r := newRig(t)
		require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(512))))

		svc := application.NewService(r.store, domain.NewKeySet(),
			slog.New(slog.NewTextHandler(io.Discard, nil))).
			WithClock(func() time.Time { return now })

		e, err := svc.Entitlement(ctx)

		require.NoError(t, err)
		assert.Equal(t, domain.ReasonTampered, e.Reason,
			"trusting no key must reject the licence, never wave it through")
		assert.Equal(t, domain.FreeChannels, e.Channels)
	})

	t.Run("a token cannot be applied when nothing is trusted", func(t *testing.T) {
		r := newRig(t)
		svc := application.NewService(r.store, domain.NewKeySet(),
			slog.New(slog.NewTextHandler(io.Discard, nil)))

		err := svc.ApplyLicenseKey(ctx, r.sign(licensed(32)))

		assert.ErrorIs(t, err, domain.ErrUntrusted)
		assert.Zero(t, r.store.saves)
	})

	t.Run("no identifier in licensing is named like a bypass", func(t *testing.T) {
		banned := []string{
			"skipverif", "skipsign", "noverif", "insecure",
			"allowunsigned", "disablelicens", "bypass", "unverified",
		}

		// "unverified" is kept in the list because AllowUnverified would
		// be a genuine bypass, and exempted here for the one identifier
		// that legitimately carries the word: StateUnverified is PRD
		// §11.2's grace state — an entitlement whose daily confirmation
		// has not succeeded — which restricts nothing and skips nothing.
		// Exempting the exact name rather than dropping the term keeps
		// the guard able to catch the thing it is for.
		exempt := map[string]bool{"StateUnverified": true}

		var found []string
		fset := token.NewFileSet()
		root := filepath.Join("..", "..", "licensing")
		require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			ast.Inspect(file, func(n ast.Node) bool {
				ident, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if exempt[ident.Name] {
					return true
				}
				lower := strings.ToLower(ident.Name)
				for _, b := range banned {
					if strings.Contains(lower, b) {
						found = append(found, path+": "+ident.Name)
					}
				}
				return true
			})
			return nil
		}))

		assert.Empty(t, found,
			"licensing must contain no identifier that reads like a way to skip verification (D-54): %v", found)
	})
}

// TestVerificationRunsForEveryEntitlementRead closes the gap the
// structural check cannot: that the verifying call is actually reached,
// not merely present. A licence signed by a key the service does not
// hold must never produce usable capacity, however the service was
// constructed.
func TestVerificationRunsForEveryEntitlementRead(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	require.NoError(t, r.svc.ApplyLicenseKey(ctx, r.sign(licensed(64))))

	// Re-sign the same claims with a key nobody trusts and write it
	// straight into the store, as an attacker with database access would.
	_, foreignPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	payload, err := json.Marshal(licensed(100000))
	require.NoError(t, err)
	r.store.lic.SignedPayload = payload
	r.store.lic.Signature = ed25519.Sign(foreignPriv, payload)

	svc := application.NewService(r.store, r.keys, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now })

	v, err := svc.ValidateCapacity(ctx, shareddomain.NewTenantID(), "call-1", 1)

	require.NoError(t, err)
	assert.True(t, v.Permitted, "the floor still permits a call — degrading never disables")
	e, err := svc.Entitlement(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.FreeChannels, e.Channels,
		"a foreign-signed licence grants the floor, not the 100000 channels it claims")
}
