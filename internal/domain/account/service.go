// Package account provides business logic for creating and managing dedicated
// virtual accounts (DVAs) backed by Nomba.
package account

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// Service orchestrates virtual-account operations across the Nomba API and the
// local data stores.
type Service struct {
	accounts  store.AccountStore
	customers store.CustomerStore
	ledger    store.LedgerStore
	nomba     *nomba.Client
	log       *zap.Logger
}

// NewService constructs an account Service.
func NewService(
	accounts store.AccountStore,
	customers store.CustomerStore,
	ledger store.LedgerStore,
	nombaClient *nomba.Client,
	log *zap.Logger,
) *Service {
	return &Service{
		accounts:  accounts,
		customers: customers,
		ledger:    ledger,
		nomba:     nombaClient,
		log:       log,
	}
}

// Create provisions a new DVA for the given customer. It:
//  1. Verifies the customer exists in our DB.
//  2. Calls the Nomba API to create the virtual account.
//  3. Persists the account record locally.
//  4. Creates the initial identity version.
func (s *Service) Create(ctx context.Context, req CreateAccountRequest) (*CreateAccountResponse, error) {
	// 1. Verify customer exists.
	customer, err := s.customers.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("account.Create: customer lookup: %w", err)
	}

	accountRef := uuid.New().String()

	// 2. Call Nomba.
	nombaResp, err := s.nomba.CreateVirtualAccount(ctx, nomba.CreateVirtualAccountRequest{
		AccountName:   req.DisplayName,
		AccountRef:    accountRef,
		CustomerEmail: req.CustomerEmail,
		CustomerName:  req.DisplayName,
		BVN:           req.BVN,
		NIN:           req.NIN,
	})
	if err != nil {
		return nil, fmt.Errorf("account.Create: nomba: %w", err)
	}

	s.log.Info("nomba virtual account created",
		zap.String("account_ref", accountRef),
		zap.String("account_number", nombaResp.Data.AccountNumber),
	)

	now := time.Now().UTC()

	// 3. Persist locally.
	va := &store.VirtualAccount{
		ID:                uuid.New(),
		CustomerID:        customer.ID,
		AccountRef:        accountRef,
		BankAccountNumber: nombaResp.Data.AccountNumber,
		BankAccountName:   nombaResp.Data.AccountName,
		Status:            store.AccountStatusActive,
		CreatedAt:         now,
	}
	if err := s.accounts.CreateAccount(ctx, va); err != nil {
		return nil, fmt.Errorf("account.Create: persist account: %w", err)
	}

	// 4. Create initial identity version.
	iv := &store.IdentityVersion{
		ID:            uuid.New(),
		CustomerID:    customer.ID,
		DisplayName:   req.DisplayName,
		EffectiveFrom: now,
		EffectiveTo:   nil, // currently active
	}
	if err := s.customers.CreateIdentityVersion(ctx, iv); err != nil {
		return nil, fmt.Errorf("account.Create: identity version: %w", err)
	}

	return &CreateAccountResponse{Account: va, Identity: iv}, nil
}

// Get retrieves a virtual account by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*store.VirtualAccount, error) {
	va, err := s.accounts.GetAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("account.Get: %w", err)
	}
	return va, nil
}

// GetBalance computes the current balance of a virtual account from the ledger.
func (s *Service) GetBalance(ctx context.Context, accountID uuid.UUID) (*AccountBalance, error) {
	if _, err := s.accounts.GetAccount(ctx, accountID); err != nil {
		return nil, fmt.Errorf("account.GetBalance: not found: %w", err)
	}

	balance, err := s.ledger.GetBalance(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("account.GetBalance: ledger: %w", err)
	}

	return &AccountBalance{
		AccountID: accountID,
		Balance:   balance,
		Currency:  "NGN",
		AsOf:      time.Now().UTC(),
	}, nil
}

// ListTransactions returns a page of ledger entries for an account.
func (s *Service) ListTransactions(ctx context.Context, accountID uuid.UUID, limit, offset int) (*TransactionPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	entries, err := s.ledger.ListEntries(ctx, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("account.ListTransactions: %w", err)
	}

	return &TransactionPage{Entries: entries, Limit: limit, Offset: offset}, nil
}

// Update changes the display name of an account by closing the current
// identity version and opening a new one.
func (s *Service) Update(ctx context.Context, req UpdateAccountRequest) error {
	now := time.Now().UTC()

	// Close current identity version.
	current, err := s.customers.GetCurrentIdentity(ctx, req.AccountID)
	if err != nil {
		return fmt.Errorf("account.Update: current identity: %w", err)
	}
	if err := s.customers.CloseIdentityVersion(ctx, current.ID, now); err != nil {
		return fmt.Errorf("account.Update: close identity: %w", err)
	}

	// Open new identity version.
	newVersion := &store.IdentityVersion{
		ID:            uuid.New(),
		CustomerID:    current.CustomerID,
		DisplayName:   req.DisplayName,
		EffectiveFrom: now,
		EffectiveTo:   nil,
	}
	if err := s.customers.CreateIdentityVersion(ctx, newVersion); err != nil {
		return fmt.Errorf("account.Update: new identity: %w", err)
	}

	s.log.Info("account identity updated",
		zap.String("account_id", req.AccountID.String()),
		zap.String("new_name", req.DisplayName),
	)
	return nil
}

// Close marks a virtual account as closed.
func (s *Service) Close(ctx context.Context, id uuid.UUID) error {
	va, err := s.accounts.GetAccount(ctx, id)
	if err != nil {
		return fmt.Errorf("account.Close: not found: %w", err)
	}
	if va.Status == store.AccountStatusClosed {
		return fmt.Errorf("account.Close: account %s is already closed", id)
	}

	if err := s.accounts.UpdateAccountStatus(ctx, id, store.AccountStatusClosed); err != nil {
		return fmt.Errorf("account.Close: update status: %w", err)
	}

	s.log.Info("account closed", zap.String("account_id", id.String()))
	return nil
}
