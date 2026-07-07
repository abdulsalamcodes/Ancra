# Authentication

All API requests must be authenticated with a Bearer token. Ancra uses opaque API keys that are created and managed through the admin interface.

## Creating an API Key

API keys are created via the admin endpoint. This requires the `Admin-Secret` header.

```bash
curl -X POST https://your-deployment.onrender.com/admin/api-keys \
  -H "Admin-Secret: your-admin-secret" \
  -H "Content-Type: application/json" \
  -d '{"name": "production-key"}'
```

**Response**

```json
{
  "id": "3f2a1b...",
  "name": "production-key",
  "key": "ancra_live_xxxxxxxxxxxxxxxxxxxx",
  "created_at": "2026-01-01T00:00:00Z"
}
```

> **Save the key immediately.** The full key is only returned once. After creation, only a hash is stored.

## Making Authenticated Requests

Include the key in the `Authorization` header on every request:

```bash
curl https://your-deployment.onrender.com/accounts \
  -H "Authorization: Bearer ancra_live_xxxxxxxxxxxxxxxxxxxx"
```

## Revoking Keys

```bash
curl -X DELETE https://your-deployment.onrender.com/admin/api-keys/{id} \
  -H "Admin-Secret: your-admin-secret"
```

## Key Format

All keys are prefixed with `ancra_` and are 32 characters after the prefix. Keys are stored as SHA-256 hashes server-side — if you lose a key, revoke it and create a new one.
