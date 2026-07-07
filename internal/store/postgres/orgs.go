package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// OrgStore is the Postgres implementation of store.OrgStore.
type OrgStore struct{ *DB }

// NewOrgStore creates an OrgStore backed by the given pool.
func NewOrgStore(db *DB) *OrgStore { return &OrgStore{db} }

// CreateOrg inserts a new organisation record.
func (s *OrgStore) CreateOrg(ctx context.Context, org *store.Organization) error {
	const q = `
		INSERT INTO organizations (id, name, slug, created_at)
		VALUES ($1, $2, $3, $4)`
	_, err := s.Pool.Exec(ctx, q, org.ID, org.Name, org.Slug, org.CreatedAt)
	if err != nil {
		return fmt.Errorf("orgs.Create: %w", err)
	}
	return nil
}

// GetOrgByID retrieves an organisation by primary key.
func (s *OrgStore) GetOrgByID(ctx context.Context, id uuid.UUID) (*store.Organization, error) {
	const q = `
		SELECT id, name, slug, created_at
		FROM organizations WHERE id = $1`
	return scanOrg(s.Pool.QueryRow(ctx, q, id))
}

// GetOrgBySlug retrieves an organisation by its unique URL slug.
func (s *OrgStore) GetOrgBySlug(ctx context.Context, slug string) (*store.Organization, error) {
	const q = `
		SELECT id, name, slug, created_at
		FROM organizations WHERE slug = $1`
	return scanOrg(s.Pool.QueryRow(ctx, q, slug))
}

func scanOrg(row scanner) (*store.Organization, error) {
	var org store.Organization
	if err := row.Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt); err != nil {
		return nil, fmt.Errorf("orgs.scan: %w", err)
	}
	return &org, nil
}
