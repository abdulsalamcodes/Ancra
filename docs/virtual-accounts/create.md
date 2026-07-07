# Provision an Account

## Request

```http
POST /accounts
Authorization: Bearer <key>
Content-Type: application/json
```

```json
{
  "customer_id": "c8a1b2d3-e4f5-6789-abcd-ef0123456789",
  "display_name": "Amara Okonkwo",
  "customer_email": "amara@example.com",
  "phone_number": "08012345678",
  "bvn": "12345678901"
}
```

### Body Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `customer_id` | UUID | Yes | The customer who will own this account |
| `display_name` | string | Yes | Name to register on the virtual account |
| `customer_email` | string | Yes | Customer's email address |
| `phone_number` | string | No | Customer's phone number |
| `bvn` | string | No | BVN for KYC tier 2+ |
| `nin` | string | No | NIN for KYC tier 3 |

## Response `201`

```json
{
  "account": {
    "id": "a1b2c3d4-...",
    "customer_id": "c8a1b2d3-...",
    "account_ref": "c8a1b2d3-...",
    "bank_account_number": "9876543210",
    "bank_account_name": "Amara Okonkwo",
    "status": "active",
    "created_at": "2026-01-01T00:00:00Z"
  },
  "identity": {
    "id": "i1b2c3d4-...",
    "customer_id": "c8a1b2d3-...",
    "display_name": "Amara Okonkwo",
    "effective_from": "2026-01-01T00:00:00Z"
  }
}
```

## Update Display Name

Renaming an account closes the current identity version and opens a new one. Historical ledger entries are unaffected.

```http
PUT /accounts/{id}
```

```json
{
  "display_name": "Amara O. Okonkwo"
}
```

## List All Accounts

```http
GET /accounts?limit=20&offset=0
```

## Get a Single Account

```http
GET /accounts/{id}
```
