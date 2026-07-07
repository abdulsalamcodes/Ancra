package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// CustomerStore is the Postgres implementation of store.CustomerStore.
type CustomerStore struct{ *DB }

// NewCustomerStore creates a CustomerStore backed by the given pool.
func NewCustomerStore(db *DB) *CustomerStore { return &CustomerStore{db} }

// CreateCustomer inserts a new customer row.
func (s *CustomerStore) CreateCustomer(ctx context.Context, c *store.Customer) error {
	const q = `INSERT INTO customers (id, org_id, kyc_tier, created_at) VALUES ($1,$2,$3,$4)`
	_, err := s.Pool.Exec(ctx, q, c.ID, c.OrgID, c.KYCTier, c.CreatedAt)
	if err != nil {
		return fmt.Errorf("customers.Create: %w", err)
	}
	return nil
}

// GetCustomer retrieves a customer by primary key, scoped to the given org,
// with their current display name joined from identity_versions.
func (s *CustomerStore) GetCustomer(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (*store.Customer, error) {
	const q = `
		SELECT c.id, c.org_id, c.kyc_tier, c.created_at, COALESCE(iv.display_name, '') AS display_name
		FROM customers c
		LEFT JOIN identity_versions iv ON iv.customer_id = c.id AND iv.effective_to IS NULL
		WHERE c.id = $1 AND c.org_id = $2`
	var c store.Customer
	if err := s.Pool.QueryRow(ctx, q, id, orgID).Scan(&c.ID, &c.OrgID, &c.KYCTier, &c.CreatedAt, &c.DisplayName); err != nil {
		return nil, fmt.Errorf("customers.Get: %w", err)
	}
	return &c, nil
}

// ListCustomers returns customers belonging to an org with their current display name, newest first.
func (s *CustomerStore) ListCustomers(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*store.Customer, error) {
	const q = `
		SELECT c.id, c.org_id, c.kyc_tier, c.created_at, COALESCE(iv.display_name, '') AS display_name
		FROM customers c
		LEFT JOIN identity_versions iv ON iv.customer_id = c.id AND iv.effective_to IS NULL
		WHERE c.org_id = $1
		ORDER BY c.created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.Pool.Query(ctx, q, orgID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("customers.List: %w", err)
	}
	defer rows.Close()

	var customers []*store.Customer
	for rows.Next() {
		var c store.Customer
		if err := rows.Scan(&c.ID, &c.OrgID, &c.KYCTier, &c.CreatedAt, &c.DisplayName); err != nil {
			return nil, fmt.Errorf("customers.List scan: %w", err)
		}
		customers = append(customers, &c)
	}
	return customers, rows.Err()
}

// CreateIdentityVersion inserts a new identity version for a customer.
func (s *CustomerStore) CreateIdentityVersion(ctx context.Context, v *store.IdentityVersion) error {
	const q = `
		INSERT INTO identity_versions (id, customer_id, display_name, effective_from, effective_to)
		VALUES ($1,$2,$3,$4,$5)`
	_, err := s.Pool.Exec(ctx, q,
		v.ID, v.CustomerID, v.DisplayName, v.EffectiveFrom, v.EffectiveTo,
	)
	if err != nil {
		return fmt.Errorf("identity.Create: %w", err)
	}
	return nil
}

// GetCurrentIdentity retrieves the active identity version (effective_to IS NULL).
func (s *CustomerStore) GetCurrentIdentity(ctx context.Context, customerID uuid.UUID) (*store.IdentityVersion, error) {
	const q = `
		SELECT id, customer_id, display_name, effective_from, effective_to
		FROM identity_versions
		WHERE customer_id = $1 AND effective_to IS NULL
		LIMIT 1`

	var v store.IdentityVersion
	if err := s.Pool.QueryRow(ctx, q, customerID).Scan(
		&v.ID, &v.CustomerID, &v.DisplayName, &v.EffectiveFrom, &v.EffectiveTo,
	); err != nil {
		return nil, fmt.Errorf("identity.GetCurrent: %w", err)
	}
	return &v, nil
}

// CloseIdentityVersion stamps effective_to on the given identity version row.
func (s *CustomerStore) CloseIdentityVersion(ctx context.Context, id uuid.UUID, closedAt time.Time) error {
	const q = `UPDATE identity_versions SET effective_to = $1 WHERE id = $2`
	_, err := s.Pool.Exec(ctx, q, closedAt, id)
	if err != nil {
		return fmt.Errorf("identity.Close: %w", err)
	}
	return nil
}

// UpgradeKYCTier atomically raises a customer's KYC tier and records the change.
// The read, validation, update, and audit insert are performed in a single
// transaction to prevent a concurrent upgrade from creating an inconsistent history.
func (s *CustomerStore) UpgradeKYCTier(ctx context.Context, orgID, customerID uuid.UUID, newTier int, now time.Time) (*store.KYCTierChange, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("customers.UpgradeKYCTier: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var currentTier int
	err = tx.QueryRow(ctx,
		`SELECT kyc_tier FROM customers WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		customerID, orgID,
	).Scan(&currentTier)
	if err != nil {
		return nil, fmt.Errorf("customers.UpgradeKYCTier: get customer: %w", err)
	}

	if newTier <= currentTier {
		return nil, store.ErrKYCTierDowngrade
	}

	if _, err = tx.Exec(ctx,
		`UPDATE customers SET kyc_tier = $1 WHERE id = $2`,
		newTier, customerID,
	); err != nil {
		return nil, fmt.Errorf("customers.UpgradeKYCTier: update tier: %w", err)
	}

	change := &store.KYCTierChange{
		ID:         uuid.New(),
		CustomerID: customerID,
		FromTier:   currentTier,
		ToTier:     newTier,
		UpgradedAt: now,
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO kyc_tier_history (id, customer_id, from_tier, to_tier, upgraded_at) VALUES ($1,$2,$3,$4,$5)`,
		change.ID, change.CustomerID, change.FromTier, change.ToTier, change.UpgradedAt,
	); err != nil {
		return nil, fmt.Errorf("customers.UpgradeKYCTier: history insert: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("customers.UpgradeKYCTier: commit: %w", err)
	}
	return change, nil
}

// ListKYCTierHistory returns all tier upgrade records for a customer, newest first.
// The JOIN on customers ensures the caller's org owns the requested customer.
func (s *CustomerStore) ListKYCTierHistory(ctx context.Context, orgID, customerID uuid.UUID) ([]*store.KYCTierChange, error) {
	const q = `
		SELECT h.id, h.customer_id, h.from_tier, h.to_tier, h.upgraded_at
		FROM kyc_tier_history h
		JOIN customers c ON c.id = h.customer_id
		WHERE h.customer_id = $1 AND c.org_id = $2
		ORDER BY h.upgraded_at DESC`

	rows, err := s.Pool.Query(ctx, q, customerID, orgID)
	if err != nil {
		return nil, fmt.Errorf("customers.ListKYCTierHistory: %w", err)
	}
	defer rows.Close()

	var history []*store.KYCTierChange
	for rows.Next() {
		var ch store.KYCTierChange
		if err := rows.Scan(&ch.ID, &ch.CustomerID, &ch.FromTier, &ch.ToTier, &ch.UpgradedAt); err != nil {
			return nil, fmt.Errorf("customers.ListKYCTierHistory scan: %w", err)
		}
		history = append(history, &ch)
	}
	if history == nil {
		history = []*store.KYCTierChange{}
	}
	return history, rows.Err()
}

