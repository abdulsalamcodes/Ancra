# Ancra

> Dedicated Virtual Account (DVA) infrastructure for Nigerian fintech — built on Nomba rails.

Ancra provisions a permanent NGN bank account number for each customer, attributes every inbound NIP transfer to the correct customer via a double-entry ledger, and exposes the result as a clean multi-tenant REST API.

**Core guarantee:** `sum(customer balances) + fees + suspense == Nomba wallet balance` — at all times, provably.

Live docs: [ancra.mintlify.app](https://ancra.mintlify.app)

---

## What it does

| Capability | Detail |
|---|---|
| **Virtual account provisioning** | Each customer gets one permanent NGN bank account number via Nomba. Re-calling create returns the same account (idempotent). |
| **Real-time payment crediting** | Inbound NIP transfers trigger an HMAC-SHA256 verified webhook → ledger credit, fully automatic. |
| **Double-entry ledger** | Every naira is double-posted (customer credit + pool credit). Balances are derived, never stored — complete audit trail. |
| **Reconciliation** | Automated sweeps cross-reference the ledger pool balance against Nomba's wallet. Missed credits are backfilled automatically. |
| **Outbound webhooks** | When a payment lands, Ancra notifies your configured endpoint with a signed event. Up to 5 attempts with exponential backoff. |
| **Outbound transfers** | Send from a customer's virtual account to any Nigerian bank. Ledger is debited atomically before the Nomba call. |
| **Account statements** | Paginated statements with a correct running balance per line — accurate across all pages, not just the first. |
| **KYC tier management** | Customers carry Tier 1/2/3 KYC status. Upgrades are one-way, append-only, and fully auditable via history endpoint. |
| **Customer identity versioning** | Renames close the current identity record and open a new one. Historical statements always reflect the name active at the time of each entry. |
| **Closed-account protection** | Payments to a closed account route to the suspense ledger rather than being lost or incorrectly credited. |
| **Multi-tenant SaaS** | Full org isolation — customers, accounts, keys, ledger entries, and Nomba credentials are all org-scoped. |
| **BYOK Nomba credentials** | Each org can supply its own Nomba client credentials (encrypted AES-256-GCM at rest). |
| **Dashboard** | Web UI for org signup, login, API key management, and reconciliation history. |

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
- **Dual-source ingest.** Credits arrive via Nomba webhook *and* are recovered by the reconciliation sweep. The system stays correct even during webhook outages.
- **Idempotent provisioning.** `accountRef` is always `customer.ID` — Nomba's own idempotency key — so re-calling `POST /accounts` for the same customer returns the existing account without a Nomba round-trip.
- **Exact-once webhook processing.** `processed_events` stores the SHA of each `transactionId`. A duplicate webhook is rejected atomically before any ledger write.
- **Point-in-time identity.** `identity_versions` is append-only. A rename doesn't touch existing ledger entries — statements always show the name that was active at the time of each transaction.
- **KYC tiers are one-way.** `UpgradeKYCTier` holds a `FOR UPDATE` lock, validates `newTier > currentTier`, and writes both the update and the `kyc_tier_history` row in a single transaction. Downgrades are rejected with a typed sentinel error.

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| Go | ≥ 1.22 | |
| PostgreSQL | ≥ 14 | |
| [golang-migrate CLI](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) | latest | for `make migrate-up` |
| Nomba sandbox account | — | obtain at [developer.nomba.com](https://developer.nomba.com) |

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

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL connection string |
| `JWT_SECRET` | yes | — | Signs dashboard access tokens; min 32 chars |
| `ENCRYPTION_KEY` | yes | — | Exactly 32 bytes; used for AES-256-GCM encryption of Nomba credentials |
| `ADMIN_SECRET` | yes | — | Protects `/admin/*`; omit to disable admin routes entirely |
| `NOMBA_CLIENT_ID` | no* | — | Global Nomba OAuth2 client ID; omit when all orgs use BYOK |
| `NOMBA_CLIENT_SECRET` | no* | — | Global Nomba client secret |
| `NOMBA_ACCOUNT_ID` | no* | — | Parent Nomba merchant account ID |
| `NOMBA_SUB_ACCOUNT_ID` | no* | — | Sub-account for DVA creation and transfers |
| `NOMBA_WEBHOOK_SECRET` | no* | — | HMAC-SHA256 signing secret for inbound Nomba webhooks |
| `NOMBA_BASE_URL` | no | `https://api.nomba.com` | Override for sandboxes; trailing `/v1` is stripped automatically |
| `PORT` | no | `8080` | HTTP listen port |
| `API_KEY` | no | — | Legacy static key for single-tenant mode |
| `SWEEP_INTERVAL_SECONDS` | no | `60` | Reconciliation worker cadence |

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

| Area | Tests |
|---|---|
| Auth — API key, admin secret, JWT middleware | 7 |
| Customers — CRUD, pagination, KYC validation | 8 |
| KYC tiers — upgrade, downgrade rejection, same-tier rejection, audit history | 7 |
| Account lifecycle — provision, idempotency, balance, transactions, statement, rename, close | 16 |
| Webhooks (inbound) — signature verify, credit, exact-once dedup, suspense routing, delivery enqueue | 9 |
| Reconciliation — trigger, delta=0 when balanced, run history | 4 |
| Transfers — happy path, ledger debit, insufficient funds, validation | 6 |
| Statement — running balance correctness, debit impact, cross-page continuity | 3 |
| API key lifecycle — admin create → authenticate → revoke → rejected | 4 |
| Edge cases — closed account → suspense, point-in-time identity after rename | 2 |

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

## License

MIT — see [LICENSE](./LICENSE).
