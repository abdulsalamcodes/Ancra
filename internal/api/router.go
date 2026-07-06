// Package api wires the HTTP router and all request handlers.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/api/handlers"
	"github.com/abdulsalamcodes/ancra/internal/api/middleware"
	"github.com/abdulsalamcodes/ancra/internal/domain/account"
	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// RouterDeps bundles everything the router needs to wire handlers.
type RouterDeps struct {
	AccountSvc  *account.Service
	LedgerSvc   *ledger.Service
	ReconSvc    *reconciliation.Service
	NombaClient *nomba.Client
	Verifier    *nomba.Verifier
	Accounts    store.AccountStore
	Events      store.EventStore
	Webhooks    store.WebhookStore
	APIKey      string
	Log         *zap.Logger
}

// NewRouter constructs and returns the fully wired chi router.
func NewRouter(d RouterDeps) http.Handler {
	r := chi.NewRouter()

	// ---------------------------------------------------------------------------
	// Global middleware
	// ---------------------------------------------------------------------------
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(zapLogger(d.Log))
	r.Use(chimw.Recoverer)
	r.Use(chimw.CleanPath)

	// ---------------------------------------------------------------------------
	// Public routes
	// ---------------------------------------------------------------------------
	r.Get("/health", healthHandler)

	// Nomba webhook — public but HMAC-verified inside the handler.
	whHandler := handlers.NewWebhookHandler(
		d.Verifier, d.LedgerSvc, d.Accounts, d.Events, d.Webhooks, d.Log,
	)
	r.Post("/webhooks/nomba", whHandler.HandleNomba)

	// ---------------------------------------------------------------------------
	// Authenticated developer API
	// ---------------------------------------------------------------------------
	acctHandler := handlers.NewAccountHandler(d.AccountSvc, d.Log)
	txnHandler := handlers.NewTransactionHandler(d.LedgerSvc, d.NombaClient, d.Log)
	reconHandler := handlers.NewReconciliationHandler(d.ReconSvc, d.Log)

	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKeyAuth(d.APIKey))
		r.Use(middleware.Idempotency)

		// Account endpoints
		r.Post("/accounts", acctHandler.Create)
		r.Get("/accounts/{id}", acctHandler.GetByID)
		r.Get("/accounts/{id}/balance", acctHandler.GetBalance)
		r.Get("/accounts/{id}/transactions", acctHandler.ListTransactions)
		r.Get("/accounts/{id}/statement", acctHandler.GetStatement)
		r.Put("/accounts/{id}", acctHandler.Update)
		r.Post("/accounts/{id}/close", acctHandler.Close)

		// Transfer
		r.Post("/accounts/{id}/transfer", txnHandler.Transfer)

		// Reconciliation
		r.Get("/reconciliation", reconHandler.GetLatest)
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
}

// zapLogger is a minimal chi-compatible request logger using zap.
func zapLogger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.Status()),
				zap.String("request_id", chimw.GetReqID(r.Context())),
			)
		})
	}
}
