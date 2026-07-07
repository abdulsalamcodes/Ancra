// Package postgres provides pgx/v5-backed implementations of the store interfaces.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and is embedded by every store implementation.
type DB struct {
	Pool *pgxpool.Pool
}

// executor is satisfied by both *pgxpool.Pool and pgx.Tx, allowing store
// methods to operate on either a connection pool or an active transaction.
type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// txKeyType is the unexported context key for a carried transaction.
type txKeyType struct{}

var txKey = txKeyType{}

// txFromCtx returns the active transaction stored in ctx, or nil.
func txFromCtx(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(txKey).(pgx.Tx)
	return tx
}

// exec returns the executor for this context: the active tx if one is
// carried, otherwise the connection pool.
func (db *DB) exec(ctx context.Context) executor {
	if tx := txFromCtx(ctx); tx != nil {
		return tx
	}
	return db.Pool
}

// RunInTx executes fn within a database transaction. The transaction is
// injected into the context so that any store method called inside fn
// that uses db.exec(ctx) or txFromCtx(ctx) will participate automatically.
// fn's error (or a commit error) rolls back the transaction.
func (db *DB) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("RunInTx: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := fn(context.WithValue(ctx, txKey, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("RunInTx: commit: %w", err)
	}
	return nil
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
