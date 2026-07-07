-- kyc_tier_history: append-only audit log for customer KYC tier upgrades.
-- Tiers can only increase (upgrades). Downgrades are rejected at the application layer.
CREATE TABLE IF NOT EXISTS kyc_tier_history (
    id          UUID        PRIMARY KEY,
    customer_id UUID        NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    from_tier   SMALLINT    NOT NULL,
    to_tier     SMALLINT    NOT NULL,
    upgraded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tier_must_increase CHECK (to_tier > from_tier)
);

CREATE INDEX IF NOT EXISTS idx_kyc_tier_history_customer
    ON kyc_tier_history (customer_id, upgraded_at DESC);
