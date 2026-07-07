// Package reconciliation computes the invariant delta between the Nomba master
// wallet balance and the sum of all customer ledger credits (the pool account).
//
// Invariant: nomba_wallet_balance == pool_account_credits - pool_account_debits
//
// Any delta != 0 indicates a missed event or a bookkeeping error and is logged
// as a mismatch for manual review.
package reconciliation

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

const poolAccountName = "pool"

// Service performs reconciliation sweeps.
type Service struct {
	ledger   store.LedgerStore
	recon    store.ReconciliationStore
	accounts store.AccountStore
	events   store.EventStore
	nomba    *nomba.Client
	log      *zap.Logger
}

// NewService constructs a reconciliation Service.
func NewService(
	ledger store.LedgerStore,
	recon store.ReconciliationStore,
	accounts store.AccountStore,
	events store.EventStore,
	nombaClient *nomba.Client,
	log *zap.Logger,
) *Service {
	return &Service{
		ledger:   ledger,
		recon:    recon,
		accounts: accounts,
		events:   events,
		nomba:    nombaClient,
		log:      log,
	}
}

// Run executes a single reconciliation sweep:
//  1. Fetch the current Nomba wallet balance.
//  2. Compute the pool account net balance from the ledger.
//  3. Compute the delta.
//  4. Persist a reconciliation_run row.
//  5. Return the run result.
func (s *Service) Run(ctx context.Context) (*store.ReconciliationRun, error) {
	runAt := time.Now().UTC()

	// 1. Nomba wallet balance (returned as a naira number-string; convert to kobo).
	balResp, err := s.nomba.GetWalletBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconciliation.Run: get nomba balance: %w", err)
	}
	balanceNaira, err := balResp.Data.Amount.Float64()
	if err != nil {
		return nil, fmt.Errorf("reconciliation.Run: parse nomba balance %q: %w", balResp.Data.Amount, err)
	}
	nombaKobo := nairaToKobo(balanceNaira)

	// 2. Pool balance from ledger.
	pool, err := s.ledger.GetSystemAccount(ctx, poolAccountName)
	if err != nil {
		return nil, fmt.Errorf("reconciliation.Run: pool account: %w", err)
	}
	poolKobo, err := s.ledger.GetBalance(ctx, pool.ID)
	if err != nil {
		return nil, fmt.Errorf("reconciliation.Run: pool balance: %w", err)
	}

	// 3. Delta: positive means Nomba holds more than we have booked.
	delta := nombaKobo - poolKobo

	status := store.ReconciliationStatusOK
	if delta != 0 {
		status = store.ReconciliationStatusMismatch
		s.log.Warn("reconciliation mismatch detected",
			zap.Int64("nomba_kobo", nombaKobo),
			zap.Int64("pool_kobo", poolKobo),
			zap.Int64("delta_kobo", delta),
		)
	} else {
		s.log.Info("reconciliation OK",
			zap.Int64("balance_kobo", nombaKobo),
		)
	}

	// 4. Persist.
	run := &store.ReconciliationRun{
		ID:                  uuid.New(),
		RunAt:               runAt,
		NombaWalletBalance:  nombaKobo,
		ComputedPoolBalance: poolKobo,
		Delta:               delta,
		Status:              status,
	}
	if err := s.recon.InsertRun(ctx, run); err != nil {
		return nil, fmt.Errorf("reconciliation.Run: persist: %w", err)
	}

	return run, nil
}

// BackfillMissedCredits fetches recent Nomba transactions and posts any credits
// that were not captured by the webhook (e.g. due to downtime). It limits the
// look-back to the window provided by the caller.
func (s *Service) BackfillMissedCredits(ctx context.Context, accounts store.AccountStore, ledger store.LedgerStore, since time.Time) error {
	listReq := nomba.ListTransactionsRequest{
		StartDate: since,
		EndDate:   time.Now().UTC(),
		Page:      1,
		Limit:     100,
	}

	resp, err := s.nomba.ListTransactions(ctx, listReq)
	if err != nil {
		return fmt.Errorf("reconciliation.Backfill: list transactions: %w", err)
	}

	backfilled := 0
	for _, txn := range resp.Data.Transactions {
		if txn.Type != "CREDIT" || txn.Status != "SUCCESSFUL" {
			continue
		}

		// Check idempotency — skip already-processed transactions.
		processed, err := s.events.IsProcessed(ctx, txn.TransactionID)
		if err != nil {
			s.log.Error("backfill: idempotency check failed",
				zap.String("txn_id", txn.TransactionID), zap.Error(err))
			continue
		}
		if processed {
			continue
		}

		// Look up the destination virtual account by its assigned bank account number.
		// For inbound virtual-account credits Nomba includes aliasAccountNumber;
		// fall back to recipientNumber for any older / alternate response shapes.
		accountNumber := txn.AliasAccountNumber
		if accountNumber == "" {
			accountNumber = txn.RecipientNumber
		}
		if accountNumber == "" {
			s.log.Warn("backfill: no account number on credit transaction",
				zap.String("txn_id", txn.TransactionID))
			continue
		}
		va, err := accounts.GetAccountByNumber(ctx, accountNumber)
		if err != nil {
			s.log.Warn("backfill: unknown destination account",
				zap.String("account_number", accountNumber),
				zap.String("txn_id", txn.TransactionID),
			)
			continue
		}

		// Post the credit.
		amountKobo := nairaToKobo(txn.Amount)
		entries := []*store.LedgerEntry{
			{
				ID:          uuid.New(),
				AccountID:   va.ID,
				Direction:   store.DirectionCredit,
				Amount:      amountKobo,
				Currency:    txn.Currency,
				TxnGroupID:  uuid.New(),
				ExternalRef: txn.TransactionID,
				EntryType:   "backfill_credit",
				CreatedAt:   txn.CreatedAt,
			},
		}
		if err := ledger.InsertEntries(ctx, entries); err != nil {
			s.log.Error("backfill: insert entries failed",
				zap.String("txn_id", txn.TransactionID), zap.Error(err))
			continue
		}

		// Mark as processed.
		_ = s.events.MarkProcessed(ctx, &store.ProcessedEvent{
			TransactionID: txn.TransactionID,
			RequestID:     "backfill",
			ReceivedAt:    time.Now().UTC(),
		})

		backfilled++
		s.log.Info("backfill credit posted",
			zap.String("txn_id", txn.TransactionID),
			zap.String("account_id", va.ID.String()),
			zap.Int64("amount_kobo", amountKobo),
		)
	}

	if backfilled > 0 {
		s.log.Info("backfill complete", zap.Int("count", backfilled))
	}

	return nil
}

// GetLatestRun returns the most recent reconciliation run for display.
func (s *Service) GetLatestRun(ctx context.Context) (*store.ReconciliationRun, error) {
	return s.recon.GetLatestRun(ctx)
}

// ListRuns returns a page of reconciliation runs.
func (s *Service) ListRuns(ctx context.Context, limit, offset int) ([]*store.ReconciliationRun, error) {
	return s.recon.ListRuns(ctx, limit, offset)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// nairaToKobo converts a naira float64 value (as returned by Nomba) to kobo int64.
// We round to the nearest kobo to avoid floating-point drift.
func nairaToKobo(naira float64) int64 {
	return int64(math.Round(naira * 100))
}

