# KYC Tiers

KYC (Know Your Customer) tiers control how much information has been collected and verified for a customer. Tiers map to the Central Bank of Nigeria's tiered KYC framework.

| Tier | Level | Typical limits |
|---|---|---|
| `1` | Basic — no ID required | Low transaction and balance limits |
| `2` | Intermediate — BVN verified | Medium limits |
| `3` | Full KYC — NIN + address verified | Full limits |

## Setting a Tier

Pass `kyc_tier` when creating a customer:

```json
{
  "kyc_tier": 2
}
```

Valid values: `1`, `2`, `3`. Defaults to `1`.

## Upgrading a Tier

Customer tiers are immutable after creation in the current API version. To upgrade a customer's KYC tier, create a new customer record with the higher tier and migrate their accounts.

> Future versions will support tier upgrades in-place via `PUT /customers/{id}`.
