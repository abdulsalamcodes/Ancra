# Reconciliation API

Ancra runs a periodic reconciliation sweep that compares the ledger against Nomba's transaction records. Any discrepancy is logged as an alert.

## Get Latest Report

```http
GET /reconciliation
Authorization: Bearer <key>
```

Returns the most recent reconciliation report.

**Response** `200 OK`

```json
{
  "id": "r1b2c3d4-...",
  "status": "clean",
  "checked_at": "2026-01-01T06:00:00Z",
  "total_checked": 1240,
  "discrepancies": 0
}
```

| Field | Description |
|---|---|
| `status` | `clean` — no discrepancies found; `alert` — manual review required |
| `total_checked` | Number of ledger entries cross-referenced |
| `discrepancies` | Number of entries where the ledger and Nomba disagree |

---

## Trigger Manual Sweep

```http
POST /reconciliation/trigger
Authorization: Bearer <key>
```

Enqueues an immediate reconciliation sweep outside of the normal schedule. Useful after a deployment or when investigating an incident.

**Response** `200 OK`

```json
{ "status": "triggered" }
```

The sweep runs asynchronously. Poll `GET /reconciliation` to check results.

---

## When Discrepancies Occur

A discrepancy means a ledger entry does not have a matching Nomba transaction, or a Nomba transaction was not credited to the ledger. Common causes:

- Transfer reversal not yet processed (`payment_reversal` event)
- Webhook delivery failure (missed `payment_success` event)
- Nomba ledger debit reversal after a failed outbound transfer

Contact support with the affected `external_ref` (Nomba transaction ID) to investigate.
