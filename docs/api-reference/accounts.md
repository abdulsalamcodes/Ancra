# Accounts API

## Provision Account

```http
POST /accounts
Authorization: Bearer <key>
```

**Request**

```json
{
  "customer_id": "c1b2c3d4-...",
  "display_name": "Amara Okonkwo",
  "customer_email": "amara@example.com",
  "phone_number": "+2348012345678",
  "bvn": "12345678901",
  "nin": "98765432101"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `customer_id` | UUID | Yes | The customer this account belongs to |
| `display_name` | string | Yes | Name registered on the bank account |
| `customer_email` | string | Yes | Customer's email |
| `phone_number` | string | No | Customer's phone number |
| `bvn` | string | No | Required for Tier 2+ |
| `nin` | string | No | Required for Tier 3 |

**Response** `201 Created`

```json
{
  "id": "a1b2c3d4-...",
  "customer_id": "c1b2c3d4-...",
  "bank_account_number": "9876543210",
  "bank_account_name": "Amara Okonkwo",
  "bank_name": "Nomba",
  "bank_code": "000026",
  "status": "active",
  "created_at": "2026-01-01T10:00:00Z"
}
```

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"missing required fields: customer_id"}}` |

See [Errors](../getting-started/errors.md) for the standard error envelope.

---

## Get Account

```http
GET /accounts/{id}
Authorization: Bearer <key>
```

**Response** `200 OK` — same shape as provision response.

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"id must be a valid UUID"}}` |
| `404` | `{"error":{"message":"account not found"}}` |

---

## List Accounts

```http
GET /accounts?limit=20&offset=0
Authorization: Bearer <key>
```

**Response** `200 OK`

```json
{
  "accounts": [...],
  "limit": 20,
  "offset": 0
}
```

---

## Update Account

```http
PUT /accounts/{id}
Authorization: Bearer <key>
```

**Request**

```json
{
  "display_name": "Amara O. Okonkwo"
}
```

**Response** `200 OK`

```json
{ "status": "updated" }
```

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"invalid JSON"}}` |
| `404` | `{"error":{"message":"account not found"}}` |

---

## Get Balance

```http
GET /accounts/{id}/balance
Authorization: Bearer <key>
```

**Response** `200 OK`

```json
{
  "AccountID": "a1b2c3d4-...",
  "Balance": 250000,
  "Currency": "NGN",
  "AsOf": "2026-01-01T12:00:00Z"
}
```

`Balance` is in **kobo**. Divide by 100 for naira.

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"id must be a valid UUID"}}` |
| `404` | `{"error":{"message":"account not found"}}` |

---

## List Transactions

```http
GET /accounts/{id}/transactions?limit=20&offset=0
Authorization: Bearer <key>
```

**Response** `200 OK`

```json
{
  "Entries": [
    {
      "id": "e1b2c3d4-...",
      "account_id": "a1b2c3d4-...",
      "direction": "credit",
      "amount": 100000,
      "currency": "NGN",
      "entry_type": "inbound_credit",
      "external_ref": "API-VACT_TRA-...",
      "narration": "Transfer from John Doe",
      "created_at": "2026-01-01T11:00:00Z"
    }
  ],
  "Limit": 20,
  "Offset": 0
}
```

---

## Account Statement

```http
GET /accounts/{id}/statement?limit=20&offset=0
Authorization: Bearer <key>
```

Returns entries with a `running_balance` per entry, accurate across all pages.

**Response** `200 OK`

```json
{
  "account_id": "a1b2c3d4-...",
  "opening_balance": 0,
  "closing_balance": 250000,
  "currency": "NGN",
  "entries": [
    {
      "id": "e1b2c3d4-...",
      "direction": "credit",
      "amount": 250000,
      "running_balance": 250000,
      "entry_type": "inbound_credit",
      "narration": "Transfer from John Doe",
      "created_at": "2026-01-01T11:00:00Z"
    }
  ],
  "limit": 20,
  "offset": 0
}
```

---

## Close Account

```http
POST /accounts/{id}/close
Authorization: Bearer <key>
```

**Response** `200 OK`

```json
{ "status": "closed" }
```

Closure is irreversible. Inbound payments after closure go to a suspense ledger. See [Close an Account](../virtual-accounts/close.md) for details.

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"id must be a valid UUID"}}` |
| `404` | `{"error":{"message":"account not found"}}` |
| `422` | `{"error":{"message":"account already closed"}}` |
