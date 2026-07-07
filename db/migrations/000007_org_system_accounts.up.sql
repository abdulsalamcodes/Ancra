-- =============================================================================
-- Ancra – Phase 4: per-org system accounts + org-scoped reconciliation runs
-- =============================================================================

-- 1. Add org_id to system_accounts.
--    Nullable so the existing global seed rows (pool/suspense/fees/returns_clearing)
--    remain valid during the migration window. Per-org rows added on signup.
ALTER TABLE system_accounts
    ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);

-- 2. Replace the old global unique-on-name constraint with two partial indexes:
--    one for global (NULL) rows and one for per-org rows. This correctly allows
--    "pool" to exist once globally and once per org without conflicting.
ALTER TABLE system_accounts DROP CONSTRAINT IF EXISTS system_accounts_name_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_system_accounts_global_name
    ON system_accounts (name) WHERE org_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_system_accounts_org_name
    ON system_accounts (org_id, name) WHERE org_id IS NOT NULL;

-- 3. Add org_id to reconciliation_runs so each run is attributed to an org.
ALTER TABLE reconciliation_runs
    ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);

CREATE INDEX IF NOT EXISTS idx_recon_runs_org_run_at
    ON reconciliation_runs (org_id, run_at DESC)
    WHERE org_id IS NOT NULL;
