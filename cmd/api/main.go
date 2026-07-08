// Command api is the entry point for the Ancra HTTP server.
package main

import (
	"context"
	"crypto/sha256"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	dbpkg "github.com/abdulsalamcodes/ancra/db"
	"github.com/abdulsalamcodes/ancra/internal/api"
	"github.com/abdulsalamcodes/ancra/internal/config"
	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/domain/account"
	domainauth "github.com/abdulsalamcodes/ancra/internal/domain/auth"
	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store/postgres"
	"github.com/abdulsalamcodes/ancra/internal/worker"
)

func main() {
	// ---------------------------------------------------------------------------
	// Logger
	// ---------------------------------------------------------------------------
	log, err := zap.NewProduction()
	if err != nil {
		panic("failed to initialise zap logger: " + err.Error())
	}
	defer log.Sync() //nolint:errcheck

	// ---------------------------------------------------------------------------
	// Config
	// ---------------------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("failed to load config", zap.Error(err))
	}

	// ---------------------------------------------------------------------------
	// Database
	// ---------------------------------------------------------------------------
	ctx := context.Background()

	db, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer db.Close()

	log.Info("postgres connected", zap.String("dsn", maskDSN(cfg.DatabaseURL)))

	if err := dbpkg.RunMigrations(ctx, db.Pool); err != nil {
		log.Fatal("failed to run migrations", zap.Error(err))
	}
	log.Info("database migrations applied")

	// ---------------------------------------------------------------------------
	// Stores
	// ---------------------------------------------------------------------------
	accountStore := postgres.NewAccountStore(db)
	ledgerStore := postgres.NewLedgerStore(db)
	customerStore := postgres.NewCustomerStore(db)
	eventStore := postgres.NewEventStore(db)
	reconStore := postgres.NewReconciliationStore(db)
	webhookStore := postgres.NewWebhookStore(db)
	apiKeyStore := postgres.NewAPIKeyStore(db)
	orgStore := postgres.NewOrgStore(db)
	userStore := postgres.NewUserStore(db)
	refreshTokenStore := postgres.NewRefreshTokenStore(db)
	nombaConfigStore := &postgres.NombaConfigStore{Pool: db.Pool}
	webhookConfigStore := &postgres.WebhookConfigStore{Pool: db.Pool}

	// ---------------------------------------------------------------------------
	// Encryption
	// ---------------------------------------------------------------------------
	key := sha256.Sum256([]byte(cfg.EncryptionKey))
	encryptor, err := crypto.NewEncryptor(key[:])
	if err != nil {
		log.Fatal("failed to create encryptor — ENCRYPTION_KEY must be exactly 32 bytes", zap.Error(err))
	}

	// ---------------------------------------------------------------------------
	// Nomba client (global — optional when all orgs use BYOK)
	// ---------------------------------------------------------------------------
	var nombaClient *nomba.Client
	var verifier *nomba.Verifier
	if cfg.NombaClientID != "" {
		nombaClient = nomba.NewClient(
			cfg.NombaBaseURL,
			cfg.NombaClientID,
			cfg.NombaClientSecret,
			cfg.NombaAccountID,
			cfg.NombaSubAccountID,
			log,
		)
	}
	if cfg.NombaWebhookSecret != "" {
		verifier = nomba.NewVerifier(cfg.NombaWebhookSecret)
	}

	nombaFactory := nomba.NewClientFactory(nombaConfigStore, encryptor, cfg.NombaBaseURL, log)
	if nombaClient != nil {
		nombaFactory.SetGlobalFallback(nombaClient, verifier)
	}

	// ---------------------------------------------------------------------------
	// Domain services
	// ---------------------------------------------------------------------------
	authSvc := domainauth.NewService(orgStore, userStore, refreshTokenStore, ledgerStore, []byte(cfg.JWTSecret), log)
	accountSvc := account.NewService(accountStore, customerStore, ledgerStore, nombaClient, log)
	ledgerSvc := ledger.NewService(ledgerStore, log)
	reconSvc := reconciliation.NewService(ledgerStore, reconStore, accountStore, eventStore, nombaFactory, log)

	// ---------------------------------------------------------------------------
	// Background workers
	// ---------------------------------------------------------------------------
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	sweepWorker := worker.NewSweepWorker(reconSvc, orgStore, accountStore, ledgerStore, cfg.SweepIntervalSeconds, log)
	go sweepWorker.Run(workerCtx)

	outboundWorker := worker.NewOutboundWorker(webhookStore, webhookConfigStore, encryptor, log)
	go outboundWorker.Run(workerCtx)

	// ---------------------------------------------------------------------------
	// HTTP server
	// ---------------------------------------------------------------------------
	router := api.NewRouter(api.RouterDeps{
		AccountSvc:     accountSvc,
		LedgerSvc:      ledgerSvc,
		ReconSvc:       reconSvc,
		AuthSvc:        authSvc,
		NombaClient:    nombaClient,
		NombaFactory:   nombaFactory,
		Verifier:       verifier,
		Accounts:       accountStore,
		Orgs:           orgStore,
		Customers:      customerStore,
		Events:         eventStore,
		Webhooks:       webhookStore,
		APIKeys:        apiKeyStore,
		NombaConfigs:   nombaConfigStore,
		WebhookConfigs: webhookConfigStore,
		Transactor:     db,
		Encryptor:      encryptor,
		StaticKey:      cfg.APIKey,
		AdminSecret:    cfg.AdminSecret,
		Log:            log,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background goroutine.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("HTTP server listening", zap.String("addr", srv.Addr))
		serverErr <- srv.ListenAndServe()
	}()

	// ---------------------------------------------------------------------------
	// Graceful shutdown
	// ---------------------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-serverErr:
		log.Error("server error", zap.Error(err))
	}

	log.Info("shutting down gracefully…")
	cancelWorkers()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server forced to shut down", zap.Error(err))
	}

	log.Info("server stopped")
}

// maskDSN redacts the password component of a Postgres DSN for safe logging.
func maskDSN(dsn string) string {
	if len(dsn) > 20 {
		return dsn[:20] + "…(masked)"
	}
	return "(masked)"
}
