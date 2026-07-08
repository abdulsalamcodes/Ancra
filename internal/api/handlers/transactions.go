package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
)

// TransactionHandler handles outbound transfer requests.
type TransactionHandler struct {
	ledgerSvc    *ledger.Service
	nombaFactory *nomba.ClientFactory
	nombaGlobal  *nomba.Client // fallback when org has no BYOK config
	log          *zap.Logger
}

// NewTransactionHandler constructs a TransactionHandler.
func NewTransactionHandler(ledgerSvc *ledger.Service, factory *nomba.ClientFactory, globalClient *nomba.Client, log *zap.Logger) *TransactionHandler {
	return &TransactionHandler{
		ledgerSvc:    ledgerSvc,
		nombaFactory: factory,
		nombaGlobal:  globalClient,
		log:          log,
	}
}

// nombaClientForOrg returns the per-org Nomba client from the factory, falling
// back to the global client for single-tenant deployments that have no BYOK config.
func (h *TransactionHandler) nombaClientForOrg(ctx context.Context, orgID uuid.UUID) (*nomba.Client, error) {
	if h.nombaFactory != nil {
		client, err := h.nombaFactory.ForOrg(ctx, orgID)
		if err == nil {
			return client, nil
		}
		// Factory miss — org has no BYOK config; try the global client.
		h.log.Debug("nomba factory miss, falling back to global client",
			zap.String("org_id", orgID.String()), zap.Error(err))
	}
	if h.nombaGlobal != nil {
		return h.nombaGlobal, nil
	}
	return nil, fmt.Errorf("no nomba client available for org %s", orgID)
}

type bankLookupRequest struct {
	AccountNumber string `json:"account_number"`
	BankCode      string `json:"bank_code"`
}

// LookupBank resolves an account number + bank code to the registered account name.
func (h *TransactionHandler) LookupBank(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	var req bankLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.AccountNumber == "" || req.BankCode == "" {
		writeError(w, http.StatusBadRequest, "account_number and bank_code are required")
		return
	}

	client, err := h.nombaClientForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "payment provider not configured for this organisation")
		return
	}

	resp, err := client.LookupBankAccount(r.Context(), nomba.BankLookupRequest{
		AccountNumber: req.AccountNumber,
		BankCode:      req.BankCode,
	})
	if err != nil {
		h.log.Error("bank lookup failed", zap.Error(err))
		writeError(w, http.StatusBadGateway, "bank account lookup failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"account_number": resp.Data.AccountNumber,
		"account_name":   resp.Data.AccountName,
	})
}

type transferRequest struct {
	Amount             int64  `json:"amount"`              // kobo
	Narration          string `json:"narration"`
	Reference          string `json:"reference"`           // → merchantTxRef
	SenderName         string `json:"sender_name"`
	DestinationBank    string `json:"destination_bank"`    // → bankCode
	DestinationAccount string `json:"destination_account"` // → accountNumber
	DestinationName    string `json:"destination_name"`    // → accountName
}

// Transfer initiates an outbound bank transfer from a customer's virtual account.
//
// Order of operations:
//  1. Validate input.
//  2. Post a ledger debit (validates sufficient funds atomically).
//  3. Submit the transfer to Nomba.
//  4. If Nomba rejects, reverse the ledger debit so the invariant is restored.
func (h *TransactionHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

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

	nombaClient, err := h.nombaClientForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "payment provider not configured for this organisation")
		return
	}

	_, err = h.ledgerSvc.PostDebit(r.Context(), ledger.DebitRequest{
		OrgID:       orgID,
		AccountID:   accountID,
		Amount:      req.Amount,
		Currency:    "NGN",
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

	nombaResp, nombaErr := nombaClient.Transfer(r.Context(), nomba.TransferRequest{
		Amount:        float64(req.Amount) / 100, // kobo → naira
		AccountNumber: req.DestinationAccount,
		AccountName:   req.DestinationName,
		BankCode:      req.DestinationBank,
		MerchantTxRef: req.Reference,
		SenderName:    req.SenderName,
	})
	if nombaErr != nil {
		h.log.Error("transfer: nomba rejected — reversing ledger debit",
			zap.String("account_id", accountID.String()),
			zap.String("reference", req.Reference),
			zap.Error(nombaErr),
		)
		h.reverseLedgerDebit(r.Context(), orgID, accountID, req.Amount, "NGN", req.Reference)
		writeError(w, http.StatusBadGateway, "transfer rejected by payment provider")
		return
	}

	h.log.Info("transfer complete",
		zap.String("account_id", accountID.String()),
		zap.String("nomba_txn_id", nombaResp.Data.ID),
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
func (h *TransactionHandler) reverseLedgerDebit(ctx context.Context, orgID uuid.UUID, accountID uuid.UUID, amount int64, currency, reference string) {
	_, err := h.ledgerSvc.PostCredit(ctx, ledger.CreditRequest{
		OrgID:       orgID,
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
	if req.Amount <= 0 {
		return newValidationError("amount must be a positive kobo value")
	}
	var missing []string
	if req.Reference == "" {
		missing = append(missing, "reference")
	}
	if req.SenderName == "" {
		missing = append(missing, "sender_name")
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
