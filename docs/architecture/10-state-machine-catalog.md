# POS Platform — State Machine Catalog v1

**Status:** Architecture Contract / Implementation Baseline  
**Version:** 1.0  
**Date:** 2026-10-06  
**Architecture:** Modular Monolith, Server Authoritative  
**Related:** Architecture Lock v1, API Contract v1, OpenAPI v1, Database v1.1 Hardened, POS Offline Sync Contract v1

---

# 1. Purpose

Dokumen ini mendefinisikan lifecycle dan state transition untuk aggregate bisnis yang kritis.

State machine digunakan sebagai:

- business invariant
- API command authorization boundary
- database validation reference
- audit contract
- workflow trigger
- frontend UI state contract
- offline synchronization contract
- test-case baseline

Critical rule:

> **State tidak boleh diubah melalui arbitrary UPDATE/PATCH. Setiap transition harus melalui command/use case yang memvalidasi current state, actor, scope, precondition, dan side effect.**

---

# 2. Global Transition Model

Setiap transition memiliki:

```text
Aggregate
Current State
Command
Actor
Permission
Scope
Precondition
Next State
Side Effects
Audit Action
Domain Event
Offline Policy
```

Conceptual flow:

```text
Request
  ↓
Authentication
  ↓
Authorization
  ↓
Scope validation
  ↓
Current state validation
  ↓
Business preconditions
  ↓
Domain transition
  ↓
Persist state + history
  ↓
Audit
  ↓
Outbox
  ↓
Commit
```

---

# 3. State Machine Rules

## 3.1 No arbitrary state mutation

Forbidden:

```http
PATCH /sales/orders/{id}
{
  "status": "COMPLETED"
}
```

Allowed:

```http
POST /sales/orders/{id}/confirm
POST /sales/orders/{id}/cancel
POST /sales/orders/{id}/void
```

## 3.2 Terminal states

Terminal state berarti aggregate tidak dapat berubah melalui normal lifecycle.

Examples:

```text
SALE.COMPLETED
SALE.VOIDED
SALE.CANCELLED
```

Exception hanya melalui explicit reversal/refund workflow jika business model mengizinkannya.

## 3.3 Every transition is auditable

Critical transition must create:

- state history
- audit action
- request ID
- trace ID
- actor
- reason where applicable

## 3.4 State history is append-only

History records are not updated/deleted by normal application operations.

---

# 4. Sales Order State Machine

## 4.1 States

```text
DRAFT
CONFIRMED
PROCESSING
COMPLETED
CANCELLED
VOIDED
```

Optional future:

```text
PARTIALLY_FULFILLED
FULFILLED
RETURN_PENDING
RETURNED
```

These optional states are not required for MVP unless fulfillment/returns are introduced.

---

## 4.2 State Diagram

```text
             ┌──────────────┐
             │    DRAFT     │
             └──────┬───────┘
                    │ confirm
                    ▼
             ┌──────────────┐
             │  CONFIRMED   │
             └──────┬───────┘
                    │ process
                    ▼
             ┌──────────────┐
             │  PROCESSING  │
             └──────┬───────┘
                    │ complete
                    ▼
             ┌──────────────┐
             │  COMPLETED   │
             └──────────────┘

DRAFT ───────────────► CANCELLED
CONFIRMED ───────────► CANCELLED
PROCESSING ──────────► CANCELLED* 
CONFIRMED ───────────► VOIDED*
COMPLETED ───────────► VOIDED* 
```

`*` requires policy/permission/workflow.

---

## 4.3 DRAFT → CONFIRMED

Command:

```text
ConfirmOrder
```

Permission:

```text
sales.confirm
```

Preconditions:

- order exists
- tenant/scope valid
- order belongs to current operational context
- order is DRAFT
- items exist
- products active
- prices valid
- promotion calculation valid
- tax calculation valid
- customer rules satisfied if applicable
- required stock policy satisfied
- payment policy satisfied

Side effects:

- freeze relevant commercial snapshot
- reserve stock if configured at confirmation
- create state history
- audit `SALE_CONFIRMED`
- outbox `SalesOrderConfirmed`

Offline:

```text
Allowed only if offline policy explicitly permits.
```

For controlled offline cash sale, server revalidates at sync.

---

# 5. CONFIRMED → PROCESSING

Command:

```text
ProcessOrder
```

Permission:

```text
sales.process
```

Preconditions:

- order CONFIRMED
- fulfillment/payment prerequisites satisfied
- reservation valid

Side effects:

- mark processing
- audit
- event

Offline:

```text
Not normally required.
```

---

# 6. PROCESSING → COMPLETED

Command:

```text
CompleteOrder
```

Permission:

```text
sales.complete
```

Preconditions:

- order PROCESSING
- required payment state satisfied
- stock deduction/fulfillment completed
- no blocking workflow
- required integration state accepted according to policy

Side effects:

```text
stock deduction
payment finalization
sales completion
audit
outbox
```

Important:

External ERP success is not necessarily a prerequisite unless business policy explicitly requires synchronous confirmation.

Default:

> ERP delivery is asynchronous and does not block POS completion.

---

# 7. DRAFT → CANCELLED

Command:

```text
CancelOrder
```

Permission:

```text
sales.cancel
```

Preconditions:

- state DRAFT
- cancellation reason

Audit:

```text
SALE_CANCELLED
```

Event:

```text
SalesOrderCancelled
```

Offline:

```text
Allowed for local-only unsubmitted cart/order.
```

Once submitted to server, normal cancellation rules apply.

---

# 8. CONFIRMED → CANCELLED

Allowed when:

- fulfillment has not irreversibly completed;
- payment can be reversed/cancelled;
- reservation can be released;
- cancellation policy allows.

May require:

```text
sales.cancel
```

or workflow approval.

Side effects:

```text
release reservation
payment reversal where applicable
audit
outbox
```

---

# 9. PROCESSING → CANCELLED

Restricted.

Requires:

- business policy
- supervisor/manager permission
- potentially workflow

Never allow generic cashier cancellation.

---

# 10. COMPLETED → VOIDED

Command:

```text
VoidOrder
```

Permission:

```text
sales.void
```

Potential approval:

```text
workflow
```

Preconditions:

- order COMPLETED
- void window/policy valid
- user authorized
- reason supplied
- financial consequences handled

Side effects:

```text
reverse inventory where applicable
reverse financial effects
reverse/adjust payment
audit
outbox
```

Event:

```text
SalesOrderVoided
```

Offline:

```text
No
```

unless an explicit high-risk offline policy is introduced later through ADR.

---

# 11. Completed Sale vs Refund

Refund is not a state shortcut.

Do not implement:

```text
COMPLETED → REFUNDED
```

by simply changing status.

Refund is a separate aggregate/workflow referencing the original sale/payment.

Recommended:

```text
SALE.COMPLETED
       ↓
RefundRequested
       ↓
RefundApproved
       ↓
RefundProcessing
       ↓
RefundCompleted
```

The original sale remains historically completed.

---

# 12. Payment State Machine

## 12.1 States

```text
PENDING
PROCESSING
SUCCESS
FAILED
CANCELLED
EXPIRED
REFUND_PENDING
PARTIALLY_REFUNDED
REFUNDED
UNKNOWN
```

`UNKNOWN` is mandatory for ambiguous provider outcomes.

---

## 12.2 State Diagram

```text
PENDING
   │
   ├── process ──► PROCESSING
   │                  │
   │                  ├── success ──► SUCCESS
   │                  │
   │                  └── failure ──► FAILED
   │
   ├── cancel ───► CANCELLED
   │
   └── expire ───► EXPIRED

PROCESSING ── provider ambiguity ──► UNKNOWN

SUCCESS ── refund ──► REFUND_PENDING
REFUND_PENDING ──► PARTIALLY_REFUNDED
REFUND_PENDING ──► REFUNDED
PARTIALLY_REFUNDED ──► REFUNDED
```

---

# 13. PENDING → PROCESSING

Command:

```text
ProcessPayment
```

Permission:

```text
payment.process
```

Preconditions:

- payment PENDING
- amount valid
- currency valid
- sale context valid
- provider active
- idempotency valid

Side effects:

- payment attempt created
- audit
- provider adapter invocation may occur outside core DB transaction

Important:

Provider call must not hold a long database transaction.

---

# 14. PROCESSING → SUCCESS

Possible sources:

- synchronous provider response
- verified webhook
- reconciliation result

Preconditions:

- provider event/response authenticated
- provider transaction ID valid
- amount matches expected amount
- currency matches
- transition valid

Side effects:

- payment SUCCESS
- payment history
- audit
- outbox

---

# 15. PROCESSING → FAILED

Sources:

- provider definitive failure
- reconciliation definitive failure

Examples:

```text
DECLINED
EXPIRED
INVALID_PAYMENT
```

Must not classify timeout automatically as FAILED if provider result is unknown.

---

# 16. PROCESSING → UNKNOWN

Required when:

```text
request may have reached provider
+
response is unknown
```

Examples:

- network timeout after submission
- connection reset
- ambiguous provider response

Next step:

```text
UNKNOWN
 ↓
Reconciliation
 ↓
SUCCESS / FAILED / STILL_UNKNOWN
```

Never blind retry a charge.

---

# 17. UNKNOWN → SUCCESS

Only through:

- provider reconciliation
- verified webhook
- authoritative provider query

Audit:

```text
PAYMENT_RECONCILED_SUCCESS
```

---

# 18. UNKNOWN → FAILED

Only when provider/backend can establish definitive failure.

Audit:

```text
PAYMENT_RECONCILED_FAILED
```

---

# 19. SUCCESS → REFUND_PENDING

Command:

```text
RequestRefund
```

Permission:

```text
sales.refund.request
```

Approval may be required.

Preconditions:

- payment SUCCESS
- refundable amount > 0
- refund amount <= remaining refundable amount

---

# 20. REFUND State Machine

States:

```text
REQUESTED
APPROVAL_PENDING
APPROVED
PROCESSING
SUCCESS
FAILED
CANCELLED
```

Diagram:

```text
REQUESTED
   ↓
APPROVAL_PENDING
   ↓ approve
APPROVED
   ↓ process
PROCESSING
   ├── success → SUCCESS
   └── failure → FAILED
```

Cancellation:

```text
REQUESTED → CANCELLED
APPROVAL_PENDING → CANCELLED
```

Offline:

```text
No
```

---

# 21. POS Session State Machine

## States

```text
OPENING
OPEN
CLOSING
CLOSED
VARIANCE_REVIEW
```

---

## 21.1 Diagram

```text
OPENING
   ↓
OPEN
   ↓
CLOSING
   ├── balanced ─────► CLOSED
   └── variance ─────► VARIANCE_REVIEW
                            ↓
                         CLOSED
```

---

# 22. OPENING → OPEN

Command:

```text
OpenSession
```

Permission:

```text
pos.session.open
```

Preconditions:

- terminal active
- no active incompatible session
- user authorized
- opening cash valid
- terminal/device valid

Concurrency:

Database must guarantee only one active session per terminal.

Existing partial unique index remains authoritative.

Audit:

```text
SESSION_OPENED
```

Event:

```text
POSSessionOpened
```

Offline:

```text
No by default.
```

---

# 23. OPEN → CLOSING

Command:

```text
CloseSession
```

Permission:

```text
pos.session.close
```

Preconditions:

- session OPEN
- cashier has submitted count
- transaction processing settled according to policy

Side effects:

- calculate expected cash
- record counted cash
- calculate variance
- audit

---

# 24. CLOSING → CLOSED

If:

```text
variance <= configured threshold
```

then:

```text
CLOSED
```

Audit:

```text
SESSION_CLOSED
```

Event:

```text
POSSessionClosed
```

---

# 25. CLOSING → VARIANCE_REVIEW

If variance exceeds policy:

```text
VARIANCE_REVIEW
```

Requires:

```text
pos.session.variance.review
```

Potential approval:

```text
store.manager
```

---

# 26. VARIANCE_REVIEW → CLOSED

Requires:

- variance acknowledged
- reason
- approver if configured

Audit:

```text
SESSION_VARIANCE_RESOLVED
```

---

# 27. Inventory Reservation State Machine

## States

```text
PENDING
RESERVED
PARTIALLY_RESERVED
RELEASED
CONSUMED
EXPIRED
CONFLICT
```

---

# 28. Reservation Diagram

```text
PENDING
   ↓
RESERVED
   ├── consume → CONSUMED
   ├── release → RELEASED
   └── expire  → EXPIRED

PENDING
   ↓
PARTIALLY_RESERVED
   ├── consume
   ├── release
   └── conflict
```

---

# 29. PENDING → RESERVED

Command:

```text
ReserveStock
```

Preconditions:

- product active
- warehouse/location valid
- quantity > 0
- available quantity sufficient
- concurrency check succeeds

Concurrency:

```text
SELECT ... FOR UPDATE
```

or equivalent version-safe strategy.

Audit:

```text
STOCK_RESERVED
```

Event:

```text
StockReserved
```

---

# 30. PENDING → CONFLICT

Occurs when:

```text
available < requested
```

or reservation version is stale.

Error:

```text
INVENTORY_CONFLICT
```

Online:

Return conflict immediately.

Offline:

The original offline sale is preserved and enters reconciliation.

---

# 31. RESERVED → CONSUMED

Occurs when sale fulfillment/payment completion requires stock deduction.

Audit:

```text
STOCK_DEDUCTED
```

Event:

```text
StockDeducted
```

Ledger entry is immutable.

---

# 32. RESERVED → RELEASED

Command:

```text
ReleaseReservation
```

Used for:

- cancelled sale
- expired reservation
- failed business flow

Audit:

```text
STOCK_RELEASED
```

---

# 33. RESERVED → EXPIRED

System/worker transition.

Requires:

- reservation expiration timestamp reached

No user permission required.

Audit:

```text
STOCK_RESERVATION_EXPIRED
```

---

# 34. Inventory Adjustment State Machine

States:

```text
DRAFT
SUBMITTED
APPROVAL_PENDING
APPROVED
POSTED
REJECTED
CANCELLED
```

Flow:

```text
DRAFT
  ↓ submit
SUBMITTED
  ↓
APPROVAL_PENDING
  ├── approve → APPROVED → POSTED
  └── reject  → REJECTED
```

High-risk adjustment requires approval.

Permission:

```text
inventory.adjust.create
inventory.adjust.approve
inventory.adjust.post
```

Segregation of duties may prohibit same user from creating and approving.

---

# 35. Stock Transfer State Machine

States:

```text
DRAFT
REQUESTED
APPROVED
IN_TRANSIT
RECEIVED
CANCELLED
```

Flow:

```text
DRAFT
 ↓
REQUESTED
 ↓ approve
APPROVED
 ↓ dispatch
IN_TRANSIT
 ↓ receive
RECEIVED
```

Inventory movement occurs at defined transition points.

The source and destination inventory effects must be auditable.

---

# 36. Promotion State Machine

## States

```text
DRAFT
PENDING_APPROVAL
APPROVED
SCHEDULED
ACTIVE
PAUSED
EXPIRED
REJECTED
CANCELLED
```

Flow:

```text
DRAFT
 ↓ submit
PENDING_APPROVAL
 ↓ approve
APPROVED
 ↓
SCHEDULED
 ↓ start
ACTIVE
 ├── pause → PAUSED
 └── expiry → EXPIRED

PAUSED → ACTIVE
```

---

# 37. Promotion Rules

Promotion activation requires:

- approved state
- validity period
- complete rule definition
- version
- required store/channel scope

Promotion cannot be activated from arbitrary PATCH.

Audit:

```text
PROMOTION_APPROVED
PROMOTION_ACTIVATED
PROMOTION_PAUSED
```

Offline:

Only explicitly offline-capable promotion versions may be cached.

---

# 38. Price Rule State Machine

States:

```text
DRAFT
APPROVED
ACTIVE
INACTIVE
EXPIRED
```

Price changes are versioned.

Historical sales retain price snapshot.

Offline:

Only cached valid price versions.

---

# 39. Purchase Order State Machine

States:

```text
DRAFT
SUBMITTED
APPROVED
PARTIALLY_RECEIVED
RECEIVED
CANCELLED
CLOSED
```

Flow:

```text
DRAFT
 ↓
SUBMITTED
 ↓
APPROVED
 ↓
PARTIALLY_RECEIVED
 ↓
RECEIVED
 ↓
CLOSED
```

Cancellation depends on receipt state.

---

# 40. Goods Receipt State Machine

States:

```text
DRAFT
POSTED
CANCELLED
```

Posting creates authoritative inventory effect.

Once posted:

```text
DRAFT → POSTED
```

cannot be arbitrarily reverted.

Correction uses reversal/adjustment workflow.

---

# 41. Loyalty Transaction State Machine

For immutable ledger transactions:

```text
PENDING
POSTED
REVERSED
```

Flow:

```text
PENDING
 ↓
POSTED
 ↓
REVERSED
```

Do not edit posted points transactions.

A correction creates a compensating transaction.

---

# 42. Voucher Redemption State Machine

States:

```text
PENDING
REDEEMED
CANCELLED
EXPIRED
```

Redemption must be atomic.

Typical:

```text
PENDING
 ↓
REDEEMED
```

The redemption count is updated atomically.

Offline:

```text
No by default.
```

Reason:

Global voucher limits cannot safely be enforced independently by disconnected terminals.

---

# 43. Workflow Instance State Machine

States:

```text
PENDING
IN_PROGRESS
APPROVED
REJECTED
CANCELLED
EXPIRED
```

Generic flow:

```text
PENDING
 ↓
IN_PROGRESS
 ├── approve → APPROVED
 ├── reject  → REJECTED
 ├── cancel  → CANCELLED
 └── timeout → EXPIRED
```

Workflow state must not bypass underlying business aggregate state.

Approval only permits the underlying command to execute.

---

# 44. Approval State Machine

States:

```text
PENDING
APPROVED
REJECTED
CANCELLED
```

Approver requirements:

- permission
- scope
- separation of duties
- MFA where required

---

# 45. Offline Sync State Machine

Client states:

```text
LOCAL_ONLY
QUEUED
SYNCING
SYNCED
FAILED_RETRYABLE
FAILED_PERMANENT
CONFLICT
RECONCILIATION_REQUIRED
CANCELLED
```

---

# 46. Offline Sync Diagram

```text
LOCAL_ONLY
    ↓
QUEUED
    ↓
SYNCING
    ├── success ───────► SYNCED
    ├── retryable ─────► FAILED_RETRYABLE
    ├── permanent ─────► FAILED_PERMANENT
    ├── conflict ──────► CONFLICT
    └── ambiguous ─────► RECONCILIATION_REQUIRED

FAILED_RETRYABLE
    ↓
QUEUED

CONFLICT
    ↓
RECONCILIATION_REQUIRED
    ↓
RESOLVED / SYNCED
```

`SYNCED` is terminal for the sync record.

---

# 47. Offline Sync Rules

## Retryable

```text
NETWORK_TIMEOUT
HTTP_502
HTTP_503
HTTP_504
RATE_LIMIT
TEMPORARY_DATABASE_UNAVAILABLE
```

Transition:

```text
SYNCING → FAILED_RETRYABLE → QUEUED
```

## Permanent

```text
VALIDATION_ERROR
PERMISSION_REVOKED
PRODUCT_INACTIVE
OFFLINE_POLICY_EXPIRED
PAYLOAD_VERSION_UNSUPPORTED
```

Transition:

```text
SYNCING → FAILED_PERMANENT
```

## Conflict

```text
STOCK_CONFLICT
PRICE_VERSION_CONFLICT
PROMOTION_VERSION_CONFLICT
SESSION_STATE_CONFLICT
```

Transition:

```text
SYNCING → CONFLICT
```

## Reconciliation

```text
PAYMENT_UNKNOWN
SERVER_COMMIT_UNKNOWN
PROVIDER_TIMEOUT_AFTER_SUBMISSION
```

Transition:

```text
SYNCING → RECONCILIATION_REQUIRED
```

---

# 48. Device Registration State Machine

States:

```text
PENDING
ACTIVE
SUSPENDED
REVOKED
```

Flow:

```text
PENDING
 ↓
ACTIVE
 ↓ suspend
SUSPENDED
 ↓ reactivate
ACTIVE
 ↓ revoke
REVOKED
```

`REVOKED` should not return to ACTIVE through normal UI.

Requires administrative device re-registration or explicit security workflow.

---

# 49. Terminal State Machine

States:

```text
ACTIVE
MAINTENANCE
DISABLED
RETIRED
```

Rules:

- DISABLED terminal cannot open new session.
- RETIRED terminal cannot authenticate operationally.
- MAINTENANCE may block transaction creation depending on policy.

---

# 50. Refund State Machine

States:

```text
REQUESTED
APPROVAL_PENDING
APPROVED
PROCESSING
SUCCESS
FAILED
CANCELLED
```

Important:

Refund is independent from original sale lifecycle.

Original sale remains:

```text
COMPLETED
```

unless separate void logic is applied.

---

# 51. External Integration Delivery State Machine

States:

```text
PENDING
PROCESSING
SUCCESS
FAILED_RETRYABLE
FAILED_PERMANENT
DEAD_LETTER
```

Flow:

```text
PENDING
 ↓
PROCESSING
 ├── SUCCESS
 ├── FAILED_RETRYABLE → PENDING
 ├── FAILED_PERMANENT
 └── DEAD_LETTER
```

Delivery is idempotent.

External document creation requires external reference reconciliation.

---

# 52. Webhook Event State Machine

States:

```text
RECEIVED
VERIFIED
PROCESSED
DUPLICATE
REJECTED
FAILED_RETRYABLE
```

Flow:

```text
RECEIVED
 ↓ verify
VERIFIED
 ↓ process
PROCESSED
```

Duplicate provider event:

```text
RECEIVED → DUPLICATE
```

Invalid signature:

```text
RECEIVED → REJECTED
```

---

# 53. Generic Transition Contract

Every command handler should conceptually follow:

```go
func Execute(ctx context.Context, id uuid.UUID, cmd Command) error {
    // 1. authorization
    // 2. load aggregate
    // 3. validate current state
    // 4. validate business preconditions
    // 5. transition aggregate
    // 6. persist
    // 7. append state history
    // 8. audit
    // 9. outbox
    // 10. commit
    return nil
}
```

The actual domain method should own the invariant:

```go
order.Confirm(...)
payment.MarkSuccess(...)
reservation.Consume(...)
```

not the HTTP handler.

---

# 54. State History

Critical aggregates must retain history.

Minimum:

```text
aggregate_type
aggregate_id
from_state
to_state
command
actor_id
request_id
trace_id
reason
metadata
created_at
```

Recommended tables:

```text
sales.sales_order_status_history
payment.payment_status_history
workflow.transition_history
```

Additional history tables may be added for inventory reservation and refund.

---

# 55. Invalid Transition Contract

Example:

```text
SALE.COMPLETED
      ↓
ConfirmOrder
```

must return:

```text
409 SALES_STATE_INVALID
```

Response:

```json
{
  "error": {
    "code": "SALES_STATE_INVALID",
    "message": "Sales order cannot be confirmed from COMPLETED state",
    "details": {
      "current_state": "COMPLETED",
      "requested_transition": "CONFIRM"
    }
  },
  "request_id": "req_123"
}
```

---

# 56. Permission Matrix by Transition

| Aggregate | Transition | Permission |
|---|---|---|
| Sale | Create | sales.create |
| Sale | Confirm | sales.confirm |
| Sale | Cancel | sales.cancel |
| Sale | Void | sales.void |
| Payment | Process | payment.process |
| Payment | Refund | payment.refund |
| Session | Open | pos.session.open |
| Session | Close | pos.session.close |
| Inventory | Reserve | inventory.reserve |
| Inventory | Adjust Create | inventory.adjust.create |
| Inventory | Adjust Approve | inventory.adjust.approve |
| Promotion | Create | promotion.create |
| Promotion | Approve | promotion.approve |
| Promotion | Activate | promotion.activate |
| Voucher | Redeem | loyalty.voucher.redeem |
| Workflow | Approve | workflow.approve |

Final permission codes are maintained in Permission Matrix v1.

---

# 57. Offline Transition Matrix

| Transition | Offline |
|---|---:|
| Create local cart | Yes |
| Create offline cash sale | Yes |
| Sale sync | Yes, when connected |
| Sale confirm authoritative | Server |
| Payment gateway process | No/default |
| Payment reconciliation | No |
| Refund | No |
| Voucher redeem | No |
| Loyalty redeem | No |
| Stock adjustment | No |
| Promotion activation | No |
| POS session open | No/default |
| POS session close | No/default |
| Product lookup | Yes |
| Price lookup | Yes, cached |
| Promotion evaluation | Limited/cacheable |

---

# 58. State Transition Side Effects

State transition must declare side effects explicitly.

Example:

```text
SALE → COMPLETED
```

may cause:

```text
Payment → SUCCESS
Inventory → CONSUMED
Audit → SALE_COMPLETED
Outbox → SalesOrderCompleted
```

Do not hide important side effects in random repository methods.

Application orchestration owns cross-domain coordination.

---

# 59. Compensation Rules

Distributed/external side effects may fail after local transaction commits.

Do not pretend rollback is always possible.

Use:

```text
compensation
reversal
retry
reconciliation
workflow
```

Example:

```text
Sale committed
↓
ERP delivery failed
↓
Sale remains completed
↓
Outbox retry
↓
ERP reconciliation
```

Do not mark sale CANCELLED merely because ERP is temporarily unavailable.

---

# 60. State Machine and Database

Database constraints should support state integrity where practical.

Application service remains the primary transition controller.

Recommended:

```text
CHECK(status IN (...))
```

and immutable history.

Do not rely exclusively on DB enum/check constraints for complex transition rules.

---

# 61. State Machine and Offline Reconciliation

Offline transaction may have a local state that does not match server state.

Example:

```text
LOCAL:
SALE = COMPLETED_LOCAL

SERVER:
STOCK_CONFLICT
```

The client must not overwrite server state.

Instead:

```text
LOCAL_COMPLETED
       ↓ sync
CONFLICT
       ↓
RECONCILIATION
```

The original local evidence remains preserved.

---

# 62. State Machine Testing

Every transition requires:

### Happy path

```text
valid state
+
valid actor
+
valid scope
+
valid preconditions
=
success
```

### Invalid state

```text
wrong current state
=
409 *_STATE_INVALID
```

### Permission

```text
insufficient permission
=
403
```

### Scope

```text
outside store/tenant scope
=
403 / tenant-safe not-found policy
```

### Concurrency

Two commands transition the same aggregate simultaneously.

Expected:

- one succeeds;
- the other receives conflict/state/version error;
- no impossible final state.

---

# 63. Mandatory State Tests

Before UAT:

```text
Sale:
- duplicate confirm
- cancel after completion
- void without permission
- concurrent void

Payment:
- duplicate process
- webhook replay
- unknown payment
- refund over remaining amount

Inventory:
- two last-stock sales
- duplicate reservation
- consume released reservation

POS Session:
- two opens on same terminal
- close twice
- close with variance

Promotion:
- activate unapproved
- expire active
- pause/activate race

Offline:
- duplicate sync
- out-of-order sync
- stock conflict
- payment unknown
- expired policy
```

---

# 64. State Machine Versioning

State names are part of the internal business contract.

Adding a state is potentially backward-compatible only if:

- clients can safely handle it;
- API response schema remains compatible;
- workflows are updated;
- reporting understands it.

Removing/renaming a state requires ADR and migration.

State transition changes require tests.

---

# 65. Forbidden Patterns

Do not:

```text
UPDATE sales_order SET status='COMPLETED'
```

from arbitrary service.

Do not:

```text
PATCH /payments/{id}
```

to manually set SUCCESS.

Do not:

```text
DELETE payment
```

to fix a failed transaction.

Do not:

```text
UPDATE loyalty.transaction
```

to correct points.

Do not:

```text
UPDATE stock_ledger
```

to fix inventory.

Use:

```text
command
+
compensation/reversal
+
audit
```

---

# 66. Architecture Lock

State Machine Catalog v1 locks:

1. Critical state transitions are command-based.
2. State changes are permission and scope controlled.
3. State history is append-only.
4. Critical transitions are audited.
5. Critical transitions emit domain/outbox events where appropriate.
6. Invalid transitions return machine-readable conflict errors.
7. Payment UNKNOWN is a first-class state.
8. Offline conflicts are first-class states.
9. Refund is separate from original Sale lifecycle.
10. Inventory corrections use ledger/adjustment, not mutation.
11. Loyalty corrections use compensating transactions.
12. ERP failure does not automatically reverse POS completion.
13. Offline client state never overrides server state.
14. Terminal/session concurrency is enforced.
15. State changes must be covered by automated tests.

---

# 67. Next Artifact

After State Machine Catalog v1:

```text
State Machine Catalog v1       ← CURRENT
        ↓
Permission Matrix v1
        ↓
Event Catalog v1
        ↓
Audit Action Catalog v1
        ↓
Offline Database Design v1
        ↓
OpenAPI v1.1
        ↓
Implementation Repository Blueprint
        ↓
CI/CD Architecture
        ↓
Backend Foundation
```

The State Machine Catalog is the authoritative reference for lifecycle transitions during implementation.
