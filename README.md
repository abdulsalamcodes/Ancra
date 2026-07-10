# Ancra

> Dedicated Virtual Account (DVA) infrastructure for Nigerian fintech — built on Nomba rails.

Ancra provisions a permanent NGN bank account number for each customer, attributes every inbound NIP transfer to the correct customer via a double-entry ledger, and exposes the result as a clean multi-tenant REST API.

**Core guarantee:** `sum(customer balances) + fees + suspense == Nomba wallet balance` — at all times, provably.

Live docs: [ancra.mintlify.app](https://ancra.mintlify.app)

---

## Reviewer Access

> Use these credentials to evaluate the live deployment without going through the signup flow.

**Live URL:** `https://ancra.onrender.com`

### Dashboard (Web UI)

| Field    | Value                        |
| -------- | ---------------------------- |
| URL      | https://ancra.onrender.com/auth |
| Email    | reviewer@ancra.dev           |
| Password | ReviewAncra2026!             |

### Developer API (Bearer token)

Use this key in the `Authorization` header for all API requests:

```
Authorization: Bearer ancra_reviewer_key_placeholder
```

**Quick smoke test:**

```bash
# List customers
curl https://ancra.onrender.com/customers \
  -H "Authorization: Bearer ancra_reviewer_key_placeholder"

# Provision a virtual account
curl -X POST https://ancra.onrender.com/accounts \
  -H "Authorization: Bearer ancra_reviewer_key_placeholder" \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "<id-from-above>", "display_name": "Test User", "customer_email": "test@example.com"}'
```

### Admin API

```bash
curl https://ancra.onrender.com/admin/orgs \
  -H "Admin-Secret: reviewer-admin-secret-placeholder"
```

> **Note to reviewers:** The server is kept alive via UptimeRobot pings — no cold-start delay expected.

---

## What it does

| Capability                       | Detail                                                                                                                                        |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| **Virtual account provisioning** | Each customer gets one permanent NGN bank account number via Nomba. Re-calling create returns the same account (idempotent).                  |
| **Real-time payment crediting**  | Inbound NIP transfers trigger an HMAC-SHA256 verified webhook → ledger credit, fully automatic.                                               |
| **Double-entry ledger**          | Every naira is double-posted (customer credit + pool credit). Balances are derived, never stored — complete audit trail.                      |
| **Reconciliation**               | Automated sweeps cross-reference the ledger pool balance against Nomba's wallet. Missed credits are backfilled automatically.                 |
| **Outbound webhooks**            | When a payment lands, Ancra notifies your configured endpoint with a signed event. Up to 5 attempts with exponential backoff.                 |
| **Outbound transfers**           | Send from a customer's virtual account to any Nigerian bank. Ledger is debited atomically before the Nomba call.                              |
| **Account statements**           | Paginated statements with a correct running balance per line — accurate across all pages, not just the first.                                 |
| **KYC tier management**          | Customers carry Tier 1/2/3 KYC status. Upgrades are one-way, append-only, and fully auditable via history endpoint.                           |
| **Customer identity versioning** | Renames close the current identity record and open a new one. Historical statements always reflect the name active at the time of each entry. |
| **Closed-account protection**    | Payments to a closed account route to the suspense ledger rather than being lost or incorrectly credited.                                     |
| **Multi-tenant SaaS**            | Full org isolation — customers, accounts, keys, ledger entries, and Nomba credentials are all org-scoped.                                     |
| **BYOK Nomba credentials**       | Each org can supply its own Nomba client credentials (encrypted AES-256-GCM at rest).                                                         |
| **Dashboard**                    | Web UI for org signup, login, API key management, and reconciliation history.                                                                 |

---

## Architecture

```
HTTP Request
      │
      ▼
  Chi Router
  ├── Public        — /health, /auth/signup, /auth/login, /webhooks/nomba
  ├── JWT auth      — /auth/me, /api-keys, /settings/*, /reconciliation (dashboard)
  ├── API key auth  — /customers, /accounts, /transfers (developer API)
  └── Admin-Secret  — /admin/api-keys, /admin/webhooks, /admin/orgs (operator)
      │
      ▼
 Domain Services
  ├── auth          — JWT sign/verify, org + user creation, refresh tokens
  ├── account       — provision, rename, close DVAs via Nomba
  ├── ledger        — double-entry bookkeeping (all amounts in kobo = NGN × 100)
  └── reconciliation — sweep Nomba wallet balance vs. pool ledger + backfill
      │
      ▼
 PostgreSQL (pgx/v5) — 8 migrations, fully versioned
  ├── orgs / users / refresh_tokens
  ├── api_keys                        (SHA-256 hashed; shown once)
  ├── customers / identity_versions   (point-in-time rename history)
  ├── virtual_accounts
  ├── ledger_entries                  (append-only, immutable)
  ├── system_accounts                 (pool · suspense · fees · returns_clearing, per org)
  ├── kyc_tier_history                (immutable upgrade audit trail)
  ├── org_nomba_configs               (per-org BYOK credentials, AES-256-GCM encrypted)
  ├── org_webhook_configs             (per-org outbound endpoint + signing secret)
  ├── processed_events                (webhook idempotency — exact-once processing)
  ├── reconciliation_runs
  └── webhook_deliveries              (outbound retry queue)
      │
      ▼
 Background Workers
  ├── SweepWorker    — runs reconciliation for every org on a configurable interval
  └── OutboundWorker — delivers webhook_deliveries to each org's endpoint (max 5 attempts, exp. backoff)
```

---

## Key design decisions

- **Ledger-first.** Balances are derived from append-only `ledger_entries`, never stored. Concurrent credits are safe; every naira has a full audit trail.
- **Dual-source ingest.** Credits arrive via Nomba webhook _and_ are recovered by the reconciliation sweep. The system stays correct even during webhook outages.
- **Idempotent provisioning.** `accountRef` is always `customer.ID` — Nomba's own idempotency key — so re-calling `POST /accounts` for the same customer returns the existing account without a Nomba round-trip.
- **Exact-once webhook processing.** `processed_events` stores the SHA of each `transactionId`. A duplicate webhook is rejected atomically before any ledger write.
- **Point-in-time identity.** `identity_versions` is append-only. A rename doesn't touch existing ledger entries — statements always show the name that was active at the time of each transaction.
- **KYC tiers are one-way.** `UpgradeKYCTier` holds a `FOR UPDATE` lock, validates `newTier > currentTier`, and writes both the update and the `kyc_tier_history` row in a single transaction. Downgrades are rejected with a typed sentinel error.

---

## Prerequisites

| Dependency                                                                              | Version | Notes                                                        |
| --------------------------------------------------------------------------------------- | ------- | ------------------------------------------------------------ |
| Go                                                                                      | ≥ 1.22  |                                                              |
| PostgreSQL                                                                              | ≥ 14    |                                                              |
| [golang-migrate CLI](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) | latest  | for `make migrate-up`                                        |
| Nomba sandbox account                                                                   | —       | obtain at [developer.nomba.com](https://developer.nomba.com) |

---

## Local Development

```bash
git clone https://github.com/abdulsalamcodes/ancra.git
cd ancra
go mod tidy
cp .env.example .env   # fill in values — see Environment Variables below
make migrate-up
make run               # starts on :8080
```

**Create your first API key:**

```bash
curl -X POST http://localhost:8080/admin/api-keys \
  -H "Admin-Secret: <ADMIN_SECRET>" \
  -H "Content-Type: application/json" \
  -d '{"name": "local-dev", "org_id": "<your-org-uuid>"}'
```

The raw key is returned once — copy it immediately.

---

## Environment Variables

| Variable                 | Required | Default                 | Description                                                            |
| ------------------------ | -------- | ----------------------- | ---------------------------------------------------------------------- |
| `DATABASE_URL`           | yes      | —                       | PostgreSQL connection string                                           |
| `JWT_SECRET`             | yes      | —                       | Signs dashboard access tokens; min 32 chars                            |
| `ENCRYPTION_KEY`         | yes      | —                       | Exactly 32 bytes; used for AES-256-GCM encryption of Nomba credentials |
| `ADMIN_SECRET`           | yes      | —                       | Protects `/admin/*`; omit to disable admin routes entirely             |
| `NOMBA_CLIENT_ID`        | no\*     | —                       | Global Nomba OAuth2 client ID; omit when all orgs use BYOK             |
| `NOMBA_CLIENT_SECRET`    | no\*     | —                       | Global Nomba client secret                                             |
| `NOMBA_ACCOUNT_ID`       | no\*     | —                       | Parent Nomba merchant account ID                                       |
| `NOMBA_SUB_ACCOUNT_ID`   | no\*     | —                       | Sub-account for DVA creation and transfers                             |
| `NOMBA_WEBHOOK_SECRET`   | no\*     | —                       | HMAC-SHA256 signing secret for inbound Nomba webhooks                  |
| `NOMBA_BASE_URL`         | no       | `https://api.nomba.com` | Override for sandboxes; trailing `/v1` is stripped automatically       |
| `PORT`                   | no       | `8080`                  | HTTP listen port                                                       |
| `API_KEY`                | no       | —                       | Legacy static key for single-tenant mode                               |
| `SWEEP_INTERVAL_SECONDS` | no       | `60`                    | Reconciliation worker cadence                                          |

\* Required when using the global Nomba client; optional when all orgs supply their own BYOK credentials via the settings API.

---

## Running Tests

The integration suite wires the **real router** against **in-memory fake stores** and a **fake Nomba HTTP server** — no database or Nomba account required.

```bash
make test                                                        # all tests, race detector
go test ./integration/... -v                                     # integration only, verbose
go test ./integration/... -run TestKYCTier_DowngradeRejected -v  # single test
```

**65 integration tests across 9 areas:**

| Area                                                                                                | Tests |
| --------------------------------------------------------------------------------------------------- | ----- |
| Auth — API key, admin secret, JWT middleware                                                        | 7     |
| Customers — CRUD, pagination, KYC validation                                                        | 8     |
| KYC tiers — upgrade, downgrade rejection, same-tier rejection, audit history                        | 7     |
| Account lifecycle — provision, idempotency, balance, transactions, statement, rename, close         | 16    |
| Webhooks (inbound) — signature verify, credit, exact-once dedup, suspense routing, delivery enqueue | 9     |
| Reconciliation — trigger, delta=0 when balanced, run history                                        | 4     |
| Transfers — happy path, ledger debit, insufficient funds, validation                                | 6     |
| Statement — running balance correctness, debit impact, cross-page continuity                        | 3     |
| API key lifecycle — admin create → authenticate → revoke → rejected                                 | 4     |
| Edge cases — closed account → suspense, point-in-time identity after rename                         | 2     |

---

## Database Migrations

```bash
make migrate-up              # apply all pending
make migrate-down            # roll back last
make migrate-force VERSION=3 # force a specific version after a failed migration
```

Migrations also run automatically on startup.

---

## Deployment

The project ships with `render.yaml` — one click deploys a web service + PostgreSQL on [Render](https://render.com).

1. Fork the repo and connect it to Render via **New → Blueprint**.
2. After the first deploy, set secrets in the Render dashboard: `JWT_SECRET`, `ENCRYPTION_KEY`, `ADMIN_SECRET`, and any Nomba credentials.

```bash
make build      # compile → ./bin/ancra
./bin/ancra     # reads env vars at startup
```

---

## API Documentation

Full docs at [ancra.mintlify.app](https://ancra.mintlify.app), including quickstart, authentication, virtual accounts, webhooks, transfers, KYC tiers, and the full API reference.

---

## Project Structure

```
ancra/
│
├── cmd/api/
│   └── main.go                     # Entry point — wires all stores, services, workers, and HTTP server
│
├── db/
│   ├── migrate.go                  # Embedded migration runner (runs on startup automatically)
│   └── migrations/
│       ├── 000001_initial          # Core tables: customers, virtual_accounts, ledger_entries, system_accounts
│       ├── 000002_api_keys         # api_keys table (hashed keys, org-scoped)
│       ├── 000003_auth             # orgs, users, refresh_tokens tables
│       ├── 000004_org_scoping      # Adds org_id to all tenant-scoped tables
│       ├── 000005_nomba_configs    # Per-org encrypted Nomba BYOK credentials
│       ├── 000006_kyc_tier_history # KYC upgrade audit trail (append-only, CHECK constraint)
│       ├── 000007_org_system_accounts # Migrates system accounts to per-org rows
│       ├── 000008_org_webhook_configs # Per-org outbound webhook endpoint + signing secret
│       └── 000009_fix_system_accounts_index
│
├── docs/                           # Mintlify documentation source (ancra.mintlify.app)
│   ├── getting-started/            # Introduction, quickstart, authentication, errors
│   ├── customers/                  # Customer overview, KYC tiers
│   ├── virtual-accounts/           # Provision, balance & transactions, close
│   ├── webhooks/                   # Overview, event reference, signature verification
│   ├── transfers/                  # Overview, initiate, bank lookup
│   └── api-reference/              # Auth API, settings API
│
├── integration/                    # End-to-end HTTP tests (real router, in-memory stores, fake Nomba server)
│   ├── setup_test.go               # Test environment, fake stores, fake Nomba HTTP server
│   ├── accounts_test.go
│   ├── apikeys_test.go
│   ├── auth_test.go
│   ├── customers_test.go
│   ├── edge_cases_test.go          # Closed-account → suspense, point-in-time identity
│   ├── kyc_test.go
│   ├── reconciliation_test.go
│   ├── statement_test.go
│   ├── transfer_test.go
│   └── webhook_test.go
│
├── internal/
│   ├── api/
│   │   ├── router.go               # Chi router — all route groups and middleware wiring
│   │   ├── handlers/
│   │   │   ├── accounts.go         # POST /accounts, GET /accounts, balance, transactions, statement, rename, close
│   │   │   ├── admin.go            # GET /admin/orgs, /admin/stats, /admin/reconciliation
│   │   │   ├── apikeys.go          # API key CRUD + admin create/list
│   │   │   ├── auth.go             # Signup, login, refresh, logout, /auth/me
│   │   │   ├── nomba_config.go     # GET/PUT /settings/nomba, test connection
│   │   │   ├── reconciliation.go   # GET /reconciliation, POST trigger, GET /webhooks
│   │   │   ├── transactions.go     # POST /transfers/lookup, POST /accounts/{id}/transfer
│   │   │   ├── webhook.go          # POST /webhooks/nomba (inbound, HMAC-verified)
│   │   │   └── webhook_config.go   # GET/PUT /settings/webhook
│   │   └── middleware/
│   │       ├── auth.go             # APIKeyAuth (60s cache), AdminAuth
│   │       ├── jwt.go              # JWTAuth (dashboard sessions)
│   │       ├── combined.go         # Shared middleware helpers
│   │       └── idempotency.go      # Idempotency-Key header support
│   │
│   ├── config/
│   │   └── config.go               # Env var loading (godotenv + os.Getenv)
│   │
│   ├── crypto/
│   │   └── aes.go                  # AES-256-GCM encrypt/decrypt for stored secrets
│   │
│   ├── domain/
│   │   ├── account/
│   │   │   ├── service.go          # Provision, rename, close, balance, statement, transfers
│   │   │   └── types.go            # Request/response types
│   │   ├── auth/
│   │   │   ├── service.go          # Org + user creation, login, JWT issue/verify, refresh
│   │   │   └── tokens.go           # JWT claims and signing helpers
│   │   ├── identity/
│   │   │   └── service.go          # Customer identity version management
│   │   ├── ledger/
│   │   │   ├── service.go          # PostCredit, PostDebit (double-entry, atomic)
│   │   │   └── types.go            # CreditRequest, DebitRequest
│   │   └── reconciliation/
│   │       └── service.go          # Run (sweep + delta), BackfillMissedCredits
│   │
│   ├── nomba/
│   │   ├── client.go               # OAuth2 token management + all Nomba API calls
│   │   ├── factory.go              # Per-org ClientFactory with 5-minute cache
│   │   ├── types.go                # Nomba request/response structs
│   │   └── webhook.go              # HMAC-SHA256 signature verifier
│   │
│   ├── store/
│   │   ├── store.go                # All store interfaces + domain model structs
│   │   └── postgres/
│   │       ├── db.go               # pgx/v5 connection pool setup
│   │       ├── accounts.go
│   │       ├── apikeys.go
│   │       ├── customers.go        # Includes UpgradeKYCTier (atomic, FOR UPDATE)
│   │       ├── events.go           # Idempotency store (processed_events)
│   │       ├── ledger.go           # InsertEntries, GetBalance, GetBalanceAsOf, ListEntries
│   │       ├── nomba_configs.go
│   │       ├── orgs.go
│   │       ├── reconciliation.go   # Reconciliation runs + webhook deliveries
│   │       ├── refresh_tokens.go
│   │       ├── users.go
│   │       └── webhook_configs.go
│   │
│   ├── tenant/
│   │   └── context.go              # WithOrgID / OrgIDFromContext (request-scoped org identity)
│   │
│   └── worker/
│       ├── sweep.go                # SweepWorker — per-org reconciliation on a ticker
│       └── outbound.go             # OutboundWorker — webhook delivery with exponential backoff
│
├── web/                            # Embedded HTML pages (served by the Go binary)
│   ├── web.go                      # http.FS embed + route handlers
│   ├── landing.html                # Public landing page
│   ├── auth.html                   # Signup / login
│   ├── dashboard.html              # Developer dashboard (API keys, reconciliation)
│   └── admin.html                  # Operator admin panel
│
├── CLAUDE.md                       # AI assistant context for this repository
├── Makefile
├── render.yaml                     # Render one-click deploy blueprint
└── docs.json                       # Mintlify configuration
```

---

## License

MIT — see [LICENSE](./LICENSE).
