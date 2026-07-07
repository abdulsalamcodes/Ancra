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
	reconSvc *reconciliation.Service
	log      *zap.Logger
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(orgs store.OrgStore, apiKeys store.APIKeyStore, reconSvc *reconciliation.Service, log *zap.Logger) *AdminHandler {
	return &AdminHandler{orgs: orgs, apiKeys: apiKeys, reconSvc: reconSvc, log: log}
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
