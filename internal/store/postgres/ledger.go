package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// LedgerStore is the Postgres implementation of store.LedgerStore.
type LedgerStore struct{ *DB }

// NewLedgerStore creates a LedgerStore backed by the given pool.
func NewLedgerStore(db *DB) *LedgerStore { return &LedgerStore{db} }

// InsertEntries writes all entries in a single batch within an implicit
// transaction. Ledger entries are append-only; no UPDATE/DELETE is ever issued.
func (s *LedgerStore) InsertEntries(ctx context.Context, entries []*store.LedgerEntry) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ledger.InsertEntries: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const q = `
		INSERT INTO ledger_entries
			(id, account_id, direction, amount, currency, txn_group_id, external_ref, entry_type, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`

	for _, e := range entries {
		if _, err := tx.Exec(ctx, q,
			e.ID, e.AccountID, string(e.Direction),
			e.Amount, e.Currency, e.TxnGroupID,
			e.ExternalRef, e.EntryType, e.CreatedAt,
		); err != nil {
			return fmt.Errorf("ledger.InsertEntries: insert %s: %w", e.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ledger.InsertEntries: commit: %w", err)
	}
	return nil
}

// GetBalance returns (sum of credits) − (sum of debits) in kobo for the given account.
func (s *LedgerStore) GetBalance(ctx context.Context, accountID uuid.UUID) (int64, error) {
	const q = `
		SELECT
			COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE 0 END), 0)
			- COALESCE(SUM(CASE WHEN direction = 'debit'  THEN amount ELSE 0 END), 0)
		FROM ledger_entries
		WHERE account_id = $1`

	var balance int64
	if err := s.Pool.QueryRow(ctx, q, accountID).Scan(&balance); err != nil {
		return 0, fmt.Errorf("ledger.GetBalance: %w", err)
	}
	return balance, nil
}

// GetBalanceAsOf returns the net balance up to and including asOf timestamp.
// This is used to compute correct running balances on paginated statements.
func (s *LedgerStore) GetBalanceAsOf(ctx context.Context, accountID uuid.UUID, asOf time.Time) (int64, error) {
	const q = `
		SELECT
			COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE 0 END), 0)
			- COALESCE(SUM(CASE WHEN direction = 'debit'  THEN amount ELSE 0 END), 0)
		FROM ledger_entries
		WHERE account_id = $1 AND created_at <= $2`

	var balance int64
	if err := s.Pool.QueryRow(ctx, q, accountID, asOf).Scan(&balance); err != nil {
		return 0, fmt.Errorf("ledger.GetBalanceAsOf: %w", err)
	}
	return balance, nil
}

// ListEntries returns ledger entries for an account, newest first.
func (s *LedgerStore) ListEntries(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]*store.LedgerEntry, error) {
	const q = `
		SELECT id, account_id, direction, amount, currency, txn_group_id, external_ref, entry_type, created_at
		FROM ledger_entries
		WHERE account_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.Pool.Query(ctx, q, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("ledger.ListEntries: %w", err)
	}
	defer rows.Close()

	var entries []*store.LedgerEntry
	for rows.Next() {
		e := &store.LedgerEntry{}
		var direction string
		if err := rows.Scan(
			&e.ID, &e.AccountID, &direction,
			&e.Amount, &e.Currency, &e.TxnGroupID,
			&e.ExternalRef, &e.EntryType, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ledger.ListEntries.scan: %w", err)
		}
		e.Direction = store.Direction(direction)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetSystemAccount retrieves a named system account (pool, suspense, fees, returns_clearing).
func (s *LedgerStore) GetSystemAccount(ctx context.Context, name string) (*store.SystemAccount, error) {
	const q = `SELECT id, name FROM system_accounts WHERE name = $1`
	var sa store.SystemAccount
	if err := s.Pool.QueryRow(ctx, q, name).Scan(&sa.ID, &sa.Name); err != nil {
		return nil, fmt.Errorf("ledger.GetSystemAccount(%q): %w", name, err)
	}
	return &sa, nil
}
