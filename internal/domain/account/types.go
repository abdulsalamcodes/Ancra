package account

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// CreateAccountRequest carries the inputs needed to provision a new DVA.
type CreateAccountRequest struct {
	CustomerID    uuid.UUID
	DisplayName   string
	CustomerEmail string
	PhoneNumber   string
	// Optional KYC fields forwarded to Nomba
	BVN string
	NIN string
}

// CreateAccountResponse is the result returned after a successful account creation.
type CreateAccountResponse struct {
	Account  *store.VirtualAccount  `json:"account"`
	Identity *store.IdentityVersion `json:"identity"`
}

// UpdateAccountRequest carries the fields that may be changed on an account.
type UpdateAccountRequest struct {
	AccountID   uuid.UUID
	DisplayName string // triggers a new IdentityVersion
}

// AccountBalance is the current balance of a virtual account.
type AccountBalance struct {
	AccountID uuid.UUID
	Balance   int64  // kobo
	Currency  string
	AsOf      time.Time
}

// TransactionPage is a paginated list of ledger entries.
type TransactionPage struct {
	Entries []*store.LedgerEntry
	Limit   int
	Offset  int
}

// StatementEntry is a ledger entry annotated with a running balance.
type StatementEntry struct {
	ID             uuid.UUID         `json:"id"`
	Direction      store.Direction   `json:"direction"`
	Amount         int64             `json:"amount_kobo"`
	Currency       string            `json:"currency"`
	EntryType      string            `json:"entry_type"`
	ExternalRef    string            `json:"external_ref"`
	RunningBalance int64             `json:"running_balance_kobo"`
	CreatedAt      time.Time         `json:"created_at"`
}

// StatementPage is a paginated account statement with correct running balances.
type StatementPage struct {
	AccountID      uuid.UUID        `json:"account_id"`
	OpeningBalance int64            `json:"opening_balance_kobo"`
	ClosingBalance int64            `json:"closing_balance_kobo"`
	Currency       string           `json:"currency"`
	Entries        []StatementEntry `json:"entries"`
	Limit          int              `json:"limit"`
	Offset         int              `json:"offset"`
}
