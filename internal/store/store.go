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

// Customer represents an end-user of the Ancra platform.
type Customer struct {
	ID        uuid.UUID
	KYCTier   int
	CreatedAt time.Time
}

// IdentityVersion is a point-in-time display name record for a customer.
type IdentityVersion struct {
	ID            uuid.UUID
	CustomerID    uuid.UUID
	DisplayName   string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time // nil means currently active
}

// VirtualAccount is a Nomba-backed dedicated virtual account owned by a customer.
type VirtualAccount struct {
	ID                uuid.UUID
	CustomerID        uuid.UUID
	AccountRef        string
	BankAccountNumber string
	BankAccountName   string
	Status            AccountStatus
	CreatedAt         time.Time
}

// LedgerEntry is an immutable double-entry line in the ledger.
type LedgerEntry struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Direction   Direction
	Amount      int64  // kobo (smallest NGN unit)
	Currency    string
	TxnGroupID  uuid.UUID
	ExternalRef string // Nomba transactionId
	EntryType   string // e.g. "inbound_credit", "fee", "transfer_out"
	CreatedAt   time.Time
}

// SystemAccount identifies one of the named system ledger accounts.
type SystemAccount struct {
	ID   uuid.UUID
	Name string // pool | suspense | fees | returns_clearing
}

// ProcessedEvent records a Nomba transaction that has already been ingested,
// providing idempotency for the webhook handler.
type ProcessedEvent struct {
	TransactionID string
	RequestID     string
	ReceivedAt    time.Time
}

// ReconciliationRun is the result of one sweep execution.
type ReconciliationRun struct {
	ID                  uuid.UUID
	RunAt               time.Time
	NombaWalletBalance  int64 // kobo
	ComputedPoolBalance int64 // kobo
	Delta               int64 // NombaWalletBalance - ComputedPoolBalance
	Status              ReconciliationStatus
}

// WebhookDelivery tracks an outbound webhook notification to a developer.
type WebhookDelivery struct {
	ID          uuid.UUID
	EventType   string
	Payload     []byte // raw JSON
	Status      WebhookStatus
	Attempts    int
	NextRetryAt *time.Time
	CreatedAt   time.Time
}

// ---------------------------------------------------------------------------
// Store interfaces
// ---------------------------------------------------------------------------

// CustomerStore manages customer and identity-version persistence.
type CustomerStore interface {
	CreateCustomer(ctx context.Context, c *Customer) error
	GetCustomer(ctx context.Context, id uuid.UUID) (*Customer, error)

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
	ListAccountsByCustomer(ctx context.Context, customerID uuid.UUID) ([]*VirtualAccount, error)
	UpdateAccountStatus(ctx context.Context, id uuid.UUID, status AccountStatus) error
}

// LedgerStore manages immutable ledger entries and system account lookups.
type LedgerStore interface {
	// InsertEntries writes multiple entries atomically (within a single txn group).
	InsertEntries(ctx context.Context, entries []*LedgerEntry) error
	// GetBalance returns the net balance (credits − debits) in kobo for an account.
	GetBalance(ctx context.Context, accountID uuid.UUID) (int64, error)
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
	UpdateDelivery(ctx context.Context, d *WebhookDelivery) error
}
