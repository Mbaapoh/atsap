package application_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	shareddomain "atsap-api/internal/shared/domain"
)

var tokenNow = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func newTestIssuer(t *testing.T) (*application.TokenIssuer, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	issuer, err := application.NewTokenIssuer(priv, application.DefaultTokenLifetime)
	require.NoError(t, err)
	return issuer.WithClock(func() time.Time { return tokenNow }), priv
}

func TestTokenIssuer_IssueAndValidate(t *testing.T) {
	issuer, _ := newTestIssuer(t)
	tenantID := shareddomain.NewTenantID()
	principalID := shareddomain.NewPrincipalID()

	token, err := issuer.Issue(tenantID, principalID, []string{"TENANT_ADMIN"})
	require.NoError(t, err)

	claims, err := issuer.Validate(token)
	require.NoError(t, err)

	assert.Equal(t, tenantID.String(), claims.TenantID)
	assert.Equal(t, principalID.String(), claims.PrincipalID)
	assert.Equal(t, []string{"TENANT_ADMIN"}, claims.Roles)
	assert.Equal(t, tokenNow.Add(application.DefaultTokenLifetime).Unix(), claims.ExpiresAt)
}

func TestNewTokenIssuer_RejectsBadKey(t *testing.T) {
	_, err := application.NewTokenIssuer(ed25519.PrivateKey("too-short"), time.Minute)
	assert.Error(t, err, "a malformed signing key must fail at construction, not at first use")
}

func TestNewTokenIssuer_DefaultsLifetime(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	issuer, err := application.NewTokenIssuer(priv, 0)
	require.NoError(t, err)

	fixed := issuer.WithClock(func() time.Time { return tokenNow })
	token, err := fixed.Issue(shareddomain.NewTenantID(), shareddomain.NewPrincipalID(), nil)
	require.NoError(t, err)

	claims, err := fixed.Validate(token)
	require.NoError(t, err)
	assert.Equal(t, tokenNow.Add(application.DefaultTokenLifetime).Unix(), claims.ExpiresAt)
}

// TestTokenIssuer_RejectsAlgNone is the headline algorithm-confusion
// case: the classic JWT attack is to strip the signature and set
// `alg: none`, betting the validator reads `alg` to decide how to
// verify. This one does not (LLD-02 §10.7, DoD item 12).
func TestTokenIssuer_RejectsAlgNone(t *testing.T) {
	issuer, _ := newTestIssuer(t)

	forged := forgeToken(t, map[string]any{"alg": "none", "typ": "JWT"}, application.Claims{
		TenantID:    shareddomain.NewTenantID().String(),
		PrincipalID: shareddomain.NewPrincipalID().String(),
		ExpiresAt:   tokenNow.Add(time.Hour).Unix(),
	}, nil)

	_, err := issuer.Validate(forged)
	assert.ErrorIs(t, err, application.ErrTokenAlgorithmNotAllowed)
}

// TestTokenIssuer_RejectsDowngradedAlgorithm covers the same class with a
// real algorithm name rather than "none" — an HMAC token whose signature
// an attacker can compute if the validator were to honour the header.
func TestTokenIssuer_RejectsDowngradedAlgorithm(t *testing.T) {
	issuer, _ := newTestIssuer(t)

	for _, alg := range []string{"HS256", "RS256", "ES256", "eddsa", "EdDSA "} {
		t.Run(alg, func(t *testing.T) {
			forged := forgeToken(t, map[string]any{"alg": alg, "typ": "JWT"}, application.Claims{
				TenantID:    shareddomain.NewTenantID().String(),
				PrincipalID: shareddomain.NewPrincipalID().String(),
				ExpiresAt:   tokenNow.Add(time.Hour).Unix(),
			}, []byte("any-signature-at-all"))

			_, err := issuer.Validate(forged)
			assert.ErrorIs(t, err, application.ErrTokenAlgorithmNotAllowed,
				"case and whitespace variants must not slip past the check either")
		})
	}
}

// TestTokenIssuer_RejectsTamperedClaims: re-encoding the payload without
// re-signing must fail. This is what stops a caller editing their own
// token to claim another tenant or a higher role.
func TestTokenIssuer_RejectsTamperedClaims(t *testing.T) {
	issuer, _ := newTestIssuer(t)

	token, err := issuer.Issue(shareddomain.NewTenantID(), shareddomain.NewPrincipalID(), []string{"AGENT"})
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	tampered, err := json.Marshal(application.Claims{
		TenantID:    shareddomain.NewTenantID().String(), // a different tenant
		PrincipalID: shareddomain.NewPrincipalID().String(),
		Roles:       []string{"TENANT_ADMIN"}, // and a better role
		ExpiresAt:   tokenNow.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)

	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(tampered) + "." + parts[2]

	_, err = issuer.Validate(forged)
	assert.ErrorIs(t, err, application.ErrTokenSignatureInvalid)
}

// TestTokenIssuer_RejectsAnotherIssuersToken: a correctly-formed,
// correctly-signed token from a *different* key must not validate here.
func TestTokenIssuer_RejectsAnotherIssuersToken(t *testing.T) {
	issuer, _ := newTestIssuer(t)
	other, _ := newTestIssuer(t)

	token, err := other.Issue(shareddomain.NewTenantID(), shareddomain.NewPrincipalID(), nil)
	require.NoError(t, err)

	_, err = issuer.Validate(token)
	assert.ErrorIs(t, err, application.ErrTokenSignatureInvalid)
}

func TestTokenIssuer_Expiry(t *testing.T) {
	issuer, _ := newTestIssuer(t)

	token, err := issuer.Issue(shareddomain.NewTenantID(), shareddomain.NewPrincipalID(), nil)
	require.NoError(t, err)

	// One second before expiry: still good.
	justBefore := issuer.WithClock(func() time.Time {
		return tokenNow.Add(application.DefaultTokenLifetime - time.Second)
	})
	_, err = justBefore.Validate(token)
	assert.NoError(t, err)

	// Exactly at expiry: refused. Boundary pinned deliberately so a
	// later refactor cannot quietly turn `>=` into `>`.
	atExpiry := issuer.WithClock(func() time.Time {
		return tokenNow.Add(application.DefaultTokenLifetime)
	})
	_, err = atExpiry.Validate(token)
	assert.ErrorIs(t, err, application.ErrTokenExpired)
}

func TestTokenIssuer_NotYetValid(t *testing.T) {
	issuer, _ := newTestIssuer(t)

	token, err := issuer.Issue(shareddomain.NewTenantID(), shareddomain.NewPrincipalID(), nil)
	require.NoError(t, err)

	earlier := issuer.WithClock(func() time.Time { return tokenNow.Add(-time.Minute) })
	_, err = earlier.Validate(token)
	assert.ErrorIs(t, err, application.ErrTokenNotYetValid)
}

func TestTokenIssuer_Malformed(t *testing.T) {
	issuer, _ := newTestIssuer(t)

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one segment", "abc"},
		{"two segments", "abc.def"},
		{"four segments", "a.b.c.d"},
		{"header not base64", "!!!.eyJ0aWQiOiJ4In0.sig"},
		{"header not json", base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".e30.sig"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := issuer.Validate(tt.token)
			assert.Error(t, err)
		})
	}
}

// TestTokenIssuer_RejectsBadSegmentsAfterAlgCheck covers the paths that
// only run once the algorithm has been accepted.
func TestTokenIssuer_RejectsBadSegmentsAfterAlgCheck(t *testing.T) {
	issuer, _ := newTestIssuer(t)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT"}`))

	t.Run("signature not base64", func(t *testing.T) {
		_, err := issuer.Validate(header + ".e30.!!!")
		assert.ErrorIs(t, err, application.ErrTokenMalformed)
	})

	t.Run("claims not json but validly signed", func(t *testing.T) {
		// Sign a payload that decodes as base64 but not as JSON, so the
		// signature passes and the claims decode is what fails.
		_, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		local, err := application.NewTokenIssuer(priv, time.Hour)
		require.NoError(t, err)

		payload := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
		signingInput := header + "." + payload
		sig := ed25519.Sign(priv, []byte(signingInput))
		token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

		_, err = local.Validate(token)
		assert.ErrorIs(t, err, application.ErrTokenMalformed)
	})
}

// forgeToken builds a token from arbitrary header/claims without access
// to the issuer's key — the position an attacker is in.
func forgeToken(t *testing.T, header map[string]any, claims application.Claims, signature []byte) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	require.NoError(t, err)
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)

	return base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON) + "." +
		base64.RawURLEncoding.EncodeToString(signature)
}
