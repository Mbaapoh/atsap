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
	"atsap-api/internal/identity/bootstrap"
	"atsap-api/internal/logging"
	corepostgres "atsap-api/internal/postgres"
)

func runBootstrap(cfg config.Config) int {
	logger := logging.New(cfg.LogLevel)

	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	name := fs.String("name", "", "tenant name (required)")
	residency := fs.String("residency", "EU", "tenant residency zone")
	email := fs.String("email", "", "administrator email; they sign in with it (required)")
	displayName := fs.String("display-name", "", "administrator display name")
	password := fs.String("password", "", "administrator password (required; 8-64 chars)")
	username := fs.String("username", "", "removed: administrators sign in with their email")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}
	if *username != "" {
		fmt.Fprintln(os.Stderr, "-username was removed: people sign in with their email now (D-61). Use -email.")
		return 2
	}
	if *name == "" || *email == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: atsap-api bootstrap -name <tenant> -email <admin email> -password <pw> [-display-name n] [-residency zone]")
		return 2
	}

	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open app database", "error", err)
		return 1
	}
	defer pool.Close()

	// Bootstrap provisions the first tenant only, and bootstrap.Run
	// refuses outright if any tenant exists. No cap can refuse the first
	// tenant (zero is unlimited, and every non-zero cap is at least one),
	// so bootstrap does not consult the licence: a fresh installation is
	// in Setup and must still be able to create its administrator.
	svc, store, err := newIdentity(cfg, pool, noTenantCap, logger)
	if err != nil {
		logger.Error("bootstrap failed", "error", err)
		return 1
	}

	tenantID, err := bootstrap.Run(ctx, store, svc, bootstrap.Params{
		TenantName:    *name,
		ResidencyZone: *residency,
		Email:         *email,
		DisplayName:   *displayName,
		Password:      *password,
	})
	if err != nil {
		logger.Error("bootstrap failed", "error", err)
		return 1
	}

	logger.Info("installation bootstrapped",
		"tenant_id", tenantID.String(),
		"tenant", *name)
	return 0
}
