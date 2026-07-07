# Transfers

Ancra lets you send money from a customer's virtual account to any Nigerian bank account. The transfer is debited from the customer's ledger before being submitted to Nomba, so the balance stays consistent even if the network call fails.

## How it works

```
API Client
    │
    ▼
POST /accounts/{id}/transfers
    │
    ├─ 1. Validate request fields
    ├─ 2. Debit customer ledger (atomic — rejects if insufficient funds)
    ├─ 3. POST to Nomba /v2/transfers/bank
    │       │
    │       ├─ Success → return Nomba transaction details
    │       └─ Failure → reverse ledger debit (compensating credit)
    │
    └─ Return result to caller
```

## Amounts

All amounts in the Ancra API are in **kobo** (integer). Ancra converts to naira internally before forwarding to Nomba.

| Value | Meaning |
|---|---|
| `500000` | ₦5,000 |
| `100` | ₦1 |

## Transfer Reference

You must supply a unique `reference` per transfer. This is sent to Nomba as `merchantTxRef` and is used for deduplication. If you reuse a reference, the second request will be rejected.

## Failure Behaviour

If Nomba rejects the transfer after the ledger debit, Ancra automatically posts a `transfer_reversal` credit entry to restore the balance. The customer is never left short. If the reversal itself fails (extremely rare), the discrepancy is caught by the next reconciliation sweep.

## Steps

1. [Look up the recipient's account name](bank-lookup.md) — always verify before sending
2. [Initiate the transfer](initiate.md)
