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

	pbxports "atsap-api/internal/pbx/ports"

	connectrpc "connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	natsgo "github.com/nats-io/nats.go"

	"atsap-api/cmd/atsap-api/internal/wiring"
	"atsap-api/internal/config"
	atsapbxv1connect "atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	identityapp "atsap-api/internal/identity/application"
	identityrpc "atsap-api/internal/identity/rpc"
	licensingrpc "atsap-api/internal/licensing/rpc"
	"atsap-api/internal/logging"
	atsapnats "atsap-api/internal/nats"
	pbxasterisk "atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/acl/sipprobe"
	pbxapp "atsap-api/internal/pbx/application"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	pbxrpc "atsap-api/internal/pbx/rpc"
	corepostgres "atsap-api/internal/postgres"
	"atsap-api/internal/server"
	shareddomain "atsap-api/internal/shared/domain"
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
	os.Exit(run())
}

// run is the service and its subcommands. It returns the process exit
// code instead of calling os.Exit, so every deferred cleanup (signal
// handling, the database pools, the event bus) runs before the process
// ends.
func run() int {
	cfg, err := config.Load()
	if err != nil {
		// Logger isn't built yet (it needs cfg.LogLevel); a bare
		// stderr message is the correct fallback for a config error
		// that happens before anything else can start.
		_, _ = os.Stderr.WriteString("failed to load config: " + err.Error() + "\n")
		return 1
	}

	if code, ran := runSubcommand(cfg); ran {
		return code
	}

	logger := logging.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Postgres: app pool (RLS-scoped, atsapbx_app) ---
	appPool, err := corepostgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to open app database pool", "error", err)
		return 1
	}
	defer appPool.Close()

	if err := corepostgres.MigrateUp(cfg.DatabaseURL, "migrations"); err != nil {
		logger.Error("failed to run migrations", "error", err)
		return 1
	}

	// --- Postgres: outbox worker pool (BYPASSRLS, atsap_outbox_worker).
	// A raw *pgxpool.Pool, not the tenant-scoped Pool wrapper: this role
	// deliberately bypasses RLS to read across every tenant's outbox
	// rows (docs/hld/03-domain-model.md §5) — WithTenant's per-transaction
	// SET LOCAL app.tenant_id would be meaningless for it.
	workerPGPool, err := pgxpool.New(ctx, cfg.DatabaseWorkerURL)
	if err != nil {
		logger.Error("failed to open outbox worker database pool", "error", err)
		return 1
	}
	defer workerPGPool.Close()

	// --- NATS + outbox worker ---
	nc, err := natsgo.Connect(cfg.NATSURL)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		return 1
	}
	defer nc.Close()

	natsPublisher, err := atsapnats.NewPublisher(ctx, nc)
	if err != nil {
		logger.Error("failed to create NATS publisher", "error", err)
		return 1
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
		return 1
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
	eventLoop.SetChannelVariableReader(ariClient)

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
	identitySvc, identityStore, err := newIdentity(cfg, appPool, licenseManager, logger)
	if err != nil {
		logger.Error("failed to start identity", "error", err)
		return 1
	}
	trusted, err := identityrpc.ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		logger.Error("invalid ATSAPBX_TRUSTED_PROXIES", "error", err)
		return 1
	}
	sources := identityrpc.NewSourceResolver(trusted)
	if cfg.CookieInsecure {
		logger.Warn("ATSAPBX_COOKIE_INSECURE is set: the session cookie is sent without Secure. Dev rig only.")
	}
	identityHandler := identityrpc.NewIdentityHandler(identitySvc).WithSourceResolver(sources)
	// newInterceptor is every service's auth interceptor: AuthenticateUser
	// exempt by exact name, and a must-change token confined to changing
	// its password (D-64) in every service alike.
	newInterceptor := func(matcher identityapp.TenantMatcher) *identityapp.AuthInterceptor {
		return identityapp.NewAuthInterceptor(identitySvc, matcher).
			ExemptProcedure(identityapp.AuthenticateUserProcedure).
			AllowWhilePasswordChange(identityrpc.PasswordChangeProcedures...)
	}
	authInterceptor := newInterceptor(identityrpc.TenantID)
	identityPath, identityRPC := atsapbxv1connect.NewIdentityServiceHandler(identityHandler,
		connectrpc.WithInterceptors(authInterceptor))

	// AuthService: console sign-in and sessions (D-62). Its public
	// procedures are exempt by exact name; SignOut and ChangePassword
	// require a bearer token like everything else.
	authServiceInterceptor := newInterceptor(noTenantInRequest).ExemptProcedures(identityrpc.AuthPublicProcedures...)
	authPath, authRPC := atsapbxv1connect.NewAuthServiceHandler(
		identityrpc.NewAuthHandler(identitySvc, sources, cfg.CookieInsecure),
		connectrpc.WithInterceptors(authServiceInterceptor))

	// TelephonyService carries its own tenant matcher, so the interceptor
	// refuses a body tenant that is not the token's rather than trusting
	// the caller's word for which tenant they are reading.
	telephonyHandler := rpc.NewTelephonyHandler(callStore, callStore)
	telephonyInterceptor := newInterceptor(rpc.TenantID)
	rpcPath, rpcHandler := atsapbxv1connect.NewTelephonyServiceHandler(telephonyHandler,
		connectrpc.WithInterceptors(telephonyInterceptor))

	handlers := map[string]http.Handler{
		rpcPath:      rpcHandler,
		identityPath: identityRPC,
		authPath:     authRPC,
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
		pbxpostgres.NewTrunkStore(),
		pbxpostgres.NewRouteStore(),
		licenseManager,
		pbxasterisk.NewProjector(pbxasterisk.Config{
			Realm:           cfg.SIPRealm,
			SIPTransport:    cfg.SIPTransport,
			WebRTCTransport: cfg.SIPWebRTCTransport,
		}, appPool),
		identitySvc,
		identityStore,
		cfg.SIPRealm,
	).WithPlacement(pbxapp.Placement{
		// PlaceCall originates through telephony-core's own CallService —
		// the same one the event loop drives — so a placed call is an
		// ordinary call from that point on (LLD-03 §4.1).
		Calls:     callService,
		Status:    callStatusAdapter{calls: callStore},
		Addresser: pbxasterisk.Addresser{},
		Legs:      pbxpostgres.NewCallLegStore(),
	})
	// Inbound numbers and the emergency profile live beside the
	// configuration stores; the router answers telephony-core's
	// InboundRouter port through the composition root, never an import
	// (LLD-03 §4.2). pbx-core is constructed before telephony needs it.
	// Where devices register: the installation's public face, which only
	// the installer knows (pbx-sip-registrar-settings).
	pbxSvc = pbxSvc.WithRegistrar(pbxports.SipRegistrar{
		Host: cfg.SIPPublicHost, Port: cfg.SIPPublicPort, Transport: cfg.SIPPublicTransport,
	})
	pbxSvc = pbxSvc.
		WithEmergencyStores(pbxpostgres.NewInboundNumberStore(), pbxpostgres.NewEmergencyStore()).
		WithInbound(pbxapp.Inbound{
			Addresser: pbxasterisk.Addresser{},
			Numbers:   pbxpostgres.NewInboundNumberStore(),
			Emergency: pbxpostgres.NewEmergencyStore(),
			Legs:      pbxpostgres.NewCallLegStore(),
			Status:    callStatusAdapter{calls: callStore},
			// Late binding on purpose: the closure reads the
			// variable, so health reports land on the fully wired
			// service, not the pre-inbound clone.
			ReportHealth: func(ctx context.Context, id shareddomain.TrunkID, code int, latency time.Duration) error {
				return pbxSvc.ReportRouteHealth(ctx, id, code, latency)
			},
		})
	// Trunk health is the platform's own observation (D-74): every trunk
	// is checked on a schedule, and at once when created or re-addressed.
	trunkProber := pbxapp.NewTrunkProber(appPool, pbxpostgres.NewTrunkStore(), &sipprobe.Prober{}, cfg.TrunkProbeInterval, logger)
	pbxSvc = pbxSvc.WithTrunkChecker(trunkProber)
	go trunkProber.Run(ctx)
	callService.SetInboundRouter(pbxSvc)
	pbxPath, pbxRPC := atsapbxv1connect.NewPbxServiceHandler(pbxrpc.NewPbxHandler(pbxSvc),
		connectrpc.WithInterceptors(authInterceptor))
	handlers[pbxPath] = pbxRPC

	// LicensingService: the installation's licence (LLD-08 §6). Mounted
	// behind the same auth interceptor; its requests name no tenant.
	licensingInterceptor := newInterceptor(noTenantInRequest)
	licensingPath, licensingRPC := atsapbxv1connect.NewLicensingServiceHandler(
		licensingrpc.NewHandler(licenseManager.inner,
			licenceAuthority{identity: identitySvc}, licenceAuditor{identity: identitySvc}),
		connectrpc.WithInterceptors(licensingInterceptor))
	handlers[licensingPath] = licensingRPC

	checkers := []server.Checker{
		{Name: "postgres", Check: func(ctx context.Context) error { return appPool.Unwrap().Ping(ctx) }},
		{Name: "nats", Check: func(context.Context) error {
			if nc.Status() != natsgo.CONNECTED {
				return context.DeadlineExceeded
			}
			return nil
		}},
		wiring.CallEventsChecker(ariClient),
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
	return 0
}

// runSubcommand runs an operator subcommand named by the first argument,
// reporting whether there was one.
func runSubcommand(cfg config.Config) (code int, ran bool) {
	if len(os.Args) < 2 {
		return 0, false
	}
	switch os.Args[1] {
	case "bootstrap":
		// `atsap-api bootstrap` creates the first tenant and administrator
		// of an empty installation. It is the one operator-only path that
		// exists outside the API's token cycle (identity-api task 3.3).
		return runBootstrap(cfg), true
	case "pbx":
		// `atsap-api pbx reconcile` compares the media engine's
		// configuration against the platform's records. Operator-invoked,
		// never scheduled (LLD-03 design D6).
		return runPbx(cfg), true
	case "admin":
		// `atsap-api admin reset-password` is off-network recovery for an
		// installation nobody can sign in to (D-66).
		return runAdmin(cfg), true
	}
	return 0, false
}
