// Command atsap-api is telephony-core's composition root: it wires
// config, Postgres, NATS, and the Asterisk ACL together into a running
// CallService, per LLD-01 §2's package layout and task 9.2. This is the
// one place in the module allowed to import both the ACL adapters and
// their concrete infrastructure drivers directly — everywhere else
// depends on ports, never on acl or a specific driver
// (docs/hld/01-architecture.md §1.2).
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	natsgo "github.com/nats-io/nats.go"

	"atsap-api/internal/config"
	atsapbxv1connect "atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	"atsap-api/internal/logging"
	atsapnats "atsap-api/internal/nats"
	corepostgres "atsap-api/internal/postgres"
	"atsap-api/internal/server"
	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/acl/ami"
	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/application"
	telephonypostgres "atsap-api/internal/telephony/postgres"
	"atsap-api/internal/telephony/rpc"
)

// outboxPollInterval is how often the outbox worker polls for
// unpublished rows. A dev-appropriate default; not yet configurable —
// no need has arisen for this walking skeleton.
const outboxPollInterval = 2 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Logger isn't built yet (it needs cfg.LogLevel); a bare
		// stderr message is the correct fallback for a config error
		// that happens before anything else can start.
		_, _ = os.Stderr.WriteString("failed to load config: " + err.Error() + "\n")
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Postgres: app pool (RLS-scoped, atsapbx_app) ---
	appPool, err := corepostgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open app database pool", "error", err)
		os.Exit(1)
	}
	defer appPool.Close()

	if err := corepostgres.MigrateUp(cfg.DatabaseURL, "migrations"); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// --- Postgres: outbox worker pool (BYPASSRLS, atsap_outbox_worker).
	// A raw *pgxpool.Pool, not the tenant-scoped Pool wrapper: this role
	// deliberately bypasses RLS to read across every tenant's outbox
	// rows (docs/hld/03-domain-model.md §5) — WithTenant's per-transaction
	// SET LOCAL app.tenant_id would be meaningless for it.
	workerPGPool, err := pgxpool.New(ctx, cfg.DatabaseWorkerURL)
	if err != nil {
		logger.Error("failed to open outbox worker database pool", "error", err)
		os.Exit(1)
	}
	defer workerPGPool.Close()

	// --- NATS + outbox worker ---
	nc, err := natsgo.Connect(cfg.NATSURL)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	natsPublisher, err := atsapnats.NewPublisher(ctx, nc)
	if err != nil {
		logger.Error("failed to create NATS publisher", "error", err)
		os.Exit(1)
	}

	outboxWorker := corepostgres.NewOutboxWorker(workerPGPool, natsPublisher, logger)
	go outboxWorker.Run(ctx, outboxPollInterval)

	// --- Asterisk ACL ---
	ariClient := ari.New(cfg.ARIURL, cfg.ARIUsername, cfg.ARIPassword, cfg.ARIAppName)
	amiClient := ami.New(cfg.AMIAddr, cfg.AMIUsername, cfg.AMIPassword, func(msg ami.Message) {
		logger.Info("ami event", "event", msg["Event"], "channel", msg["Channel"])
	}, logger)

	registry := acl.NewCorrelationRegistry()
	mediaGateway := acl.NewMediaGatewayAdapter(ariClient)
	callStore := telephonypostgres.NewCallStore(appPool)

	callService := application.NewService(
		mediaGateway,
		callStore,
		application.NewAlwaysPermitLicense(),    // LLD-02 replaces this
		application.NewAlwaysPermitCompliance(), // a later change replaces this
		registry,
		20, // TimeoutSeconds, PRD §11.1 Presenting default
		logger,
	)

	eventLoop := acl.NewEventLoop(registry, callService, logger)

	go func() {
		for {
			if err := amiClient.Connect(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Warn("ami: connection lost, retrying", "error", err)
				time.Sleep(5 * time.Second)
				continue
			}
		}
	}()

	go func() {
		err := ariClient.StreamEvents(ctx, func(ev ari.Event) {
			eventLoop.Handle(ctx, ev)
		}, logger)
		if err != nil && ctx.Err() == nil {
			logger.Error("ari: event stream stopped", "error", err)
		}
	}()

	// --- Public API ---
	telephonyHandler := rpc.NewTelephonyHandler(callStore)
	rpcPath, rpcHandler := atsapbxv1connect.NewTelephonyServiceHandler(telephonyHandler)

	checkers := []server.Checker{
		{Name: "postgres", Check: func(ctx context.Context) error { return appPool.Unwrap().Ping(ctx) }},
		{Name: "nats", Check: func(context.Context) error {
			if nc.Status() != natsgo.CONNECTED {
				return context.DeadlineExceeded
			}
			return nil
		}},
	}

	httpSrv := server.NewWithHandlers(cfg.HTTPAddr, checkers, map[string]http.Handler{
		rpcPath: rpcHandler,
	})
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil {
			logger.Info("http server stopped", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	_ = server.Shutdown(context.Background(), httpSrv, 10*time.Second)
	_ = amiClient.Close()
}
