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

| Status | Meaning |
|---|---|
| `400` | `id` is not a valid UUID |
| `404` | Customer not found |

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
