# errors: Internal Error Specification (account-service)

This document describes only internal account-service error codes defined in `internal/domain/errors`.

## 1) Standard Error Response Structure

All errors use the unified HTTP response envelope:

```json
{
  "code": 300006,
  "message": "withdrawal is already in progress.",
  "data": {},
  "meta": {
    "requestId": "010a0998-4b1a-4e70-a951-d27a40b49c0c",
    "timestamp": "2025-10-07T12:00:45Z"
  }
}
```

### Response fields

- `code` (`number`) - internal error code (`> 0` indicates error).
- `message` (`string`) - backend message (English).
- `data` (`object`) - extra details. If nothing is provided, backend returns `{}`.
- `meta.requestId` (`string`) - request identifier for tracing/support.
- `meta.timestamp` (`string`, RFC3339) - response timestamp.

## 2) Internal Service Error Codes

Service code base is `300000`, internal codes are `300001+`.

| Code | Constant | HTTP | Message | Where emitted |
|---:|---|---:|---|---|
| 300001 | `AccountIntegrityError` | 403 | `account integrity verification failed.` | `TransactionCtrl.lockOrCreateAndLockAccount` when integrity check fails (`internal/controllers/transaction.go`). |
| 300002 | `WithdrawalRequestExistsError` | 409 | `existing pending withdrawal request.` | Registered in templates, currently not emitted from controllers/handlers. |
| 300003 | `UserNumberNotFoundError` | 404 | `the user with the specified number does not exist.` | `TransferCtrl.Init` when recipient lookup by number fails (`internal/controllers/transfer.go`). |
| 300004 | `SelfPaymentError` | 404 | `self-payment transactions are forbidden.` | `TransferCtrl.Init` when sender and recipient are the same user (`internal/controllers/transfer.go`). |
| 300005 | `FeeMoreThanAmountError` | 400 | `commission is greater or equal than the requested amount.` | `WithdrawalCtrl.Init` when `amount <= fee` (`internal/controllers/withdrawal.go`). |
| 300006 | `WithdrawalAlreadyInProgressError` | 409 | `withdrawal is already in progress.` | `WithdrawalCtrl.Init` when withdrawal lock already exists (`internal/controllers/withdrawal.go`). |
| 300007 | `DepositAlreadyInProgressError` | 409 | `deposit is already in progress.` | `DepositCtrl.Init` when deposit lock already exists (`internal/controllers/deposit.go`). |
| 300008 | `TransferAlreadyInProgressError` | 409 | `transfer is already in progress.` | `TransferCtrl.Init` when transfer lock already exists (`internal/controllers/transfer.go`). |
| 300009 | `BlockchainTxInProgressError` | 409 | `blockchain transaction is already in progress.` | `BlockchainCtrl.ProcessTransaction` when blockchain transaction lock already exists (`internal/controllers/blockchain.go`). |
| 300010 | `DepositRequestInProgressError` | 409 | `deposit request is already in progress.` | `DepositCtrl.Process` when deposit processing lock already exists (`internal/controllers/deposit.go`). |
| 300011 | `WithdrawalRequestInProgressError` | 409 | `withdrawal request is already in progress.` | `WithdrawalCtrl.SendToBlockchain` when withdrawal processing lock already exists (`internal/controllers/withdrawal.go`). |

Note: currently internal codes `300001..300011` are returned without `WithData`, so `data` is expected to be `{}`.

## 3) Extra `data` Payloads Found in Controllers/Handlers

Below are actual `WithData` / `WithValidationData` payloads produced by account-service code paths.
They are not internal `300xxx` codes, but they are generated inside this service and useful for FE integration.

### 3.1 Restriction payload (`WithData`)

Used in:
- `DepositCtrl.Init` (`internal/controllers/deposit.go`)
- `WithdrawalCtrl.Init` (`internal/controllers/withdrawal.go`)
- `TransferCtrl.Init` (`internal/controllers/transfer.go`)

Payload shape:

```json
{
  "operation_type": "deposit|withdrawal|transfer",
  "restriction_decision": "allow|allow_with_2fa|deny_limit_exceeded|deny_account|deny_geo|required_verification",
  "restriction_reason": "optional string"
}
```

`restriction_reason` is optional and appears only when restriction service provides it.

### 3.2 Manual validation payload (`WithValidationData`)

Used in:
- `InitWithdrawalHandler.Handle` (`internal/transport/http/handler/accounts/protected/init_withdrawal.go`)
- `TransferHandler.Handle` (`internal/transport/http/handler/accounts/protected/init_transfer.go`)
- `WithdrawalCtrl.Init` (`internal/controllers/withdrawal.go`)

Payload shape:

```json
{
  "errors": [
    {
      "field": "amount",
      "tag": "gt",
      "params": ["0"]
    }
  ]
}
```

Observed service-specific validation entries:
- `{"field":"amount","tag":"gt","params":["0"]}` (amount must be greater than zero)
- `{"field":"amount","tag":"max_fractional_digits","params":["<decimals>"]}` (fraction precision limit)
- `{"field":"to_address","tag":"format","params":["trx"]}` (invalid network address format)

### 3.3 Auto-validation payload from request binding (`WithValidationData`)

Used in:
- `BindAndValidateJSON` helper (`internal/transport/http/handler/request_helpers.go`)

Payload shape:

```json
{
  "errors": [
    {
      "field": "toUserNumber",
      "tag": "required|oneof|min|max|...",
      "params": []
    }
  ]
}
```

`field/tag/params` are built by `validation.ToValidationData` (`internal/transport/http/validation/validator.go`) from Gin/go-playground validator results.

## 4) Frontend Integration Checklist (Internal Codes)

- [ ] Keep a centralized map for internal codes `300001..300011`.
- [ ] Treat unknown `300xxx` as generic service error fallback.
- [ ] Assume `data = {}` for internal codes unless contract explicitly changes.
- [ ] Log and expose `meta.requestId` for diagnostics/support.
