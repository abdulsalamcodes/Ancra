# Webhooks

Ancra receives payment notifications from Nomba as webhooks and automatically credits the correct customer ledger. You do not need to poll for new payments.

## How it works

```
Sender → NIP transfer → Nomba
                           ↓
                  POST /webhooks/nomba (signed)
                           ↓
                     Ancra verifies HMAC
                           ↓
               Resolves virtual account number
                           ↓
                  Credits customer ledger
```

## Webhook Endpoint

```
POST /webhooks/nomba
```

This endpoint is **public** but HMAC-SHA256 verified. Any request with an invalid or missing signature is rejected with `401`.

## Setting Up

1. Deploy Ancra with a public URL
2. Go to Nomba Dashboard → Developer → Webhook Setup
3. Set your webhook URL to `https://your-domain.com/webhooks/nomba`
4. Set a **Signature Key** (any strong secret, e.g. `openssl rand -hex 32`)
5. Set `WEBHOOK_SECRET` environment variable to the same value on your Ancra deployment
6. Subscribe to the `payment_success` event

## Idempotency

Every webhook event is deduplicated by `transactionId`. If Nomba retries the same event (up to 5 times on failure), Ancra detects the duplicate and responds `200` without double-crediting.

## Retry Policy

If your server returns any non-`2xx` status, Nomba retries using exponential backoff:

| Attempt | Wait |
|---|---|
| 1 | 2 minutes |
| 2 | ~5 minutes |
| 3 | ~11 minutes |
| 4 | 24 minutes |
| 5 | ~53 minutes |

Always respond `200` as quickly as possible. Do your processing synchronously before responding — Ancra handles this internally.
