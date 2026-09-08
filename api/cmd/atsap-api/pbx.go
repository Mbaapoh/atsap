// pbx subcommands: operator tools for the pbx-core context.
//
// `atsap-api pbx reconcile` compares the media engine's configuration
// against the platform's records and reports any divergence; `--fix`
// repairs it, treating the platform as the sole source of truth.
//
// It is deliberately operator-invoked and never scheduled (design D6).
// Configuration writes are transactional, so drift caused by our own code
// cannot occur; any drift that does appear is either a bug or a human
// editing the database directly, and repairing that silently on a timer
// would hide both.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"atsap-api/internal/config"
	"atsap-api/internal/logging"
	pbxasterisk "atsap-api/internal/pbx/acl/asterisk"
	pbxapp "atsap-api/internal/pbx/application"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	corepostgres "atsap-api/internal/postgres"
)

// Exit codes. A divergent estate exits non-zero even when the command
// itself succeeded, so a monitoring job can tell "checked, and it is
// wrong" from "checked, and it is fine" without parsing output.
const (
	exitOK        = 0
	exitDiverged  = 1
	exitCommandNo = 2
	exitFailed    = 3
)

func runPbx(cfg config.Config) int {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: atsap-api pbx reconcile [--fix]")
		return exitCommandNo
	}
	if os.Args[2] != "reconcile" {
		fmt.Fprintf(os.Stderr, "unknown pbx subcommand %q; expected: reconcile\n", os.Args[2])
		return exitCommandNo
	}

	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	fix := fs.Bool("fix", false, "repair divergence by making the engine match the platform")
	if err := fs.Parse(os.Args[3:]); err != nil {
		return exitCommandNo
	}

	logger := logging.New(cfg.LogLevel)
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open database pool", "error", err)
		return exitFailed
	}
	defer pool.Close()

	projector := pbxasterisk.NewProjector(pbxasterisk.Config{
		Realm:           cfg.SIPRealm,
		SIPTransport:    cfg.SIPTransport,
		WebRTCTransport: cfg.SIPWebRTCTransport,
	}, pool)
	reconciler := pbxapp.NewReconciler(pool, pbxpostgres.NewStore(), projector, projector)

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		logger.Error("reconcile failed", "error", err)
		return exitFailed
	}

	printReport(report)

	if report.Clean() {
		return exitOK
	}
	if !*fix {
		fmt.Println("\nNothing was changed. Re-run with --fix to repair.")
		return exitDiverged
	}

	repaired, err := reconciler.Repair(ctx, report)
	if err != nil {
		logger.Error("repair failed", "error", err, "repaired_before_failure", repaired)
		return exitFailed
	}
	fmt.Printf("\nRepaired %d item(s).\n", repaired)

	// Re-run rather than assume: a repair that reports success while
	// leaving the estate divergent is exactly what this command exists to
	// catch, and it should catch it in itself too.
	after, err := reconciler.Reconcile(ctx)
	if err != nil {
		logger.Error("verification pass failed", "error", err)
		return exitFailed
	}
	switch {
	case after.Clean():
		fmt.Println("Verified: the engine now matches the platform.")
		return exitOK

	case len(after.Diverged) == 0 && len(after.Orphaned) == 0:
		// Everything repairable was repaired. What is left is rows this
		// platform did not write, which repair deliberately never touches —
		// saying "divergence remains" without that distinction reads like a
		// failure and sends an operator hunting for a bug that is not there.
		fmt.Printf("\nRepair complete. %d engine row(s) remain that this platform did not write;\n"+
			"they are left alone by design and need a human decision:\n", len(after.Unattributable))
		for _, id := range after.Unattributable {
			fmt.Printf("  UNKNOWN   %q\n", id)
		}
		// Still non-zero: an unexplained row in the engine is something an
		// operator should keep being told about until they deal with it.
		return exitDiverged

	default:
		fmt.Println("\nDivergence REMAINS after repair — this is a bug in reconciliation:")
		printReport(after)
		return exitDiverged
	}
}

func printReport(r pbxapp.Report) {
	fmt.Printf("Checked %d extension(s).\n", r.Checked)

	if r.Clean() {
		fmt.Println("No divergence: the engine matches the platform.")
		return
	}

	for _, d := range r.Diverged {
		fmt.Printf("  DIVERGED  extension %s (%s, tenant %s): %v\n",
			d.Number, d.ExtensionID, d.TenantID, d.Fields)
	}
	for _, id := range r.Orphaned {
		fmt.Printf("  ORPHANED  projected endpoint for extension %s, which no longer exists\n", id)
	}
	for _, id := range r.Unattributable {
		fmt.Printf("  UNKNOWN   engine row %q was not written by this platform; left alone\n", id)
	}
}
