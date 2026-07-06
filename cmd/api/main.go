// Command api is the entry point for the Ancra HTTP server.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/api"
	"github.com/abdulsalamcodes/ancra/internal/config"
	"github.com/abdulsalamcodes/ancra/internal/domain/account"
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

	// ---------------------------------------------------------------------------
	// Stores
	// ---------------------------------------------------------------------------
	accountStore := postgres.NewAccountStore(db)
	ledgerStore := postgres.NewLedgerStore(db)
	customerStore := postgres.NewCustomerStore(db)
	eventStore := postgres.NewEventStore(db)
	reconStore := postgres.NewReconciliationStore(db)
	webhookStore := postgres.NewWebhookStore(db)

	// ---------------------------------------------------------------------------
	// Nomba client
	// ---------------------------------------------------------------------------
	nombaClient := nomba.NewClient(
		cfg.NombaBaseURL,
		cfg.NombaClientID,
		cfg.NombaClientSecret,
		cfg.NombaAccountID,
		log,
	)
	verifier := nomba.NewVerifier(cfg.NombaWebhookSecret)

	// ---------------------------------------------------------------------------
	// Domain services
	// ---------------------------------------------------------------------------
	accountSvc := account.NewService(accountStore, customerStore, ledgerStore, nombaClient, log)
	ledgerSvc := ledger.NewService(ledgerStore, log)
	reconSvc := reconciliation.NewService(ledgerStore, reconStore, accountStore, eventStore, nombaClient, log)

	// ---------------------------------------------------------------------------
	// Background workers
	// ---------------------------------------------------------------------------
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	sweepWorker := worker.NewSweepWorker(reconSvc, accountStore, ledgerStore, cfg.SweepIntervalSeconds, log)
	go sweepWorker.Run(workerCtx)

	// Outbound webhook worker — endpoint URL can be set via WEBHOOK_ENDPOINT env var.
	webhookEndpoint := os.Getenv("WEBHOOK_ENDPOINT")
	outboundWorker := worker.NewOutboundWorker(webhookStore, webhookEndpoint, cfg.APIKey, log)
	go outboundWorker.Run(workerCtx)

	// ---------------------------------------------------------------------------
	// HTTP server
	// ---------------------------------------------------------------------------
	router := api.NewRouter(api.RouterDeps{
		AccountSvc:  accountSvc,
		LedgerSvc:   ledgerSvc,
		ReconSvc:    reconSvc,
		NombaClient: nombaClient,
		Verifier:    verifier,
		Accounts:    accountStore,
		Events:      eventStore,
		Webhooks:    webhookStore,
		APIKey:      cfg.APIKey,
		Log:         log,
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
