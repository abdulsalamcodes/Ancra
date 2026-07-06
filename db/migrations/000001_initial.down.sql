-- =============================================================================
-- Ancra – Rollback Initial Schema
-- =============================================================================
-- Drop in reverse dependency order.

DROP TABLE  IF EXISTS webhook_deliveries;
DROP TABLE  IF EXISTS reconciliation_runs;
DROP TABLE  IF EXISTS processed_events;
DROP TABLE  IF EXISTS ledger_entries;
DROP TABLE  IF EXISTS system_accounts;
DROP TABLE  IF EXISTS virtual_accounts;
DROP TABLE  IF EXISTS identity_versions;
DROP TABLE  IF EXISTS customers;

DROP TYPE IF EXISTS webhook_status;
DROP TYPE IF EXISTS recon_status;
DROP TYPE IF EXISTS entry_direction;
DROP TYPE IF EXISTS account_status;
