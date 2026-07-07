# Ancra

> Persistent Dedicated Virtual Account (DVA) infrastructure on Nomba rails.

Ancra provisions a permanent Nigerian bank account number for each customer, attributes every inbound transfer to the correct customer via a double-entry ledger, and exposes the result as a clean REST API that other services can build on.

**Core guarantee:** `sum(customer balances) + fees + suspense == Nomba wallet balance` — at all times, provably.

---

## Table of Contents

- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Local Development](#local-development)
- [Environment Variables](#environment-variables)
- [Running Tests](#running-tests)
- [Database Migrations](#database-migrations)
- [Deployment](#deployment)
- [API Documentation](#api-documentation)
- [Project Structure](#project-structure)
- [License](#license)

---

## Architecture

```
Client Request
      │
      ▼
  Chi Router  ─── Admin routes (Admin-Secret)
      │        ─── Developer API (Bearer API key)
      │        ─── Webhook endpoint (HMAC-SHA512 verified)
      │
      ▼
 Domain Services
  ├── account      — provision, rename, close DVAs via Nomba
  ├── ledger       — double-entry bookkeeping (all amounts in kobo)
  └── reconciliation — sweep Nomba wallet balance vs. pool ledger
      │
      ▼
 PostgreSQL (pgx/v5)
  ├── customers / identity_versions  (point-in-time rename history)
  ├── virtual_accounts
  ├── ledger_entries                 (append-only)
  ├── system_accounts                (pool, suspense, fees, returns_clearing)
  ├── processed_events               (webhook idempotency)
  ├── reconciliation_runs
  ├── webhook_deliveries             (outbound retry queue)
  └── api_keys                       (hashed)
      │
      ▼
 Background Workers
  ├── SweepWorker    — reconciliation on a configurable interval
  └── OutboundWorker — retries failed webhook deliveries with exponential backoff
```

**Key design decisions:**

- **Ledger-first.** Balances are never stored directly — they are derived from immutable append-only ledger entries. This makes concurrent credits safe and provides a complete audit trail.
- **Dual-source ingest.** Inbound credits are posted via webhook *and* recovered by the reconciliation sweep. The system stays correct even when webhooks are dropped.
- **Identity versioning.** Customer renames close the current `identity_version` row and open a new one. Historical statements remain correct retroactively.
- **Closed-account protection.** Credits to a closed account are routed to the `suspense` system account rather than silently credited or lost.

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

### 1. Clone and install dependencies

```bash
git clone https://github.com/abdulsalamcodes/ancra.git
cd ancra
go mod tidy
```

### 2. Configure environment

```bash
cp .env.example .env
# Edit .env with your values — see Environment Variables below
```

### 3. Apply database migrations

```bash
make migrate-up
```

### 4. Run the server

```bash
make run
# Server starts on :8080 by default
```

### 5. Create your first API key

```bash
curl -X POST http://localhost:8080/admin/api-keys \
  -H "Admin-Secret: <your-ADMIN_SECRET>" \
  -H "Content-Type: application/json" \
  -d '{"name": "local-dev"}'
```

The response contains the raw key — copy it now, it is shown only once.

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL connection string |
| `NOMBA_CLIENT_ID` | yes | — | OAuth2 client ID from Nomba dashboard |
| `NOMBA_CLIENT_SECRET` | yes | — | OAuth2 client secret |
| `NOMBA_ACCOUNT_ID` | yes | — | Parent Nomba merchant account ID |
| `NOMBA_SUB_ACCOUNT_ID` | yes | — | Sub-account used for DVA creation and transfers |
| `NOMBA_WEBHOOK_SECRET` | yes | — | HMAC-SHA512 signing secret for inbound webhooks |
| `ADMIN_SECRET` | yes | — | Protects `/admin/*` endpoints; keep this out of client code |
| `PORT` | no | `8080` | HTTP listen port |
| `NOMBA_BASE_URL` | no | `https://api.nomba.com/v1` | Override for sandbox testing |
| `API_KEY` | no | — | Legacy static key; prefer DB-managed keys for production |
| `SWEEP_INTERVAL_SECONDS` | no | `60` | How often the reconciliation worker runs |
| `WEBHOOK_ENDPOINT` | no | — | Your endpoint to receive outbound event notifications |

Create a `.env.example` by copying `.env` and blanking the secret values before committing.

---

## Running Tests

The integration suite uses in-memory fake stores and a fake Nomba HTTP server — no real database or Nomba credentials are required.

```bash
# All tests (unit + integration), with race detector
make test

# Integration tests only
go test ./integration/... -v

# A single test by name
go test ./integration/... -run TestWebhook_DuplicateCreditIsNoop -v
```

The suite covers:

| Area | Tests |
|---|---|
| Auth — missing / wrong key, admin secret | 7 |
| Customer CRUD and pagination | 7 |
| Account lifecycle — provision, balance, transactions, statement, rename, close | 16 |
| Webhook — signature verify, credit, exact-once dedup, suspense, ignored events | 9 |
| Reconciliation — trigger, delta=0, run history | 4 |
| Edge cases — closed account → suspense, point-in-time identity | 2 |
| Transfers — happy path, debit ledger entry, insufficient funds, validation | 6 |
| Statement — running balance, debit impact, cross-page balance continuity | 3 |
| API key lifecycle — create → use → revoke → rejected | 4 |

---

## Database Migrations

Migrations live in `db/migrations/` and are managed by [golang-migrate](https://github.com/golang-migrate/migrate).

```bash
# Apply all pending migrations
make migrate-up

# Roll back the last migration
make migrate-down

# Force a specific version (use after a failed migration)
make migrate-force VERSION=1
```

The application also runs migrations automatically on startup via the embedded migration runner in `db/migrate.go`.

---

## Deployment

The project ships with a `render.yaml` that provisions a web service and a free-tier PostgreSQL database on [Render](https://render.com).

### Deploy via Blueprint (recommended)

1. Fork or push this repository to GitHub.
2. In the Render dashboard, click **New → Blueprint**.
3. Connect your repository — Render detects `render.yaml` automatically.
4. Set the secret environment variables (`NOMBA_CLIENT_ID`, `NOMBA_CLIENT_SECRET`, `NOMBA_ACCOUNT_ID`, `NOMBA_SUB_ACCOUNT_ID`, `NOMBA_WEBHOOK_SECRET`, `ADMIN_SECRET`, `WEBHOOK_ENDPOINT`) in the Render dashboard after the first deploy.

### Manual deploy

```bash
# Build a production binary
make build

# The binary at ./bin/ancra reads environment variables at startup
./bin/ancra
```

---

## API Documentation

Full developer documentation lives in [`docs/`](./docs/README.md), including:

- [Authentication](./docs/getting-started/authentication.md)
- [Quickstart](./docs/getting-started/quickstart.md)
- [Virtual Accounts](./docs/virtual-accounts/overview.md)
- [Webhooks](./docs/webhooks/overview.md)
- [Transfers](./docs/transfers/overview.md)
- [Full API Reference](./docs/api-reference/accounts.md)

---

## Project Structure

```
ancra/
├── cmd/api/          — application entry point (main.go)
├── db/               — migration runner and SQL migration files
├── docs/             — developer-facing API documentation
├── integration/      — HTTP integration tests (no DB required)
├── internal/
│   ├── api/          — chi router, middleware, HTTP handlers
│   ├── config/       — environment variable loading
│   ├── domain/       — business logic (account, ledger, reconciliation)
│   ├── nomba/        — Nomba API client, webhook verifier, types
│   ├── store/        — store interfaces and PostgreSQL implementations
│   └── worker/       — background goroutines (sweep, outbound webhooks)
├── web/              — embedded dashboard SPA
├── Makefile
└── render.yaml       — Render IaC definition
```

---

## License

MIT — see [LICENSE](./LICENSE).
