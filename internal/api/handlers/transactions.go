package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
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

type transferRequest struct {
	Amount             int64  `json:"amount"` // kobo
	Currency           string `json:"currency"`
	Narration          string `json:"narration"`
	Reference          string `json:"reference"`
	DestinationBank    string `json:"destination_bank"`
	DestinationAccount string `json:"destination_account"`
	DestinationName    string `json:"destination_name"`
}

// Transfer initiates an outbound bank transfer from a customer's virtual account.
//
// Order of operations:
//  1. Validate input.
//  2. Post a ledger debit (validates sufficient funds atomically).
//  3. Submit the transfer to Nomba.
//  4. If Nomba rejects, reverse the ledger debit so the invariant is restored.
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

	if err := validateTransferRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Currency == "" {
		req.Currency = "NGN"
	}

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
		if strings.Contains(err.Error(), "insufficient funds") {
			writeError(w, http.StatusUnprocessableEntity, "insufficient funds")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to process transfer")
		return
	}

	nombaResp, nombaErr := h.nomba.Transfer(r.Context(), nomba.TransferRequest{
		Amount:             req.Amount,
		Currency:           req.Currency,
		Narration:          req.Narration,
		Reference:          req.Reference,
		DestinationBank:    req.DestinationBank,
		DestinationAccount: req.DestinationAccount,
		DestinationName:    req.DestinationName,
	})
	if nombaErr != nil {
		h.log.Error("transfer: nomba rejected — reversing ledger debit",
			zap.String("account_id", accountID.String()),
			zap.String("reference", req.Reference),
			zap.Error(nombaErr),
		)
		h.reverseLedgerDebit(r.Context(), accountID, req.Amount, req.Currency, req.Reference)
		writeError(w, http.StatusBadGateway, "transfer rejected by payment provider")
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

// reverseLedgerDebit posts a compensating credit when a Nomba transfer fails
// after the ledger debit has already been written. If the reversal itself fails,
// the discrepancy will be caught by the next reconciliation sweep.
func (h *TransactionHandler) reverseLedgerDebit(ctx context.Context, accountID uuid.UUID, amount int64, currency, reference string) {
	_, err := h.ledgerSvc.PostCredit(ctx, ledger.CreditRequest{
		AccountID:   accountID,
		Amount:      amount,
		Currency:    currency,
		ExternalRef: reference + "_reversal",
		Narration:   "reversal: nomba transfer rejected",
		EntryType:   "transfer_reversal",
	})
	if err != nil {
		h.log.Error("transfer: CRITICAL — ledger reversal failed; reconciliation sweep will detect delta",
			zap.String("account_id", accountID.String()),
			zap.String("reference", reference),
			zap.Error(err),
		)
	}
}

func validateTransferRequest(req transferRequest) error {
	var missing []string
	if req.Amount <= 0 {
		return newValidationError("amount must be a positive kobo value")
	}
	if req.Reference == "" {
		missing = append(missing, "reference")
	}
	if req.DestinationBank == "" {
		missing = append(missing, "destination_bank")
	}
	if req.DestinationAccount == "" {
		missing = append(missing, "destination_account")
	}
	if req.DestinationName == "" {
		missing = append(missing, "destination_name")
	}
	if len(missing) > 0 {
		return newValidationError("missing required fields: " + strings.Join(missing, ", "))
	}
	return nil
}
