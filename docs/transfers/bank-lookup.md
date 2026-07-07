# Bank Account Lookup

Resolve an account number and bank code to the registered account name before initiating a transfer. Always verify the recipient name with your user — an unexpected name is the most reliable signal of a wrong account number.

```http
POST /transfers/lookup
Authorization: Bearer <key>
```

## Request

```json
{
  "account_number": "0123456789",
  "bank_code": "044"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `account_number` | string | Yes | 10-digit NUBAN account number |
| `bank_code` | string | Yes | 3-digit CBN bank code (see [bank list](#bank-codes)) |

## Response

```json
{
  "account_number": "0123456789",
  "account_name": "Jane Doe"
}
```

## Errors

| Status | Meaning |
|---|---|
| `400` | Missing or malformed fields |
| `502` | Nomba could not resolve the account (wrong number or bank code) |

## Bank Codes

Common bank codes:

| Bank | Code |
|---|---|
| Access Bank | `044` |
| GTBank | `058` |
| First Bank | `011` |
| Zenith Bank | `057` |
| UBA | `033` |
| Kuda | `090267` |
| OPay | `305` |
| PalmPay | `999991` |
| Moniepoint | `50515` |

For the full list, call `GET /transfers/banks` (returns all banks supported by Nomba).
