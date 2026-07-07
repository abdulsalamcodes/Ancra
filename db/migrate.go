// Package db provides database migration utilities.
package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/000001_initial.up.sql
var migration001 string

//go:embed migrations/000002_api_keys.up.sql
var migration002 string

//go:embed migrations/000003_auth.up.sql
var migration003 string

//go:embed migrations/000004_org_scoping.up.sql
var migration004 string

//go:embed migrations/000005_nomba_configs.up.sql
var migration005 string

//go:embed migrations/000006_kyc_tier_history.up.sql
var migration006 string

//go:embed migrations/000007_org_system_accounts.up.sql
var migration007 string

//go:embed migrations/000008_org_webhook_configs.up.sql
var migration008 string

//go:embed migrations/000009_fix_system_accounts_index.up.sql
var migration009 string

// migrations lists all SQL migration scripts in order.
// Every statement must use IF NOT EXISTS / DO-EXCEPTION guards so this is
// safe to call on every startup without a migration tracking table.
var migrations = []string{
	migration001,
	migration002,
	migration003,
	migration004,
	migration005,
	migration006,
	migration007,
	migration008,
	migration009,
}

// RunMigrations applies all schema migrations in order.
// Safe to call on every startup — all DDL is idempotent.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	for i, sql := range migrations {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("migrations: apply migration %03d: %w", i+1, err)
		}
	}
	return nil
}
