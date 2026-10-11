# POS Offline Sync Contract v1

**Status:** Architecture Contract / Implementation Baseline  
**Version:** 1.0  
**Date:** 2026-10-06  
**Architecture:** Offline-Capable POS, Server Authoritative  
**Backend:** Go Modular Monolith  
**Database:** PostgreSQL  
**Frontend:** Next.js + React + TypeScript + IndexedDB  
**Related:** POS Frontend Architecture v1, POS OpenAPI v1, POS API Contract v1, Database Design v1.1 Hardened

---

## 1. Executive Decision

POS menggunakan pendekatan **Offline-Capable + Server Authoritative**.

Offline capability dimaksudkan untuk menjaga operational continuity ketika koneksi backend terputus, tetapi tidak mengubah server menjadi non-authoritative.

Prinsip:

1. Terminal dapat membuat transaksi lokal yang memenuhi offline policy.
2. Setiap transaksi offline mempunyai `client_transaction_id` yang immutable.
3. Setiap logical command mempunyai `idempotency_key` yang tetap selama seluruh retry.
4. Local transaction disimpan secara durable di IndexedDB.
5. Synchronization bersifat ordered, idempotent, resumable, dan observable.
6. Server tetap menentukan authoritative business state.
7. Stok global tidak dianggap authoritative ketika terminal offline.
8. Payment provider online tidak boleh dianggap sukses hanya karena transaksi lokal tersimpan.
9. Conflict tidak boleh diselesaikan diam-diam oleh client.
10. Failed transaction tidak boleh otomatis dihapus.
11. Setiap transaksi dapat ditelusuri dari terminal/device sampai server transaction, audit, dan outbox.
12. Exactly-once business effect dicapai melalui idempotent server processing, bukan dengan mengandalkan jaringan exactly-once.

Core principle:

> **Offline may create a pending operational transaction; only the server establishes authoritative business state.**

---

# 2. Scope

Contract ini mendefinisikan:

- device registration
- terminal identity
- offline authorization
- offline capability policy
- sync envelope
- sync queue
- batch synchronization
- FIFO ordering
- idempotency
- retry classification
- conflict classification
- reconciliation
- server acknowledgement
- sync cursor
- heartbeat
- clock synchronization
- offline quota
- audit correlation
- payment ambiguity
- recovery after browser/application restart
- duplicate delivery
- security controls
- observability
- acceptance criteria

Tidak mendefinisikan:

- UI detail
- IndexedDB implementation detail
- printer protocol
- barcode scanner protocol
- provider-specific payment SDK
- ERP implementation detail
- SQL repository implementation

---

# 3. Authority Model

## 3.1 Server authoritative

Server adalah source of truth untuk:

- inventory
- final sales state
- payment state
- loyalty
- voucher
- promotion activation
- pricing master
- customer master
- POS session authoritative state
- user permission
- workflow
- finance
- ERP integration
- audit

## 3.2 Client authoritative only for local operational state

Client boleh menjadi authoritative sementara untuk:

- active cart
- local UI
- queued transaction
- local scanner input
- local print job
- local sync state

Local state tidak boleh diperlakukan sebagai final financial truth.

---

# 4. Offline Capability Policy

| Capability | Offline | Rule |
|---|---:|---|
| Product lookup | Yes | Cached master only |
| Barcode lookup | Yes | Cached barcode only |
| Cart | Yes | Local |
| Cash sale | Yes | Policy + offline quota |
| Cached price | Yes | Validity must be known |
| Cached promotion | Limited | Must be explicitly offline-safe |
| Customer lookup | Yes | Cached subset |
| Customer creation | Limited | Prefer online |
| Card gateway | No/default | Provider dependent |
| Online gateway | No | Network required |
| Loyalty earn | Deferred | Reconciled server-side |
| Loyalty redeem | No/default | Prevent double-spend |
| Voucher redeem | No/default | Prevent global double-spend |
| Refund | No/default | Server-controlled |
| Stock adjustment | No | Server workflow |
| User administration | No | Server only |
| Promotion administration | No | Server only |
| ERP operation | No | Server worker |
| Audit queue | Yes | Local then sync |

---

# 5. Device Identity

Device identity is different from user identity.

Required identifiers:

```text
tenant_id
company_id
store_id
terminal_id
device_id
installation_id
```

Recommended semantics:

- `terminal_id`: logical POS terminal assigned by organization.
- `device_id`: physical/browser installation identity.
- `installation_id`: generated per application installation/profile.
- `user_id`: currently authenticated cashier/operator.

A cashier may change without changing terminal identity.

A browser profile reset must not silently impersonate the old device.

---

# 6. Device Registration

Recommended endpoint:

```http
POST /api/v1/pos/devices/register
```

Request:

```json
{
  "terminal_id": "uuid",
  "installation_id": "uuid",
  "device_name": "POS-JKT01-01",
  "client_version": "1.0.0",
  "platform": "WINDOWS_CHROMIUM"
}
```

Response:

```json
{
  "device_id": "uuid",
  "terminal_id": "uuid",
  "store_id": "uuid",
  "status": "ACTIVE",
  "offline_policy_version": 3,
  "server_time": "2026-10-06T03:40:00Z"
}
```

Registration must verify:

- terminal exists
- terminal belongs to store/company/tenant
- terminal is active
- device is allowed
- user/device policy
- duplicate installation policy

Device registration is not equivalent to authorization to perform all offline operations.

---

# 7. Offline Authorization

Offline authorization is a server-issued policy.

Suggested policy:

```json
{
  "offline_enabled": true,
  "max_offline_duration_minutes": 240,
  "max_queued_transactions": 100,
  "allow_cash_sale": true,
  "allow_offline_promotion": true,
  "allow_offline_customer": false,
  "allow_offline_void": false,
  "allow_offline_refund": false,
  "offline_quota_version": 12
}
```

Policy must have an expiry.

Do not allow unlimited offline operation.

Privileged users require stricter policy.

---

# 8. Offline Quota

Offline quota is a controlled risk boundary.

Example:

```text
Store authoritative available stock = 100

Terminal A offline quota = 20
Terminal B offline quota = 20
Terminal C offline quota = 10
```

The quota is not a replacement for authoritative stock.

It is a risk control that limits the maximum exposure while disconnected.

Quota should be:

- scoped by terminal/store
- product/category aware where necessary
- versioned
- server-issued
- auditable
- expirable

Suggested API:

```http
GET /api/v1/pos/offline-policy
GET /api/v1/pos/offline-quota
```

---

# 9. Transaction Identity

Every offline transaction requires:

```text
client_transaction_id
idempotency_key
terminal_id
device_id
store_id
user_id
```

Recommended format:

```text
client_transaction_id:
POS01-20261006-000123

idempotency_key:
POS01-20261006-000123:CREATE_SALE
```

The client transaction ID is immutable.

The server must never use `business_number` supplied by the client.

Business document number remains server-generated.

---

# 10. Offline Transaction Envelope

Canonical envelope:

```json
{
  "client_transaction_id": "POS01-20261006-000123",
  "operation": "SALE_CREATE",
  "payload_version": 1,
  "idempotency_key": "POS01-20261006-000123:SALE_CREATE",
  "tenant_id": "uuid",
  "store_id": "uuid",
  "terminal_id": "uuid",
  "device_id": "uuid",
  "user_id": "uuid",
  "client_sequence": 123,
  "created_at_device": "2026-10-06T03:30:01Z",
  "client_timezone": "Asia/Jakarta",
  "payload": {},
  "client_metadata": {
    "app_version": "1.0.0"
  }
}
```

Server must validate all security-sensitive fields against authenticated context.

Client-provided tenant/user/store IDs are evidence/context, not authorization.

---

# 11. Client Sequence

Each terminal/device maintains a monotonically increasing local sequence:

```text
1001
1002
1003
1004
```

It is useful for:

- FIFO ordering
- duplicate detection
- missing transaction detection
- troubleshooting
- reconciliation

A sequence gap is not automatically an error because transactions can be cancelled locally.

However, unexplained gaps should be observable.

---

# 12. Sync States

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

Rules:

- `SYNCED` is terminal for successful synchronization.
- `FAILED_RETRYABLE` may return to `QUEUED`.
- `FAILED_PERMANENT` requires user/system action.
- `CONFLICT` requires explicit reconciliation.
- `RECONCILIATION_REQUIRED` is mandatory for ambiguous payment/network outcomes.
- Failed records are never silently deleted.

---

# 13. Sync API

Recommended endpoint:

```http
POST /api/v1/pos/sync
```

Request:

```json
{
  "device_id": "uuid",
  "terminal_id": "uuid",
  "cursor": "opaque-server-cursor",
  "items": [
    {
      "client_transaction_id": "POS01-20261006-000123",
      "operation": "SALE_CREATE",
      "payload_version": 1,
      "idempotency_key": "POS01-20261006-000123:SALE_CREATE",
      "client_sequence": 123,
      "created_at_device": "2026-10-06T03:30:01Z",
      "payload": {}
    }
  ]
}
```

Recommended batch size:

```text
Default: 20
Maximum: 100
```

Batch size should be configurable server-side.

---

# 14. Sync Response

Example:

```json
{
  "server_time": "2026-10-06T03:40:00Z",
  "next_cursor": "opaque-cursor",
  "results": [
    {
      "client_transaction_id": "POS01-20261006-000123",
      "client_sequence": 123,
      "status": "SYNCED",
      "server_transaction_id": "uuid",
      "business_number": "POS-JKT01-20261006-000987",
      "reconciliation_status": "MATCHED",
      "request_id": "req_123",
      "processed_at": "2026-10-06T03:40:01Z"
    }
  ]
}
```

---

# 15. Result Status

Supported server result states:

```text
SYNCED
DUPLICATE_ALREADY_PROCESSED
RETRYABLE
PERMANENT_ERROR
CONFLICT
RECONCILIATION_REQUIRED
REJECTED
```

## DUPLICATE_ALREADY_PROCESSED

Returned when the same logical command was already successfully applied.

It must include the original authoritative result.

Never create a second sale.

---

# 16. Idempotency Contract

Idempotency key is tied to:

```text
tenant_id
operation
idempotency_key
request_hash
```

Example:

```text
(tenant-A, SALE_CREATE, KEY-123)
```

If the same key arrives with the same request hash:

```text
return original result
```

If same key arrives with a different request hash:

```text
409 IDEMPOTENCY_KEY_REUSED
```

Retry must preserve the original idempotency key.

Never generate a new key merely because the network request timed out.

---

# 17. Exactly-Once Business Effect

Network delivery is **at-least-once**.

The server must make business processing idempotent.

Correct model:

```text
Client
  │
  ├── request ─────► Server
  │                     │
  │                     ├─ commit
  │                     │
  │◄── timeout ─────────┘
  │
  ├── retry same key ──► Server
  │                     │
  │                     └─ return original result
```

Thus:

```text
network delivery = at-least-once
business effect = exactly-once
```

where exactly-once is enforced by database uniqueness/idempotency.

---

# 18. FIFO Ordering

For dependent operations within one terminal:

```text
sequence 101
sequence 102
sequence 103
```

must be processed in order.

Default rule:

> Do not advance the dependent queue after a blocking conflict.

Example:

```text
101 = SYNCED
102 = CONFLICT
103 = QUEUED
```

The client should not blindly process 103 if 103 depends on the authoritative outcome of 102.

Independent telemetry/read operations may run concurrently.

---

# 19. Retry Classification

## Retryable

Examples:

```text
NETWORK_TIMEOUT
DNS_FAILURE
CONNECTION_RESET
HTTP_502
HTTP_503
HTTP_504
RATE_LIMIT
TEMPORARY_DATABASE_UNAVAILABLE
```

## Permanent

Examples:

```text
VALIDATION_ERROR
PERMISSION_DENIED
PRODUCT_INACTIVE
INVALID_PRICE
INVALID_PROMOTION
TERMINAL_DISABLED
OFFLINE_POLICY_EXPIRED
```

## Conflict

Examples:

```text
STOCK_CONFLICT
PRICE_VERSION_CONFLICT
PROMOTION_VERSION_CONFLICT
SESSION_STATE_CONFLICT
CUSTOMER_VERSION_CONFLICT
```

## Reconciliation Required

Examples:

```text
PAYMENT_UNKNOWN
PROVIDER_TIMEOUT_AFTER_SUBMISSION
SERVER_COMMIT_UNKNOWN
```

---

# 20. Retry Backoff

Recommended:

```text
1s
2s
4s
8s
16s
30s
60s
```

With jitter.

Maximum retry interval should be configurable.

Retry count must be persisted.

Do not busy-loop.

---

# 21. Payment Ambiguity

This is the highest-risk offline scenario.

Example:

```text
POS → Payment Provider
             ↓
          charged
             ↓
       network timeout
             ↓
POS does not know result
```

The client must mark:

```text
PAYMENT_UNKNOWN
```

It must not create a new payment blindly.

Recommended flow:

```text
PAYMENT_UNKNOWN
       ↓
Reconciliation Request
       ↓
Backend checks payment record/provider
       ↓
┌───────────────┬────────────────┐
│ SUCCESS       │ NOT_FOUND      │
│               │                │
▼               ▼                │
mark paid       controlled retry
```

For provider-specific reconciliation, backend/provider adapter owns the logic.

---

# 22. Offline Cash Payment

Cash is simpler because there is no remote provider.

Offline cash transaction can be recorded:

```json
{
  "method": "CASH",
  "amount": 250000,
  "status": "RECORDED_OFFLINE"
}
```

Receipt status:

```text
OFFLINE / PENDING SYNC
```

After synchronization:

```text
SERVER CONFIRMED
```

Cash session reconciliation remains server-controlled.

---

# 23. Offline Sale

Offline sale flow:

```text
Scan
 ↓
Local product validation
 ↓
Local price validation
 ↓
Offline promotion validation
 ↓
Offline policy/quota validation
 ↓
Local cash payment
 ↓
Persist sale envelope
 ↓
Print pending receipt
 ↓
Queue
 ↓
Reconnect
 ↓
Sync
 ↓
Server authoritative result
```

The client must not manufacture a server business number.

---

# 24. Offline Pricing

Every cached price needs:

```text
price_list_id
price_version
variant_id
currency
unit_price
valid_from
valid_to
```

Offline sale may only use a price when validity can be proven locally.

Expired/unknown pricing:

```text
BLOCK
```

unless store policy explicitly permits a controlled fallback.

---

# 25. Offline Promotion

Only promotions explicitly marked as offline-capable may be cached.

Cache must include:

```text
promotion_id
promotion_version
valid_from
valid_to
priority
stacking_mode
condition_snapshot
reward_snapshot
offline_allowed
```

Completed offline sale stores the applied promotion snapshot.

A later promotion change must not rewrite historical transaction meaning.

---

# 26. Inventory Conflict

Offline inventory is inherently uncertain.

Example:

```text
Server stock = 1

Terminal A offline → sells 1
Terminal B offline → sells 1
```

Both may locally appear valid.

When synchronized:

```text
Sale A → MATCHED
Sale B → STOCK_CONFLICT
```

Never silently modify Sale B to quantity zero.

The conflict becomes a business reconciliation workflow.

Possible policies:

```text
MANAGER_REVIEW
BACKORDER
PARTIAL_FULFILLMENT
CUSTOMER_CONTACT
CANCEL_LINE
```

Policy is business-configurable.

---

# 27. Reconciliation Contract

Endpoint:

```http
GET /api/v1/pos/sync/reconciliation/{client_transaction_id}
```

Example matched response:

```json
{
  "client_transaction_id": "POS01-20261006-000123",
  "server_transaction_id": "uuid",
  "status": "SYNCED",
  "reconciliation_status": "MATCHED",
  "business_number": "POS-JKT01-20261006-000987"
}
```

Conflict:

```json
{
  "client_transaction_id": "POS01-20261006-000124",
  "status": "CONFLICT",
  "reconciliation_status": "REQUIRES_REVIEW",
  "conflict_code": "STOCK_CONFLICT",
  "details": {
    "requested_qty": 2,
    "accepted_qty": 1
  }
}
```

Client must not silently rewrite the original transaction.

---

# 28. Reconciliation States

```text
MATCHED
MATCHED_WITH_WARNING
REQUIRES_REVIEW
RESOLVED
CANCELLED
```

Every resolution must be auditable.

Resolution requires:

```text
resolved_by
resolved_at
resolution_code
resolution_reason
```

---

# 29. Audit Correlation

Every offline transaction must be traceable through:

```text
client_transaction_id
idempotency_key
client_sequence
device_id
terminal_id
request_id
trace_id
server_transaction_id
business_number
```

Audit should record both local-origin metadata and server processing metadata.

---

# 30. Clock Synchronization

Client clock cannot be trusted as authoritative.

Server returns:

```text
server_time
```

Client calculates approximate offset:

```text
server_time - device_time
```

Device-created timestamps are retained for diagnostics.

Authoritative business timestamps are server-generated.

Large clock drift should produce:

```text
CLOCK_DRIFT_WARNING
```

or block offline mode depending on policy.

Suggested threshold:

```text
warning: > 2 minutes
block: configurable, e.g. > 10 minutes
```

---

# 31. Heartbeat

Endpoint:

```http
POST /api/v1/pos/terminals/{terminal_id}/heartbeat
```

Payload:

```json
{
  "device_id": "uuid",
  "client_version": "1.0.0",
  "queue_size": 3,
  "last_sync_at": "2026-10-06T03:35:00Z",
  "client_time": "2026-10-06T03:39:59Z"
}
```

Response:

```json
{
  "server_time": "2026-10-06T03:40:00Z",
  "terminal_status": "ACTIVE",
  "offline_policy_version": 12,
  "force_update": false,
  "sync_required": true
}
```

Heartbeat is operational telemetry, not transaction authorization by itself.

---

# 32. Sync Cursor

Cursor is opaque.

Client must not interpret its internal structure.

Example:

```text
cursor = "eyJ2ZXJzaW9uIjoxMjM..."
```

Server uses cursor to communicate changes relevant to the terminal.

Client stores:

```text
last_successful_cursor
```

Only advance cursor after the corresponding server response is durably processed.

---

# 33. Master Data Sync

Recommended endpoint:

```http
GET /api/v1/pos/sync/master-data?cursor=<cursor>
```

Potential datasets:

```text
products
variants
barcodes
prices
offline-promotions
taxes
payment-methods
store-config
terminal-config
```

Master data changes should be versioned.

Client must be able to rebuild local cache after corruption.

---

# 34. Local Durability

Before acknowledging an offline sale to the cashier:

1. validate local transaction;
2. persist transaction envelope;
3. persist payment record;
4. persist queue state;
5. commit IndexedDB transaction;
6. only then print/display final local receipt.

Browser crash before step 5 must not result in an acknowledged transaction.

---

# 35. Browser / Tab Concurrency

A terminal must not allow multiple active POS tabs to independently process the same cash session.

Recommended controls:

- Web Locks API where available
- local lease/heartbeat
- terminal session token
- single active POS tab policy
- recovery after stale lock

A second tab should show:

```text
POS terminal sedang digunakan pada tab lain.
```

---

# 36. Security

Never store in IndexedDB:

- password
- password hash
- CVV
- full PAN/card number
- provider secret
- JWT signing secret
- encryption master key
- MFA secret

Avoid storing long-lived refresh tokens in plaintext.

Offline authentication should use a device-bound, expiring mechanism.

Privileged users should have stricter offline capability.

---

# 37. Tamper Considerations

Browser storage is not a trusted security boundary.

Therefore:

> Never trust an offline payload simply because it was generated by the official frontend.

Server must revalidate:

- terminal
- device
- user
- product
- price
- promotion
- quantity
- payment
- permissions
- policy
- business state

For higher-risk deployments, future versions may add signed transaction envelopes, but signatures are defense-in-depth and do not replace server validation.

---

# 38. Sync Worker

Lifecycle:

```text
Application Start
       ↓
Load Device Context
       ↓
Open IndexedDB
       ↓
Check Connectivity
       ↓
Check Offline Policy
       ↓
Read Queue
       ↓
Send Batch
       ↓
Persist Result
       ↓
Advance Cursor
       ↓
Continue / Backoff
```

The worker must survive:

- page refresh
- browser restart
- temporary network loss
- API restart

---

# 39. Server Processing Pipeline

For each sync item:

```text
Authenticate
   ↓
Validate Device
   ↓
Validate Terminal Scope
   ↓
Validate Offline Policy
   ↓
Validate Payload Version
   ↓
Validate Idempotency
   ↓
Begin DB Transaction
   ↓
Set RLS Context
   ↓
Execute Domain Command
   ↓
Write Audit
   ↓
Write Outbox
   ↓
Commit
   ↓
Return Authoritative Result
```

External provider calls must not be performed inside the core database transaction.

---

# 40. Database Requirements

Server-side persistence should include or extend idempotency records with:

```text
tenant_id
operation
idempotency_key
request_hash
status
response_code
response_payload
resource_type
resource_id
created_at
completed_at
```

Unique logical key:

```text
(tenant_id, operation, idempotency_key)
```

Request hash protects against key reuse with a different payload.

---

# 41. Outbox Interaction

Successful sync transaction:

```text
Sales
+
Payment state
+
Inventory effect
+
Audit
+
Outbox
+
Idempotency result
```

must commit atomically where they belong to the same business transaction.

Then:

```text
Outbox Worker
    ↓
ERP
Notification
Integration
```

Failure of external delivery must not roll back the already committed sale.

---

# 42. Error Contract

Example:

```json
{
  "error": {
    "code": "STOCK_CONFLICT",
    "message": "Requested quantity is no longer available",
    "details": {
      "variant_id": "uuid",
      "requested_qty": 2,
      "available_qty": 1
    }
  },
  "request_id": "req_123"
}
```

Sync result must classify the error:

```text
retryable
permanent
conflict
reconciliation_required
```

---

# 43. Conflict Code Catalog

Baseline:

```text
STOCK_CONFLICT
PRICE_VERSION_CONFLICT
PROMOTION_VERSION_CONFLICT
SESSION_STATE_CONFLICT
CUSTOMER_VERSION_CONFLICT
TERMINAL_DISABLED
DEVICE_REVOKED
OFFLINE_POLICY_EXPIRED
CLOCK_DRIFT_EXCEEDED
PAYMENT_UNKNOWN
IDEMPOTENCY_KEY_REUSED
PAYLOAD_VERSION_UNSUPPORTED
PERMISSION_REVOKED
```

---

# 44. Payload Versioning

Every syncable operation includes:

```text
payload_version
```

Example:

```text
SALE_CREATE v1
SALE_CREATE v2
```

Server may support multiple versions temporarily.

Old versions must have a deprecation policy.

Never silently reinterpret an old payload into different business meaning.

---

# 45. Partial Batch Failure

A batch is not all-or-nothing.

Example:

```text
1 → SYNCED
2 → SYNCED
3 → STOCK_CONFLICT
4 → QUEUED
5 → QUEUED
```

The client stores each result individually.

However, dependent operations must respect ordering.

---

# 46. Network Recovery

When connectivity returns:

1. perform health probe;
2. refresh server time;
3. validate offline policy;
4. sync oldest queue items;
5. process results;
6. refresh master data;
7. reconcile conflicts;
8. report final sync state.

Do not send thousands of requests simultaneously.

---

# 47. Observability

Metrics:

```text
pos_offline_transactions_total
pos_sync_success_total
pos_sync_retry_total
pos_sync_conflict_total
pos_sync_permanent_failure_total
pos_payment_unknown_total
pos_sync_latency_ms
pos_queue_depth
pos_offline_duration_seconds
pos_clock_drift_seconds
```

Important logs must include:

```text
tenant_id
store_id
terminal_id
device_id
client_transaction_id
idempotency_key
request_id
trace_id
operation
status
```

Do not log sensitive payment data.

---

# 48. Operational Dashboard

Recommended dashboard:

```text
Store
 ├─ Online terminals
 ├─ Offline terminals
 ├─ Queue depth
 ├─ Oldest pending transaction
 ├─ Conflicts
 ├─ Payment unknown
 ├─ Sync failure rate
 └─ Clock drift
```

Alert examples:

```text
Terminal offline > configured threshold
Queue > threshold
Payment unknown > 0
Conflict rate > baseline
Sync failure spike
Device revoked but still sending
```

---

# 49. Offline Transaction Lifecycle Example

```text
LOCAL_ONLY
   ↓
QUEUED
   ↓
SYNCING
   ↓
SYNCED
```

Retry case:

```text
QUEUED
   ↓
SYNCING
   ↓
FAILED_RETRYABLE
   ↓
QUEUED
   ↓
SYNCED
```

Conflict:

```text
SYNCING
   ↓
CONFLICT
   ↓
RECONCILIATION_REQUIRED
   ↓
RESOLVED
```

Payment ambiguity:

```text
SYNCING
   ↓
RECONCILIATION_REQUIRED
   ↓
PAYMENT_CONFIRMED
```

---

# 50. Example End-to-End Offline Sale

### Local

```text
client_transaction_id:
POS01-20261006-000123

sequence:
123

operation:
SALE_CREATE
```

Customer pays cash.

Terminal persists transaction and prints:

```text
OFFLINE SALE
Pending server synchronization
Reference: POS01-20261006-000123
```

### Reconnect

Client sends:

```http
POST /api/v1/pos/sync
Idempotency-Key: POS01-20261006-000123:SALE_CREATE
```

### Server

Server:

```text
validate
→ RLS context
→ price/promotion validation
→ inventory policy
→ create sale
→ ledger
→ audit
→ outbox
→ idempotency result
→ commit
```

### Response

```json
{
  "status": "SYNCED",
  "server_transaction_id": "uuid",
  "business_number": "POS-JKT01-20261006-000987",
  "reconciliation_status": "MATCHED"
}
```

---

# 51. Example Duplicate Sync

First request:

```text
SALE_CREATE
KEY=ABC123
→ committed
```

Response lost.

Retry:

```text
SALE_CREATE
KEY=ABC123
```

Server detects existing idempotency record.

Result:

```text
DUPLICATE_ALREADY_PROCESSED
```

with original transaction ID.

No second sale is created.

---

# 52. Example Stock Conflict

Offline terminal has:

```text
SKU X = locally available
```

Two terminals sell the final unit.

Server receives:

```text
Terminal A → sequence 100
Terminal B → sequence 200
```

First committed transaction consumes stock.

Second gets:

```text
CONFLICT
STOCK_CONFLICT
```

The second transaction remains preserved.

It is not deleted.

Manager resolves it through explicit workflow.

---

# 53. Example Payment Unknown

```text
POS
 ↓
Provider
 ↓
Request accepted
 ↓
Network timeout
```

Client state:

```text
RECONCILIATION_REQUIRED
PAYMENT_UNKNOWN
```

Backend checks provider using provider-specific reconciliation.

Possible authoritative result:

```text
SUCCESS
FAILED
NOT_FOUND
STILL_PROCESSING
```

Only then can payment state transition.

---

# 54. Offline Policy Expiration

If:

```text
offline_policy.expired = true
```

the terminal may:

- display existing local queue;
- allow synchronization;
- block new offline financial transactions.

It must not discard queued transactions.

---

# 55. Disaster Recovery

If IndexedDB is corrupted:

1. terminal enters recovery mode;
2. local queue is exported/recovered where possible;
3. server reconciliation searches by `client_transaction_id`;
4. unknown transactions are reviewed;
5. no transaction is recreated blindly.

Server idempotency records remain the authoritative duplicate detector.

---

# 56. API Additions Required

OpenAPI v1 should be extended with:

```text
POST /pos/devices/register
GET  /pos/offline-policy
GET  /pos/offline-quota
POST /pos/sync
GET  /pos/sync/reconciliation/{client_transaction_id}
GET  /pos/sync/master-data
POST /pos/terminals/{terminal_id}/heartbeat
```

Additional schemas:

```text
OfflineSyncEnvelope
OfflineSyncRequest
OfflineSyncResult
OfflineSyncResponse
OfflinePolicy
OfflineQuota
ReconciliationResult
SyncConflict
DeviceRegistration
TerminalHeartbeat
```

---

# 57. Backend Modules Required

Go backend should add:

```text
internal/possync/
    application/
    domain/
    repository/
    service/
```

Responsibilities:

- device registration
- offline policy
- sync orchestration
- idempotency
- reconciliation
- conflict classification
- sync audit
- heartbeat
- master-data delta

The domain command itself remains owned by the original domain.

Example:

```text
possync
   ↓
sales.CreateOfflineSale()
```

not:

```text
possync
   ↓
direct SQL INSERT sales
```

---

# 58. Security Boundary

Offline sync is an untrusted input boundary.

Never assume:

```text
client_transaction_id
price
discount
tax
stock
user_id
terminal_id
```

are trustworthy merely because they came from the POS application.

Server revalidates authoritative values.

Client snapshots are used for:

- audit
- reconciliation
- diagnostics

not blind authorization.

---

# 59. Performance Targets

Initial targets:

```text
Sync API p95 < 500ms for normal batch
Single sync item processing < 300ms excluding external provider
Batch size default = 20
Maximum batch = 100
Queue persistence = durable
No unbounded client retry loop
```

Targets must be validated by load testing.

---

# 60. Testing Strategy

Mandatory tests:

### Idempotency

- duplicate request
- same key/different payload
- concurrent same key

### Offline

- browser restart
- power loss simulation
- network loss
- reconnect
- queue recovery

### Ordering

- sequence gaps
- out-of-order delivery
- blocking conflict

### Inventory

- two terminals selling final stock
- duplicate sale
- reservation conflict

### Payment

- provider timeout
- response lost
- duplicate webhook
- payment reconciliation

### Security

- wrong tenant
- wrong store
- revoked device
- revoked user
- expired offline policy
- modified client payload

### Data integrity

- no duplicate business transaction
- audit correlation
- outbox creation
- idempotency result persistence

---

# 61. Acceptance Criteria

The contract is considered implemented only when:

- duplicate sync cannot duplicate a sale;
- same idempotency key + same request returns same logical result;
- same idempotency key + different request is rejected;
- queued transactions survive browser restart;
- failed transactions are never silently deleted;
- FIFO ordering works for dependent operations;
- stock conflicts are explicit;
- payment ambiguity has reconciliation;
- offline policy expires;
- revoked device cannot create new offline transactions;
- client cannot bypass tenant/store authorization;
- server remains authoritative;
- audit contains client and server correlation;
- outbox is transactional with core business transaction;
- master data can be incrementally synchronized;
- clock drift is detectable;
- sync queue depth is observable;
- payment secrets are never stored locally;
- printer failure cannot create duplicate transaction;
- multiple tabs cannot independently control one terminal;
- conflict resolution is auditable;
- recovery after application restart works.

---

# 62. Architecture Lock

The following decisions are locked for v1:

1. Offline-capable, not unrestricted offline-first.
2. Server authoritative.
3. Durable IndexedDB queue.
4. At-least-once network delivery.
5. Exactly-once business effect via idempotency.
6. FIFO per terminal for dependent operations.
7. Explicit conflict state.
8. Payment ambiguity requires reconciliation.
9. Offline inventory is policy-controlled, not authoritative.
10. Offline voucher/loyalty redemption is disabled by default.
11. Device identity is separate from user identity.
12. Client timestamps are diagnostic; server timestamps are authoritative.
13. External provider calls are outside core DB transactions.
14. Audit + outbox + idempotency participate in the authoritative transaction where applicable.
15. No silent mutation of historical offline transactions.
16. Sync failures are observable and recoverable.

---

# 63. Next Step

After this contract, the architecture sequence becomes:

```text
Blueprint v2.1
      ↓
ERD v1
      ↓
Database v1.1 Hardened
      ↓
Domain/Security Review
      ↓
API Contract v1
      ↓
Go Backend Architecture v1
      ↓
OpenAPI v1
      ↓
POS Frontend Architecture v1
      ↓
POS Offline Sync Contract v1        ← CURRENT
      ↓
ADR / Architecture Lock
      ↓
Implementation Plan
      ↓
Backend Skeleton
      ↓
Frontend Skeleton
      ↓
Online POS MVP
      ↓
Offline POS
```

**Architecture principle:** offline mode increases operational resilience; it must never become a mechanism for bypassing authoritative security, inventory, payment, audit, or financial rules.
