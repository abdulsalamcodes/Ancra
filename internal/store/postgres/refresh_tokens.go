package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// RefreshTokenStore is the Postgres implementation of store.RefreshTokenStore.
type RefreshTokenStore struct{ *DB }

// NewRefreshTokenStore creates a RefreshTokenStore backed by the given pool.
func NewRefreshTokenStore(db *DB) *RefreshTokenStore { return &RefreshTokenStore{db} }

// CreateRefreshToken inserts a new refresh token record.
func (s *RefreshTokenStore) CreateRefreshToken(ctx context.Context, t *store.RefreshToken) error {
	const q = `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`
	_, err := s.Pool.Exec(ctx, q, t.ID, t.UserID, t.TokenHash, t.ExpiresAt, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("refresh_tokens.Create: %w", err)
	}
	return nil
}

// GetRefreshTokenByHash retrieves an active (non-revoked, non-expired) token by hash.
func (s *RefreshTokenStore) GetRefreshTokenByHash(ctx context.Context, hash string) (*store.RefreshToken, error) {
	const q = `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()`
	return scanRefreshToken(s.Pool.QueryRow(ctx, q, hash))
}

// RevokeRefreshToken stamps revoked_at on the given token.
func (s *RefreshTokenStore) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE refresh_tokens SET revoked_at = $1 WHERE id = $2`
	_, err := s.Pool.Exec(ctx, q, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("refresh_tokens.Revoke: %w", err)
	}
	return nil
}

// RevokeAllUserTokens revokes every active refresh token for the given user.
// Used on logout-all-devices or password change.
func (s *RefreshTokenStore) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	const q = `
		UPDATE refresh_tokens SET revoked_at = $1
		WHERE user_id = $2 AND revoked_at IS NULL`
	_, err := s.Pool.Exec(ctx, q, time.Now().UTC(), userID)
	if err != nil {
		return fmt.Errorf("refresh_tokens.RevokeAll: %w", err)
	}
	return nil
}

func scanRefreshToken(row scanner) (*store.RefreshToken, error) {
	var t store.RefreshToken
	if err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
		return nil, fmt.Errorf("refresh_tokens.scan: %w", err)
	}
	return &t, nil
}
