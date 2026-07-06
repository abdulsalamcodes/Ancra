package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// ReconciliationStore is the Postgres implementation of store.ReconciliationStore.
type ReconciliationStore struct{ *DB }

// NewReconciliationStore creates a ReconciliationStore backed by the given pool.
func NewReconciliationStore(db *DB) *ReconciliationStore { return &ReconciliationStore{db} }

// InsertRun persists the result of a reconciliation sweep.
func (s *ReconciliationStore) InsertRun(ctx context.Context, run *store.ReconciliationRun) error {
	const q = `
		INSERT INTO reconciliation_runs
			(id, run_at, nomba_wallet_balance, computed_pool_balance, delta, status)
		VALUES ($1,$2,$3,$4,$5,$6)`

	_, err := s.Pool.Exec(ctx, q,
		run.ID, run.RunAt,
		run.NombaWalletBalance, run.ComputedPoolBalance,
		run.Delta, string(run.Status),
	)
	if err != nil {
		return fmt.Errorf("reconciliation.InsertRun: %w", err)
	}
	return nil
}

// ListRuns returns reconciliation runs ordered newest first.
func (s *ReconciliationStore) ListRuns(ctx context.Context, limit, offset int) ([]*store.ReconciliationRun, error) {
	const q = `
		SELECT id, run_at, nomba_wallet_balance, computed_pool_balance, delta, status
		FROM reconciliation_runs
		ORDER BY run_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("reconciliation.ListRuns: %w", err)
	}
	defer rows.Close()

	var runs []*store.ReconciliationRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// GetLatestRun returns the most recent reconciliation run.
func (s *ReconciliationStore) GetLatestRun(ctx context.Context) (*store.ReconciliationRun, error) {
	const q = `
		SELECT id, run_at, nomba_wallet_balance, computed_pool_balance, delta, status
		FROM reconciliation_runs
		ORDER BY run_at DESC
		LIMIT 1`

	return scanRun(s.Pool.QueryRow(ctx, q))
}

func scanRun(row scanner) (*store.ReconciliationRun, error) {
	var r store.ReconciliationRun
	var status string
	var runAt time.Time
	if err := row.Scan(
		&r.ID, &runAt,
		&r.NombaWalletBalance, &r.ComputedPoolBalance,
		&r.Delta, &status,
	); err != nil {
		return nil, fmt.Errorf("reconciliation.scan: %w", err)
	}
	r.RunAt = runAt
	r.Status = store.ReconciliationStatus(status)
	return &r, nil
}

// ---------------------------------------------------------------------------
// WebhookStore
// ---------------------------------------------------------------------------

// WebhookStore is the Postgres implementation of store.WebhookStore.
type WebhookStore struct{ *DB }

// NewWebhookStore creates a WebhookStore backed by the given pool.
func NewWebhookStore(db *DB) *WebhookStore { return &WebhookStore{db} }

// CreateDelivery inserts a new outbound webhook delivery record.
func (s *WebhookStore) CreateDelivery(ctx context.Context, d *store.WebhookDelivery) error {
	const q = `
		INSERT INTO webhook_deliveries
			(id, event_type, payload, status, attempts, next_retry_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`

	_, err := s.Pool.Exec(ctx, q,
		d.ID, d.EventType, d.Payload,
		string(d.Status), d.Attempts, d.NextRetryAt, d.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("webhook.CreateDelivery: %w", err)
	}
	return nil
}

// GetDelivery retrieves a webhook delivery by ID.
func (s *WebhookStore) GetDelivery(ctx context.Context, id uuid.UUID) (*store.WebhookDelivery, error) {
	const q = `
		SELECT id, event_type, payload, status, attempts, next_retry_at, created_at
		FROM webhook_deliveries WHERE id = $1`

	return scanDelivery(s.Pool.QueryRow(ctx, q, id))
}

// ListPending returns webhook deliveries that are ready for (re-)delivery.
func (s *WebhookStore) ListPending(ctx context.Context, now time.Time, limit int) ([]*store.WebhookDelivery, error) {
	const q = `
		SELECT id, event_type, payload, status, attempts, next_retry_at, created_at
		FROM webhook_deliveries
		WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= $1)
		ORDER BY created_at ASC
		LIMIT $2`

	rows, err := s.Pool.Query(ctx, q, now, limit)
	if err != nil {
		return nil, fmt.Errorf("webhook.ListPending: %w", err)
	}
	defer rows.Close()

	var deliveries []*store.WebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// UpdateDelivery persists updated status/attempts/next_retry_at fields.
func (s *WebhookStore) UpdateDelivery(ctx context.Context, d *store.WebhookDelivery) error {
	const q = `
		UPDATE webhook_deliveries
		SET status = $1, attempts = $2, next_retry_at = $3
		WHERE id = $4`

	_, err := s.Pool.Exec(ctx, q,
		string(d.Status), d.Attempts, d.NextRetryAt, d.ID,
	)
	if err != nil {
		return fmt.Errorf("webhook.UpdateDelivery: %w", err)
	}
	return nil
}

// ListDeliveries returns all webhook deliveries, newest first.
func (s *WebhookStore) ListDeliveries(ctx context.Context, limit, offset int) ([]*store.WebhookDelivery, error) {
	const q = `
		SELECT id, event_type, payload, status, attempts, next_retry_at, created_at
		FROM webhook_deliveries
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("webhook.ListDeliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []*store.WebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

func scanDelivery(row scanner) (*store.WebhookDelivery, error) {
	var d store.WebhookDelivery
	var status string
	if err := row.Scan(
		&d.ID, &d.EventType, &d.Payload,
		&status, &d.Attempts, &d.NextRetryAt, &d.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("webhook.scan: %w", err)
	}
	d.Status = store.WebhookStatus(status)
	return &d, nil
}
