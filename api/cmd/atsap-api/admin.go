// Admin subcommands: operator-only recovery run on the server itself,
// never reachable over the network (D-66 §3).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"atsap-api/internal/config"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/logging"
	corepostgres "atsap-api/internal/postgres"
)

func runAdmin(cfg config.Config) int {
	if len(os.Args) < 3 || os.Args[2] != "reset-password" {
		fmt.Fprintln(os.Stderr, "usage: atsap-api admin reset-password -email <address>")
		return 2
	}
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	email := fs.String("email", "", "the account to recover (required)")
	if err := fs.Parse(os.Args[3:]); err != nil || *email == "" {
		fmt.Fprintln(os.Stderr, "usage: atsap-api admin reset-password -email <address>")
		return 2
	}
	ctx := context.Background()
	pool, err := corepostgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open the database:", err)
		return 1
	}
	defer pool.Close()
	return adminResetPassword(ctx, cfg, pool, *email, os.Stdout, os.Stderr)
}

// adminResetPassword is the recovery itself, separated from flag parsing
// so an integration test can drive it. The temporary password is written
// once to out and nowhere else — the logger never sees it.
func adminResetPassword(ctx context.Context, cfg config.Config, pool *corepostgres.Pool, email string, out, errOut io.Writer) int {
	logger := logging.New(cfg.LogLevel)
	svc, _, err := newIdentity(cfg, pool, noTenantCap, logger)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "cannot start identity:", err)
		return 1
	}
	temporary, err := svc.RecoverByEmail(ctx, email)
	if errors.Is(err, identityports.ErrNotFound) {
		_, _ = fmt.Fprintln(errOut, "no account with that email")
		return 1
	}
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "recovery failed:", err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "Temporary password (shown once; it must be changed at next sign-in):\n%s\n", temporary)
	return 0
}
