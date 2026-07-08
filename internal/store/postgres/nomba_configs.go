package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// NombaConfigStore persists per-org Nomba BYOK credentials in PostgreSQL.
type NombaConfigStore struct {
	Pool *pgxpool.Pool
}

const upsertNombaConfigSQL = `
INSERT INTO org_nomba_configs
    (org_id, client_id, client_secret_encrypted, account_id, sub_account_id, webhook_secret_encrypted, sandbox, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
ON CONFLICT (org_id) DO UPDATE SET
    client_id               = EXCLUDED.client_id,
    client_secret_encrypted = EXCLUDED.client_secret_encrypted,
    account_id              = EXCLUDED.account_id,
    sub_account_id          = EXCLUDED.sub_account_id,
    webhook_secret_encrypted= EXCLUDED.webhook_secret_encrypted,
    sandbox                 = EXCLUDED.sandbox,
    updated_at              = now()`

// UpsertNombaConfig creates or replaces the Nomba config for an org.
func (s *NombaConfigStore) UpsertNombaConfig(ctx context.Context, cfg *store.OrgNombaConfig) error {
	_, err := s.Pool.Exec(ctx, upsertNombaConfigSQL,
		cfg.OrgID,
		cfg.ClientID,
		cfg.ClientSecretEncrypted,
		cfg.AccountID,
		cfg.SubAccountID,
		cfg.WebhookSecretEncrypted,
		cfg.Sandbox,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert nomba config: %w", err)
	}
	return nil
}

const getNombaConfigSQL = `
SELECT org_id, client_id, client_secret_encrypted, account_id, sub_account_id,
       webhook_secret_encrypted, sandbox, created_at, updated_at
FROM   org_nomba_configs
WHERE  org_id = $1`

// GetNombaConfig retrieves the Nomba config for the given org.
func (s *NombaConfigStore) GetNombaConfig(ctx context.Context, orgID uuid.UUID) (*store.OrgNombaConfig, error) {
	row := s.Pool.QueryRow(ctx, getNombaConfigSQL, orgID)

	var cfg store.OrgNombaConfig
	if err := row.Scan(
		&cfg.OrgID,
		&cfg.ClientID,
		&cfg.ClientSecretEncrypted,
		&cfg.AccountID,
		&cfg.SubAccountID,
		&cfg.WebhookSecretEncrypted,
		&cfg.Sandbox,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNombaConfigNotFound
		}
		return nil, fmt.Errorf("postgres: get nomba config: %w", err)
	}
	return &cfg, nil
}
