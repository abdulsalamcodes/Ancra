// Package db provides database migration utilities.
package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/000001_initial.up.sql
var initSchema string

// RunMigrations applies the initial schema to the database.
// All statements use IF NOT EXISTS / DO-EXCEPTION guards, so this is safe
// to call on every startup.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, initSchema); err != nil {
		return fmt.Errorf("migrations: apply schema: %w", err)
	}
	return nil
}
