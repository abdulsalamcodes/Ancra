-- =============================================================================
-- Ancra – Multi-tenant auth: organisations, users, refresh tokens
-- =============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id         UUID        PRIMARY KEY,
    name       TEXT        NOT NULL,
    slug       TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id            UUID        PRIMARY KEY,
    org_id        UUID        NOT NULL REFERENCES organizations(id),
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'owner', -- owner | member
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Opaque refresh tokens stored as SHA-256 hashes; plaintext never persisted.
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users(id),
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Scope API keys to an organisation.
-- Nullable during the migration window; Phase 2 will backfill and add NOT NULL.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);

CREATE INDEX IF NOT EXISTS idx_users_email
    ON users (email);

CREATE INDEX IF NOT EXISTS idx_users_org
    ON users (org_id);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_active
    ON refresh_tokens (user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_api_keys_org
    ON api_keys (org_id)
    WHERE org_id IS NOT NULL;
