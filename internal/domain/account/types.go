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
	// Optional KYC fields forwarded to Nomba
	BVN string
	NIN string
}

// CreateAccountResponse is the result returned after a successful account creation.
type CreateAccountResponse struct {
	Account  *store.VirtualAccount
	Identity *store.IdentityVersion
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
