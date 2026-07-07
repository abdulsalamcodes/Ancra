-- =============================================================================
-- Ancra – Fix: drop the stale global unique index on system_accounts.name
-- =============================================================================
--
-- Migration 000007 intended to replace the original global unique constraint
-- with two partial indexes (one for NULL org_id, one for non-NULL org_id).
-- It dropped the CONSTRAINT named "system_accounts_name_key", but the actual
-- object created in 000001 was an INDEX named "idx_system_accounts_name".
-- The DROP CONSTRAINT was therefore a no-op, leaving the global unique index
-- in place and silently blocking per-org SeedSystemAccounts calls.
--
-- This migration drops the stale index and back-fills system accounts for any
-- org that was created before the fix.

DROP INDEX IF EXISTS idx_system_accounts_name;

-- Back-fill system accounts for orgs that signed up before this fix.
-- ON CONFLICT DO NOTHING is safe because any org that already has per-org
-- accounts (created after a future fix) will not be duplicated.
INSERT INTO system_accounts (id, org_id, name)
SELECT gen_random_uuid(), o.id, a.name
FROM organizations o
CROSS JOIN (VALUES ('pool'), ('suspense'), ('fees'), ('returns_clearing')) AS a(name)
WHERE NOT EXISTS (
    SELECT 1 FROM system_accounts sa
    WHERE sa.org_id = o.id AND sa.name = a.name
)
ON CONFLICT DO NOTHING;
