package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityoidc "atsap-api/internal/identity/acl/oidc"
)

const (
	clientID = "client-123"
	nonce    = "nonce-abc"
)

// fakeIssuer serves just enough OIDC discovery, JWKS and token endpoint to
// prove the adapter verifies a Google-style ID token, including nonce.
func fakeIssuer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/auth",
			"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig",
		}}}
		_ = json.NewEncoder(w).Encode(set)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithHeader("kid", "k1"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		claims, _ := json.Marshal(map[string]any{
			"iss": srv.URL, "aud": clientID, "sub": "google-sub", "email": "ana@example.com",
			"email_verified": true, "nonce": nonce, "exp": 4102444800, "iat": 1700000000,
		})
		obj, err := signer.Sign(claims)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		raw, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"a","token_type":"Bearer","id_token":%q}`, raw)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, key
}

func TestGoogle_ExchangeVerifiesTheIDToken(t *testing.T) {
	srv, _ := fakeIssuer(t)
	g, err := identityoidc.New(context.Background(), identityoidc.Config{
		Issuer: srv.URL, ClientID: clientID, ClientSecret: "secret", RedirectURL: "http://console.example/auth/google",
	})
	require.NoError(t, err)

	url := g.AuthCodeURL("state-1", nonce, "verifier-1", "/")
	assert.Contains(t, url, "state=state-1")
	assert.Contains(t, url, "code_challenge=")
	assert.Contains(t, url, "nonce="+nonce)

	got, err := g.Exchange(context.Background(), "code-1", "verifier-1", nonce)
	require.NoError(t, err)
	assert.Equal(t, "google-sub", got.Subject)
	assert.Equal(t, "ana@example.com", got.Email)
	assert.True(t, got.EmailVerified)

	_, err = g.Exchange(context.Background(), "code-2", "verifier-1", "other-nonce")
	assert.Error(t, err, "an ID token for another request is refused")
}

func TestGoogle_RequiresCompleteConfiguration(t *testing.T) {
	_, err := identityoidc.New(context.Background(), identityoidc.Config{ClientID: "only"})
	assert.Error(t, err)
}

func TestGoogle_DiscoveryFailureIsAnError(t *testing.T) {
	_, err := identityoidc.New(context.Background(), identityoidc.Config{
		Issuer: "http://127.0.0.1:1/unreachable", ClientID: "c", ClientSecret: "s", RedirectURL: "http://x/auth",
	})
	assert.Error(t, err)
}
