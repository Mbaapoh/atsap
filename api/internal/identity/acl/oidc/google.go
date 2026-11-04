// Package oidc implements the installation-wide Google OpenID Connect
// client (D-67). It is generic OIDC so another provider is mostly
// configuration; only Google is shipped and supported.
package oidc

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"atsap-api/internal/identity/ports"
)

// Config is the installation's one Google OAuth client. The secret comes
// from the deployment's secret store (D-83); it is never written to the
// database or a log.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Google is the adapter behind ports.GoogleAuthenticator.
type Google struct {
	issuer   string
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

var _ ports.GoogleAuthenticator = (*Google)(nil)

// New discovers the provider and builds the client. Discovery is a
// network call; the caller decides whether a failure disables Google or
// stops the process.
func New(ctx context.Context, cfg Config) (*Google, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("google OIDC: issuer, client id, client secret and redirect URL are required")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("google OIDC discovery: %w", err)
	}
	return &Google{
		issuer: cfg.Issuer,
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// AuthCodeURL builds the provider redirect. The nonce binds the ID token
// to this request and the verifier is the PKCE challenge.
func (g *Google) AuthCodeURL(state, nonce, verifier, _ string) string {
	return g.oauth2.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
}

// Exchange redeems the code, verifies the ID token and checks the nonce.
func (g *Google) Exchange(ctx context.Context, code, verifier, nonce string) (ports.FederatedAssertion, error) {
	token, err := g.oauth2.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return ports.FederatedAssertion{}, fmt.Errorf("exchange code: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return ports.FederatedAssertion{}, fmt.Errorf("no id_token in the token response")
	}
	idt, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return ports.FederatedAssertion{}, fmt.Errorf("verify id_token: %w", err)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Nonce         string `json:"nonce"`
	}
	if err := idt.Claims(&claims); err != nil {
		return ports.FederatedAssertion{}, fmt.Errorf("read id_token claims: %w", err)
	}
	if claims.Nonce != nonce {
		return ports.FederatedAssertion{}, fmt.Errorf("id_token nonce does not match this request")
	}
	return ports.FederatedAssertion{
		Issuer: idt.Issuer, Subject: idt.Subject,
		Email: claims.Email, EmailVerified: claims.EmailVerified,
	}, nil
}
