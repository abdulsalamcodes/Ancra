package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/account"
	"github.com/abdulsalamcodes/ancra/internal/store"
	"github.com/abdulsalamcodes/ancra/internal/tenant"
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
	PhoneNumber   string `json:"phone_number,omitempty"`
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
		PhoneNumber:   req.PhoneNumber,
		BVN:           req.BVN,
		NIN:           req.NIN,
	})
	if err != nil {
		h.log.Error("create account failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// ---------------------------------------------------------------------------
// GET /accounts
// ---------------------------------------------------------------------------

// List returns a paginated list of all virtual accounts.
func (h *AccountHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	accounts, err := h.svc.ListAccounts(r.Context(), limit, offset)
	if err != nil {
		h.log.Error("list accounts failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accounts": accounts,
		"limit":    limit,
		"offset":   offset,
	})
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
		writeError(w, http.StatusInternalServerError, "failed to retrieve balance")
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
		writeError(w, http.StatusInternalServerError, "failed to list transactions")
		return
	}

	writeJSON(w, http.StatusOK, page)
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/statement  (alias — same as list transactions)
// ---------------------------------------------------------------------------

// GetStatement returns a paginated account statement with a running balance
// per entry that is correct across all pages.
func (h *AccountHandler) GetStatement(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	statement, err := h.svc.GetStatement(r.Context(), id, limit, offset)
	if err != nil {
		h.log.Error("get statement failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to retrieve statement")
		return
	}

	writeJSON(w, http.StatusOK, statement)
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
		writeError(w, http.StatusInternalServerError, "failed to update account")
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
		writeError(w, http.StatusInternalServerError, "failed to close account")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// ---------------------------------------------------------------------------
// CustomerHandler
// ---------------------------------------------------------------------------

// CustomerHandler exposes customer endpoints.
type CustomerHandler struct {
	customers store.CustomerStore
	log       *zap.Logger
}

// NewCustomerHandler constructs a CustomerHandler.
func NewCustomerHandler(customers store.CustomerStore, log *zap.Logger) *CustomerHandler {
	return &CustomerHandler{customers: customers, log: log}
}

type createCustomerRequest struct {
	KYCTier int `json:"kyc_tier"`
}

const (
	minKYCTier = 1
	maxKYCTier = 3
)

// Create provisions a new customer.
func (h *CustomerHandler) Create(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	var req createCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.KYCTier == 0 {
		req.KYCTier = minKYCTier
	}
	if req.KYCTier < minKYCTier || req.KYCTier > maxKYCTier {
		writeError(w, http.StatusBadRequest, "kyc_tier must be 1, 2, or 3")
		return
	}

	c := &store.Customer{
		ID:        uuid.New(),
		OrgID:     orgID,
		KYCTier:   req.KYCTier,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.customers.CreateCustomer(r.Context(), c); err != nil {
		h.log.Error("create customer failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create customer")
		return
	}

	writeJSON(w, http.StatusCreated, c)
}

// GetCustomerByID retrieves a single customer by UUID, scoped to the requesting org.
//
// GET /customers/{id}
func (h *CustomerHandler) GetCustomerByID(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	customer, err := h.customers.GetCustomer(r.Context(), orgID, id)
	if err != nil {
		h.log.Error("get customer failed", zap.String("id", id.String()), zap.Error(err))
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}

	writeJSON(w, http.StatusOK, customer)
}

// List returns a paginated list of customers for the requesting org.
//
// GET /customers
func (h *CustomerHandler) List(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	customers, err := h.customers.ListCustomers(r.Context(), orgID, limit, offset)
	if err != nil {
		h.log.Error("list customers failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list customers")
		return
	}
	if customers == nil {
		customers = []*store.Customer{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"customers": customers,
		"limit":     limit,
		"offset":    offset,
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// requireOrgID reads the organisation UUID from the request context.
// It writes a 403 and returns false if the context is missing org identity,
// which indicates unauthenticated access bypassed middleware.
func requireOrgID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := tenant.OrgIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusForbidden, "missing organisation context")
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid organisation context")
		return uuid.UUID{}, false
	}
	return id, true
}

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
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"error": map[string]string{"message": msg},
	})
}

// newValidationError returns an error with a user-facing validation message.
func newValidationError(msg string) error {
	return validationError(msg)
}

type validationError string

func (e validationError) Error() string { return string(e) }
