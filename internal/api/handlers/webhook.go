package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
	"github.com/abdulsalamcodes/ancra/internal/store/postgres"
)

// nilOrgID is used when posting to the global suspense account because
// the org cannot be determined (e.g. the bank account number is unknown).
var nilOrgID = uuid.Nil

// WebhookHandler processes inbound Nomba webhook events.
type WebhookHandler struct {
	verifier *nomba.Verifier
	ledger   *ledger.Service
	accounts store.AccountStore
	events   store.EventStore
	webhooks store.WebhookStore
	log      *zap.Logger
}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler(
	verifier *nomba.Verifier,
	ledgerSvc *ledger.Service,
	accounts store.AccountStore,
	events store.EventStore,
	webhooks store.WebhookStore,
	log *zap.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		verifier: verifier,
		ledger:   ledgerSvc,
		accounts: accounts,
		events:   events,
		webhooks: webhooks,
		log:      log,
	}
}

// HandleNomba is the POST /webhooks/nomba endpoint. It:
//  1. Reads + buffers the raw body.
//  2. Verifies the HMAC-SHA256 signature.
//  3. Decodes the payload.
//  4. Deduplicates using the processed_events table.
//  5. Routes based on event type (currently: collection.credit.success).
//  6. Posts the appropriate ledger entry.
//  7. Enqueues an outbound webhook delivery for the developer.
func (h *WebhookHandler) HandleNomba(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// 1. Verify signature.
	if err := h.verifier.Verify(r, body); err != nil {
		h.log.Warn("webhook: signature verification failed", zap.Error(err))
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	// 2. Decode payload.
	var payload nomba.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	txnID := payload.Data.Transaction.TransactionID
	requestID := payload.RequestID

	h.log.Info("webhook received",
		zap.String("event", payload.EventType),
		zap.String("txn_id", txnID),
		zap.String("request_id", requestID),
	)

	// 3. Idempotency: reject duplicates.
	err = h.events.MarkProcessed(r.Context(), &store.ProcessedEvent{
		TransactionID: txnID,
		RequestID:     requestID,
		ReceivedAt:    time.Now().UTC(),
	})
	if errors.Is(err, postgres.ErrAlreadyProcessed) {
		h.log.Info("webhook: duplicate event, ignoring", zap.String("txn_id", txnID))
		// Respond 200 so Nomba does not retry.
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	if err != nil {
		h.log.Error("webhook: mark processed failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 4. Route by event type.
	switch payload.EventType {
	case "payment_success":
		h.handleCredit(w, r, &payload, body)
	default:
		h.log.Info("webhook: unhandled event type", zap.String("event", payload.EventType))
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}

func (h *WebhookHandler) handleCredit(w http.ResponseWriter, r *http.Request, payload *nomba.WebhookPayload, rawBody []byte) {
	ctx := r.Context()
	txn := payload.Data.Transaction

	// Resolve the destination virtual account by its assigned virtual account number.
	va, err := h.accounts.GetAccountByNumber(ctx, txn.AliasAccountNumber)
	if err != nil {
		h.log.Error("webhook: unknown destination account",
			zap.String("alias_account_number", txn.AliasAccountNumber),
			zap.String("txn_id", txn.TransactionID),
		)
		amountKobo := nairaToKobo(txn.TransactionAmount)
		// Org is unknown — post to global (NULL org) suspense for manual review.
		_, _ = h.ledger.PostSuspense(ctx, nilOrgID, amountKobo, "NGN", txn.TransactionID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "suspense"})
		return
	}

	// Closed accounts must not receive credits — route to suspense so funds
	// can be returned to the sender.
	amountKobo := nairaToKobo(txn.TransactionAmount)
	if va.Status == store.AccountStatusClosed {
		h.log.Warn("webhook: credit to closed account — routing to suspense",
			zap.String("alias_account_number", txn.AliasAccountNumber),
			zap.String("account_id", va.ID.String()),
			zap.String("txn_id", txn.TransactionID),
		)
		_, _ = h.ledger.PostSuspense(ctx, va.OrgID, amountKobo, "NGN", txn.TransactionID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "suspense"})
		return
	}

	_, err = h.ledger.PostCredit(ctx, ledger.CreditRequest{
		OrgID:       va.OrgID,
		AccountID:   va.ID,
		Amount:      amountKobo,
		Currency:    "NGN",
		ExternalRef: txn.TransactionID,
		Narration:   txn.Narration,
	})
	if err != nil {
		h.log.Error("webhook: post credit failed",
			zap.String("txn_id", txn.TransactionID), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to post credit")
		return
	}

	// Enqueue an outbound webhook delivery for the developer.
	// OrgID is taken from the resolved virtual account so the delivery is
	// routed to the correct developer's webhook endpoint in Phase 5.
	now := time.Now().UTC()
	delivery := &store.WebhookDelivery{
		ID:        uuid.New(),
		OrgID:     va.OrgID,
		EventType: payload.EventType,
		Payload:   rawBody,
		Status:    store.WebhookStatusPending,
		Attempts:  0,
		CreatedAt: now,
	}
	if err := h.webhooks.CreateDelivery(ctx, delivery); err != nil {
		h.log.Error("webhook: create delivery record failed", zap.Error(err))
		// Non-fatal — credit was already posted.
	}

	h.log.Info("webhook: credit processed",
		zap.String("txn_id", txn.TransactionID),
		zap.String("account_id", va.ID.String()),
		zap.Int64("amount_kobo", amountKobo),
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "processed"})
}

func nairaToKobo(naira float64) int64 {
	return int64(math.Round(naira * 100))
}
