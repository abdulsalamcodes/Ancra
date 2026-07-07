package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// APIKeyStore is the Postgres implementation of store.APIKeyStore.
type APIKeyStore struct{ *DB }

// NewAPIKeyStore creates an APIKeyStore backed by the given pool.
func NewAPIKeyStore(db *DB) *APIKeyStore { return &APIKeyStore{db} }

// CreateKey inserts a new API key record.
func (s *APIKeyStore) CreateKey(ctx context.Context, k *store.APIKey) error {
	const q = `
		INSERT INTO api_keys (id, org_id, name, key_hash, created_at)
		VALUES ($1, $2, $3, $4, $5)`
	_, err := s.Pool.Exec(ctx, q, k.ID, k.OrgID, k.Name, k.KeyHash, k.CreatedAt)
	if err != nil {
		return fmt.Errorf("apikeys.Create: %w", err)
	}
	return nil
}

// GetByHash looks up an active (non-revoked) key by its SHA-256 hash.
func (s *APIKeyStore) GetByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	const q = `
		SELECT id, org_id, name, key_hash, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE key_hash = $1 AND revoked_at IS NULL`
	return scanKey(s.Pool.QueryRow(ctx, q, hash))
}

// GetByID retrieves any key (including revoked) by its primary key.
func (s *APIKeyStore) GetByID(ctx context.Context, id uuid.UUID) (*store.APIKey, error) {
	const q = `
		SELECT id, org_id, name, key_hash, created_at, last_used_at, revoked_at
		FROM api_keys WHERE id = $1`
	return scanKey(s.Pool.QueryRow(ctx, q, id))
}

// ListKeys returns all API keys for an org ordered by creation time descending.
func (s *APIKeyStore) ListKeys(ctx context.Context, orgID uuid.UUID) ([]*store.APIKey, error) {
	const q = `
		SELECT id, org_id, name, key_hash, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE org_id = $1
		ORDER BY created_at DESC`
	rows, err := s.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("apikeys.List: %w", err)
	}
	defer rows.Close()

	var keys []*store.APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListAllKeys returns all API keys across every org, ordered by creation time descending.
func (s *APIKeyStore) ListAllKeys(ctx context.Context) ([]*store.APIKey, error) {
	const q = `
		SELECT id, org_id, name, key_hash, created_at, last_used_at, revoked_at
		FROM api_keys
		ORDER BY created_at DESC`
	rows, err := s.Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("apikeys.ListAll: %w", err)
	}
	defer rows.Close()

	var keys []*store.APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeKey stamps revoked_at on the given key.
func (s *APIKeyStore) RevokeKey(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE api_keys SET revoked_at = $1 WHERE id = $2`
	_, err := s.Pool.Exec(ctx, q, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("apikeys.Revoke: %w", err)
	}
	return nil
}

// TouchLastUsed updates last_used_at to now for the given key.
func (s *APIKeyStore) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE api_keys SET last_used_at = $1 WHERE id = $2`
	_, err := s.Pool.Exec(ctx, q, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("apikeys.Touch: %w", err)
	}
	return nil
}

func scanKey(row scanner) (*store.APIKey, error) {
	var k store.APIKey
	if err := row.Scan(
		&k.ID, &k.OrgID, &k.Name, &k.KeyHash, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt,
	); err != nil {
		return nil, fmt.Errorf("apikeys.scan: %w", err)
	}
	return &k, nil
}
