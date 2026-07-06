package handlers

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
)

// TransactionHandler handles outbound transfer requests.
type TransactionHandler struct {
	ledgerSvc *ledger.Service
	nomba     *nomba.Client
	log       *zap.Logger
}

// NewTransactionHandler constructs a TransactionHandler.
func NewTransactionHandler(ledgerSvc *ledger.Service, nombaClient *nomba.Client, log *zap.Logger) *TransactionHandler {
	return &TransactionHandler{
		ledgerSvc: ledgerSvc,
		nomba:     nombaClient,
		log:       log,
	}
}

// ---------------------------------------------------------------------------
// POST /accounts/{id}/transfer  (optional extension endpoint)
// ---------------------------------------------------------------------------

type transferRequest struct {
	Amount          int64  `json:"amount"` // kobo
	Currency        string `json:"currency"`
	Narration       string `json:"narration"`
	Reference       string `json:"reference"`
	DestinationBank    string `json:"destination_bank"`
	DestinationAccount string `json:"destination_account"`
	DestinationName    string `json:"destination_name"`
}

// Transfer initiates an outbound bank transfer from a customer's virtual account.
func (h *TransactionHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseUUID(w, r, "id")
	if !ok {
		return
	}

	var req transferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive (in kobo)")
		return
	}
	if req.Currency == "" {
		req.Currency = "NGN"
	}

	// 1. Post the debit to the ledger (this validates sufficient funds).
	_, err := h.ledgerSvc.PostDebit(r.Context(), ledger.DebitRequest{
		AccountID:   accountID,
		Amount:      req.Amount,
		Currency:    req.Currency,
		ExternalRef: req.Reference,
		Narration:   req.Narration,
	})
	if err != nil {
		h.log.Error("transfer: ledger debit failed",
			zap.String("account_id", accountID.String()), zap.Error(err))
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// 2. Initiate the actual transfer via Nomba.
	nombaResp, err := h.nomba.Transfer(r.Context(), nomba.TransferRequest{
		Amount:             req.Amount,
		Currency:           req.Currency,
		Narration:          req.Narration,
		Reference:          req.Reference,
		DestinationBank:    req.DestinationBank,
		DestinationAccount: req.DestinationAccount,
		DestinationName:    req.DestinationName,
	})
	if err != nil {
		// NOTE: The ledger debit has already been posted. In production you
		// would reverse it here or use a saga/outbox pattern. For now we log
		// the discrepancy so the reconciliation sweep can detect it.
		h.log.Error("transfer: nomba transfer failed — ledger debit already posted",
			zap.String("account_id", accountID.String()),
			zap.String("reference", req.Reference),
			zap.Error(err),
		)
		writeError(w, http.StatusBadGateway, "transfer submitted to ledger but Nomba rejected: "+err.Error())
		return
	}

	h.log.Info("transfer complete",
		zap.String("account_id", accountID.String()),
		zap.String("nomba_txn_id", nombaResp.Data.TransactionID),
		zap.Int64("amount_kobo", req.Amount),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "submitted",
		"nomba_transaction": nombaResp.Data,
	})
}
