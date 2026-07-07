# Quickstart

Get from zero to a funded virtual account in four steps.

## Prerequisites

- An Ancra API key (see [Authentication](authentication.md))
- Your base URL

```bash
export ANCRA_KEY="ancra_live_xxxxxxxxxxxxxxxxxxxx"
export BASE="https://your-deployment.onrender.com"
```

---

## Step 1: Create a Customer

Every virtual account belongs to a customer. Create one first.

```bash
curl -X POST $BASE/customers \
  -H "Authorization: Bearer $ANCRA_KEY" \
  -H "Content-Type: application/json" \
  -d '{"kyc_tier": 1}'
```

```json
{
  "id": "c8a1b2d3-...",
  "kyc_tier": 1,
  "created_at": "2026-01-01T00:00:00Z"
}
```

Save the `id` — you'll need it in the next step.

---

## Step 2: Provision a Virtual Account

```bash
curl -X POST $BASE/accounts \
  -H "Authorization: Bearer $ANCRA_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "c8a1b2d3-...",
    "display_name": "Amara Okonkwo",
    "customer_email": "amara@example.com",
    "bvn": "12345678901"
  }'
```

```json
{
  "account": {
    "id": "a1b2c3d4-...",
    "bank_account_number": "9876543210",
    "bank_account_name": "Amara Okonkwo",
    "status": "active",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

The `bank_account_number` is the virtual account number your customer shares with senders.

---

## Step 3: Receive a Payment

When someone transfers money to `9876543210`, Nomba fires a `payment_success` webhook to your registered URL. Ancra verifies the signature, credits the customer's ledger, and responds `200` to Nomba.

See [Webhooks](../webhooks/overview.md) for setup and signature verification.

---

## Step 4: Check the Balance

```bash
curl $BASE/accounts/a1b2c3d4-.../balance \
  -H "Authorization: Bearer $ANCRA_KEY"
```

```json
{
  "AccountID": "a1b2c3d4-...",
  "Balance": 100000,
  "Currency": "NGN",
  "AsOf": "2026-01-01T00:01:00Z"
}
```

All balances are in **kobo** (1 NGN = 100 kobo). ₦1,000 = `100000`.
