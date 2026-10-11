# POS Enterprise Platform — Audit Action Catalog v1

**Status:** Architecture Draft / Proposed Lock  
**Version:** v1.0  
**Scope:** Business audit, data-change audit, security audit, authentication history, privileged actions, integration audit, offline synchronization audit, audit access and retention.

---

## 1. Purpose

Audit Action Catalog v1 defines the canonical audit actions that must be recorded by the POS Enterprise Platform.

Audit exists to provide:

- accountability;
- security investigation;
- business traceability;
- regulatory/compliance evidence;
- fraud investigation;
- operational troubleshooting;
- reconciliation evidence;
- before/after data history for sensitive changes;
- traceability across POS, API, payment, inventory, ERP and offline sync.

Audit is a **platform capability** and MUST NOT be treated as an optional logging feature.

---

# 2. Audit Architecture

```text
Command / API / Worker / Integration
              |
              v
        Authorization
              |
              v
        Domain Command
              |
       +------+------+
       |             |
       v             v
 Business State    Audit Record
 Change             |
       |             |
       +------+------+
              |
              v
        Transaction COMMIT
```

For critical business actions, the audit record MUST be persisted in the same transaction as the business state change.

Technical/security events that occur outside a business transaction may be persisted independently, but must still carry correlation and actor context.

---

# 3. Audit vs Application Log vs Event

| Concern | Audit | Application Log | Domain Event |
|---|---|---|---|
| Accountability | Primary | Secondary | Partial |
| Before/after | Yes, where required | No | Usually no |
| Business history | Yes | No | Yes |
| Downstream processing | No | No | Yes |
| Security investigation | Yes | Yes | Sometimes |
| Immutable | Yes | Usually retention-based | Yes |
| Replay | No | No | Yes |
| User-visible history | Yes | No | Sometimes |

Do not substitute ordinary application logs for audit records.

---

# 4. Canonical Audit Envelope

Recommended logical structure:

```json
{
  "audit_id": "aud_01J...",
  "tenant_id": "uuid",
  "company_id": "uuid",
  "region_id": "uuid",
  "store_id": "uuid",
  "terminal_id": "uuid",
  "user_id": "uuid",
  "actor_type": "USER",
  "action_code": "SALE_COMPLETED",
  "action_category": "BUSINESS",
  "severity": "INFO",
  "result": "SUCCESS",
  "entity_type": "sales_order",
  "entity_id": "uuid",
  "entity_number": "SO-202610-000001",
  "request_id": "req_123",
  "correlation_id": "corr_123",
  "trace_id": "trace_123",
  "source": "pos-api",
  "ip_address": "masked-or-policy-controlled",
  "user_agent": "...",
  "reason_code": null,
  "reason": null,
  "before_data": null,
  "after_data": {},
  "metadata": {},
  "occurred_at": "2026-10-06T09:00:00Z"
}
```

---

# 5. Mandatory Audit Fields

| Field | Required | Rule |
|---|---:|---|
| audit_id | Yes | Unique immutable identifier |
| tenant_id | Yes where tenant-scoped | Never inferred from client payload alone |
| actor_type | Yes | USER / SERVICE / SYSTEM |
| actor_id | Conditional | Required for identified actor |
| action_code | Yes | Must exist in catalog |
| category | Yes | BUSINESS / SECURITY / DATA / INTEGRATION / PLATFORM |
| severity | Yes | INFO / NOTICE / WARNING / HIGH / CRITICAL |
| result | Yes | SUCCESS / FAILURE / DENIED |
| entity_type | Conditional | Target entity |
| entity_id | Conditional | Target identifier |
| request_id | Yes for request-driven action | Correlation to API request |
| correlation_id | Yes where workflow exists | Cross-component trace |
| trace_id | Recommended | Distributed tracing |
| source | Yes | API / POS / WORKER / ERP / SYSTEM etc. |
| occurred_at | Yes | Server authoritative time |
| reason_code | Conditional | Required for selected sensitive actions |
| before_data | Conditional | Required for sensitive mutations |
| after_data | Conditional | Required for sensitive mutations |
| metadata | Yes | Safe contextual metadata |

---

# 6. Actor Types

Canonical actor types:

```text
USER
SERVICE
SYSTEM
EXTERNAL_SYSTEM
```

Examples:

- cashier completing sale → `USER`
- ERP integration worker → `SERVICE`
- scheduled promotion expiry → `SYSTEM`
- SAP callback → `EXTERNAL_SYSTEM`

Never record a service account as a human user merely because it executed the request.

---

# 7. Audit Categories

| Category | Purpose |
|---|---|
| BUSINESS | Business transaction/action history |
| DATA | Sensitive master/configuration changes |
| SECURITY | Authentication, authorization, privileged security actions |
| INTEGRATION | External system interaction |
| PLATFORM | Device, terminal, configuration, operational lifecycle |
| SYNC | Offline synchronization/reconciliation |
| AUDIT | Audit access, export, replay, retention operations |

---

# 8. Severity

### INFO
Normal successful business activity.

### NOTICE
Important but expected administrative/business activity.

### WARNING
Potentially risky or unusual activity.

### HIGH
High-impact privileged or financial action.

### CRITICAL
Security compromise, major financial risk, tenant isolation violation attempt, or emergency access.

Severity does not replace permission or workflow requirements.

---

# 9. Result Values

```text
SUCCESS
FAILURE
DENIED
```

A denied action MUST be auditable when the action is security-sensitive, privileged, or otherwise defined by the catalog.

---

# 10. Action Naming Convention

Use uppercase stable codes:

```text
SALE_CREATED
SALE_CONFIRMED
SALE_COMPLETED
PAYMENT_COMPLETED
STOCK_ADJUSTMENT_APPROVED
USER_ROLE_CHANGED
```

Action codes are contract identifiers and MUST NOT be casually renamed.

---

# 11. Identity & Access Audit Catalog

| Action | Severity | Result Audit | Before/After | Reason |
|---|---|---|---|---|
| USER_CREATED | NOTICE | Yes | After | No |
| USER_UPDATED | NOTICE | Yes | Before + After | Conditional |
| USER_DISABLED | HIGH | Yes | Before + After | Yes |
| USER_ENABLED | NOTICE | Yes | Before + After | Yes |
| USER_DELETED_REQUESTED | HIGH | Yes | Before | Yes |
| USER_ROLE_ASSIGNED | HIGH | Yes | Before + After | Yes |
| USER_ROLE_REVOKED | HIGH | Yes | Before + After | Yes |
| USER_SCOPE_CHANGED | HIGH | Yes | Before + After | Yes |
| PERMISSION_DENIED | WARNING/HIGH | Yes | No | Recommended |
| LOGIN_SUCCESS | INFO | Yes | No | No |
| LOGIN_FAILURE | WARNING | Yes | No | No |
| MFA_SUCCESS | INFO | Yes | No | No |
| MFA_FAILURE | HIGH | Yes | No | No |
| SESSION_CREATED | INFO | Yes | No | No |
| SESSION_REVOKED | NOTICE/HIGH | Yes | No | Yes |
| API_KEY_CREATED | HIGH | Yes | After metadata only | Yes |
| API_KEY_REVOKED | HIGH | Yes | Before metadata | Yes |
| BREAK_GLASS_GRANTED | CRITICAL | Yes | After | Mandatory |
| BREAK_GLASS_EXPIRED | HIGH | Yes | After | No |

Never audit passwords, password hashes, MFA secrets, API secrets, refresh tokens or raw access tokens.

---

# 12. Organization Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| COMPANY_CREATED | NOTICE | After |
| COMPANY_UPDATED | NOTICE | Before + After |
| REGION_CREATED | NOTICE | After |
| REGION_UPDATED | NOTICE | Before + After |
| STORE_CREATED | NOTICE | After |
| STORE_UPDATED | NOTICE | Before + After |
| STORE_ENABLED | NOTICE | Before + After |
| STORE_DISABLED | HIGH | Before + After |
| WAREHOUSE_CREATED | NOTICE | After |
| WAREHOUSE_UPDATED | NOTICE | Before + After |
| TERMINAL_REGISTERED | NOTICE | After |
| TERMINAL_ENABLED | NOTICE | Before + After |
| TERMINAL_DISABLED | HIGH | Before + After |
| TERMINAL_RETIRED | HIGH | Before + After |
| DEVICE_REGISTERED | NOTICE | After |
| DEVICE_SUSPENDED | HIGH | Before + After |
| DEVICE_REVOKED | CRITICAL | Before + After |

---

# 13. Product / Master Data Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| PRODUCT_CREATED | NOTICE | After |
| PRODUCT_UPDATED | NOTICE | Before + After |
| PRODUCT_ACTIVATED | NOTICE | Before + After |
| PRODUCT_DEACTIVATED | HIGH | Before + After |
| PRODUCT_VARIANT_CREATED | NOTICE | After |
| PRODUCT_VARIANT_UPDATED | NOTICE | Before + After |
| BARCODE_CREATED | NOTICE | After |
| BARCODE_CHANGED | HIGH | Before + After |
| SERIAL_REGISTERED | NOTICE | After |
| SERIAL_STATUS_CHANGED | HIGH | Before + After |
| CATEGORY_CREATED | NOTICE | After |
| CATEGORY_UPDATED | NOTICE | Before + After |
| UOM_CREATED | NOTICE | After |
| UOM_UPDATED | NOTICE | Before + After |

Master data changes that can alter transaction valuation or inventory MUST have stronger authorization and audit coverage.

---

# 14. Customer / CRM Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| CUSTOMER_CREATED | NOTICE | After, masked PII |
| CUSTOMER_UPDATED | NOTICE | Before + After, masked PII |
| CUSTOMER_DISABLED | HIGH | Before + After |
| CUSTOMER_ADDRESS_CREATED | NOTICE | After, masked |
| CUSTOMER_ADDRESS_UPDATED | NOTICE | Before + After, masked |
| CUSTOMER_ADDRESS_DELETED | HIGH | Before, masked |
| CUSTOMER_GROUP_CHANGED | NOTICE | Before + After |

PII fields must be masked according to data classification.

---

# 15. Pricing Audit Catalog

| Action | Severity | Before/After | Approval |
|---|---|---|---|
| PRICE_CREATED | NOTICE | After | Policy-dependent |
| PRICE_UPDATED | HIGH | Before + After | Yes where configured |
| PRICE_SUBMITTED | NOTICE | After | Workflow |
| PRICE_APPROVED | HIGH | Before + After | Yes |
| PRICE_REJECTED | NOTICE | Before + After | Yes |
| PRICE_ACTIVATED | HIGH | Before + After | Yes |
| PRICE_DEACTIVATED | HIGH | Before + After | Yes |
| PRICE_EXPIRED | INFO | Before + After | System |

---

# 16. Promotion Audit Catalog

| Action | Severity | Before/After | SoD |
|---|---|---|---|
| PROMOTION_CREATED | NOTICE | After | Creator recorded |
| PROMOTION_UPDATED | HIGH | Before + After | Policy |
| PROMOTION_SUBMITTED | NOTICE | After | Creator |
| PROMOTION_APPROVED | HIGH | Before + After | Creator != Approver |
| PROMOTION_REJECTED | NOTICE | Before + After | Approver |
| PROMOTION_ACTIVATED | HIGH | Before + After | Policy |
| PROMOTION_PAUSED | HIGH | Before + After | Policy |
| PROMOTION_EXPIRED | INFO | Before + After | System |
| PROMOTION_VERSION_CREATED | NOTICE | After | Versioned |
| PROMOTION_RULE_CHANGED | HIGH | Before + After | Approval |
| PROMOTION_APPLIED | INFO | Snapshot | Transaction |

Promotion approval must record approver identity and approval workflow reference.

---

# 17. Sales Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| SALE_CREATED | INFO | Snapshot/after |
| SALE_CONFIRMED | NOTICE | After |
| SALE_PROCESSING | INFO | After |
| SALE_COMPLETED | NOTICE | Transaction snapshot |
| SALE_CANCELLED | HIGH | Before + After |
| SALE_VOIDED | CRITICAL/HIGH | Before + After |
| SALE_ITEM_ADDED | INFO | Before + After |
| SALE_ITEM_REMOVED | NOTICE | Before + After |
| SALE_DISCOUNT_APPLIED | NOTICE | Snapshot |
| SALE_MANUAL_PRICE_OVERRIDE | HIGH | Before + After |
| SALE_CUSTOMER_CHANGED | NOTICE | Before + After |
| SALE_TAX_CHANGED | HIGH | Before + After |
| SALE_TOTAL_OVERRIDE_ATTEMPTED | CRITICAL | Before/attempt |

`SALE_VOIDED` requires elevated permission and must reference the original sale and reason.

---

# 18. POS Session / Cash Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| POS_SESSION_OPENED | NOTICE | After |
| POS_CASH_MOVEMENT_CREATED | NOTICE | After |
| POS_CASH_IN | NOTICE | After |
| POS_CASH_OUT | NOTICE | After |
| POS_SESSION_CLOSING | NOTICE | After |
| POS_SESSION_VARIANCE_DETECTED | HIGH | Before + After |
| POS_SESSION_VARIANCE_APPROVED | HIGH | Before + After |
| POS_SESSION_CLOSED | HIGH | Before + After |
| POS_SESSION_FORCE_CLOSED | CRITICAL | Before + After + reason |

Cash variance approval should enforce SoD where business policy requires it.

---

# 19. Payment Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| PAYMENT_CREATED | NOTICE | After |
| PAYMENT_PROCESSING | INFO | After |
| PAYMENT_COMPLETED | NOTICE | After |
| PAYMENT_FAILED | WARNING | After |
| PAYMENT_CANCELLED | NOTICE | Before + After |
| PAYMENT_EXPIRED | NOTICE | Before + After |
| PAYMENT_UNKNOWN | CRITICAL/HIGH | Before + After |
| PAYMENT_RECONCILIATION_STARTED | HIGH | After |
| PAYMENT_RECONCILIATION_RESOLVED | HIGH | Before + After |
| REFUND_REQUESTED | HIGH | After |
| REFUND_APPROVED | HIGH | Before + After |
| REFUND_PROCESSING | HIGH | After |
| REFUND_COMPLETED | HIGH | After |
| REFUND_FAILED | HIGH | After |
| REFUND_CANCELLED | HIGH | Before + After |

Never log full card number, CVV, provider secret or authorization credentials.

---

# 20. Inventory Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| STOCK_RESERVED | INFO | After |
| STOCK_RELEASED | INFO | After |
| STOCK_DEDUCTED | NOTICE | After |
| STOCK_ADJUSTMENT_CREATED | HIGH | After |
| STOCK_ADJUSTMENT_SUBMITTED | NOTICE | After |
| STOCK_ADJUSTMENT_APPROVED | HIGH | Before + After |
| STOCK_ADJUSTMENT_REJECTED | NOTICE | Before + After |
| STOCK_ADJUSTED | HIGH | Before + After |
| STOCK_TRANSFER_CREATED | NOTICE | After |
| STOCK_TRANSFER_APPROVED | HIGH | Before + After |
| STOCK_TRANSFER_SENT | NOTICE | After |
| STOCK_TRANSFER_RECEIVED | HIGH | Before + After |
| STOCK_OPNAME_STARTED | NOTICE | After |
| STOCK_OPNAME_POSTED | HIGH | Before + After |
| STOCK_NEGATIVE_ATTEMPT | CRITICAL/HIGH | Attempt |

Stock ledger records are immutable. Corrections must create compensating movements, not edit historical ledger rows.

---

# 21. Purchasing Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| PURCHASE_ORDER_CREATED | INFO | After |
| PURCHASE_ORDER_UPDATED | NOTICE | Before + After |
| PURCHASE_ORDER_SUBMITTED | NOTICE | After |
| PURCHASE_ORDER_APPROVED | HIGH | Before + After |
| PURCHASE_ORDER_REJECTED | NOTICE | Before + After |
| PURCHASE_ORDER_CANCELLED | HIGH | Before + After |
| GOODS_RECEIPT_CREATED | INFO | After |
| GOODS_RECEIPT_POSTED | HIGH | Before + After |
| GOODS_RECEIPT_CANCELLED | HIGH | Before + After |

---

# 22. Loyalty / Voucher Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| LOYALTY_ACCOUNT_CREATED | NOTICE | After |
| LOYALTY_EARNED | NOTICE | After |
| LOYALTY_REDEEMED | HIGH | After |
| LOYALTY_REVERSED | HIGH | Before + After |
| VOUCHER_ISSUED | NOTICE | After |
| VOUCHER_RESERVED | NOTICE | After |
| VOUCHER_REDEEMED | HIGH | Before + After |
| VOUCHER_RELEASED | NOTICE | Before + After |
| VOUCHER_CANCELLED | HIGH | Before + After |
| VOUCHER_EXPIRED | INFO | Before + After |

Voucher and loyalty balance corrections must use compensating ledger actions.

---

# 23. Workflow / Approval Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| WORKFLOW_STARTED | INFO | After |
| APPROVAL_REQUESTED | NOTICE | After |
| APPROVAL_APPROVED | HIGH | Before + After |
| APPROVAL_REJECTED | NOTICE | Before + After |
| APPROVAL_CANCELLED | NOTICE | Before + After |
| APPROVAL_EXPIRED | NOTICE | Before + After |
| WORKFLOW_COMPLETED | INFO | After |
| WORKFLOW_FORCE_COMPLETED | CRITICAL | Before + After + reason |

Approval actions must record workflow instance, approval step, actor, decision, and reason where required.

---

# 24. Integration / ERP Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| INTEGRATION_REQUESTED | INFO | Metadata |
| INTEGRATION_SENT | INFO | Metadata |
| INTEGRATION_SUCCEEDED | NOTICE | Result metadata |
| INTEGRATION_FAILED_RETRYABLE | WARNING | Error metadata |
| INTEGRATION_FAILED_PERMANENT | HIGH | Error metadata |
| INTEGRATION_DEAD_LETTERED | HIGH | Error metadata |
| ERP_DOCUMENT_CREATED | NOTICE | External reference |
| ERP_DOCUMENT_LINK_FAILED | HIGH | Error metadata |
| ERP_RECONCILIATION_REQUIRED | HIGH | Metadata |
| ERP_RECONCILIATION_RESOLVED | HIGH | Before + After |
| EXTERNAL_REFERENCE_LINKED | INFO | After |

Never store raw authentication secrets in integration audit metadata.

---

# 25. Offline Sync Audit Catalog

| Action | Severity | Before/After |
|---|---|---|
| OFFLINE_TRANSACTION_QUEUED | INFO | Metadata |
| OFFLINE_TRANSACTION_SYNC_STARTED | INFO | Metadata |
| OFFLINE_TRANSACTION_SYNCED | NOTICE | Result |
| OFFLINE_TRANSACTION_RETRYABLE_FAILURE | WARNING | Error metadata |
| OFFLINE_TRANSACTION_CONFLICT | HIGH | Before/After where applicable |
| OFFLINE_TRANSACTION_RECONCILIATION_REQUIRED | HIGH | Metadata |
| OFFLINE_TRANSACTION_RESOLVED | HIGH | Before + After |
| OFFLINE_POLICY_EXPIRED | WARNING | Policy metadata |
| DEVICE_CLOCK_DRIFT_DETECTED | WARNING | Metadata |
| DEVICE_REVOKED_DURING_SYNC | CRITICAL | Metadata |
| IDEMPOTENCY_KEY_REUSED | HIGH | Request metadata |

Client payload must not automatically be treated as authoritative audit truth. Server validation and final server result are authoritative.

---

# 26. Security Audit Catalog

Required actions include:

```text
LOGIN_SUCCESS
LOGIN_FAILURE
MFA_SUCCESS
MFA_FAILURE
SESSION_CREATED
SESSION_REVOKED
PERMISSION_DENIED
RATE_LIMIT_TRIGGERED
SUSPICIOUS_AUTH_ACTIVITY
USER_DISABLED
ROLE_CHANGED
SCOPE_CHANGED
API_KEY_CREATED
API_KEY_REVOKED
DEVICE_REVOKED
BREAK_GLASS_GRANTED
BREAK_GLASS_EXPIRED
TENANT_ACCESS_DENIED
CROSS_SCOPE_ACCESS_DENIED
RLS_CONTEXT_MISSING
```

`RLS_CONTEXT_MISSING` and cross-tenant/cross-scope violations should be treated as high-severity security telemetry and investigated according to operational policy.

---

# 27. Audit Access Audit

Reading/exporting audit data is itself sensitive and must be audited.

| Action | Severity |
|---|---|
| AUDIT_SEARCHED | INFO/NOTICE |
| AUDIT_RECORD_VIEWED | NOTICE |
| AUDIT_EXPORTED | HIGH |
| AUDIT_EXPORT_FAILED | WARNING |
| AUDIT_REPLAY_REQUESTED | HIGH |
| AUDIT_RETENTION_EXECUTED | HIGH |
| AUDIT_ACCESS_DENIED | HIGH |

Audit export MUST record:

- actor;
- scope;
- filters;
- time range;
- number of records;
- export format;
- destination/reference where applicable;
- reason where policy requires.

Never allow an auditor to silently export the entire tenant dataset without explicit permission/policy.

---

# 28. High-Risk Action Catalog

The following actions require elevated permission and stronger audit:

```text
SALE_VOIDED
SALE_MANUAL_PRICE_OVERRIDE
PAYMENT_UNKNOWN
REFUND_REQUESTED
REFUND_APPROVED
REFUND_COMPLETED
STOCK_ADJUSTMENT_APPROVED
STOCK_ADJUSTED
STOCK_OPNAME_POSTED
PROMOTION_APPROVED
PROMOTION_ACTIVATED
PRICE_APPROVED
PRICE_ACTIVATED
USER_ROLE_ASSIGNED
USER_SCOPE_CHANGED
API_KEY_CREATED
DEVICE_REVOKED
BREAK_GLASS_GRANTED
AUDIT_EXPORTED
ERP_RECONCILIATION_RESOLVED
OFFLINE_TRANSACTION_RESOLVED
```

---

# 29. Critical Actions

Critical actions require:

1. explicit permission;
2. correct scope;
3. valid state transition;
4. workflow/approval where defined;
5. mandatory audit;
6. actor identity;
7. reason code/reason;
8. request/correlation ID;
9. before/after where applicable;
10. immutable persistence;
11. event/outbox linkage where applicable.

Examples:

```text
SALE_VOIDED
REFUND_APPROVED
PAYMENT_RECONCILIATION_RESOLVED
STOCK_ADJUSTED
PROMOTION_APPROVED
USER_ROLE_ASSIGNED
BREAK_GLASS_GRANTED
DEVICE_REVOKED
```

---

# 30. Before / After Data Policy

Do not indiscriminately store full database rows for every action.

### Before + after required for:

- permission changes;
- role assignment;
- user scope changes;
- price changes;
- promotion rule changes;
- inventory adjustments;
- stock transfers;
- approval decisions;
- refund decisions;
- terminal/device status changes;
- configuration affecting financial behavior.

### Snapshot/after sufficient for:

- successful creation;
- immutable transaction posting where the resulting transaction snapshot already exists;
- event-style operational records.

### Avoid before/after for:

- passwords;
- tokens;
- secrets;
- encryption keys;
- raw payment credentials.

---

# 31. PII Masking

Audit records must follow data classification.

Examples:

```text
Email:     j***@example.com
Phone:     ******1234
Card:      never store full PAN
Address:   masked according to policy
```

The exact masking policy should be configurable by data classification, but security secrets are never recoverable from audit.

---

# 32. Audit Immutability

Audit records MUST be append-only.

Application users MUST NOT be able to:

- UPDATE audit records;
- DELETE audit records;
- change actor;
- change timestamp;
- change action code;
- change before/after evidence.

Database enforcement should include a trigger/guard that rejects UPDATE/DELETE from application roles.

Privileged retention/archival operations must use a controlled operational process and produce their own audit evidence.

---

# 33. Audit Tamper Evidence

For higher maturity, the platform SHOULD support tamper-evident chaining.

Concept:

```text
hash_n = SHA256(
    canonical(audit_n) + hash_(n-1)
)
```

This is an enhancement and does not replace database access controls, backups, and immutable storage.

For very high-risk environments, audit archives SHOULD be exported to immutable/WORM-capable storage.

---

# 34. Audit Retention

Retention must be configurable by action category and legal/business requirement.

Suggested baseline:

| Category | Suggested Minimum |
|---|---:|
| Security | 1–3 years |
| Financial/business transaction | 5–10 years or applicable policy |
| Integration | 1–3 years |
| Operational/platform | 1–2 years |
| Debug/technical logs | Shorter, separate from audit |

These are architecture defaults, not legal advice. Local regulatory, tax, finance, privacy and contractual requirements override the baseline.

---

# 35. Audit Partitioning

For enterprise scale, `audit.audit_log` SHOULD be partitioned by time, typically monthly or quarterly depending on volume.

Partitioning must not break:

- immutability;
- retention policy;
- tenant filtering;
- audit search;
- backup/recovery.

A future partition strategy should be validated using actual production-like volume.

---

# 36. Tenant and Scope Security

Audit records are tenant-scoped.

Authorization sequence:

```text
Identity
  ↓
Permission
  ↓
Tenant
  ↓
Company / Region / Store / Warehouse / Terminal Scope
  ↓
Audit Access Policy
  ↓
RLS
```

A user with `audit.read` for Store A MUST NOT see Store B records.

A user with global audit access must have an explicit scope/permission rather than inheriting access accidentally from a broad role.

---

# 37. Audit Export Security

Export is more sensitive than normal read access.

Recommended permissions:

```text
audit.read
audit.export
audit.export.sensitive
audit.retention.manage
```

An export should have:

- maximum time range;
- maximum row limit;
- mandatory filters for non-global roles;
- PII masking;
- rate limit;
- asynchronous generation for large exports;
- immutable export reference;
- export audit record.

---

# 38. Relationship with Permission Matrix

Audit does not grant permission.

Example:

```text
User has:
  sales.void

User scope:
  Store A

Target sale:
  Store B

Result:
  DENIED

Audit:
  SALE_VOID_DENIED
```

Sensitive denied attempts SHOULD be cataloged separately from successful actions.

Recommended denial actions:

```text
SALE_VOID_DENIED
REFUND_APPROVAL_DENIED
STOCK_ADJUSTMENT_DENIED
PROMOTION_APPROVAL_DENIED
AUDIT_ACCESS_DENIED
CROSS_SCOPE_ACCESS_DENIED
```

---

# 39. Relationship with State Machine

Audit must reference the state transition when a state change occurs.

Example:

```json
{
  "action_code": "SALE_VOIDED",
  "entity_type": "sales_order",
  "entity_id": "...",
  "before_data": {
    "state": "COMPLETED"
  },
  "after_data": {
    "state": "VOIDED"
  },
  "metadata": {
    "command": "VoidOrder",
    "reason_code": "CUSTOMER_REQUEST",
    "workflow_instance_id": "..."
  }
}
```

Direct status mutation without a corresponding command/audit path is prohibited.

---

# 40. Relationship with Event Catalog

A critical business action can create both:

```text
Audit Action
      +
Domain Event
```

Example:

```text
SALE_COMPLETED
  ├── audit: SALE_COMPLETED
  └── event: SalesOrderCompleted
```

They are not duplicates:

- audit answers **who/what/when/before/after/why**;
- event answers **what happened for downstream processing**.

Both should be transactionally consistent for critical state changes.

---

# 41. API Audit Mapping

Every command endpoint must resolve to an action code.

Examples:

| API Command | Audit |
|---|---|
| POST /sales/orders | SALE_CREATED |
| POST /sales/orders/{id}/confirm | SALE_CONFIRMED |
| POST /sales/orders/{id}/complete | SALE_COMPLETED |
| POST /sales/orders/{id}/void | SALE_VOIDED |
| POST /payments/{id}/capture | PAYMENT_COMPLETED |
| POST /refunds | REFUND_REQUESTED |
| POST /refunds/{id}/approve | REFUND_APPROVED |
| POST /inventory/adjustments | STOCK_ADJUSTMENT_CREATED |
| POST /inventory/adjustments/{id}/approve | STOCK_ADJUSTMENT_APPROVED |
| POST /promotions/{id}/approve | PROMOTION_APPROVED |
| POST /users/{id}/roles | USER_ROLE_ASSIGNED |
| POST /pos/sync | OFFLINE_TRANSACTION_SYNCED / conflict actions |

Handlers MUST NOT invent arbitrary action codes.

---

# 42. Worker Audit Mapping

Background workers must also produce audit records where they perform business actions.

Examples:

```text
Promotion expiry worker
  -> PROMOTION_EXPIRED

Payment reconciliation worker
  -> PAYMENT_RECONCILIATION_RESOLVED

ERP retry worker
  -> INTEGRATION_SUCCEEDED / INTEGRATION_FAILED_RETRYABLE

Offline reconciliation worker
  -> OFFLINE_TRANSACTION_RESOLVED
```

Actor type should normally be `SYSTEM` or `SERVICE`, not a human user unless a human explicitly initiated the action.

---

# 43. Integration Audit Mapping

External calls should produce integration evidence without exposing credentials.

Example:

```text
INTEGRATION_SENT
INTEGRATION_SUCCEEDED
INTEGRATION_FAILED_RETRYABLE
```

Metadata may contain:

```text
provider
endpoint_identifier
external_request_id
external_reference
http_status
attempt_number
latency_ms
```

Do not store:

```text
Authorization header
API secret
Card data
Access token
Refresh token
```

---

# 44. Offline Audit Principle

Offline operations create audit evidence only after server validation.

```text
Client local action
      ↓
Local queue
      ↓
Server sync
      ↓
Authorization + validation
      ↓
Business transaction
      ↓
Server audit
```

The client may provide diagnostic metadata such as:

```text
client_transaction_id
installation_id
client_sequence
created_at_device
```

but cannot dictate the authoritative actor, scope, permission or final state.

---

# 45. Break-Glass Access

Emergency privileged access is allowed only through a controlled mechanism.

Required evidence:

```text
BREAK_GLASS_GRANTED
BREAK_GLASS_ACTION_PERFORMED
BREAK_GLASS_EXPIRED
```

Grant MUST include:

- actor;
- requested scope;
- reason;
- approver where required;
- start time;
- expiry time;
- permissions granted;
- ticket/incident reference.

Break-glass access MUST be time-limited and strongly authenticated.

---

# 46. Segregation of Duties Evidence

The audit system must preserve evidence for SoD rules.

Examples:

```text
PROMOTION_CREATED by User A
PROMOTION_APPROVED by User B

STOCK_ADJUSTMENT_CREATED by User A
STOCK_ADJUSTMENT_APPROVED by User B

REFUND_REQUESTED by User A
REFUND_APPROVED by User B
```

A single user performing both steps must be rejected where SoD is mandatory and the rejection should be auditable.

---

# 47. Audit Correlation

A business operation should be traceable across:

```text
request_id
    ↓
command
    ↓
audit_id
    ↓
event_id
    ↓
outbox delivery
    ↓
external request
    ↓
external reference
```

Recommended correlation metadata:

```text
request_id
correlation_id
causation_id
trace_id
client_transaction_id
external_request_id
external_reference
```

---

# 48. Audit Search Requirements

Audit search should support filtering by:

- tenant;
- company;
- region;
- store;
- terminal;
- user;
- actor type;
- action code;
- category;
- severity;
- result;
- entity type;
- entity ID;
- entity number;
- request ID;
- correlation ID;
- trace ID;
- time range;
- IP/device where policy permits.

Search should be indexed according to actual usage patterns.

---

# 49. Audit Data Change Design

The existing `audit.data_change` concept should be used for field-level changes where required.

Recommended logical fields:

```text
change_id
audit_id
entity_type
entity_id
field_name
old_value_masked
new_value_masked
change_type
```

This prevents the main audit record from becoming an unsearchable giant JSON blob.

Sensitive values should be masked or omitted according to classification.

---

# 50. Audit Security Event Design

`audit.security_event` should capture security-specific evidence such as:

```text
LOGIN_FAILURE
MFA_FAILURE
PERMISSION_DENIED
CROSS_SCOPE_ACCESS_DENIED
TENANT_ACCESS_DENIED
RATE_LIMIT_TRIGGERED
SUSPICIOUS_AUTH_ACTIVITY
DEVICE_REVOKED
BREAK_GLASS_GRANTED
```

Security events may contain security-specific metadata not appropriate for general business audit.

---

# 51. Audit Login History

`audit.login_history` should retain authentication evidence separately where operational/security requirements justify it.

Recommended fields:

```text
user_id
login_at
result
method
ip_address
user_agent
device_id
failure_reason_code
request_id
```

Do not store credentials.

---

# 52. Failure Handling

If a critical business action succeeds but audit persistence fails, the transaction MUST fail/rollback rather than silently commit without mandatory audit evidence.

For security events where business state is not being changed, failure handling may differ, but high-value security evidence should have a durable fallback/alerting mechanism.

Audit infrastructure failure must be observable.

---

# 53. Audit and Outbox Transaction Boundary

For a critical command:

```text
BEGIN
  SET LOCAL tenant/user/store context

  authorize
  validate state
  mutate domain state
  insert audit record
  insert data-change records
  insert outbox event
  persist idempotency result
COMMIT
```

If any mandatory step fails:

```text
ROLLBACK
```

No partial business transaction should remain.

---

# 54. Prohibited Audit Patterns

Never:

- audit only successful actions while ignoring critical denials;
- allow application users to update audit rows;
- log secrets as audit metadata;
- use application logs as the only audit source;
- let client payload define actor identity;
- let client payload define tenant authorization;
- silently discard audit failure;
- use mutable current master data as historical audit evidence;
- overwrite previous before/after values;
- delete transaction audit because a transaction was voided;
- expose unrestricted audit export to normal users.

---

# 55. Minimum Audit Coverage by Domain

| Domain | Minimum Coverage |
|---|---|
| Identity | User, role, scope, login, MFA, session |
| Organization | Company, store, warehouse, terminal, device |
| Master | Product, variant, barcode, serial |
| CRM | Customer and sensitive profile changes |
| Pricing | Create/change/approve/activate |
| Promotion | Create/change/approve/activate/pause |
| Sales | Create/confirm/complete/cancel/void/override |
| POS | Session/cash/variance |
| Payment | Lifecycle/refund/reconciliation |
| Inventory | Reservation/adjustment/transfer/opname |
| Purchasing | PO/approval/receipt |
| Loyalty | Earn/redeem/reverse |
| Voucher | Issue/reserve/redeem/release/cancel |
| Workflow | Request/approve/reject/expire |
| Integration | Request/result/retry/DLQ/reconciliation |
| Offline | Sync/conflict/reconciliation/device policy |
| Audit | Search/view/export/replay/retention |

---

# 56. Initial Permission Pairing

Audit permissions should be granular:

```text
audit.read
audit.read.security
audit.read.financial
audit.export
audit.export.sensitive
audit.replay
integration.event.read
integration.event.retry
integration.event.replay
audit.retention.manage
```

A user having `audit.read` does not automatically receive `audit.export` or `audit.read.security`.

---

# 57. Event / Audit / State Example

Successful sale:

```text
Command: CompleteOrder

State:
PROCESSING → COMPLETED

Audit:
SALE_COMPLETED

Event:
SalesOrderCompleted v1

Outbox:
PENDING → DELIVERED

Consumers:
Inventory
Finance
Loyalty
ERP
Reporting
```

Void:

```text
Command: VoidOrder

Authorization:
sales.void

Approval:
Policy-dependent

State:
COMPLETED → VOIDED

Audit:
SALE_VOIDED

Event:
SalesOrderVoided

Compensation:
Inventory / Finance / Loyalty / ERP
```

---

# 58. Acceptance Criteria

Audit Action Catalog v1 is accepted when:

- every critical business command maps to a canonical action code;
- identity/security activity is covered;
- privileged actions are explicitly cataloged;
- state transitions have audit mapping;
- before/after policy is defined;
- PII masking rules are defined;
- secrets are prohibited from audit payloads;
- audit records are immutable;
- audit access itself is audited;
- tenant and scope authorization is enforced;
- SoD evidence is preserved;
- offline reconciliation is auditable;
- ERP/payment integration actions are traceable;
- audit and outbox transaction boundaries are defined;
- retention/partitioning is defined as policy-driven;
- failure of mandatory audit prevents unsafe commit;
- audit search/export is permissioned;
- replay/retention operations are audited.

---

# 59. Architecture Lock Candidates

The following should be treated as locked unless changed by ADR:

1. Audit is a first-class platform capability.
2. Critical business mutations and their mandatory audit records are transactionally consistent.
3. Audit records are append-only and immutable.
4. Audit action codes are catalog-controlled.
5. State transitions map to explicit audit actions.
6. Critical denied/unauthorized attempts are auditable.
7. Audit access/export is itself audited.
8. Secrets and payment credentials are never stored in audit.
9. PII is masked according to data classification.
10. Actor identity comes from authenticated server context, not client payload.
11. Tenant/scope authorization applies to audit access.
12. Before/after evidence is mandatory for defined sensitive mutations.
13. SoD actions preserve creator/approver evidence.
14. Offline audit is authoritative only after server validation/commit.
15. External integration references are retained without credentials.
16. Audit retention is policy-driven and must not be implemented as arbitrary application DELETE.
17. High-risk replay/export/retention operations require elevated permissions and audit evidence.
18. Audit and domain events remain separate concerns.

---

# 60. Next Architecture Step

After Audit Action Catalog v1:

```text
Audit Action Catalog v1
        ↓
OFFLINE DATABASE DESIGN v1
        ↓
OPENAPI v1.1
        ↓
MONOREPO / REPOSITORY BLUEPRINT
        ↓
CI/CD ARCHITECTURE
        ↓
GO BACKEND FOUNDATION
```

Offline Database Design must incorporate the finalized concepts for:

- device identity;
- offline transaction queue;
- sync attempts;
- conflict/reconciliation;
- idempotency;
- event/outbox linkage;
- audit linkage;
- offline policy/quota;
- clock drift;
- terminal/device state;
- local-to-server transaction mapping.

---

# 61. Final Principle

Audit is not a text log.

It is an immutable business/security evidence layer:

```text
WHO
  +
DID WHAT
  +
TO WHICH OBJECT
  +
WHEN
  +
FROM WHERE / WHICH DEVICE
  +
UNDER WHICH SCOPE
  +
WITH WHICH PERMISSION
  +
WHY
  +
BEFORE
  +
AFTER
  +
WHAT DOWNSTREAM EVENT FOLLOWED
```

For critical transactions:

```text
Authorize
   ↓
Validate State
   ↓
Domain Mutation
   +
Audit
   +
Outbox Event
   +
Idempotency Result
   ↓
COMMIT
```

This design makes the platform operationally traceable, security-auditable, ERP-reconcilable, and suitable for future enterprise/ERP maturity.
