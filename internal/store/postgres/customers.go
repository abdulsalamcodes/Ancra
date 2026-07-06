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
	const q = `INSERT INTO customers (id, kyc_tier, created_at) VALUES ($1,$2,$3)`
	_, err := s.Pool.Exec(ctx, q, c.ID, c.KYCTier, c.CreatedAt)
	if err != nil {
		return fmt.Errorf("customers.Create: %w", err)
	}
	return nil
}

// GetCustomer retrieves a customer by primary key.
func (s *CustomerStore) GetCustomer(ctx context.Context, id uuid.UUID) (*store.Customer, error) {
	const q = `SELECT id, kyc_tier, created_at FROM customers WHERE id = $1`
	var c store.Customer
	if err := s.Pool.QueryRow(ctx, q, id).Scan(&c.ID, &c.KYCTier, &c.CreatedAt); err != nil {
		return nil, fmt.Errorf("customers.Get: %w", err)
	}
	return &c, nil
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

// ListCustomers returns customers with their current display name, newest first.
func (s *CustomerStore) ListCustomers(ctx context.Context, limit, offset int) ([]*store.Customer, error) {
	const q = `
		SELECT c.id, c.kyc_tier, c.created_at, COALESCE(iv.display_name, '') AS display_name
		FROM customers c
		LEFT JOIN identity_versions iv ON iv.customer_id = c.id AND iv.effective_to IS NULL
		ORDER BY c.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("customers.List: %w", err)
	}
	defer rows.Close()

	var customers []*store.Customer
	for rows.Next() {
		var c store.Customer
		if err := rows.Scan(&c.ID, &c.KYCTier, &c.CreatedAt, &c.DisplayName); err != nil {
			return nil, fmt.Errorf("customers.List scan: %w", err)
		}
		customers = append(customers, &c)
	}
	return customers, rows.Err()
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
