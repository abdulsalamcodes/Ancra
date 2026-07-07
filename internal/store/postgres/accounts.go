package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// AccountStore is the Postgres implementation of store.AccountStore.
type AccountStore struct{ *DB }

// NewAccountStore creates an AccountStore backed by the given pool.
func NewAccountStore(db *DB) *AccountStore { return &AccountStore{db} }

// CreateAccount inserts a new virtual account row.
func (s *AccountStore) CreateAccount(ctx context.Context, a *store.VirtualAccount) error {
	const q = `
		INSERT INTO virtual_accounts
			(id, customer_id, account_ref, bank_account_number, bank_account_name, status, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`

	_, err := s.Pool.Exec(ctx, q,
		a.ID, a.CustomerID, a.AccountRef,
		a.BankAccountNumber, a.BankAccountName,
		string(a.Status), a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("accounts.Create: %w", err)
	}
	return nil
}

// GetAccount retrieves a virtual account by primary key.
func (s *AccountStore) GetAccount(ctx context.Context, id uuid.UUID) (*store.VirtualAccount, error) {
	const q = `
		SELECT id, customer_id, account_ref, bank_account_number, bank_account_name, status, created_at
		FROM virtual_accounts WHERE id = $1`

	row := s.Pool.QueryRow(ctx, q, id)
	return scanAccount(row)
}

// GetAccountByNumber retrieves a virtual account by its bank account number.
func (s *AccountStore) GetAccountByNumber(ctx context.Context, accountNumber string) (*store.VirtualAccount, error) {
	const q = `
		SELECT id, customer_id, account_ref, bank_account_number, bank_account_name, status, created_at
		FROM virtual_accounts WHERE bank_account_number = $1`

	row := s.Pool.QueryRow(ctx, q, accountNumber)
	return scanAccount(row)
}

// ListAccounts returns a paginated list of all virtual accounts, newest first.
func (s *AccountStore) ListAccounts(ctx context.Context, limit, offset int) ([]*store.VirtualAccount, error) {
	const q = `
		SELECT id, customer_id, account_ref, bank_account_number, bank_account_name, status, created_at
		FROM virtual_accounts ORDER BY created_at DESC LIMIT $1 OFFSET $2`

	rows, err := s.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("accounts.List: %w", err)
	}
	defer rows.Close()

	var accounts []*store.VirtualAccount
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// ListAccountsByCustomer returns all accounts belonging to a customer.
func (s *AccountStore) ListAccountsByCustomer(ctx context.Context, customerID uuid.UUID) ([]*store.VirtualAccount, error) {
	const q = `
		SELECT id, customer_id, account_ref, bank_account_number, bank_account_name, status, created_at
		FROM virtual_accounts WHERE customer_id = $1 ORDER BY created_at DESC`

	rows, err := s.Pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("accounts.ListByCustomer: %w", err)
	}
	defer rows.Close()

	var accounts []*store.VirtualAccount
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// UpdateAccountStatus changes the status of a virtual account.
func (s *AccountStore) UpdateAccountStatus(ctx context.Context, id uuid.UUID, status store.AccountStatus) error {
	const q = `UPDATE virtual_accounts SET status = $1 WHERE id = $2`
	_, err := s.Pool.Exec(ctx, q, string(status), id)
	if err != nil {
		return fmt.Errorf("accounts.UpdateStatus: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (*store.VirtualAccount, error) {
	var a store.VirtualAccount
	var status string
	err := row.Scan(
		&a.ID, &a.CustomerID, &a.AccountRef,
		&a.BankAccountNumber, &a.BankAccountName,
		&status, &a.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("accounts.scan: %w", err)
	}
	a.Status = store.AccountStatus(status)
	return &a, nil
}
