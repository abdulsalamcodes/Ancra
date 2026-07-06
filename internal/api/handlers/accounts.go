package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/account"
)

// AccountHandler exposes virtual-account endpoints.
type AccountHandler struct {
	svc *account.Service
	log *zap.Logger
}

// NewAccountHandler constructs an AccountHandler.
func NewAccountHandler(svc *account.Service, log *zap.Logger) *AccountHandler {
	return &AccountHandler{svc: svc, log: log}
}

// ---------------------------------------------------------------------------
// POST /accounts
// ---------------------------------------------------------------------------

type createAccountRequest struct {
	CustomerID    string `json:"customer_id"`
	DisplayName   string `json:"display_name"`
	CustomerEmail string `json:"customer_email"`
	BVN           string `json:"bvn,omitempty"`
	NIN           string `json:"nin,omitempty"`
}

// Create provisions a new virtual account.
func (h *AccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	customerID, err := uuid.Parse(req.CustomerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "customer_id must be a valid UUID")
		return
	}

	if req.DisplayName == "" || req.CustomerEmail == "" {
		writeError(w, http.StatusBadRequest, "display_name and customer_email are required")
		return
	}

	result, err := h.svc.Create(r.Context(), account.CreateAccountRequest{
		CustomerID:    customerID,
		DisplayName:   req.DisplayName,
		CustomerEmail: req.CustomerEmail,
		BVN:           req.BVN,
		NIN:           req.NIN,
	})
	if err != nil {
		h.log.Error("create account failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}
// ---------------------------------------------------------------------------

// GetByID retrieves a virtual account by its UUID.
func (h *AccountHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	va, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.log.Error("get account failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	writeJSON(w, http.StatusOK, va)
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/balance
// ---------------------------------------------------------------------------

// GetBalance returns the current ledger balance for an account.
func (h *AccountHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	balance, err := h.svc.GetBalance(r.Context(), id)
	if err != nil {
		h.log.Error("get balance failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, balance)
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/transactions
// ---------------------------------------------------------------------------

// ListTransactions returns a paginated list of ledger entries.
func (h *AccountHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	page, err := h.svc.ListTransactions(r.Context(), id, limit, offset)
	if err != nil {
		h.log.Error("list transactions failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, page)
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/statement  (alias — same as list transactions)
// ---------------------------------------------------------------------------

// GetStatement returns the full ledger statement for an account.
func (h *AccountHandler) GetStatement(w http.ResponseWriter, r *http.Request) {
	h.ListTransactions(w, r)
}

// ---------------------------------------------------------------------------
// PUT /accounts/{id}
// ---------------------------------------------------------------------------

type updateAccountRequest struct {
	DisplayName string `json:"display_name"`
}

// Update changes the display name on an account.
func (h *AccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	var req updateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	if err := h.svc.Update(r.Context(), account.UpdateAccountRequest{
		AccountID:   id,
		DisplayName: req.DisplayName,
	}); err != nil {
		h.log.Error("update account failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// ---------------------------------------------------------------------------
// POST /accounts/{id}/close
// ---------------------------------------------------------------------------

// Close marks a virtual account as closed.
func (h *AccountHandler) Close(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	if err := h.svc.Close(r.Context(), id); err != nil {
		h.log.Error("close account failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func parseUUID(w http.ResponseWriter, r *http.Request, param string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, param)
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, param+" must be a valid UUID")
		return uuid.UUID{}, false
	}
	return id, true
}

func queryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
