// Package ledger implements double-entry bookkeeping for Ancra.
//
// Every financial movement is posted as a pair of offsetting entries:
//   - An inbound credit posts: credit → customer account, debit → pool account.
//   - An outbound debit posts: debit → customer account, credit → pool account.
//
// All amounts are in kobo (1 NGN = 100 kobo).
package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

const (
	poolAccountName    = "pool"
	suspenseAccountName = "suspense"
)

// Service handles double-entry postings to the ledger.
type Service struct {
	ledger store.LedgerStore
	log    *zap.Logger
}

// NewService constructs a ledger Service.
func NewService(ledger store.LedgerStore, log *zap.Logger) *Service {
	return &Service{ledger: ledger, log: log}
}

// PostCredit records an inbound credit on a customer account.
// It posts two entries in the same transaction group:
//   - CREDIT customer account (money arrived)
//   - DEBIT  pool system account (pool liability increases)
func (s *Service) PostCredit(ctx context.Context, req CreditRequest) (*PostingResult, error) {
	pool, err := s.ledger.GetSystemAccount(ctx, req.OrgID, poolAccountName)
	if err != nil {
		return nil, fmt.Errorf("ledger.PostCredit: pool account: %w", err)
	}

	txnGroupID := uuid.New()
	now := time.Now().UTC()

	customerEntryType := req.EntryType
	if customerEntryType == "" {
		customerEntryType = "inbound_credit"
	}

	entries := []*store.LedgerEntry{
		{
			ID:          uuid.New(),
			AccountID:   req.AccountID,
			Direction:   store.DirectionCredit,
			Amount:      req.Amount,
			Currency:    req.Currency,
			TxnGroupID:  txnGroupID,
			ExternalRef: req.ExternalRef,
			EntryType:   customerEntryType,
			CreatedAt:   now,
		},
		{
			ID:          uuid.New(),
			AccountID:   pool.ID,
			Direction:   store.DirectionDebit,
			Amount:      req.Amount,
			Currency:    req.Currency,
			TxnGroupID:  txnGroupID,
			ExternalRef: req.ExternalRef,
			EntryType:   "pool_liability",
			CreatedAt:   now,
		},
	}

	if err := s.ledger.InsertEntries(ctx, entries); err != nil {
		return nil, fmt.Errorf("ledger.PostCredit: insert: %w", err)
	}

	s.log.Info("ledger credit posted",
		zap.String("account_id", req.AccountID.String()),
		zap.Int64("amount_kobo", req.Amount),
		zap.String("external_ref", req.ExternalRef),
		zap.String("txn_group_id", txnGroupID.String()),
	)

	return &PostingResult{TxnGroupID: txnGroupID, Entries: entries}, nil
}

// PostDebit records an outbound debit on a customer account.
// It posts two entries:
//   - DEBIT  customer account (money leaving)
//   - CREDIT pool system account (pool liability decreases)
func (s *Service) PostDebit(ctx context.Context, req DebitRequest) (*PostingResult, error) {
	// Guard: ensure the account has sufficient balance.
	balance, err := s.ledger.GetBalance(ctx, req.AccountID)
	if err != nil {
		return nil, fmt.Errorf("ledger.PostDebit: balance check: %w", err)
	}
	if balance < req.Amount {
		return nil, fmt.Errorf("ledger.PostDebit: insufficient funds — balance %d kobo, requested %d kobo", balance, req.Amount)
	}

	pool, err := s.ledger.GetSystemAccount(ctx, req.OrgID, poolAccountName)
	if err != nil {
		return nil, fmt.Errorf("ledger.PostDebit: pool account: %w", err)
	}

	txnGroupID := uuid.New()
	now := time.Now().UTC()

	entries := []*store.LedgerEntry{
		{
			ID:          uuid.New(),
			AccountID:   req.AccountID,
			Direction:   store.DirectionDebit,
			Amount:      req.Amount,
			Currency:    req.Currency,
			TxnGroupID:  txnGroupID,
			ExternalRef: req.ExternalRef,
			EntryType:   "transfer_out",
			CreatedAt:   now,
		},
		{
			ID:          uuid.New(),
			AccountID:   pool.ID,
			Direction:   store.DirectionCredit,
			Amount:      req.Amount,
			Currency:    req.Currency,
			TxnGroupID:  txnGroupID,
			ExternalRef: req.ExternalRef,
			EntryType:   "pool_release",
			CreatedAt:   now,
		},
	}

	if err := s.ledger.InsertEntries(ctx, entries); err != nil {
		return nil, fmt.Errorf("ledger.PostDebit: insert: %w", err)
	}

	s.log.Info("ledger debit posted",
		zap.String("account_id", req.AccountID.String()),
		zap.Int64("amount_kobo", req.Amount),
		zap.String("external_ref", req.ExternalRef),
		zap.String("txn_group_id", txnGroupID.String()),
	)

	return &PostingResult{TxnGroupID: txnGroupID, Entries: entries}, nil
}

// GetBalance returns the net balance in kobo for an account.
func (s *Service) GetBalance(ctx context.Context, accountID uuid.UUID) (int64, error) {
	balance, err := s.ledger.GetBalance(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("ledger.GetBalance: %w", err)
	}
	return balance, nil
}

// PostSuspense moves an amount into the suspense system account when the
// destination customer account cannot be determined (e.g. unknown bank number)
// or belongs to a closed account.
//
// Pass uuid.Nil for orgID when the org is unknown (e.g. unresolvable bank number);
// in that case the global (NULL org_id) suspense account is used.
func (s *Service) PostSuspense(ctx context.Context, orgID uuid.UUID, amount int64, currency, externalRef string) (*PostingResult, error) {
	suspense, err := s.ledger.GetSystemAccount(ctx, orgID, suspenseAccountName)
	if err != nil {
		return nil, fmt.Errorf("ledger.PostSuspense: suspense account: %w", err)
	}

	txnGroupID := uuid.New()
	now := time.Now().UTC()

	entries := []*store.LedgerEntry{
		{
			ID:          uuid.New(),
			AccountID:   suspense.ID,
			Direction:   store.DirectionCredit,
			Amount:      amount,
			Currency:    currency,
			TxnGroupID:  txnGroupID,
			ExternalRef: externalRef,
			EntryType:   "suspense_credit",
			CreatedAt:   now,
		},
	}

	if err := s.ledger.InsertEntries(ctx, entries); err != nil {
		return nil, fmt.Errorf("ledger.PostSuspense: insert: %w", err)
	}

	s.log.Warn("amount posted to suspense",
		zap.Int64("amount_kobo", amount),
		zap.String("external_ref", externalRef),
	)

	return &PostingResult{TxnGroupID: txnGroupID, Entries: entries}, nil
}
