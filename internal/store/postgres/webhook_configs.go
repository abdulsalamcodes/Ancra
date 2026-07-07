package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// WebhookConfigStore persists per-org outbound webhook settings in PostgreSQL.
type WebhookConfigStore struct {
	Pool *pgxpool.Pool
}

const upsertWebhookConfigSQL = `
INSERT INTO org_webhook_configs
    (org_id, endpoint_url, signing_secret_encrypted, created_at, updated_at)
VALUES ($1, $2, $3, now(), now())
ON CONFLICT (org_id) DO UPDATE SET
    endpoint_url             = EXCLUDED.endpoint_url,
    signing_secret_encrypted = EXCLUDED.signing_secret_encrypted,
    updated_at               = now()`

// UpsertWebhookConfig creates or replaces the outbound webhook config for an org.
func (s *WebhookConfigStore) UpsertWebhookConfig(ctx context.Context, cfg *store.OrgWebhookConfig) error {
	_, err := s.Pool.Exec(ctx, upsertWebhookConfigSQL,
		cfg.OrgID,
		cfg.EndpointURL,
		cfg.SigningSecretEncrypted,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert webhook config: %w", err)
	}
	return nil
}

const getWebhookConfigSQL = `
SELECT org_id, endpoint_url, signing_secret_encrypted, created_at, updated_at
FROM   org_webhook_configs
WHERE  org_id = $1`

// GetWebhookConfig retrieves the outbound webhook config for the given org.
func (s *WebhookConfigStore) GetWebhookConfig(ctx context.Context, orgID uuid.UUID) (*store.OrgWebhookConfig, error) {
	row := s.Pool.QueryRow(ctx, getWebhookConfigSQL, orgID)

	var cfg store.OrgWebhookConfig
	if err := row.Scan(
		&cfg.OrgID,
		&cfg.EndpointURL,
		&cfg.SigningSecretEncrypted,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("postgres: get webhook config: %w", err)
	}
	return &cfg, nil
}
