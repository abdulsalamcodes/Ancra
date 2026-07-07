# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build          # compile → ./bin/ancra
make run            # go run ./cmd/api/main.go (loads .env automatically)
make test           # go test -race -count=1 ./...
make lint           # staticcheck ./...
make migrate-up     # apply all pending migrations (requires golang-migrate CLI)
make migrate-down   # roll back the last migration

# Run a single integration test
go test ./integration/... -run TestWebhook_DuplicateCreditIsNoop -v -count=1

# Run all integration tests (no real DB or Nomba account required)
go test ./integration/... -race -count=1
```

Required env vars (non-optional): `DATABASE_URL`, `JWT_SECRET`, `ENCRYPTION_KEY`.  
All others have documented defaults in `internal/config/config.go`. Migrations also run automatically on startup.

## Architecture

### Request flow

```
HTTP request
  → Chi router (internal/api/router.go)
  → Middleware chain (JWT / API key / Admin-Secret auth)
  → Handler (internal/api/handlers/)
  → Domain service (internal/domain/{account,ledger,reconciliation,auth}/)
  → Store interface (internal/store/store.go)
  → Postgres implementation (internal/store/postgres/)
```

### Three auth layers, three route groups

| Group | Middleware | Who uses it |
|---|---|---|
| JWT (`/auth/me`, settings, dashboard reconciliation) | `middleware.JWTAuth` | Browser dashboard |
| API key (`/customers`, `/accounts`, `/transfers`, etc.) | `middleware.APIKeyAuth` | Developer integrations |
| Admin-Secret (`/admin/*`) | `middleware.AdminAuth` | Operator tooling only |

The API key middleware has a 60-second in-process cache (`sync.Map`) and touches `last_used_at` asynchronously. `middleware.InvalidateKey(hash)` evicts a key immediately on revocation.

### Tenant context

Org identity flows through `context.Context` via `internal/tenant`. Middleware sets it with `tenant.WithOrgID`; domain services read it with `tenant.OrgIDFromContext`. Every store method that is org-scoped takes an explicit `orgID uuid.UUID` parameter — never reads from context itself.

### Ledger model

All amounts are stored in **kobo** (NGN × 100). Balances are never stored — they are always derived by summing `ledger_entries`. The invariant is:

```
sum(customer account balances) + fees + suspense == Nomba wallet balance
```

Every inbound credit posts two entries atomically (customer credit + pool credit). Every outbound transfer posts debit + fee entries. Credits to a **closed** account go to the `suspense` system account instead.

### Idempotency

- **Inbound webhooks**: `processed_events` table; `MarkProcessed` is atomic. Duplicate `transactionId` returns `ErrAlreadyProcessed` (`internal/store/postgres/events.go`).
- **Account provisioning**: `account.Create` checks `ListAccountsByCustomer` before calling Nomba. The `accountRef` is always `customer.ID.String()` — Nomba uses this as its own idempotency key, so the same customer always maps to the same physical account.
- **API keys**: hashed with SHA-256 before storage; plain-text key is returned once only.

### Customer identity model

`identity_versions` is an append-only table. A rename closes the current row (`EffectiveTo = now`) and inserts a new open row. This means `GET /accounts/{id}/statement` always reflects the name that was active at the time of each entry — no need to store the name on ledger entries.

### Nomba client factory (BYOK)

`internal/nomba/factory.go` — each org can store its own Nomba credentials (`org_nomba_configs` table, encrypted with AES-256-GCM via `internal/crypto`). `ClientFactory.ForOrg` builds and caches a `*nomba.Client` per org for 5 minutes. The reconciliation service and outbound worker use the factory; the account service still uses the single global client passed at startup.

### Background workers

Both workers run in goroutines started in `main.go` with a shared cancellable context.

- **SweepWorker** (`internal/worker/sweep.go`): calls `reconciliation.Run` for every org on a configurable interval (`SWEEP_INTERVAL_SECONDS`, default 60). Also calls `BackfillMissedCredits` to recover any credits that arrived while webhooks were down.
- **OutboundWorker** (`internal/worker/outbound.go`): polls `webhook_deliveries` for pending rows and POSTs them to each org's configured endpoint. Max 5 attempts; exponential backoff starting at `initialBackoff` doubling per attempt.

### Integration tests

`integration/setup_test.go` wires the **real router** against **in-memory fake stores** and a **fake Nomba HTTP server** (`httptest.Server`). No database or Nomba credentials are needed. All fake stores implement the interfaces in `internal/store/store.go`. The `fakeNombaConfigStore` + `mustTestEncryptor` allow the `ClientFactory` to be exercised in tests. `testStaticKey` / `testOrgID` / `testAdminSecret` are the fixed credentials used across all tests.

### Key non-obvious decisions

- `uuid.Nil` is used as a sentinel for "all orgs" in admin store queries (`ListKeys`, `ListDeliveries`, `ListRuns`).
- `json.Number` (not `float64`) is used for Nomba's balance field because Nomba returns amounts as decimal strings like `"281946.0"`.
- Statement running balances are computed by anchoring to the closing balance of the page and walking backwards — this keeps multi-page pagination accurate even as new transactions arrive.
- The `StaticKeyOrgID` field in `RouterDeps` injects an org context for the legacy static API key, enabling single-tenant deployments without a database key.
