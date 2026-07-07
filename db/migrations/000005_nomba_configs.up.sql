-- =============================================================================
-- Ancra – Phase 3: per-org Nomba credentials (BYOK)
-- =============================================================================

-- Stores one Nomba integration config per organisation.
-- client_secret and webhook_secret are stored AES-256-GCM encrypted;
-- the encryption key lives in ENCRYPTION_KEY env var, never in the DB.
CREATE TABLE IF NOT EXISTS org_nomba_configs (
    org_id                   UUID        PRIMARY KEY REFERENCES organizations(id),
    client_id                TEXT        NOT NULL,
    client_secret_encrypted  TEXT        NOT NULL,
    account_id               TEXT        NOT NULL,
    sub_account_id           TEXT        NOT NULL,
    webhook_secret_encrypted TEXT        NOT NULL,
    sandbox                  BOOLEAN     NOT NULL DEFAULT true,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
