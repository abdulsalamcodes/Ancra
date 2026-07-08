// Package store defines storage interfaces for all Ancra domain entities.
// The domain layer depends on these interfaces; concrete implementations live
// in store/postgres.
package store

import (
	"context"
	"errors"
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

// UserRole is the access level of a user within their organisation.
type UserRole string

const (
	UserRoleOwner  UserRole = "owner"
	UserRoleMember UserRole = "member"
)

// Organization is the top-level tenant that owns all resources (customers,
// accounts, API keys) in the multi-tenant model.
type Organization struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// User is a human member of an organisation who authenticates via email/password.
type User struct {
	ID           uuid.UUID `json:"id"`
	OrgID        uuid.UUID `json:"org_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// RefreshToken is an opaque long-lived token stored as a SHA-256 hash.
type RefreshToken struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// APIKey represents a hashed developer API key.
type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	OrgID      *uuid.UUID `json:"org_id"` // nullable until Phase 2 backfill
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// Customer represents an end-user of the Ancra platform.
type Customer struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
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

// KYCTierChange is an immutable audit record of a customer KYC tier upgrade.
type KYCTierChange struct {
	ID         uuid.UUID `json:"id"`
	CustomerID uuid.UUID `json:"customer_id"`
	FromTier   int       `json:"from_tier"`
	ToTier     int       `json:"to_tier"`
	UpgradedAt time.Time `json:"upgraded_at"`
}

// ErrKYCTierDowngrade is returned when the requested tier is not higher than the current tier.
var ErrKYCTierDowngrade = errors.New("kyc_tier can only be upgraded, not downgraded")

// VirtualAccount is a Nomba-backed dedicated virtual account owned by a customer.
type VirtualAccount struct {
	ID                uuid.UUID     `json:"id"`
	OrgID             uuid.UUID     `json:"org_id"`
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
// OrgID is nil for legacy global accounts; non-nil for per-org accounts.
type SystemAccount struct {
	ID    uuid.UUID  `json:"id"`
	OrgID *uuid.UUID `json:"org_id"`
	Name  string     `json:"name"` // pool | suspense | fees | returns_clearing
}

// ProcessedEvent records a Nomba transaction that has already been ingested,
// providing idempotency for the webhook handler.
type ProcessedEvent struct {
	TransactionID string    `json:"transaction_id"`
	RequestID     string    `json:"request_id"`
	ReceivedAt    time.Time `json:"received_at"`
}

// ReconciliationRun is the result of one sweep execution, scoped to an org.
type ReconciliationRun struct {
	ID                  uuid.UUID            `json:"id"`
	OrgID               uuid.UUID            `json:"org_id"`
	RunAt               time.Time            `json:"run_at"`
	NombaWalletBalance  int64                `json:"nomba_wallet_balance"`  // kobo
	ComputedPoolBalance int64                `json:"computed_pool_balance"` // kobo
	Delta               int64                `json:"delta"`                 // NombaWalletBalance - ComputedPoolBalance
	Status              ReconciliationStatus `json:"status"`
}

// WebhookDelivery tracks an outbound webhook notification to a developer.
type WebhookDelivery struct {
	ID          uuid.UUID     `json:"id"`
	OrgID       uuid.UUID     `json:"org_id"`
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

// OrgStore manages organisation persistence.
type OrgStore interface {
	CreateOrg(ctx context.Context, org *Organization) error
	GetOrgByID(ctx context.Context, id uuid.UUID) (*Organization, error)
	GetOrgBySlug(ctx context.Context, slug string) (*Organization, error)
	// ListAllOrgs returns every organisation. Used by the sweep worker to run
	// per-org reconciliation without requiring HTTP context.
	ListAllOrgs(ctx context.Context) ([]*Organization, error)
}

// UserStore manages human user persistence.
type UserStore interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*User, error)
}

// RefreshTokenStore manages opaque refresh token persistence.
type RefreshTokenStore interface {
	CreateRefreshToken(ctx context.Context, t *RefreshToken) error
	GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id uuid.UUID) error
	RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error
}

// APIKeyStore manages developer API key persistence.
type APIKeyStore interface {
	CreateKey(ctx context.Context, k *APIKey) error
	GetByHash(ctx context.Context, hash string) (*APIKey, error)
	GetByID(ctx context.Context, id uuid.UUID) (*APIKey, error)
	ListKeys(ctx context.Context, orgID uuid.UUID) ([]*APIKey, error)
	// ListAllKeys returns every API key across all organisations, ordered by
	// creation time descending. Intended for admin/operator use only.
	ListAllKeys(ctx context.Context) ([]*APIKey, error)
	RevokeKey(ctx context.Context, id uuid.UUID) error
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}

// CustomerStore manages customer and identity-version persistence.
type CustomerStore interface {
	CreateCustomer(ctx context.Context, c *Customer) error
	GetCustomer(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (*Customer, error)
	ListCustomers(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*Customer, error)

	CreateIdentityVersion(ctx context.Context, v *IdentityVersion) error
	GetCurrentIdentity(ctx context.Context, customerID uuid.UUID) (*IdentityVersion, error)
	CloseIdentityVersion(ctx context.Context, id uuid.UUID, closedAt time.Time) error

	// UpgradeKYCTier atomically raises a customer's KYC tier and writes an audit
	// record. Returns ErrKYCTierDowngrade if newTier is not strictly greater than
	// the current tier.
	UpgradeKYCTier(ctx context.Context, orgID, customerID uuid.UUID, newTier int, now time.Time) (*KYCTierChange, error)
	// ListKYCTierHistory returns all tier upgrade records for a customer, newest first.
	ListKYCTierHistory(ctx context.Context, orgID, customerID uuid.UUID) ([]*KYCTierChange, error)
}

// AccountStore manages virtual account persistence.
type AccountStore interface {
	CreateAccount(ctx context.Context, a *VirtualAccount) error
	// GetAccount retrieves an account scoped to the given org.
	GetAccount(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (*VirtualAccount, error)
	// GetAccountByNumber looks up an account by bank account number regardless of org.
	// Used by the inbound webhook handler which resolves the org from the account itself.
	GetAccountByNumber(ctx context.Context, accountNumber string) (*VirtualAccount, error)
	ListAccounts(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*VirtualAccount, error)
	ListAccountsByCustomer(ctx context.Context, orgID uuid.UUID, customerID uuid.UUID) ([]*VirtualAccount, error)
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
	// GetSystemAccount retrieves a named system account scoped to the given org.
	// Pass uuid.Nil to retrieve a global (NULL org_id) system account.
	GetSystemAccount(ctx context.Context, orgID uuid.UUID, name string) (*SystemAccount, error)
	// SeedSystemAccounts creates the four system accounts (pool, suspense, fees,
	// returns_clearing) for a newly created org. Idempotent — safe to call on
	// every signup.
	SeedSystemAccounts(ctx context.Context, orgID uuid.UUID) error
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
	ListRuns(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*ReconciliationRun, error)
	// ListAllRuns returns reconciliation runs across all organisations, newest first.
	// Intended for admin/operator use only.
	ListAllRuns(ctx context.Context, limit, offset int) ([]*ReconciliationRun, error)
	GetLatestRun(ctx context.Context, orgID uuid.UUID) (*ReconciliationRun, error)
}

// WebhookStore manages outbound webhook delivery records.
type WebhookStore interface {
	CreateDelivery(ctx context.Context, d *WebhookDelivery) error
	GetDelivery(ctx context.Context, id uuid.UUID) (*WebhookDelivery, error)
	// ListPending returns deliveries due for (re-)delivery regardless of org.
	// Used by the outbound worker which processes all pending deliveries.
	ListPending(ctx context.Context, now time.Time, limit int) ([]*WebhookDelivery, error)
	ListDeliveries(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*WebhookDelivery, error)
	// ListAllDeliveries returns webhook deliveries across all organisations,
	// newest first. Intended for admin/operator use only.
	ListAllDeliveries(ctx context.Context, limit, offset int) ([]*WebhookDelivery, error)
	UpdateDelivery(ctx context.Context, d *WebhookDelivery) error
}

// OrgNombaConfig holds per-organisation Nomba BYOK credentials.
// Secrets are stored AES-256-GCM encrypted; decryption is the caller's responsibility.
type OrgNombaConfig struct {
	OrgID                  uuid.UUID `json:"org_id"`
	ClientID               string    `json:"client_id"`
	ClientSecretEncrypted  string    `json:"-"`
	AccountID              string    `json:"account_id"`
	SubAccountID           string    `json:"sub_account_id"`
	WebhookSecretEncrypted string    `json:"-"`
	Sandbox                bool      `json:"sandbox"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// NombaConfigStore manages per-org Nomba credential persistence.
type NombaConfigStore interface {
	// UpsertNombaConfig creates or replaces the Nomba config for the given org.
	UpsertNombaConfig(ctx context.Context, cfg *OrgNombaConfig) error
	// GetNombaConfig retrieves the Nomba config for the given org.
	GetNombaConfig(ctx context.Context, orgID uuid.UUID) (*OrgNombaConfig, error)
}

// OrgWebhookConfig holds per-org outbound webhook delivery settings.
// The signing secret is stored AES-256-GCM encrypted; it is shown to the
// developer once on creation and never returned in plain text again.
type OrgWebhookConfig struct {
	OrgID                   uuid.UUID `json:"org_id"`
	EndpointURL             string    `json:"endpoint_url"`
	SigningSecretEncrypted   string    `json:"-"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// Transactor runs a function inside a single database transaction.
// Both store operations that participate in the same call to RunInTx will
// share the transaction carried in the context, achieving atomicity without
// coupling individual store methods to each other.
type Transactor interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// WebhookConfigStore manages per-org outbound webhook configuration.
type WebhookConfigStore interface {
	// UpsertWebhookConfig creates or replaces the webhook config for the given org.
	UpsertWebhookConfig(ctx context.Context, cfg *OrgWebhookConfig) error
	// GetWebhookConfig retrieves the webhook config for the given org.
	GetWebhookConfig(ctx context.Context, orgID uuid.UUID) (*OrgWebhookConfig, error)
}
