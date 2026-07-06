-- =============================================================================
-- Ancra – API Keys
-- =============================================================================

CREATE TABLE IF NOT EXISTS api_keys (
    id           UUID        PRIMARY KEY,
    name         TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

-- Fast lookup by hash for every authenticated request.
CREATE INDEX IF NOT EXISTS idx_api_keys_active_hash
    ON api_keys (key_hash)
    WHERE revoked_at IS NULL;
