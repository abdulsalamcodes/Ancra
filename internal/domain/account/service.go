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
	"github.com/abdulsalamcodes/ancra/internal/tenant"
)

// Service orchestrates virtual-account operations across the Nomba API and the
// local data stores.
type Service struct {
	accounts      store.AccountStore
	customers     store.CustomerStore
	ledger        store.LedgerStore
	nombaFactory  *nomba.ClientFactory
	log           *zap.Logger
}

// NewService constructs an account Service.
func NewService(
	accounts store.AccountStore,
	customers store.CustomerStore,
	ledger store.LedgerStore,
	nombaFactory *nomba.ClientFactory,
	log *zap.Logger,
) *Service {
	return &Service{
		accounts:     accounts,
		customers:    customers,
		ledger:       ledger,
		nombaFactory: nombaFactory,
		log:          log,
	}
}

// Create provisions a new DVA for the given customer. It:
//  1. Verifies the customer exists in our DB.
//  2. Calls the Nomba API to create the virtual account.
//  3. Persists the account record locally.
//  4. Creates the initial identity version.
func (s *Service) Create(ctx context.Context, req CreateAccountRequest) (*CreateAccountResponse, error) {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// 1. Verify customer exists within this org.
	customer, err := s.customers.GetCustomer(ctx, orgID, req.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("account.Create: customer lookup: %w", err)
	}

	// 1a. Idempotency: return the existing active account rather than
	// provisioning a duplicate on Nomba. accountRef is the customer UUID so
	// the same customer always maps to the same Nomba virtual account.
	existing, _ := s.accounts.ListAccountsByCustomer(ctx, orgID, customer.ID)
	for _, va := range existing {
		if va.Status == store.AccountStatusActive {
			identity, _ := s.customers.GetCurrentIdentity(ctx, customer.ID)
			return &CreateAccountResponse{Account: va, Identity: identity}, nil
		}
	}

	accountRef := customer.ID.String()

	nombaClient, err := s.nombaFactory.ForOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("account.Create: nomba client: %w", err)
	}

	nombaResp, err := nombaClient.CreateVirtualAccount(ctx, nomba.CreateVirtualAccountRequest{
		AccountName: req.DisplayName,
		AccountRef:  accountRef,
		Currency:    "NGN",
		BVN:         req.BVN,
	})
	if err != nil {
		return nil, fmt.Errorf("account.Create: nomba: %w", err)
	}

	s.log.Info("nomba virtual account created",
		zap.String("account_ref", accountRef),
		zap.String("bank_account_number", nombaResp.Data.BankAccountNumber),
	)

	now := time.Now().UTC()

	// 3. Persist locally.
	va := &store.VirtualAccount{
		ID:                uuid.New(),
		OrgID:             orgID,
		CustomerID:        customer.ID,
		AccountRef:        accountRef,
		BankAccountNumber: nombaResp.Data.BankAccountNumber,
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

// ListAccounts returns a paginated list of virtual accounts for the requesting org.
func (s *Service) ListAccounts(ctx context.Context, limit, offset int) ([]*store.VirtualAccount, error) {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	accounts, err := s.accounts.ListAccounts(ctx, orgID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("account.ListAccounts: %w", err)
	}
	if accounts == nil {
		accounts = []*store.VirtualAccount{}
	}
	return accounts, nil
}

// Get retrieves a virtual account by ID, scoped to the requesting org.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*store.VirtualAccount, error) {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return nil, err
	}
	va, err := s.accounts.GetAccount(ctx, orgID, id)
	if err != nil {
		return nil, fmt.Errorf("account.Get: %w", err)
	}
	return va, nil
}

// GetBalance computes the current balance of a virtual account from the ledger.
func (s *Service) GetBalance(ctx context.Context, accountID uuid.UUID) (*AccountBalance, error) {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.accounts.GetAccount(ctx, orgID, accountID); err != nil {
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
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return nil, err
	}
	// Verify account belongs to the requesting org before returning ledger data.
	if _, err := s.accounts.GetAccount(ctx, orgID, accountID); err != nil {
		return nil, fmt.Errorf("account.ListTransactions: not found: %w", err)
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	entries, err := s.ledger.ListEntries(ctx, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("account.ListTransactions: %w", err)
	}

	return &TransactionPage{Entries: entries, Limit: limit, Offset: offset}, nil
}

// GetStatement returns a paginated account statement with correct running
// balances per entry. Unlike ListTransactions, the running balance is computed
// as of the page boundary — not against the current total balance — so it
// remains accurate across all pages.
func (s *Service) GetStatement(ctx context.Context, accountID uuid.UUID, limit, offset int) (*StatementPage, error) {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.accounts.GetAccount(ctx, orgID, accountID); err != nil {
		return nil, fmt.Errorf("account.GetStatement: not found: %w", err)
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	entries, err := s.ledger.ListEntries(ctx, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("account.GetStatement: list entries: %w", err)
	}

	// Closing balance = balance as of the newest entry on this page.
	// Falls back to 0 when the page is empty.
	var closingBalance int64
	if len(entries) > 0 {
		closingBalance, err = s.ledger.GetBalanceAsOf(ctx, accountID, entries[0].CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("account.GetStatement: closing balance: %w", err)
		}
	}

	// Walk entries newest → oldest, undoing each entry to derive the balance
	// before it. This gives the correct running_balance for every page.
	statementEntries := make([]StatementEntry, len(entries))
	running := closingBalance
	for i, e := range entries {
		statementEntries[i] = StatementEntry{
			ID:             e.ID,
			Direction:      e.Direction,
			Amount:         e.Amount,
			Currency:       e.Currency,
			EntryType:      e.EntryType,
			ExternalRef:    e.ExternalRef,
			RunningBalance: running,
			CreatedAt:      e.CreatedAt,
		}
		if e.Direction == store.DirectionCredit {
			running -= e.Amount
		} else {
			running += e.Amount
		}
	}

	return &StatementPage{
		AccountID:      accountID,
		OpeningBalance: running, // balance before the oldest entry on this page
		ClosingBalance: closingBalance,
		Currency:       "NGN",
		Entries:        statementEntries,
		Limit:          limit,
		Offset:         offset,
	}, nil
}

// Update changes the display name of an account by closing the current
// identity version and opening a new one.
func (s *Service) Update(ctx context.Context, req UpdateAccountRequest) error {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	// Resolve the virtual account first to get the owning customer ID.
	// req.AccountID is a virtual account ID, not a customer ID.
	va, err := s.accounts.GetAccount(ctx, orgID, req.AccountID)
	if err != nil {
		return fmt.Errorf("account.Update: account lookup: %w", err)
	}

	// Close current identity version.
	current, err := s.customers.GetCurrentIdentity(ctx, va.CustomerID)
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

// parseOrgID extracts and parses the organisation UUID from the request context.
// Returns a clear error if the context is missing org identity, which indicates
// a misconfigured middleware chain rather than a client error.
func parseOrgID(ctx context.Context) (uuid.UUID, error) {
	raw := tenant.OrgIDFromContext(ctx)
	if raw == "" {
		return uuid.UUID{}, fmt.Errorf("account: org identity missing from context")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("account: invalid org id in context: %w", err)
	}
	return id, nil
}

// Close marks a virtual account as closed.
func (s *Service) Close(ctx context.Context, id uuid.UUID) error {
	orgID, err := parseOrgID(ctx)
	if err != nil {
		return err
	}
	va, err := s.accounts.GetAccount(ctx, orgID, id)
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
