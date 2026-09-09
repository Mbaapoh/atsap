package domain

import (
	"crypto/ed25519"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signedToken mints a licence with a freshly generated keypair.
//
// Tests generate their own keys in-process: no private key material
// exists anywhere in this repository (D-39, D-54). That also means each
// test is independent of the real vendor key, which is held offline.
func signedToken(t *testing.T, tok LicenseToken) (payload, signature []byte, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	payload, err = json.Marshal(tok)
	require.NoError(t, err)
	return payload, ed25519.Sign(priv, payload), pub
}

func TestVerifyToken(t *testing.T) {
	valid := LicenseToken{
		Edition: "core", Capacity: 32, MaxTenants: Unlimited, MaxExtensions: Unlimited,
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour), InstanceID: "instance-1",
	}

	t.Run("a validly signed payload is accepted and interpreted", func(t *testing.T) {
		payload, sig, pub := signedToken(t, valid)

		got, err := VerifyToken(payload, sig, NewKeySet(pub))

		require.NoError(t, err)
		assert.Equal(t, "core", got.Edition)
		assert.Equal(t, 32, got.Capacity)
	})

	t.Run("an altered payload is rejected", func(t *testing.T) {
		payload, sig, pub := signedToken(t, valid)
		tampered := append([]byte{}, payload...)
		// Flip a byte in the middle of the JSON.
		tampered[len(tampered)/2] ^= 0xFF

		_, err := VerifyToken(tampered, sig, NewKeySet(pub))

		assert.ErrorIs(t, err, ErrUntrusted)
	})

	t.Run("a payload signed by an untrusted key is rejected", func(t *testing.T) {
		payload, sig, _ := signedToken(t, valid)
		otherPub, _, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)

		_, err = VerifyToken(payload, sig, NewKeySet(otherPub))

		assert.ErrorIs(t, err, ErrUntrusted)
	})

	t.Run("edge cases are refused before anything is parsed", func(t *testing.T) {
		payload, sig, pub := signedToken(t, valid)
		ks := NewKeySet(pub)

		for name, tc := range map[string]struct{ payload, sig []byte }{
			"empty payload":      {nil, sig},
			"empty signature":    {payload, nil},
			"truncated payload":  {payload[:len(payload)/2], sig},
			"truncated sig":      {payload, sig[:len(sig)-1]},
			"oversized sig":      {payload, append(append([]byte{}, sig...), 0x00)},
			"both empty":         {nil, nil},
			"payload as sig":     {payload, payload},
			"no trusted keys":    {payload, sig},
			"signature all zero": {payload, make([]byte, ed25519.SignatureSize)},
		} {
			t.Run(name, func(t *testing.T) {
				set := ks
				if name == "no trusted keys" {
					set = NewKeySet()
				}
				_, err := VerifyToken(tc.payload, tc.sig, set)
				assert.ErrorIs(t, err, ErrUntrusted)
			})
		}
	})

	t.Run("the rejection does not say which failure it was", func(t *testing.T) {
		payload, sig, pub := signedToken(t, valid)
		otherPub, _, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)

		_, wrongKey := VerifyToken(payload, sig, NewKeySet(otherPub))
		_, malformed := VerifyToken(payload, sig[:4], NewKeySet(pub))

		assert.Equal(t, wrongKey.Error(), malformed.Error(),
			"a wrong key and a malformed signature must be indistinguishable to a caller (LLD-08 §10)")
	})
}

// TestZeroTokenIsNotAnEntitlement closes the hazard that Unlimited == 0
// creates: a LicenseToken nobody verified must never read as an
// unlimited licence. It cannot, because VerifyToken is the only producer
// — this asserts the consequence so a second constructor added later
// fails here (LLD-08 §3).
func TestZeroTokenIsNotAnEntitlement(t *testing.T) {
	var zero LicenseToken

	e := zero.Entitlement(time.Now())

	assert.False(t, e.PermitsCalls(),
		"a zero-valued token must not yield usable capacity even though MaxTenants/MaxExtensions read as Unlimited")
	assert.Zero(t, e.Channels)
}

// TestExpiryDegradesRatherThanDisables is AC-06.5 and D-12 at the token
// level: an expired but validly signed licence still runs.
func TestExpiryDegradesRatherThanDisables(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	tok := LicenseToken{Edition: "core", Capacity: 64, ExpiresAt: now.Add(-time.Second)}

	e := tok.Entitlement(now)

	assert.Equal(t, StateDegraded, e.State)
	assert.Equal(t, ReasonExpired, e.Reason, "an administrator must be told to renew, not to buy channels")
	assert.Equal(t, FreeChannels, e.Channels)
	assert.True(t, e.PermitsCalls(), "expiry never disables (D-12)")
}

func TestExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	t.Run("one second before expiry is still valid", func(t *testing.T) {
		tok := LicenseToken{Capacity: 64, ExpiresAt: now.Add(time.Second)}
		assert.Equal(t, StateValid, tok.Entitlement(now).State)
	})

	t.Run("exactly at expiry is expired", func(t *testing.T) {
		tok := LicenseToken{Capacity: 64, ExpiresAt: now}
		assert.Equal(t, StateDegraded, tok.Entitlement(now).State)
	})

	t.Run("no expiry set means perpetual", func(t *testing.T) {
		tok := LicenseToken{Capacity: 64}
		assert.Equal(t, StateValid, tok.Entitlement(now).State,
			"the Free Community tier is perpetual and carries no expiry (BR-LIC-01)")
	})
}

// TestTrustedKeys_DefaultBuildTrustsExactlyOne is D-54's central
// assertion and risk R-14's mitigation: a binary built with no -ldflags
// trusts the production key and nothing else, so a development-signed
// token is inert in a release.
//
// If this fails, the development key has reached a build that ships.
func TestTrustedKeys_DefaultBuildTrustsExactlyOne(t *testing.T) {
	if developmentPublicKeyHex != "" {
		t.Skip("built with a development key injected; this assertion covers the default build")
	}

	assert.Equal(t, 1, TrustedKeys().Len(),
		"a build with no -ldflags must trust exactly one key (D-54, risk R-14)")
}

// TestDevelopmentSignedTokenIsRejectedByDefaultBuild is the other half:
// not just that the set is small, but that a token signed outside it
// actually fails.
func TestDevelopmentSignedTokenIsRejectedByDefaultBuild(t *testing.T) {
	if developmentPublicKeyHex != "" {
		t.Skip("built with a development key injected")
	}
	payload, sig, _ := signedToken(t, LicenseToken{Edition: "dev", Capacity: 9999})

	_, err := VerifyToken(payload, sig, TrustedKeys())

	assert.ErrorIs(t, err, ErrUntrusted,
		"a token signed by any key this build does not embed must be rejected exactly as a forgery is")
}

// TestTrustedKeys_InjectedBuildTrustsTwo is the positive half of D-54,
// and it only runs when a development key was injected:
//
//	go test -ldflags "-X atsap-api/internal/licensing/domain.developmentPublicKeyHex=<hex>"
//
// Without it, the two tests above would pass on a build where the
// -ldflags mechanism was silently broken — they assert that a key is
// ABSENT, which is also true if injection never works. This asserts the
// mechanism itself functions, so "the dev build trusts an extra key" is
// verified rather than assumed.
func TestTrustedKeys_InjectedBuildTrustsTwo(t *testing.T) {
	if developmentPublicKeyHex == "" {
		t.Skip("no development key injected; this assertion covers the -ldflags build")
	}

	assert.Equal(t, 2, TrustedKeys().Len(),
		"a development build trusts the production key plus the injected one")
}

// TestTrustedKeys_MalformedInjectionDoesNotWiden guards the failure mode
// that matters if someone injects a bad value: a corrupt key must remove
// a key from the set, never admit a broken one that might verify
// something unexpected.
func TestTrustedKeys_MalformedInjectionDoesNotWiden(t *testing.T) {
	assert.Equal(t, 0, NewKeySet(decodeKey("not-hex")).Len(),
		"a malformed key is dropped, not stored")
	assert.Equal(t, 0, NewKeySet(decodeKey("abcd")).Len(),
		"a hex string of the wrong length is dropped, not stored")
	assert.Equal(t, 0, NewKeySet(nil).Len())
}

// TestNoExportedParseWithoutVerify is task 2.3. VerifyToken must remain
// the only exported way to obtain a LicenseToken; adding a ParseToken,
// DecodeToken or similar later would let a caller skip verification by
// choosing a different function, and would do so silently.
//
// Implemented by parsing this package's own AST rather than by
// reflection, because what matters is the shape of the exported API, not
// what happens to be reachable at runtime.
func TestNoExportedParseWithoutVerify(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var producers []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// ImportsOnly is not enough here: the declarations are the point.
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Recv != nil || fn.Type.Results == nil {
				continue
			}
			for _, res := range fn.Type.Results.List {
				if ident, ok := res.Type.(*ast.Ident); ok && ident.Name == "LicenseToken" {
					producers = append(producers, fn.Name.Name)
				}
			}
		}
	}

	assert.Equal(t, []string{"VerifyToken"}, producers,
		"VerifyToken must be the only exported function returning a LicenseToken — anything else is a way to hold an unverified licence")
}

func TestSplitToken(t *testing.T) {
	payload, sig, pub := signedToken(t, LicenseToken{Edition: "core", Capacity: 32})
	compact := EncodeToken(payload, sig)

	t.Run("round-trips through the compact form", func(t *testing.T) {
		gotPayload, gotSig, err := SplitToken(compact)

		require.NoError(t, err)
		assert.Equal(t, payload, gotPayload)
		assert.Equal(t, sig, gotSig)

		tok, err := VerifyToken(gotPayload, gotSig, NewKeySet(pub))
		require.NoError(t, err)
		assert.Equal(t, 32, tok.Capacity)
	})

	t.Run("the compact form is safe to paste anywhere", func(t *testing.T) {
		// No padding, no '+' or '/': a token must survive a URL, a shell
		// argument and a YAML scalar without escaping.
		assert.NotContains(t, compact, "=")
		assert.NotContains(t, compact, "+")
		assert.NotContains(t, compact, "/")
	})

	t.Run("malformed envelopes are refused", func(t *testing.T) {
		for name, token := range map[string]string{
			"empty":                 "",
			"no separator":          "abcdef",
			"empty payload half":    ".abcdef",
			"empty signature half":  "abcdef.",
			"separator only":        ".",
			"payload not base64":    "!!!.abcdef",
			"signature not base64":  "abcdef.!!!",
			"standard base64 chars": "ab+cd/ef.abcdef",
		} {
			t.Run(name, func(t *testing.T) {
				_, _, err := SplitToken(token)
				assert.ErrorIs(t, err, ErrUntrusted)
			})
		}
	})

	t.Run("a well-formed envelope with a bad signature still fails verification", func(t *testing.T) {
		// Splitting succeeding proves nothing about trust — the envelope
		// is encoding, the signature is the control.
		otherPub, _, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)

		p, s, err := SplitToken(compact)
		require.NoError(t, err, "the envelope is well-formed")

		_, err = VerifyToken(p, s, NewKeySet(otherPub))
		assert.ErrorIs(t, err, ErrUntrusted)
	})
}
