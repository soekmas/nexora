# POS Enterprise Platform — Event Catalog v1

**Status:** Architecture Draft / Proposed Lock
**Version:** v1.0
**Scope:** Domain events, transactional outbox, event envelope, consumers, delivery semantics, versioning, idempotency, audit relationship, integration boundaries, offline sync, ERP/payment integration.

---

## 1. Purpose

Event Catalog v1 defines the canonical asynchronous business events for the POS Enterprise Platform.

Events are used for:

- decoupling domains inside the modular monolith;
- transactional outbox delivery;
- integration with ERP/SAP and external systems;
- reporting/read-model projection;
- notifications;
- loyalty and downstream business processing;
- reconciliation;
- operational monitoring;
- future extraction of modules into services without changing domain semantics.

Events are **not** the primary mechanism for enforcing synchronous business invariants. Critical commands remain synchronous and transactional inside their owning domain. Events communicate the committed business fact after the transaction succeeds.

---

## 2. Architecture Position

```text
Client / POS / API
       |
       v
Application Command
       |
       v
Domain Aggregate / Use Case
       |
       +---- Business State Change
       |
       +---- Audit Record
       |
       +---- Outbox Event  <--- same DB transaction
       |
       v
PostgreSQL COMMIT
       |
       v
Outbox Worker
       |
       +---- Internal Consumer
       +---- ERP / SAP Adapter
       +---- Payment Reconciliation
       +---- Notification
       +---- Reporting Projection
       +---- Integration Delivery
```

### Mandatory rule

A business event that represents a committed state change MUST be written to the transactional outbox in the same database transaction as the state change and its required audit record.

The application MUST NOT:

- commit a business transaction and then rely on an independent best-effort event publish;
- publish an event to an external broker before the database transaction commits;
- call SAP/payment/notification APIs inside the critical database transaction merely to publish an event;
- allow consumers to assume exactly-once network delivery.

---

# 3. Event Categories

| Category | Meaning | Examples |
|---|---|---|
| Domain Event | Committed business fact | SalesOrderCompleted |
| Integration Event | Domain fact intended for external boundary | SalesOrderCompleted / PaymentCompleted |
| Projection Event | Fact consumed to update read model | StockAdjusted |
| Security Event | Security activity | LoginFailed |
| Platform Event | Technical/platform lifecycle | DeviceRegistered |
| Sync Event | Offline synchronization result | OfflineTransactionSynced |

A domain event MAY be reused as an integration event only when its contract is sufficiently stable and does not expose internal implementation details.

Do not create events merely because a database row was updated. Events must represent meaningful business facts or explicitly required technical lifecycle facts.

---

# 4. Canonical Event Envelope

Every durable event MUST use a common envelope.

```json
{
  "event_id": "evt_01J...",
  "event_type": "SalesOrderCompleted",
  "event_version": 1,
  "aggregate_type": "sales_order",
  "aggregate_id": "uuid",
  "tenant_id": "uuid",
  "company_id": "uuid",
  "occurred_at": "2026-10-06T09:00:00Z",
  "recorded_at": "2026-10-06T09:00:00Z",
  "source": "pos-api",
  "correlation_id": "req_123",
  "causation_id": "cmd_456",
  "trace_id": "trace_789",
  "actor_type": "USER",
  "actor_id": "uuid",
  "payload": {},
  "metadata": {}
}
```

## 4.1 Envelope fields

| Field | Required | Rule |
|---|---:|---|
| event_id | Yes | Globally unique event identifier |
| event_type | Yes | Stable canonical name |
| event_version | Yes | Integer contract version |
| aggregate_type | Yes | Owning domain aggregate |
| aggregate_id | Yes | Public aggregate identifier |
| tenant_id | Yes | Tenant context |
| company_id | Conditional | Required where company-scoped |
| occurred_at | Yes | Business occurrence time, server authoritative |
| recorded_at | Yes | Persistence time |
| source | Yes | Originating application/component |
| correlation_id | Yes | Request/workflow correlation |
| causation_id | Conditional | Command/event that caused this event |
| trace_id | Recommended | Distributed tracing |
| actor_type | Yes | USER, SERVICE, SYSTEM |
| actor_id | Conditional | Actor identity where applicable |
| payload | Yes | Versioned business data |
| metadata | Yes | Non-business delivery/context metadata |

### Security

Never include:

- password/password hash;
- CVV;
- full card number;
- access token / refresh token;
- API secret;
- encryption key;
- MFA secret;
- unnecessary PII;
- raw payment credentials.

Payment events should contain provider reference/tokenized identifiers where required, never sensitive card data.

---

# 5. Event Naming Rules

Use past-tense business facts:

- `SalesOrderCreated`
- `SalesOrderConfirmed`
- `SalesOrderCompleted`
- `PaymentCompleted`
- `StockAdjusted`

Avoid command names:

- `CreateSalesOrder`
- `ConfirmPayment`
- `AdjustStock`

Commands request an action. Events state that the action occurred.

Event names are case-sensitive canonical identifiers.

---

# 6. Versioning Rules

`event_type` identifies the semantic event. `event_version` identifies the payload contract.

Example:

```text
SalesOrderCompleted v1
SalesOrderCompleted v2
```

### Backward compatibility

Minor compatible additions SHOULD preserve the same event version only when the serialization contract explicitly permits optional fields. Breaking changes MUST increment `event_version`.

Breaking changes include:

- removing a field;
- changing field meaning;
- changing data type incompatibly;
- changing enum semantics;
- changing aggregate meaning;
- changing required/optional semantics in a breaking manner.

Consumers MUST NOT infer meaning from undocumented fields.

Event contracts MUST be tested in CI.

---

# 7. Delivery Semantics

The platform uses:

```text
At-least-once delivery
        +
Consumer idempotency
        =
Exactly-once business effect where required
```

The platform MUST NOT promise exactly-once network delivery.

Every consumer that performs a non-idempotent side effect MUST have a durable deduplication mechanism.

Recommended consumer key:

```text
(event_id, consumer_name)
```

For aggregate ordering, consumers SHOULD additionally track:

```text
aggregate_type + aggregate_id + aggregate_version
```

where aggregate versioning is available.

---

# 8. Outbox Contract

The existing `integration.outbox` is the system-of-record queue for durable event publication.

Minimum conceptual fields:

```text
id
 tenant_id
event_id
event_type
event_version
aggregate_type
aggregate_id
payload
metadata
status
attempt_count
available_at
locked_at
processed_at
last_error
created_at
```

Recommended statuses:

```text
PENDING
PROCESSING
DELIVERED
FAILED_RETRYABLE
FAILED_PERMANENT
DEAD_LETTER
```

### Worker rules

1. Select eligible records with row locking / skip-locked semantics.
2. Claim work safely.
3. Publish/execute consumer delivery outside the originating transaction.
4. Record success/failure.
5. Retry transient failures using bounded exponential backoff + jitter.
6. Move permanently failing records to dead letter after policy threshold.
7. Preserve original event payload.
8. Never silently delete failed business events.

---

# 9. Global Domain Event Catalog

## 9.1 Sales

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| SalesOrderCreated | Order enters DRAFT | Sales | Reporting, audit projection |
| SalesOrderConfirmed | DRAFT → CONFIRMED | Sales | Inventory, payment, reporting |
| SalesOrderProcessing | CONFIRMED → PROCESSING | Sales | Fulfillment, reporting |
| SalesOrderCompleted | PROCESSING → COMPLETED | Sales | Inventory, loyalty, finance, reporting, ERP |
| SalesOrderCancelled | Order cancelled | Sales | Inventory release, payment, reporting |
| SalesOrderVoided | COMPLETED → VOIDED | Sales | Inventory reversal, finance, loyalty reversal, ERP |
| SalesOrderItemChanged | Controlled item mutation before completion | Sales | Pricing/promotion projection |

### Important

`SalesOrderCompleted` is the major business fact for a successful sale. Consumers MUST NOT independently recompute the final sale amount from current pricing/promotion tables. They use the transaction snapshot.

---

# 10. Payment Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| PaymentCreated | Payment record created | Payment | Reporting |
| PaymentProcessing | PENDING → PROCESSING | Payment | Monitoring |
| PaymentCompleted | PROCESSING → SUCCESS | Payment | Sales, finance, ERP, notification |
| PaymentFailed | PROCESSING → FAILED | Payment | Sales, notification, reporting |
| PaymentCancelled | Payment cancelled | Payment | Sales, reporting |
| PaymentExpired | Payment expired | Payment | Sales, reporting |
| PaymentUnknown | Provider result ambiguous | Payment | Reconciliation, operations |
| RefundRequested | Refund request created | Payment/Refund | Workflow, finance |
| RefundApproved | Approval completed | Workflow/Refund | Payment |
| RefundProcessing | Refund sent to provider | Payment | Monitoring |
| RefundCompleted | Refund succeeded | Payment | Sales, finance, ERP |
| RefundFailed | Refund failed | Payment | Operations, reconciliation |

### PaymentUnknown

`PaymentUnknown` is a high-priority event. Consumers MUST NOT interpret it as success or failure.

It triggers reconciliation workflow rather than blind retry.

---

# 11. Inventory Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| StockReserved | Reservation created | Inventory | Sales, reporting |
| StockReservationPartiallyReserved | Partial reservation | Inventory | Sales, operations |
| StockReleased | Reservation released | Inventory | Sales, reporting |
| StockReservationExpired | Reservation expired | Inventory | Sales, operations |
| StockReservationConflict | Reservation cannot be fulfilled | Inventory | Sales, reconciliation |
| StockDeducted | Sale/fulfillment deducts stock | Inventory | Sales, finance, reporting, ERP |
| StockAdjusted | Approved adjustment posted | Inventory | Finance, reporting, ERP |
| StockTransferred | Transfer completed | Inventory | Warehouse, reporting, ERP |
| StockTransferReceived | Destination receipt completed | Inventory | Warehouse, reporting |
| StockOpnamePosted | Stock count posted | Inventory | Finance, reporting, ERP |

Stock ledger remains authoritative. Events communicate the committed ledger/business result.

---

# 12. Pricing Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| PriceCreated | Price rule created | Pricing | Approval, audit |
| PriceApproved | Price rule approved | Pricing/Workflow | Cache, reporting |
| PriceActivated | Price becomes active | Pricing | POS master-data sync |
| PriceDeactivated | Price becomes inactive | Pricing | POS cache invalidation |
| PriceExpired | Price validity ends | Pricing | POS cache invalidation |

Price events MUST carry enough version/effective-date information for downstream caches to invalidate or refresh safely.

---

# 13. Promotion Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| PromotionCreated | Promotion created | Promotion | Workflow, audit |
| PromotionSubmitted | Submitted for approval | Promotion | Workflow |
| PromotionApproved | Approved | Promotion/Workflow | POS master data, reporting |
| PromotionScheduled | Scheduled | Promotion | Activation scheduler |
| PromotionActivated | ACTIVE | Promotion | POS cache, reporting |
| PromotionPaused | Paused | Promotion | POS cache |
| PromotionExpired | Validity ended | Promotion | POS cache |
| PromotionApplied | Promotion applied to sale | Sales/Promotion | Reporting, analytics |

`PromotionApplied` should contain the applied promotion/version and calculated reward/discount snapshot necessary for audit/reproducibility.

---

# 14. POS Session Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| POSSessionOpened | OPENING → OPEN | POS | Reporting, cash management |
| POSSessionClosing | OPEN → CLOSING | POS | Cash management |
| POSSessionVarianceReviewRequired | Variance detected | POS | Supervisor/Manager workflow |
| POSSessionClosed | CLOSING → CLOSED | POS | Finance, reporting |

A session event MUST identify terminal and store context.

---

# 15. Device / Terminal Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| DeviceRegistered | Device registration completed | Identity/POS | Security, audit |
| DeviceActivated | Device becomes active | Identity/POS | Sync |
| DeviceSuspended | Device suspended | Identity/POS | Sync/security |
| DeviceRevoked | Device revoked | Identity/POS | Sync/security |
| TerminalEnabled | Terminal active | Organization/POS | Device/session |
| TerminalDisabled | Terminal disabled | Organization/POS | Device/session |
| TerminalRetired | Terminal retired | Organization/POS | Operations |

Revocation/disable events should propagate quickly because they can affect offline transaction eligibility.

---

# 16. Loyalty Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| LoyaltyEarnPending | Earn transaction queued | Loyalty | Reporting |
| LoyaltyEarned | Earn posted | Loyalty | Customer profile, reporting |
| LoyaltyRedeemed | Redemption posted | Loyalty | Customer profile, reporting |
| LoyaltyReversed | Ledger reversal posted | Loyalty | Reporting, finance |

Loyalty transactions are immutable ledger records. Corrections are represented by compensating transactions/events, not direct mutation.

---

# 17. Voucher Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| VoucherIssued | Voucher issued | Loyalty/Promotion | Customer notification |
| VoucherReserved | Voucher reserved | Loyalty/Promotion | Sales |
| VoucherRedeemed | Redemption completed | Loyalty/Promotion | Sales, reporting |
| VoucherReleased | Reservation released | Loyalty/Promotion | Sales |
| VoucherExpired | Voucher expires | Loyalty/Promotion | Reporting |
| VoucherCancelled | Voucher cancelled | Loyalty/Promotion | Reporting |

Voucher redemption is offline-disabled by default. Offline POS MUST NOT emit a successful `VoucherRedeemed` event unless the architecture policy explicitly supports and authorizes that capability.

---

# 18. Purchasing Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| PurchaseOrderCreated | PO created | Purchasing | Workflow, reporting |
| PurchaseOrderSubmitted | PO submitted | Purchasing | Workflow |
| PurchaseOrderApproved | PO approved | Purchasing/Workflow | Supplier/warehouse |
| PurchaseOrderCancelled | PO cancelled | Purchasing | Reporting |
| GoodsReceiptPosted | GR posted | Purchasing/Inventory | Inventory, finance, ERP |
| GoodsReceiptCancelled | GR reversed/cancelled | Purchasing/Inventory | Inventory, finance, ERP |

Goods receipt events must not directly imply accounting completion unless finance processing actually occurred.

---

# 19. Workflow / Approval Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| WorkflowStarted | Workflow instance created | Workflow | Reporting |
| ApprovalRequested | Approval required | Workflow | Notification |
| ApprovalApproved | Approval granted | Workflow | Owning domain |
| ApprovalRejected | Approval rejected | Workflow | Owning domain |
| WorkflowCompleted | Workflow completed | Workflow | Owning domain |
| WorkflowCancelled | Workflow cancelled | Workflow | Reporting |
| WorkflowExpired | Workflow expired | Workflow | Reporting |

Approval events are not themselves permission grants. The owning domain must still validate the required business state before committing the next transition.

---

# 20. Integration / ERP Events

| Event | Purpose | Main Consumer |
|---|---|---|
| ERPPostingRequested | Domain transaction requires ERP posting | ERP adapter |
| ERPPostingSucceeded | ERP posting completed | Finance/reporting |
| ERPPostingFailedRetryable | ERP temporary failure | Integration worker |
| ERPPostingFailedPermanent | ERP failure requiring intervention | Operations |
| ERPReconciliationRequired | ERP/domain mismatch | Finance/operations |
| ExternalReferenceLinked | External document/reference linked | Reporting/reconciliation |

SAP-specific payload structures MUST remain inside the SAP adapter/integration boundary. Core events should use canonical business concepts.

---

# 21. Offline Sync Events

| Event | Trigger | Owner | Main Consumers |
|---|---|---|---|
| OfflineTransactionQueued | Client queues transaction | POS sync | Monitoring |
| OfflineTransactionSyncStarted | Server begins sync | POS sync | Monitoring |
| OfflineTransactionSynced | Server commits transaction | POS sync | POS client/read model |
| OfflineTransactionRetryableFailure | Temporary failure | POS sync | Client/operations |
| OfflineTransactionConflict | Business conflict | POS sync/domain | Reconciliation |
| OfflineTransactionReconciliationRequired | Ambiguous/conflicting result | POS sync | Operations/client |
| OfflineTransactionResolved | Reconciliation completed | POS sync/domain | Client/operations |

The server remains authoritative. Client-side state must not override authoritative business events.

---

# 22. Security Events

Security events are separate from normal domain events but may share the same outbox/integration delivery infrastructure where appropriate.

Canonical events include:

- UserCreated
- UserDisabled
- UserRoleAssigned
- UserRoleRevoked
- PermissionDenied
- LoginSucceeded
- LoginFailed
- MFAChallengeFailed
- SessionRevoked
- APIKeyCreated
- APIKeyRevoked
- DeviceRevoked
- BreakGlassAccessGranted
- BreakGlassAccessExpired

Sensitive authentication material MUST NOT be included in payloads.

---

# 23. Event → State Transition Mapping

| Domain | Transition | Event |
|---|---|---|
| Sales | DRAFT → CONFIRMED | SalesOrderConfirmed |
| Sales | CONFIRMED → PROCESSING | SalesOrderProcessing |
| Sales | PROCESSING → COMPLETED | SalesOrderCompleted |
| Sales | * → CANCELLED | SalesOrderCancelled |
| Sales | COMPLETED → VOIDED | SalesOrderVoided |
| Payment | PENDING → PROCESSING | PaymentProcessing |
| Payment | PROCESSING → SUCCESS | PaymentCompleted |
| Payment | PROCESSING → FAILED | PaymentFailed |
| Payment | PROCESSING → UNKNOWN | PaymentUnknown |
| POS Session | OPENING → OPEN | POSSessionOpened |
| POS Session | OPEN → CLOSING | POSSessionClosing |
| POS Session | CLOSING → CLOSED | POSSessionClosed |
| Reservation | PENDING → RESERVED | StockReserved |
| Reservation | PENDING → PARTIALLY_RESERVED | StockReservationPartiallyReserved |
| Reservation | * → RELEASED | StockReleased |
| Adjustment | APPROVED → POSTED | StockAdjusted |
| Promotion | PENDING_APPROVAL → APPROVED | PromotionApproved |
| Promotion | SCHEDULED → ACTIVE | PromotionActivated |
| Purchase Order | APPROVED → PARTIALLY_RECEIVED | PurchaseOrder / receipt event |
| Goods Receipt | DRAFT → POSTED | GoodsReceiptPosted |
| Workflow | PENDING → APPROVED | ApprovalApproved |
| Device | PENDING → ACTIVE | DeviceActivated |
| Device | * → REVOKED | DeviceRevoked |

Every state transition need not produce a public integration event. Internal state-history records may be more appropriate for low-value technical transitions.

---

# 24. Event vs Audit

Events and audit records serve different purposes.

| Concern | Event | Audit |
|---|---|---|
| Business fact | Yes | Yes, when critical |
| Downstream processing | Yes | No |
| User accountability | Metadata/actor | Primary purpose |
| Before/after data | Usually no | Yes where required |
| Immutable | Yes | Yes |
| External integration | Yes | Usually no |
| Security investigation | Some | Primary |
| Replay | Yes | Generally no |

A critical command MAY create both:

```text
Business State Change
      +
Audit Record
      +
Domain Event
```

all inside the same transaction where they represent the same committed action.

---

# 25. Event Consumer Rules

Consumers MUST:

1. validate event version;
2. validate tenant context;
3. validate event schema;
4. perform idempotency check;
5. execute business-safe side effect;
6. commit consumer state atomically where possible;
7. acknowledge delivery only after durable processing;
8. expose metrics and failures;
9. preserve correlation/trace metadata;
10. avoid modifying the source aggregate directly unless explicitly owning that domain.

Consumers MUST NOT:

- trust event payload as authorization;
- bypass domain invariants;
- directly mutate another domain's tables;
- perform irreversible external action before durable dedupe state is recorded;
- assume event ordering globally;
- assume one delivery only.

---

# 26. Ordering

Global event ordering is NOT guaranteed.

Where ordering is required, preserve ordering per aggregate:

```text
aggregate_type + aggregate_id
```

Example:

```text
SalesOrderConfirmed
SalesOrderProcessing
SalesOrderCompleted
```

A consumer must not process `SalesOrderCompleted` before the required prior event if its own projection depends on that sequence.

Preferred solution: include aggregate version/sequence where needed.

Example:

```json
{
  "aggregate_version": 7
}
```

Consumers can detect:

- duplicate version;
- stale version;
- skipped version;
- out-of-order event.

---

# 27. Idempotency

### Producer

The same business transaction must not create multiple semantically distinct events for the same committed transition.

### Consumer

Recommended table/concept:

```text
integration.event_consumer_checkpoint
--------------------------------------
consumer_name
aggregate_type
aggregate_id
last_event_version
updated_at
```

For events where aggregate ordering is not sufficient, use:

```text
integration.event_delivery
--------------------------
event_id
consumer_name
status
processed_at
attempt_count
last_error
```

Unique constraint:

```text
UNIQUE(event_id, consumer_name)
```

The exact physical implementation can be finalized during Offline Database Design / implementation phase.

---

# 28. Retry Policy

Suggested baseline:

```text
1st retry: 5 sec
2nd:      15 sec
3rd:      30 sec
4th:      1 min
5th:      5 min
6th+:     exponential backoff with cap
```

Use jitter to prevent retry storms.

Retryable examples:

- network timeout;
- temporary database unavailable;
- ERP temporary unavailable;
- HTTP 429;
- HTTP 5xx;
- provider temporary unavailable.

Permanent examples:

- invalid event contract;
- unsupported event version;
- invalid mandatory business reference;
- authentication/configuration error requiring intervention;
- malformed payload that cannot succeed by retry.

Permanent failures go to `DEAD_LETTER` after policy threshold and generate operational alerts.

---

# 29. External Integration Rule

External systems such as SAP Business One, payment providers, email/SMS, and third-party APIs are downstream consumers.

Never:

```text
POS transaction
   -> call SAP
   -> call payment provider
   -> commit POS DB
```

Preferred:

```text
POS transaction
   -> commit DB + outbox
   -> worker
   -> external API
   -> record result
```

For payment itself, the payment authorization flow is a domain command with its own provider interaction pattern. Provider callbacks/webhooks are independently verified and converted into canonical payment state changes/events.

---

# 30. ERP / SAP Integration Principle

Canonical event:

```text
SalesOrderCompleted
```

SAP adapter decides whether this becomes:

```text
SAP B1 AR Invoice
SAP Journal Entry
SAP Inventory Issue
SAP Incoming Payment
```

depending on configured business mapping.

Core Sales domain MUST NOT contain fields such as:

```text
sap_doc_entry
sap_obj_type
sap_series
sap_doc_num
```

unless a value is genuinely a business concept. External identifiers belong in integration/external-reference structures.

---

# 31. Payment Integration Principle

Payment provider-specific events are normalized.

Example:

```text
Xendit callback
Nicepay callback
Midtrans callback
        |
        v
Provider Adapter
        |
        v
Canonical Payment State Change
        |
        v
PaymentCompleted / PaymentFailed / PaymentUnknown
```

Core consumers should not need to understand provider-specific status codes.

---

# 32. Offline Event Principle

Offline POS does not create a separate alternate business truth.

Example:

```text
Offline client transaction
      |
      v
Sync queue
      |
      v
Server command
      |
      v
Authoritative DB transaction
      |
      +-- audit
      +-- outbox
      +-- domain event
```

The authoritative event is created by the server after successful commit.

The client may maintain local operational events, but those are not authoritative enterprise domain events.

---

# 33. Event Payload Design

Payloads SHOULD be self-contained enough for downstream processing without requiring direct database access to the source domain.

Example `SalesOrderCompleted` v1:

```json
{
  "order_id": "uuid",
  "order_number": "SO-202610-000001",
  "customer_id": "uuid",
  "store_id": "uuid",
  "terminal_id": "uuid",
  "currency": "IDR",
  "subtotal": 1000000,
  "discount_total": 100000,
  "tax_total": 99000,
  "grand_total": 999000,
  "items": [
    {
      "line_id": "uuid",
      "product_id": "uuid",
      "variant_id": "uuid",
      "quantity": 1,
      "unit_price": 1000000,
      "discount_amount": 100000,
      "net_amount": 900000,
      "tax_amount": 99000
    }
  ]
}
```

Do not include unnecessary full customer profile, internal DB IDs that have no consumer purpose, or mutable current master-data values.

---

# 34. Monetary Values

Events carrying money MUST include:

```text
amount
currency
```

Prefer decimal/numeric semantics over floating point.

Do not infer currency from tenant/store configuration at consumer time.

---

# 35. Tenant Isolation

Every event must carry tenant context where the domain is tenant-scoped.

Consumers MUST enforce tenant boundaries.

A consumer MUST NOT use an event's tenant ID to elevate authorization.

The event identifies context; the consumer still executes under its own authorized service identity.

---

# 36. Observability

Required metrics:

- outbox pending count;
- outbox processing latency;
- event delivery success rate;
- retry count;
- dead-letter count;
- consumer processing latency;
- consumer failure rate;
- event age;
- oldest pending event age;
- duplicate delivery count;
- out-of-order event count;
- reconciliation-required count.

Logs MUST include:

```text
request_id
trace_id
correlation_id
event_id
event_type
aggregate_type
aggregate_id
consumer_name
```

Do not log sensitive payload fields.

---

# 37. Dead Letter / Recovery

Dead-letter events must be recoverable.

Operations UI/API should eventually support:

- inspect event metadata;
- inspect failure reason;
- retry event;
- replay from original payload;
- mark resolved;
- quarantine event;
- link incident/ticket;
- view attempt history.

Manual replay MUST be permission-controlled and audited.

Suggested permissions:

```text
integration.event.read
integration.event.retry
integration.event.replay
integration.event.dead_letter.resolve
```

---

# 38. Event Replay Rules

Replay is allowed only when the consumer is designed to be replay-safe.

Replay MUST NOT:

- create duplicate financial transactions;
- charge a customer twice;
- deduct stock twice;
- award loyalty points twice;
- create duplicate SAP documents.

For financial/ERP consumers, external reference and idempotency checks are mandatory before replay.

---

# 39. Event Security

Event access is permissioned.

Recommended permissions:

```text
integration.event.read
integration.event.retry
integration.event.replay
audit.event.read
```

Only service identities and explicitly authorized administrators may consume/replay sensitive event streams.

Event payloads containing customer PII must follow data classification and retention policies.

---

# 40. Initial Event-to-Consumer Matrix

| Event | Inventory | Finance | Loyalty | ERP | Reporting | Notification |
|---|---:|---:|---:|---:|---:|---:|
| SalesOrderCompleted | Yes | Yes | Yes | Yes | Yes | Optional |
| SalesOrderCancelled | Yes | Conditional | Conditional | Yes | Yes | Optional |
| SalesOrderVoided | Yes | Yes | Yes | Yes | Yes | Optional |
| PaymentCompleted | No | Yes | No | Yes | Yes | Yes |
| PaymentFailed | No | Conditional | No | No | Yes | Yes |
| PaymentUnknown | No | Yes | No | Reconcile | Yes | Yes |
| StockAdjusted | Yes | Yes | No | Yes | Yes | No |
| StockTransferred | Yes | Conditional | No | Yes | Yes | No |
| PromotionActivated | No | No | No | No | Yes | Optional |
| LoyaltyEarned | No | Conditional | Yes | Conditional | Yes | Optional |
| RefundCompleted | Conditional | Yes | Conditional | Yes | Yes | Yes |
| GoodsReceiptPosted | Yes | Yes | No | Yes | Yes | No |

This matrix is a baseline. Each consumer must still have a documented business reason and ownership.

---

# 41. Event Ownership

Each event has exactly one domain owner.

Examples:

```text
SalesOrderCompleted -> Sales
PaymentCompleted    -> Payment
StockAdjusted       -> Inventory
PromotionActivated  -> Promotion
LoyaltyEarned       -> Loyalty
POSSessionClosed    -> POS
```

Other domains may consume the event but may not redefine its semantics.

If two domains need fundamentally different semantics, create separate events rather than overloading one event.

---

# 42. Event Contract Governance

Any event contract change requires:

1. compatibility review;
2. consumer impact assessment;
3. version decision;
4. OpenAPI/integration contract review if externally exposed;
5. migration strategy;
6. test fixtures update;
7. documentation update;
8. ADR if architecture semantics change.

Breaking event changes MUST NOT be silently deployed.

---

# 43. Testing Requirements

## Unit

- event creation;
- payload mapping;
- versioning;
- transition → event mapping.

## Integration

- state change + outbox committed atomically;
- rollback means no event is published;
- duplicate consumer delivery is safe;
- tenant isolation;
- audit/event consistency.

## Concurrency

- two workers cannot claim same outbox event incorrectly;
- consumer dedupe under concurrent delivery;
- aggregate ordering where required.

## Failure

- DB rollback;
- worker crash after external call;
- worker crash before acknowledgement;
- provider timeout;
- ERP unavailable;
- repeated 429/5xx;
- malformed event;
- unsupported version.

## Replay

- replay does not duplicate financial effect;
- replay does not double-deduct stock;
- replay does not double-award loyalty;
- ERP duplicate protection works.

---

# 44. Acceptance Criteria

Event Catalog v1 is accepted when:

- every critical state transition has a defined event or documented reason not to publish one;
- every event has exactly one owner;
- event envelope is standardized;
- event versioning rules are defined;
- transactional outbox is mandatory for durable domain events;
- consumers are at-least-once safe;
- consumer idempotency is defined;
- payment ambiguity is represented explicitly;
- tenant context is preserved;
- audit/event relationship is defined;
- SAP/provider-specific concepts remain behind adapters;
- offline authoritative events originate from the server;
- retry/dead-letter/replay rules are defined;
- sensitive data rules are defined;
- observability metrics are defined;
- event contract changes are governed;
- concurrency and rollback tests are mandatory.

---

# 45. Architecture Lock Candidates

The following should be treated as locked unless changed by ADR:

1. Transactional outbox is the canonical durable event mechanism.
2. Events represent committed facts, not commands.
3. At-least-once delivery is the baseline.
4. Consumers must be idempotent.
5. Event IDs are globally unique.
6. Event versions are explicit.
7. Domain ownership is explicit.
8. Critical events include tenant/context metadata.
9. Server-generated events are authoritative for offline transactions.
10. External APIs are downstream from the core transaction except where the domain command explicitly requires provider interaction, such as payment authorization.
11. Payment `UNKNOWN` is never treated as success/failure automatically.
12. SAP-specific structures remain in integration adapters.
13. Financial/ERP event replay must be idempotent.
14. Audit and event records are distinct concerns.
15. Dead-letter events are retained and recoverable.
16. Event replay is permissioned and audited.
17. Redis or an in-memory queue is never the system of record for durable business events.

---

# 46. Relationship to Next Artifacts

```text
Permission Matrix v1
        |
        v
Event Catalog v1
        |
        +---- Audit Action Catalog v1
        |
        +---- Offline Database Design v1
        |
        +---- OpenAPI v1.1
        |
        +---- Monorepo Blueprint
        |
        +---- CI/CD
        |
        +---- Go Backend Foundation
```

Event Catalog v1 is a prerequisite for finalizing the audit action catalog and offline persistence because audit, outbox, idempotency, sync, and integration records must agree on identifiers and transaction boundaries.

---

# 47. Final Principle

The POS platform uses events as a **durable communication contract around committed business facts**, not as a replacement for transactional domain logic.

```text
COMMAND
   ↓
AUTHORIZE
   ↓
VALIDATE STATE
   ↓
DOMAIN TRANSACTION
   ↓
STATE CHANGE
   +
AUDIT
   +
OUTBOX EVENT
   ↓
COMMIT
   ↓
ASYNC CONSUMERS
   ↓
ERP / REPORTING / LOYALTY / NOTIFICATION / RECONCILIATION
```

This preserves transactional correctness while allowing the platform to evolve from a modular monolith toward more distributed architecture without prematurely introducing distributed transaction complexity.
