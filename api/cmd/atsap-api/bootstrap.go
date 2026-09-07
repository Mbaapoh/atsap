// Bootstrap subcommand: creates the first tenant and platform
// administrator of an empty installation (identity-api task 3.3).
//
// It runs against the database with the operator's own credentials and
// refuses once any tenant exists. It is not reachable over the network —
// provisioning needs a token, a token needs a principal, and nothing may
// mint a second administrator through this path.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"atsap-api/internal/config"
	identityapp "atsap-api/internal/identity/application"
	"atsap-api/internal/identity/bootstrap"
	identitypostgres "atsap-api/internal/identity/postgres"
	"atsap-api/internal/logging"
	corepostgres "atsap-api/internal/postgres"
)

func runBootstrap(cfg config.Config) int {
	logger := logging.New(cfg.LogLevel)

	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	name := fs.String("name", "", "tenant name (required)")
	residency := fs.String("residency", "EU", "tenant residency zone")
	username := fs.String("username", "", "administrator username (required)")
	email := fs.String("email", "", "administrator email")
	password := fs.String("password", "", "administrator password (required; min 8 chars)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}
	if *name == "" || *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: atsap-api bootstrap -name <tenant> -username <admin> -password <pw> [-email e] [-residency zone]")
		return 2
	}

	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open app database", "error", err)
		return 1
	}
	defer pool.Close()

	key, err := identityapp.TokenKeyFromHex(cfg.JWTPrivateKeyHex)
	if err != nil {
		logger.Error("invalid ATSAPBX_JWT_KEY", "error", err)
		return 1
	}
	issuer, err := identityapp.NewTokenIssuer(key, identityapp.DefaultTokenLifetime)
	if err != nil {
		logger.Error("failed to build token issuer", "error", err)
		return 1
	}

	store := identitypostgres.NewStore(pool)
	svc := identityapp.NewService(store, store, store, store, store,
		identityapp.NewPasswordHasher(), issuer, logger)

	tenantID, err := bootstrap.Run(ctx, store, svc, bootstrap.Params{
		TenantName:    *name,
		ResidencyZone: *residency,
		Username:      *username,
		Email:         *email,
		Password:      *password,
	})
	if err != nil {
		logger.Error("bootstrap failed", "error", err)
		return 1
	}

	logger.Info("installation bootstrapped",
		"tenant_id", tenantID.String(),
		"tenant", *name,
		"administrator", *username)
	return 0
}
