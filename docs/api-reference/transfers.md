# Transfers API

## Bank Account Lookup

```http
POST /transfers/lookup
Authorization: Bearer <key>
```

**Request**

```json
{
  "account_number": "0123456789",
  "bank_code": "044"
}
```

**Response** `200 OK`

```json
{
  "account_number": "0123456789",
  "account_name": "Jane Doe"
}
```

| Status | Meaning |
|---|---|
| `400` | Missing fields |
| `502` | Could not resolve account via Nomba |

---

## Initiate Transfer

```http
POST /accounts/{id}/transfer
Authorization: Bearer <key>
```

`{id}` is the source virtual account UUID.

**Request**

```json
{
  "amount": 500000,
  "narration": "Invoice payment",
  "reference": "TXN-2026-0042",
  "sender_name": "Acme Corp",
  "destination_bank": "044",
  "destination_account": "0123456789",
  "destination_name": "Jane Doe"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `amount` | integer | Yes | Amount in kobo |
| `narration` | string | No | Shown to recipient |
| `reference` | string | Yes | Unique idempotency key |
| `sender_name` | string | Yes | Shown as the sender |
| `destination_bank` | string | Yes | 3-digit bank code |
| `destination_account` | string | Yes | 10-digit NUBAN |
| `destination_name` | string | Yes | Verified account name |

**Response** `200 OK`

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

| Status | Meaning |
|---|---|
| `400` | Missing or invalid fields |
| `422` | Insufficient funds |
| `502` | Nomba rejected the transfer — ledger debit automatically reversed |
