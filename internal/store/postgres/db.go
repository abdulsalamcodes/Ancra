// Package postgres provides pgx/v5-backed implementations of the store interfaces.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and is embedded by every store implementation.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect opens and validates a connection pool to Postgres using pgxpool.
func Connect(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close releases all connections in the pool.
func (db *DB) Close() {
	db.Pool.Close()
}
