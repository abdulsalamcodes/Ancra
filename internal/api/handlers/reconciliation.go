package handlers

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// ReconciliationHandler exposes reconciliation run data.
type ReconciliationHandler struct {
	svc      *reconciliation.Service
	webhooks store.WebhookStore
	log      *zap.Logger
}

// NewReconciliationHandler constructs a ReconciliationHandler.
func NewReconciliationHandler(svc *reconciliation.Service, webhooks store.WebhookStore, log *zap.Logger) *ReconciliationHandler {
	return &ReconciliationHandler{svc: svc, webhooks: webhooks, log: log}
}

// GetLatest returns the most recent reconciliation run.
//
// GET /reconciliation
func (h *ReconciliationHandler) GetLatest(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 10)
	offset := queryInt(r, "offset", 0)

	runs, err := h.svc.ListRuns(r.Context(), limit, offset)
	if err != nil {
		h.log.Error("list reconciliation runs failed", zap.Error(err))
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

// Trigger manually executes a reconciliation sweep.
//
// POST /reconciliation/trigger
func (h *ReconciliationHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	run, err := h.svc.Run(r.Context())
	if err != nil {
		h.log.Error("reconciliation trigger failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "reconciliation run failed")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ListWebhooks returns outbound webhook delivery records for the requesting org.
//
// GET /webhooks
func (h *ReconciliationHandler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	deliveries, err := h.webhooks.ListDeliveries(r.Context(), orgID, limit, offset)
	if err != nil {
		h.log.Error("list webhook deliveries failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}
	if deliveries == nil {
		deliveries = []*store.WebhookDelivery{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deliveries": deliveries,
		"limit":      limit,
		"offset":     offset,
	})
}
