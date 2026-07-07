-- =============================================================================
-- Ancra – Phase 5: per-org outbound webhook configuration
-- =============================================================================

-- Stores one outbound webhook endpoint per organisation.
-- The signing secret is stored AES-256-GCM encrypted; it is shown to the
-- developer exactly once on creation and never returned again in plain text.
CREATE TABLE IF NOT EXISTS org_webhook_configs (
    org_id                   UUID        PRIMARY KEY REFERENCES organizations(id),
    endpoint_url             TEXT        NOT NULL,
    signing_secret_encrypted TEXT        NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
