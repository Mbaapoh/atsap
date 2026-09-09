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

	connectrpc "connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	natsgo "github.com/nats-io/nats.go"

	"atsap-api/internal/config"
	atsapbxv1connect "atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	identityapp "atsap-api/internal/identity/application"
	identitypostgres "atsap-api/internal/identity/postgres"
	identityrpc "atsap-api/internal/identity/rpc"
	"atsap-api/internal/logging"
	atsapnats "atsap-api/internal/nats"
	pbxasterisk "atsap-api/internal/pbx/acl/asterisk"
	pbxapp "atsap-api/internal/pbx/application"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	pbxrpc "atsap-api/internal/pbx/rpc"
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

	// `atsap-api bootstrap` creates the first tenant and administrator of
	// an empty installation. It is the one operator-only path that exists
	// outside the API's token cycle (identity-api task 3.3).
	if len(os.Args) > 1 && os.Args[1] == "bootstrap" {
		os.Exit(runBootstrap(cfg))
	}

	// `atsap-api pbx reconcile` compares the media engine's configuration
	// against the platform's records. Operator-invoked, never scheduled
	// (LLD-03 design D6).
	if len(os.Args) > 1 && os.Args[1] == "pbx" {
		os.Exit(runPbx(cfg))
	}

	logger := logging.New(cfg.LogLevel)

	key, err := identityapp.TokenKeyFromHex(cfg.JWTPrivateKeyHex)
	if err != nil {
		logger.Error("invalid ATSAPBX_JWT_KEY", "error", err)
		os.Exit(1)
	}

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

	licenseManager, err := newLicenseManager(ctx, appPool, cfg.LicenseToken, logger)
	if err != nil {
		logger.Error("licensing", "error", err)
		os.Exit(1)
	}

	callService := application.NewService(
		mediaGateway,
		callStore,
		licenseManager,
		application.NewAlwaysPermitCompliance(), // LLD-04 replaces this
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
	//
	// Every service is mounted WITH the auth interceptor. AuthenticateUser
	// is the single exempt procedure, by its exact name, because a caller
	// cannot hold a token before obtaining one.
	//
	// TelephonyService was previously mounted without it, taking whatever
	// tenant the caller put in the request body — so anyone who could
	// reach the port could read any tenant's call by naming it. That was a
	// documented concession from LLD-02, made when no wire-level
	// AuthenticateUser existed to mint the e2e suite a token; it is closed
	// here (auth-cutover-connectrpc).
	identityStore := identitypostgres.NewStore(appPool)
	identityIssuer, err := identityapp.NewTokenIssuer(key, identityapp.DefaultTokenLifetime)
	if err != nil {
		logger.Error("failed to build token issuer", "error", err)
		os.Exit(1)
	}
	identitySvc := identityapp.NewService(identityStore, identityStore, identityStore, identityStore,
		identityStore, identityapp.NewPasswordHasher(), identityIssuer, logger)
	identityHandler := identityrpc.NewIdentityHandler(identitySvc)
	authInterceptor := identityapp.NewAuthInterceptor(identitySvc, identityrpc.TenantID).
		ExemptProcedure(identityapp.AuthenticateUserProcedure)
	identityPath, identityRPC := atsapbxv1connect.NewIdentityServiceHandler(identityHandler,
		connectrpc.WithInterceptors(authInterceptor))

	// TelephonyService carries its own tenant matcher, so the interceptor
	// refuses a body tenant that is not the token's rather than trusting
	// the caller's word for which tenant they are reading.
	telephonyHandler := rpc.NewTelephonyHandler(callStore)
	telephonyInterceptor := identityapp.NewAuthInterceptor(identitySvc, rpc.TenantID).
		ExemptProcedure(identityapp.AuthenticateUserProcedure)
	rpcPath, rpcHandler := atsapbxv1connect.NewTelephonyServiceHandler(telephonyHandler,
		connectrpc.WithInterceptors(telephonyInterceptor))

	handlers := map[string]http.Handler{
		rpcPath:      rpcHandler,
		identityPath: identityRPC,
	}

	// PbxService: extension configuration. Mounted WITH the same auth
	// interceptor — every method mutates or reads tenant configuration,
	// and none of it is reachable without a token.
	//
	// The realm passed here MUST match the engine's: the credential
	// digest is computed over it (LLD-03 §7.3).
	pbxSvc := pbxapp.NewService(
		appPool,
		pbxpostgres.NewStore(),
		pbxasterisk.NewProjector(pbxasterisk.Config{
			Realm:           cfg.SIPRealm,
			SIPTransport:    cfg.SIPTransport,
			WebRTCTransport: cfg.SIPWebRTCTransport,
		}, appPool),
		identitySvc,
		identityStore,
		cfg.SIPRealm,
	)
	pbxPath, pbxRPC := atsapbxv1connect.NewPbxServiceHandler(pbxrpc.NewPbxHandler(pbxSvc),
		connectrpc.WithInterceptors(authInterceptor))
	handlers[pbxPath] = pbxRPC

	checkers := []server.Checker{
		{Name: "postgres", Check: func(ctx context.Context) error { return appPool.Unwrap().Ping(ctx) }},
		{Name: "nats", Check: func(context.Context) error {
			if nc.Status() != natsgo.CONNECTED {
				return context.DeadlineExceeded
			}
			return nil
		}},
	}

	httpSrv := server.NewWithHandlers(cfg.HTTPAddr, checkers, handlers)
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
