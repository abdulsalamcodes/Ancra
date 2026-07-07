package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// UserStore is the Postgres implementation of store.UserStore.
type UserStore struct{ *DB }

// NewUserStore creates a UserStore backed by the given pool.
func NewUserStore(db *DB) *UserStore { return &UserStore{db} }

// CreateUser inserts a new user record.
func (s *UserStore) CreateUser(ctx context.Context, u *store.User) error {
	const q = `
		INSERT INTO users (id, org_id, email, password_hash, role, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.Pool.Exec(ctx, q, u.ID, u.OrgID, u.Email, u.PasswordHash, u.Role, u.CreatedAt)
	if err != nil {
		return fmt.Errorf("users.Create: %w", err)
	}
	return nil
}

// GetUserByEmail retrieves a user by their unique email address.
func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (*store.User, error) {
	const q = `
		SELECT id, org_id, email, password_hash, role, created_at
		FROM users WHERE email = $1`
	return scanUser(s.Pool.QueryRow(ctx, q, email))
}

// GetUserByID retrieves a user by primary key.
func (s *UserStore) GetUserByID(ctx context.Context, id uuid.UUID) (*store.User, error) {
	const q = `
		SELECT id, org_id, email, password_hash, role, created_at
		FROM users WHERE id = $1`
	return scanUser(s.Pool.QueryRow(ctx, q, id))
}

func scanUser(row scanner) (*store.User, error) {
	var u store.User
	if err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		return nil, fmt.Errorf("users.scan: %w", err)
	}
	return &u, nil
}
