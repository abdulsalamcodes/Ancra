// Package store defines storage interfaces for all Ancra domain entities.
// The domain layer depends on these interfaces; concrete implementations live
// in store/postgres.
package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Domain value types
// ---------------------------------------------------------------------------

// Direction indicates whether a ledger entry is a debit or credit.
type Direction string

const (
	DirectionDebit  Direction = "debit"
	DirectionCredit Direction = "credit"
)

// AccountStatus is the lifecycle state of a virtual account.
type AccountStatus string

const (
	AccountStatusActive AccountStatus = "active"
	AccountStatusClosed AccountStatus = "closed"
)

// WebhookStatus tracks the delivery state of an outbound webhook.
type WebhookStatus string

const (
	WebhookStatusPending   WebhookStatus = "pending"
	WebhookStatusDelivered WebhookStatus = "delivered"
	WebhookStatusFailed    WebhookStatus = "failed"
)

// ReconciliationStatus reflects whether a sweep run found the system balanced.
type ReconciliationStatus string

const (
	ReconciliationStatusOK      ReconciliationStatus = "ok"
	ReconciliationStatusMismatch ReconciliationStatus = "mismatch"
)

// ---------------------------------------------------------------------------
// Domain models
// ---------------------------------------------------------------------------

// APIKey represents a hashed developer API key.
type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// Customer represents an end-user of the Ancra platform.
type Customer struct {
	ID          uuid.UUID `json:"id"`
	KYCTier     int       `json:"kyc_tier"`
	CreatedAt   time.Time `json:"created_at"`
	DisplayName string    `json:"display_name"` // joined from identity_versions, may be empty
}

// IdentityVersion is a point-in-time display name record for a customer.
type IdentityVersion struct {
	ID            uuid.UUID  `json:"id"`
	CustomerID    uuid.UUID  `json:"customer_id"`
	DisplayName   string     `json:"display_name"`
	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to"`
}

// VirtualAccount is a Nomba-backed dedicated virtual account owned by a customer.
type VirtualAccount struct {
	ID                uuid.UUID     `json:"id"`
	CustomerID        uuid.UUID     `json:"customer_id"`
	AccountRef        string        `json:"account_ref"`
	BankAccountNumber string        `json:"bank_account_number"`
	BankAccountName   string        `json:"bank_account_name"`
	Status            AccountStatus `json:"status"`
	CreatedAt         time.Time     `json:"created_at"`
}

// LedgerEntry is an immutable double-entry line in the ledger.
type LedgerEntry struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"account_id"`
	Direction   Direction `json:"direction"`
	Amount      int64     `json:"amount"` // kobo (smallest NGN unit)
	Currency    string    `json:"currency"`
	TxnGroupID  uuid.UUID `json:"txn_group_id"`
	ExternalRef string    `json:"external_ref"` // Nomba transactionId
	EntryType   string    `json:"entry_type"`   // e.g. "inbound_credit", "fee", "transfer_out"
	CreatedAt   time.Time `json:"created_at"`
}

// SystemAccount identifies one of the named system ledger accounts.
type SystemAccount struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"` // pool | suspense | fees | returns_clearing
}

// ProcessedEvent records a Nomba transaction that has already been ingested,
// providing idempotency for the webhook handler.
type ProcessedEvent struct {
	TransactionID string    `json:"transaction_id"`
	RequestID     string    `json:"request_id"`
	ReceivedAt    time.Time `json:"received_at"`
}

// ReconciliationRun is the result of one sweep execution.
type ReconciliationRun struct {
	ID                  uuid.UUID            `json:"id"`
	RunAt               time.Time            `json:"run_at"`
	NombaWalletBalance  int64                `json:"nomba_wallet_balance"`  // kobo
	ComputedPoolBalance int64                `json:"computed_pool_balance"` // kobo
	Delta               int64                `json:"delta"`                 // NombaWalletBalance - ComputedPoolBalance
	Status              ReconciliationStatus `json:"status"`
}

// WebhookDelivery tracks an outbound webhook notification to a developer.
type WebhookDelivery struct {
	ID          uuid.UUID     `json:"id"`
	EventType   string        `json:"event_type"`
	Payload     []byte        `json:"payload"` // raw JSON
	Status      WebhookStatus `json:"status"`
	Attempts    int           `json:"attempts"`
	NextRetryAt *time.Time    `json:"next_retry_at"`
	CreatedAt   time.Time     `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Store interfaces
// ---------------------------------------------------------------------------

// APIKeyStore manages developer API key persistence.
type APIKeyStore interface {
	CreateKey(ctx context.Context, k *APIKey) error
	GetByHash(ctx context.Context, hash string) (*APIKey, error)
	GetByID(ctx context.Context, id uuid.UUID) (*APIKey, error)
	ListKeys(ctx context.Context) ([]*APIKey, error)
	RevokeKey(ctx context.Context, id uuid.UUID) error
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}

// CustomerStore manages customer and identity-version persistence.
type CustomerStore interface {
	CreateCustomer(ctx context.Context, c *Customer) error
	GetCustomer(ctx context.Context, id uuid.UUID) (*Customer, error)
	ListCustomers(ctx context.Context, limit, offset int) ([]*Customer, error)

	CreateIdentityVersion(ctx context.Context, v *IdentityVersion) error
	GetCurrentIdentity(ctx context.Context, customerID uuid.UUID) (*IdentityVersion, error)
	// CloseIdentityVersion sets effective_to on the current identity record.
	CloseIdentityVersion(ctx context.Context, id uuid.UUID, closedAt time.Time) error
}

// AccountStore manages virtual account persistence.
type AccountStore interface {
	CreateAccount(ctx context.Context, a *VirtualAccount) error
	GetAccount(ctx context.Context, id uuid.UUID) (*VirtualAccount, error)
	GetAccountByNumber(ctx context.Context, accountNumber string) (*VirtualAccount, error)
	ListAccounts(ctx context.Context, limit, offset int) ([]*VirtualAccount, error)
	ListAccountsByCustomer(ctx context.Context, customerID uuid.UUID) ([]*VirtualAccount, error)
	UpdateAccountStatus(ctx context.Context, id uuid.UUID, status AccountStatus) error
}

// LedgerStore manages immutable ledger entries and system account lookups.
type LedgerStore interface {
	// InsertEntries writes multiple entries atomically (within a single txn group).
	InsertEntries(ctx context.Context, entries []*LedgerEntry) error
	// GetBalance returns the net balance (credits − debits) in kobo for an account.
	GetBalance(ctx context.Context, accountID uuid.UUID) (int64, error)
	// GetBalanceAsOf returns the net balance up to and including the given timestamp.
	// Used to compute correct opening/closing balances for paginated statements.
	GetBalanceAsOf(ctx context.Context, accountID uuid.UUID, asOf time.Time) (int64, error)
	// ListEntries returns ledger entries for an account ordered by created_at DESC.
	ListEntries(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]*LedgerEntry, error)
	// GetSystemAccount retrieves a named system account row.
	GetSystemAccount(ctx context.Context, name string) (*SystemAccount, error)
}

// EventStore provides idempotency for inbound Nomba webhook events.
type EventStore interface {
	// MarkProcessed atomically records a transaction as processed.
	// Returns an error (or a sentinel) if already recorded.
	MarkProcessed(ctx context.Context, e *ProcessedEvent) error
	// IsProcessed returns true if the transaction has already been ingested.
	IsProcessed(ctx context.Context, transactionID string) (bool, error)
}

// ReconciliationStore persists the results of sweep runs.
type ReconciliationStore interface {
	InsertRun(ctx context.Context, run *ReconciliationRun) error
	ListRuns(ctx context.Context, limit, offset int) ([]*ReconciliationRun, error)
	GetLatestRun(ctx context.Context) (*ReconciliationRun, error)
}

// WebhookStore manages outbound webhook delivery records.
type WebhookStore interface {
	CreateDelivery(ctx context.Context, d *WebhookDelivery) error
	GetDelivery(ctx context.Context, id uuid.UUID) (*WebhookDelivery, error)
	// ListPending returns deliveries that are due for (re-)delivery.
	ListPending(ctx context.Context, now time.Time, limit int) ([]*WebhookDelivery, error)
	ListDeliveries(ctx context.Context, limit, offset int) ([]*WebhookDelivery, error)
	UpdateDelivery(ctx context.Context, d *WebhookDelivery) error
}
