-- =============================================================================
-- Ancra – Phase 2: scope all developer resources to an organisation
-- =============================================================================

-- Nullable during migration window; Phase 4 will add NOT NULL after backfill.
ALTER TABLE customers         ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
ALTER TABLE virtual_accounts  ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);

CREATE INDEX IF NOT EXISTS idx_customers_org
    ON customers (org_id)
    WHERE org_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_virtual_accounts_org
    ON virtual_accounts (org_id)
    WHERE org_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_org
    ON webhook_deliveries (org_id)
    WHERE org_id IS NOT NULL;
