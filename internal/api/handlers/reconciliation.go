package handlers

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
)

// ReconciliationHandler exposes reconciliation run data.
type ReconciliationHandler struct {
	svc *reconciliation.Service
	log *zap.Logger
}

// NewReconciliationHandler constructs a ReconciliationHandler.
func NewReconciliationHandler(svc *reconciliation.Service, log *zap.Logger) *ReconciliationHandler {
	return &ReconciliationHandler{svc: svc, log: log}
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":   runs,
		"limit":  limit,
		"offset": offset,
	})
}
