package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// AdminHandler exposes platform-wide operational views to authorised operators.
type AdminHandler struct {
	orgs     store.OrgStore
	apiKeys  store.APIKeyStore
	accounts store.AccountStore
	ledger   store.LedgerStore
	reconSvc *reconciliation.Service
	log      *zap.Logger
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(
	orgs store.OrgStore,
	apiKeys store.APIKeyStore,
	accounts store.AccountStore,
	ledger store.LedgerStore,
	reconSvc *reconciliation.Service,
	log *zap.Logger,
) *AdminHandler {
	return &AdminHandler{
		orgs:     orgs,
		apiKeys:  apiKeys,
		accounts: accounts,
		ledger:   ledger,
		reconSvc: reconSvc,
		log:      log,
	}
}

// ListOrgs returns all organisations on the platform.
//
// GET /admin/orgs
func (h *AdminHandler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.orgs.ListAllOrgs(r.Context())
	if err != nil {
		h.log.Error("admin list orgs failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list organisations")
		return
	}
	if orgs == nil {
		orgs = []*store.Organization{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orgs": orgs})
}

// GetStats returns platform-wide aggregate counts useful for the admin overview.
//
// GET /admin/stats
func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.orgs.ListAllOrgs(r.Context())
	if err != nil {
		h.log.Error("admin stats: list orgs failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch stats")
		return
	}

	keys, err := h.apiKeys.ListAllKeys(r.Context())
	if err != nil {
		h.log.Error("admin stats: list keys failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch stats")
		return
	}

	activeKeys := 0
	for _, k := range keys {
		if k.RevokedAt == nil {
			activeKeys++
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_orgs":  len(orgs),
		"total_keys":  len(keys),
		"active_keys": activeKeys,
	})
}

// ListAllReconciliationRuns returns a paginated list of reconciliation runs across
// all organisations, ordered newest first.
//
// GET /admin/reconciliation
func (h *AdminHandler) ListAllReconciliationRuns(w http.ResponseWriter, r *http.Request) {
	limit  := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	runs, err := h.reconSvc.ListAllRuns(r.Context(), limit, offset)
	if err != nil {
		h.log.Error("admin list all reconciliation runs failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch reconciliation runs")
		return
	}
	if runs == nil {
		runs = []*store.ReconciliationRun{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":   runs,
		"limit":  limit,
		"offset": offset,
	})
}

// ListOrgReconciliationRuns returns recent reconciliation runs for the given org.
//
// GET /admin/orgs/{orgID}/reconciliation
func (h *AdminHandler) ListOrgReconciliationRuns(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	limit  := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	runs, err := h.reconSvc.ListRuns(r.Context(), orgID, limit, offset)
	if err != nil {
		h.log.Error("admin list reconciliation runs failed",
			zap.String("org_id", orgID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch reconciliation runs")
		return
	}
	if runs == nil {
		runs = []*store.ReconciliationRun{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":   runs,
		"limit":  limit,
		"offset": offset,
	})
}

// TriggerOrgReconciliation manually runs a reconciliation sweep for the given org.
//
// POST /admin/orgs/{orgID}/reconciliation/trigger
func (h *AdminHandler) TriggerOrgReconciliation(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}

	run, err := h.reconSvc.Run(r.Context(), orgID)
	if err != nil {
		h.log.Error("admin reconciliation trigger failed",
			zap.String("org_id", orgID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "reconciliation run failed")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ListOrgAccounts returns all virtual accounts for the given org, with their
// current ledger balance included inline.
//
// GET /admin/orgs/{orgID}/accounts
func (h *AdminHandler) ListOrgAccounts(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	limit  := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	accounts, err := h.accounts.ListAccounts(r.Context(), orgID, limit, offset)
	if err != nil {
		h.log.Error("admin list org accounts failed",
			zap.String("org_id", orgID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}
	if accounts == nil {
		accounts = []*store.VirtualAccount{}
	}

	type accountWithBalance struct {
		*store.VirtualAccount
		Balance int64 `json:"balance"`
	}
	enriched := make([]accountWithBalance, 0, len(accounts))
	for _, a := range accounts {
		balance, err := h.ledger.GetBalance(r.Context(), a.ID)
		if err != nil {
			h.log.Warn("admin: could not fetch balance for account",
				zap.String("account_id", a.ID.String()), zap.Error(err))
		}
		enriched = append(enriched, accountWithBalance{VirtualAccount: a, Balance: balance})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accounts": enriched,
		"limit":    limit,
		"offset":   offset,
	})
}

// ListAccountLedger returns the raw ledger entries for a single account,
// regardless of which org owns it. Intended for operator inspection only.
//
// GET /admin/accounts/{id}/ledger
func (h *AdminHandler) ListAccountLedger(w http.ResponseWriter, r *http.Request) {
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	limit  := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	entries, err := h.ledger.ListEntries(r.Context(), accountID, limit, offset)
	if err != nil {
		h.log.Error("admin list account ledger failed",
			zap.String("account_id", accountID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list ledger entries")
		return
	}
	if entries == nil {
		entries = []*store.LedgerEntry{}
	}

	balance, err := h.ledger.GetBalance(r.Context(), accountID)
	if err != nil {
		h.log.Warn("admin: could not fetch balance",
			zap.String("account_id", accountID.String()), zap.Error(err))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"balance": balance,
		"limit":   limit,
		"offset":  offset,
	})
}
