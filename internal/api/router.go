// Package api wires the HTTP router and all request handlers.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/api/handlers"
	"github.com/abdulsalamcodes/ancra/internal/api/middleware"
	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/domain/account"
	domainauth "github.com/abdulsalamcodes/ancra/internal/domain/auth"
	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
	"github.com/abdulsalamcodes/ancra/web"
)

// RouterDeps bundles everything the router needs to wire handlers.
type RouterDeps struct {
	AccountSvc     *account.Service
	LedgerSvc      *ledger.Service
	ReconSvc       *reconciliation.Service
	AuthSvc        *domainauth.Service
	NombaClient    *nomba.Client   // optional; nil when all orgs use BYOK
	NombaFactory   *nomba.ClientFactory
	Verifier       *nomba.Verifier // optional; nil when all orgs use BYOK
	Accounts       store.AccountStore
	Ledger         store.LedgerStore
	Orgs           store.OrgStore
	Customers      store.CustomerStore
	Events         store.EventStore
	Webhooks       store.WebhookStore
	APIKeys        store.APIKeyStore
	NombaConfigs   store.NombaConfigStore
	WebhookConfigs store.WebhookConfigStore
	Transactor     store.Transactor
	Encryptor      *crypto.Encryptor
	StaticKey      string // legacy env var key, optional
	StaticKeyOrgID string // org pinned to the static key (single-tenant mode)
	AdminSecret    string
	Log            *zap.Logger
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
	r.Get("/status", healthHandler) // alias — some ad blockers flag /health

	// Web pages
	r.Get("/", web.LandingHandler())
	r.Get("/auth", web.AuthHandler())
	r.Get("/dashboard", web.DashboardHandler())
	r.Get("/admin", web.AdminHandler())
	// /app was the old connect-to-instance portal — redirect to the new auth page.
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/auth", http.StatusMovedPermanently)
	})

	// Auth endpoints — no token required
	authHandler := handlers.NewAuthHandler(d.AuthSvc, d.Log)
	r.Post("/auth/signup", authHandler.Signup)
	r.Post("/auth/login", authHandler.Login)
	r.Post("/auth/refresh", authHandler.Refresh)
	r.Post("/auth/logout", authHandler.Logout)

	// Nomba webhook — public but HMAC-verified inside the handler.
	whHandler := handlers.NewWebhookHandler(
		d.Verifier, d.LedgerSvc, d.Accounts, d.Events, d.Webhooks, d.Transactor, d.Log,
	)
	r.Post("/webhooks/nomba", whHandler.HandleNomba)

	apiKeyHandler := handlers.NewAPIKeyHandler(d.APIKeys, d.Log)
	reconHandler := handlers.NewReconciliationHandler(d.ReconSvc, d.Webhooks, d.Log)
	acctHandler := handlers.NewAccountHandler(d.AccountSvc, d.Log)
	txnHandler := handlers.NewTransactionHandler(d.LedgerSvc, d.NombaFactory, d.NombaClient, d.Log)
	customerHandler := handlers.NewCustomerHandler(d.Customers, d.Log)
	nombaConfigHandler := handlers.NewNombaConfigHandler(d.NombaConfigs, d.Encryptor, d.NombaFactory, d.Log)
	webhookConfigHandler := handlers.NewWebhookConfigHandler(d.WebhookConfigs, d.Encryptor, d.Log)

	// ---------------------------------------------------------------------------
	// JWT-only routes — dashboard session management and org configuration.
	// These actions are scoped to the authenticated user's session and are not
	// appropriate for programmatic API key access.
	// ---------------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(d.AuthSvc))
		r.Use(chimw.StripSlashes)

		r.Get("/auth/me", authHandler.Me)

		r.Post("/api-keys", apiKeyHandler.Create)
		r.Get("/api-keys", apiKeyHandler.List)
		r.Delete("/api-keys/{id}", apiKeyHandler.Revoke)

		r.Get("/webhooks", reconHandler.ListWebhooks)

		r.Get("/settings/nomba", nombaConfigHandler.Get)
		r.Put("/settings/nomba", nombaConfigHandler.Upsert)
		r.Post("/settings/nomba/test", nombaConfigHandler.TestConnection)

		r.Get("/settings/webhook", webhookConfigHandler.Get)
		r.Put("/settings/webhook", webhookConfigHandler.Upsert)
	})

	// ---------------------------------------------------------------------------
	// JWT-or-API-key routes — customer and account operations accessible from
	// both the dashboard (JWT session) and server-side integrations (API key).
	// ---------------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTOrAPIKeyAuth(d.AuthSvc, d.APIKeys, d.StaticKey, d.StaticKeyOrgID))
		r.Use(chimw.StripSlashes)

		r.Post("/customers", customerHandler.Create)
		r.Get("/customers", customerHandler.List)
		r.Get("/customers/{id}", customerHandler.GetCustomerByID)
		r.Put("/customers/{id}/kyc-tier", customerHandler.UpgradeKYCTier)
		r.Get("/customers/{id}/kyc-tier/history", customerHandler.ListKYCTierHistory)

		r.Post("/accounts", acctHandler.Create)
		r.Get("/accounts", acctHandler.List)
		r.Get("/accounts/{id}", acctHandler.GetByID)
		r.Put("/accounts/{id}", acctHandler.Update)
		r.Post("/accounts/{id}/close", acctHandler.Close)
		r.Get("/accounts/{id}/balance", acctHandler.GetBalance)
		r.Get("/accounts/{id}/transactions", acctHandler.ListTransactions)
		r.Get("/accounts/{id}/statement", acctHandler.GetStatement)
	})

	// ---------------------------------------------------------------------------
	// Admin routes — protected by Admin-Secret header only (operator use)
	// ---------------------------------------------------------------------------
	adminHandler := handlers.NewAdminHandler(d.Orgs, d.APIKeys, d.Accounts, d.Ledger, d.ReconSvc, d.Log)

	r.Group(func(r chi.Router) {
		r.Use(middleware.AdminAuth(d.AdminSecret))

		r.Get("/admin/orgs", adminHandler.ListOrgs)
		r.Get("/admin/stats", adminHandler.GetStats)
		r.Get("/admin/reconciliation", adminHandler.ListAllReconciliationRuns)
		r.Get("/admin/orgs/{orgID}/reconciliation", adminHandler.ListOrgReconciliationRuns)
		r.Post("/admin/orgs/{orgID}/reconciliation/trigger", adminHandler.TriggerOrgReconciliation)
		r.Get("/admin/orgs/{orgID}/accounts", adminHandler.ListOrgAccounts)
		r.Get("/admin/accounts/{id}/ledger", adminHandler.ListAccountLedger)

		r.Post("/admin/api-keys", apiKeyHandler.AdminCreateKey)
		r.Get("/admin/api-keys", apiKeyHandler.AdminListAllKeys)
		r.Delete("/admin/api-keys/{id}", apiKeyHandler.Revoke)

		r.Get("/admin/webhooks", reconHandler.AdminListWebhooks)
	})

	// ---------------------------------------------------------------------------
	// API-key-only routes — transfer operations for server-side integrations.
	// ---------------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKeyAuth(d.APIKeys, d.StaticKey, d.StaticKeyOrgID))
		r.Use(chimw.StripSlashes)

		r.Post("/transfers/lookup", txnHandler.LookupBank)
		r.Post("/accounts/{id}/transfer", txnHandler.Transfer)
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"ancra","version":"0.1.0"}`)) //nolint:errcheck
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
