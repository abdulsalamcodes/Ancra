# Webhooks API

## Nomba Webhook Receiver

```http
POST /webhooks/nomba
```

This endpoint is **public** but HMAC-SHA256 verified. Requests with an invalid or missing signature are rejected with `401`.

This endpoint is called by Nomba — you do not call it yourself. See [Webhook Overview](../webhooks/overview.md) for setup instructions.

**Successful processing** `200 OK`

```json
{ "status": "ok" }
```

**Duplicate event** (idempotent) `200 OK`

```json
{ "status": "ok" }
```

**Unhandled event type** `200 OK`

```json
{ "status": "ignored" }
```

**Invalid signature** `401 Unauthorized`

```json
{ "error": { "message": "unauthorized" } }
```

---

## List Webhook Deliveries

```http
GET /webhooks?limit=20&offset=0
Authorization: Bearer <key>
```

Returns a paginated list of outbound webhook delivery attempts for your organisation.

**Query Parameters**

| Parameter | Default | Description |
|---|---|---|
| `limit` | `20` | Max results per page |
| `offset` | `0` | Number of results to skip |

**Response** `200 OK`

```json
{
  "deliveries": [
    {
      "id": "w1b2c3d4-...",
      "org_id": "o1b2c3d4-...",
      "event_type": "payment_success",
      "status": "delivered",
      "attempts": 1,
      "next_retry_at": null,
      "created_at": "2026-01-01T10:00:00Z"
    }
  ],
  "limit": 20,
  "offset": 0
}
```

| Status | Meaning |
|---|---|
| `pending` | Delivery queued, not yet sent |
| `delivered` | Successfully delivered (HTTP 2xx) |
| `failed` | All retry attempts exhausted |

**Errors**

| Status | Meaning |
|---|---|
| `400` | Invalid query parameters |

---

## Admin: List Webhook Deliveries

```http
GET /admin/webhooks
Admin-Secret: <secret>
```

Returns a paginated list of all webhook delivery attempts (outbound developer webhooks).

**Query Parameters**

| Parameter | Default | Description |
|---|---|---|
| `limit` | `20` | Max results per page |
| `offset` | `0` | Number of results to skip |

**Response** `200 OK`

```json
{
  "webhooks": [
    {
      "id": "w1b2c3d4-...",
      "event_type": "payment_success",
      "status": "delivered",
      "attempts": 1,
      "created_at": "2026-01-01T10:00:00Z"
    }
  ],
  "limit": 20,
  "offset": 0
}
```
