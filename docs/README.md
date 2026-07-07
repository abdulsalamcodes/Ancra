# Ancra Developer Documentation

Ancra is a Dedicated Virtual Account (DVA) infrastructure API. It lets you provision Nigerian bank account numbers for your customers, receive payments via webhook, and initiate outbound bank transfers — all through a simple REST API.

## What you can build

- Collect payments from customers via dedicated virtual accounts (NIP transfers)
- Credit customer wallets automatically when payments arrive
- Initiate outbound bank transfers from customer accounts
- Maintain a full double-entry ledger per account
- Reconcile your ledger against Nomba's settlement records

## Base URL

```
https://your-deployment.onrender.com
```

## Quick navigation

| Section | Description |
|---|---|
| [Authentication](getting-started/authentication.md) | API key setup and request signing |
| [Quickstart](getting-started/quickstart.md) | Create a customer, provision an account, receive a payment |
| [Customers](customers/overview.md) | Manage customer records and KYC tiers |
| [Virtual Accounts](virtual-accounts/overview.md) | Provision and manage dedicated virtual accounts |
| [Webhooks](webhooks/overview.md) | Receive real-time payment notifications |
| [Transfers](transfers/overview.md) | Send funds to any Nigerian bank account |
| [API Reference](api-reference/accounts.md) | Full endpoint reference |
| [Settings](api-reference/settings.md) | Nomba and webhook configuration |
| [Auth](api-reference/auth.md) | User profile and API key management |
