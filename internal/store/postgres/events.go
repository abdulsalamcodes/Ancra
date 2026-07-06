package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// ErrAlreadyProcessed is returned when a transaction has already been ingested.
var ErrAlreadyProcessed = errors.New("event already processed")

// EventStore is the Postgres implementation of store.EventStore.
type EventStore struct{ *DB }

// NewEventStore creates an EventStore backed by the given pool.
func NewEventStore(db *DB) *EventStore { return &EventStore{db} }

// MarkProcessed atomically records a processed event, enforcing the unique
// constraint on transaction_id. Returns ErrAlreadyProcessed on duplicate.
func (s *EventStore) MarkProcessed(ctx context.Context, e *store.ProcessedEvent) error {
	const q = `
		INSERT INTO processed_events (transaction_id, request_id, received_at)
		VALUES ($1,$2,$3)`

	_, err := s.Pool.Exec(ctx, q, e.TransactionID, e.RequestID, e.ReceivedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		// 23505 = unique_violation
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAlreadyProcessed
		}
		return fmt.Errorf("events.MarkProcessed: %w", err)
	}
	return nil
}

// IsProcessed returns true if the transaction has already been ingested.
func (s *EventStore) IsProcessed(ctx context.Context, transactionID string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM processed_events WHERE transaction_id = $1)`
	var exists bool
	if err := s.Pool.QueryRow(ctx, q, transactionID).Scan(&exists); err != nil {
		return false, fmt.Errorf("events.IsProcessed: %w", err)
	}
	return exists, nil
}
