package ledger

import (
	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// CreditRequest describes an inbound credit to be posted to the ledger.
// Money flows: Nomba pool account → customer virtual account.
type CreditRequest struct {
	AccountID   uuid.UUID
	Amount      int64  // kobo
	Currency    string
	ExternalRef string // Nomba transactionId
	Narration   string
}

// DebitRequest describes an outbound debit from a customer account.
// Money flows: customer virtual account → Nomba pool account (then onward).
type DebitRequest struct {
	AccountID   uuid.UUID
	Amount      int64  // kobo
	Currency    string
	ExternalRef string
	Narration   string
}

// PostingResult is returned after a successful double-entry posting.
type PostingResult struct {
	TxnGroupID uuid.UUID
	Entries    []*store.LedgerEntry
}
