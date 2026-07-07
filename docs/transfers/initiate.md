# Initiate a Transfer

Send funds from a customer's virtual account to a Nigerian bank account.

```http
POST /accounts/{id}/transfers
Authorization: Bearer <key>
```

`{id}` is the Ancra account UUID.

## Request

```json
{
  "amount": 500000,
  "narration": "Invoice payment #1042",
  "reference": "TXN-2026-0042",
  "sender_name": "Acme Corp",
  "destination_bank": "044",
  "destination_account": "0123456789",
  "destination_name": "Jane Doe"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `amount` | integer | Yes | Amount in **kobo** |
| `narration` | string | No | Payment note shown to recipient |
| `reference` | string | Yes | Your unique reference — used for deduplication |
| `sender_name` | string | Yes | Name shown as sender |
| `destination_bank` | string | Yes | 3-digit bank code |
| `destination_account` | string | Yes | 10-digit NUBAN account number |
| `destination_name` | string | Yes | Account name from [bank lookup](bank-lookup.md) |

## Response

```json
{
  "status": "submitted",
  "nomba_transaction": {
    "id": "TXN-abc123",
    "amount": 5000.00,
    "fee": 26.88,
    "type": "bank_transfer",
    "status": "successful",
    "timeCreated": "2026-01-01T12:00:00Z",
    "meta": {
      "merchantTxRef": "TXN-2026-0042",
      "rrn": "060012345678"
    }
  }
}
```

`status: "submitted"` means the transfer was accepted by Nomba. The `nomba_transaction.status` reflects Nomba's own status for the payment.

## Errors

| Status | Body | Meaning |
|---|---|---|
| `400` | `"missing required fields: ..."` | One or more required fields absent |
| `422` | `"insufficient funds"` | Account balance is below the requested amount |
| `502` | `"transfer rejected by payment provider"` | Nomba rejected the transfer; ledger debit has been reversed |

## Idempotency

`reference` is your idempotency key. Sending the same `reference` twice within the same account will be rejected by Nomba. Use a unique value per transfer (e.g. a UUID or `TXN-{timestamp}`).
