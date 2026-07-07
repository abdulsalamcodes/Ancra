# Customers API

## Create Customer

```http
POST /customers
Authorization: Bearer <key>
```

**Request**

```json
{
  "kyc_tier": 1
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `kyc_tier` | integer | No | `1`, `2`, or `3`. Defaults to `1` |

**Response** `201 Created`

```json
{
  "id": "c1b2c3d4-e5f6-...",
  "kyc_tier": 1,
  "created_at": "2026-01-01T10:00:00Z"
}
```

---

## Get Customer

```http
GET /customers/{id}
Authorization: Bearer <key>
```

**Response** `200 OK`

```json
{
  "id": "c1b2c3d4-e5f6-...",
  "kyc_tier": 1,
  "created_at": "2026-01-01T10:00:00Z"
}
```

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"id must be a valid UUID"}}` |
| `404` | `{"error":{"message":"customer not found"}}` |

---

## List Customers

```http
GET /customers?limit=20&offset=0
Authorization: Bearer <key>
```

**Query Parameters**

| Parameter | Default | Description |
|---|---|---|
| `limit` | `20` | Max results per page |
| `offset` | `0` | Number of results to skip |

**Response** `200 OK`

```json
{
  "customers": [
    {
      "id": "c1b2c3d4-...",
      "kyc_tier": 1,
      "created_at": "2026-01-01T10:00:00Z"
    }
  ],
  "limit": 20,
  "offset": 0
}
```

**Errors**

| Status | Example |
|---|---|
| `400` | `{"error":{"message":"invalid query parameters"}}` |

---

## Upgrade KYC Tier

```http
PUT /customers/{id}/kyc-tier
Authorization: Bearer <key>
```

**Request**

```json
{
  "kyc_tier": 2
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `kyc_tier` | integer | Yes | Must be strictly higher than the current tier (`2` or `3`) |

**Response** `200 OK`

```json
{
  "id": "h1b2c3d4-...",
  "customer_id": "c1b2c3d4-...",
  "from_tier": 1,
  "to_tier": 2,
  "upgraded_at": "2026-01-01T12:00:00Z"
}
```

**Errors**

| Status | Meaning |
|---|---|
| `400` | Invalid JSON or `kyc_tier` out of range |
| `422` | Not an upgrade (same or lower tier) |
| `500` | Store failure |

---

## KYC Tier History

```http
GET /customers/{id}/kyc-tier/history
Authorization: Bearer <key>
```

**Response** `200 OK`

```json
{
  "history": [
    {
      "id": "h1b2c3d4-...",
      "customer_id": "c1b2c3d4-...",
      "from_tier": 0,
      "to_tier": 1,
      "upgraded_at": "2026-01-01T10:00:00Z"
    }
  ]
}
```

**Errors**

| Status | Meaning |
|---|---|
| `500` | Store failure |
