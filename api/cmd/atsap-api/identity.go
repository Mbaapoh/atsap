package main

import (
	"context"
	"fmt"
	"log/slog"

	"atsap-api/internal/config"
	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	"atsap-api/internal/mail"
	corepostgres "atsap-api/internal/postgres"
)

// newIdentity builds the identity service the same way for the server,
// bootstrap and the admin CLI: one token issuer with the current and any
// previous signing keys (D-66), the Postgres store, and mail when SMTP is
// configured (design D10).
func newIdentity(cfg config.Config, pool *corepostgres.Pool, tenantCap identityports.TenantCap, logger *slog.Logger) (*identityapp.Service, *identitypostgres.Store, error) {
	key, err := identityapp.TokenKeyFromHex(cfg.JWTPrivateKeyHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid ATSAPBX_JWT_KEY: %w", err)
	}
	issuer, err := identityapp.NewTokenIssuer(key, identityapp.DefaultTokenLifetime)
	if err != nil {
		return nil, nil, fmt.Errorf("build token issuer: %w", err)
	}
	previous, err := identityapp.PreviousKeysFromHex(cfg.JWTPreviousKeysHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid ATSAPBX_JWT_PREVIOUS_KEYS: %w", err)
	}
	if issuer, err = issuer.WithPreviousKeys(previous...); err != nil {
		return nil, nil, fmt.Errorf("invalid ATSAPBX_JWT_PREVIOUS_KEYS: %w", err)
	}

	store := identitypostgres.NewStore(pool)
	svc := identityapp.NewService(identityapp.StoresOf(store), tenantCap, identityapp.NewPasswordHasher(), issuer, logger)

	var mailer identityports.Mailer
	if cfg.SMTPAddr != "" {
		sender, err := mail.New(mail.Config{
			Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, User: cfg.SMTPUser, Password: cfg.SMTPPassword,
			Insecure: cfg.SMTPInsecure,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("smtp: %w", err)
		}
		mailer = sender
		if cfg.SMTPInsecure {
			logger.Warn("ATSAPBX_SMTP_INSECURE is set: mail may be sent without TLS. Dev rig only.")
		}
	} else {
		logger.Warn("ATSAPBX_SMTP_ADDR is not set: password-reset and invitation mail will not be sent")
	}
	return svc.WithMailer(mailer, cfg.ConsoleURL), store, nil
}

// noTenantCap is for operator commands that never provision past the
// first tenant.
var noTenantCap = identityports.TenantCapFunc(func(context.Context) (int, error) { return 0, nil })
