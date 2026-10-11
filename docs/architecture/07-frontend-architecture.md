# POS Frontend Architecture v1 — Offline-Capable Enterprise POS

**Status:** Proposed / Architecture Draft  
**Version:** 1.0  
**Date:** 2026-10-06  
**Backend Contract:** POS OpenAPI v1  
**Backend Architecture:** Go Backend Architecture v1

## 1. Executive Decision

POS akan menggunakan pendekatan **offline-capable / online-preferred**, bukan full offline-first.

Prinsip utama:

- Server tetap menjadi source of truth untuk master data, inventory, pricing, promotion, payment, customer, loyalty, dan financial state.
- Terminal dapat membuat transaksi lokal ketika koneksi backend terputus.
- Transaksi offline disimpan di local durable storage dan diberi client transaction ID serta idempotency key.
- Saat koneksi kembali, terminal melakukan synchronization secara ordered dan idempotent.
- Payment yang membutuhkan provider online tidak dianggap sukses hanya karena transaksi POS tersimpan lokal.
- Stok offline harus menggunakan policy terbatas karena server tidak dapat menjamin global stock availability secara real time.
- Semua hasil sync dapat ditelusuri melalui terminal ID, device ID, client transaction ID, request ID, idempotency key, dan server transaction ID.

## 2. Why Not Full Offline-First

Full offline-first berbahaya untuk enterprise POS apabila diterapkan tanpa batas karena:

1. dua terminal dapat menjual unit stok yang sama ketika offline;
2. harga/promo dapat berubah sementara terminal masih menggunakan data lama;
3. payment provider biasanya membutuhkan koneksi;
4. voucher dan loyalty memiliki global usage limit;
5. refund/void dapat memiliki dependency pada server;
6. ERP/finance membutuhkan authoritative server transaction.

Karena itu offline capability dibatasi berdasarkan jenis operasi.

## 3. Capability Matrix

| Capability | Online | Offline | Policy |
|---|---|---|---|
| Login existing terminal user | Yes | Limited | Cached session only |
| Product lookup | Yes | Yes | Cached master |
| Barcode scan | Yes | Yes | Cached barcode |
| Cart | Yes | Yes | Local |
| Price calculation | Yes | Yes | Cached version with validity rules |
| Promotion | Yes | Limited | Only approved/cacheable promotions |
| Customer lookup | Yes | Yes | Cached subset |
| Create sale | Yes | Yes | Local queue if allowed |
| Stock reservation | Yes | No authoritative reservation | Local soft availability only |
| Cash payment | Yes | Yes | Pending server sync |
| Card payment | Yes | Depends | Provider terminal/device policy |
| Online gateway payment | Yes | No | Requires network |
| Refund | Yes | No by default | Server approval required |
| Void | Yes | Restricted | Policy dependent |
| Loyalty earn | Yes | Deferred | Server reconciliation |
| Loyalty redeem | Yes | No by default | Prevent global double-spend |
| Voucher redeem | Yes | No by default | Prevent duplicate redemption |
| Stock adjustment | Yes | No | Server approval |
| User/role admin | Yes | No | Server only |
| Promotion admin | Yes | No | Server only |
| ERP sync | Async server | No | Server worker |
| Audit event | Yes | Yes | Local queue then sync |

## 4. Frontend Technology

Recommended baseline:

- Next.js + React + TypeScript
- App Router
- TanStack Query for server state
- Zustand or equivalent for local UI/session state
- IndexedDB for durable offline storage
- Service Worker only where useful; do not make the entire application depend on service-worker magic
- Zod or equivalent runtime validation for API payloads
- WebSocket/SSE optional for server-originated notifications; not required for transaction correctness

The POS shell should be optimized for Chromium-based kiosk/desktop environments and touch/mouse/keyboard operation.

## 5. Application Layers

```text
POS UI
  ↓
Presentation / Screens
  ↓
Application Hooks / Use Cases
  ↓
POS Client Services
  ├── SalesService
  ├── PaymentService
  ├── InventoryService
  ├── PromotionService
  ├── CustomerService
  ├── SyncService
  └── SessionService
  ↓
Data Access
  ├── API Client
  ├── IndexedDB Repository
  └── Sync Queue
```

UI components must not call fetch directly for business operations.

## 6. Recommended Frontend Structure

```text
apps/pos/
  app/
    (auth)/
    (pos)/
      sale/
      payment/
      receipt/
      session/
      offline/
    layout.tsx

  src/
    features/
      cart/
      product-search/
      barcode/
      pricing/
      promotion/
      customer/
      sales/
      payment/
      receipt/
      pos-session/
      offline/
      sync/

    application/
      commands/
      queries/
      hooks/

    domain/
      cart/
      sale/
      payment/
      sync/
      session/

    infrastructure/
      api/
      indexeddb/
      printer/
      scanner/
      device/
      storage/

    shared/
      components/
      types/
      validation/
      telemetry/
      utils/
```

## 7. State Classification

Do not put everything in one global store.

### Server state

Use TanStack Query or equivalent for:

- products
- customers
- prices
- promotions
- store configuration
- payment methods
- user permissions

### Local transactional state

Use dedicated state for:

- active cart
- selected customer
- current sale draft
- payment selection
- POS session UI state

### Durable offline state

Use IndexedDB for:

- cached master data
- pending sales
- sync queue
- sync attempts
- local audit events
- terminal configuration
- cached user/session metadata

## 8. Offline Data Model

Every offline transaction should contain:

```text
client_transaction_id
idempotency_key
terminal_id
device_id
store_id
user_id
created_at_device
payload_version
sync_status
attempt_count
last_attempt_at
last_error_code
server_transaction_id
server_received_at
```

Suggested sync states:

```text
LOCAL_ONLY
QUEUED
SYNCING
SYNCED
FAILED_RETRYABLE
FAILED_PERMANENT
CONFLICT
CANCELLED
```

Never delete a failed transaction automatically.

## 9. IndexedDB Stores

Recommended stores:

```text
pos_config
products
barcodes
price_lists
promotion_cache
customers_cache
payment_methods
active_cart
sales_queue
sync_queue
sync_attempts
local_audit_queue
printer_jobs
```

Do not cache sensitive payment credentials or raw card data.

## 10. Sync Architecture

```text
Offline Sale
    ↓
IndexedDB
    ↓
Sync Queue
    ↓
Network Available
    ↓
Sync Worker
    ↓
POST /api/v1/sales/orders
    ↓
Idempotency-Key
    ↓
Go Backend
    ↓
PostgreSQL Transaction
    ↓
Response
    ↓
Mark SYNCED
```

The sync process must be idempotent.

## 11. Sync Ordering

Within a terminal, default ordering is FIFO.

Example:

```text
T001-SALE-001
T001-SALE-002
T001-SALE-003
```

Do not sync transaction 003 before transaction 001 if business dependency exists.

Independent read-only operations can run concurrently.

## 12. Retry Policy

Retry only retryable errors:

- network timeout
- DNS failure
- 502/503/504
- temporary connection failure
- rate limit where server provides retry guidance

Do not blindly retry:

- invalid request
- permission denied
- promotion invalid
- product inactive
- permanent business conflict
- invalid payment state

Use exponential backoff with jitter.

Example:

```text
1s → 2s → 4s → 8s → 16s → 30s max
```

The queue remains durable after application restart.

## 13. Idempotency

Every syncable command must carry:

```http
Idempotency-Key: <stable-client-operation-key>
X-Request-ID: <request-id>
```

The idempotency key must remain unchanged across retries of the same logical operation.

Do not generate a new idempotency key for every retry.

## 14. Sale Transaction Strategy

### Online

```text
Scan
 ↓
Cart
 ↓
Price/Promotion
 ↓
Checkout
 ↓
Payment
 ↓
Server Sale
 ↓
Receipt
```

### Offline cash sale

```text
Scan
 ↓
Cached Price/Promo
 ↓
Offline Eligibility Check
 ↓
Cash Payment Recorded Locally
 ↓
Receipt marked OFFLINE/PENDING SYNC
 ↓
Sync Later
```

The receipt must clearly distinguish local pending state from server-confirmed state.

## 15. Offline Pricing

Cached prices must include:

```text
price_list_id
price_version
valid_from
valid_to
currency
product_variant_id
unit_price
```

When offline:

- use only prices whose validity period can be determined locally;
- do not silently use expired pricing;
- if pricing validity cannot be established, block or require supervisor decision based on store policy.

## 16. Offline Promotion

Only promotions explicitly marked/cacheable should be usable offline.

Promotion cache should include:

```text
promotion_id
promotion_version
priority
stack_mode
conditions_snapshot
reward_snapshot
valid_from
valid_to
offline_allowed
```

Offline promotion result is provisional until server reconciliation.

For high-risk promotions, require online validation.

## 17. Offline Inventory Policy

This is the most important limitation.

The POS may show:

```text
cached_available_qty
```

but this is not authoritative global stock.

Recommended policies:

### Policy A — Safe

Offline sale allowed only for products with an offline-sale flag and sufficient configured offline quota.

### Policy B — Store allocation

Each terminal receives an offline allocation quota from the server.

Example:

```text
Store stock: 100
Offline terminal allocation: 20
```

The terminal may sell at most 20 offline units until synchronization.

### Policy C — No offline sale for scarce SKUs

High-value or low-stock products require online validation.

Recommended enterprise default: **B + C**.

## 18. Offline Payment

### Cash

Can be recorded offline subject to store policy.

### Card

Depends on payment terminal capabilities. If authorization cannot be independently verified, POS must not represent the payment as online-provider SUCCESS.

### Online gateway

Do not allow offline initiation.

### Refund

Online only by default.

## 19. Conflict Handling

Conflict does not mean automatic overwrite.

Example:

```text
Offline sale:
Product A qty 2

Server stock at sync:
Available qty 1
```

Result:

```text
CONFLICT
```

The system must apply a business resolution policy rather than changing history.

Possible outcomes:

- accept sale and create negative/exception stock according to policy;
- partially fulfill;
- create stock exception for supervisor;
- reject sync and require operational reconciliation.

The default recommendation is **do not silently modify the sale**.

## 20. Reconciliation

After synchronization, backend should return authoritative result:

```json
{
  "clientTransactionId": "POS01-20261006-000123",
  "serverTransactionId": "SO-JKT01-20261006-001923",
  "status": "SYNCED",
  "reconciliationStatus": "MATCHED"
}
```

For conflicts:

```json
{
  "status": "CONFLICT",
  "reconciliationStatus": "REQUIRES_REVIEW"
}
```

## 21. Authentication Offline

Do not allow unlimited offline authentication.

Recommended:

- user must have previously authenticated on that terminal;
- cached credential/session is device-bound;
- offline session has expiry;
- privileged users should have stricter offline policy;
- disable offline login after configured inactivity period;
- revoke offline authorization after server policy update when terminal reconnects.

Never store plaintext passwords.

## 22. POS Session

POS session remains server-owned.

Offline terminal may maintain a local mirror of session state.

Opening/closing cash session should preferably be online.

If business requires offline close:

```text
LOCAL_CLOSING
    ↓
SYNC
    ↓
SERVER_CLOSING
```

Cash variance remains subject to server approval workflow.

## 23. Receipt Printing

Printing should be decoupled from API response.

```text
Sale Confirmed
    ↓
Receipt Document
    ↓
Local Printer Queue
    ↓
Printer Adapter
```

Printer failures must not create a duplicate sale.

Reprint uses the existing server transaction ID/client transaction ID.

## 24. Barcode Scanner

Scanner abstraction:

```text
ScannerAdapter
  ├── KeyboardWedgeScanner
  ├── USBScanner
  └── FutureNativeScanner
```

Barcode input must be normalized before product lookup.

## 25. Device Identity

Each POS terminal should have:

```text
tenant_id
company_id
store_id
terminal_id
device_id
installation_id
```

Device identity is not the same as user identity.

A cashier can change; terminal identity should remain stable.

## 26. Connectivity Detection

Do not rely only on `navigator.onLine`.

Use:

1. browser connectivity signal;
2. backend health probe;
3. last successful API timestamp;
4. sync queue state.

States:

```text
ONLINE
DEGRADED
OFFLINE
SYNCING
```

## 27. UX Requirements

Offline status must be obvious but not disruptive.

Display:

```text
● Online

or

● Offline — 3 transaksi menunggu sinkronisasi
```

Every offline receipt should include a clear status.

Never make the cashier guess whether the transaction reached the server.

## 28. Security Requirements

Do not store:

- password
- password hash
- CVV
- full card number
- provider secret
- JWT signing secret
- refresh token in plaintext
- MFA secret

Encrypt sensitive local data where feasible and supported by the deployment environment.

Use a secure browser profile / kiosk environment for dedicated POS devices.

## 29. Service Worker Policy

Service Worker is optional and should be used primarily for controlled asset caching and application availability.

Do not place critical financial business logic exclusively inside Service Worker code.

IndexedDB remains the durable transaction queue.

## 30. Sync Worker Lifecycle

```text
Application Start
   ↓
Load terminal config
   ↓
Open IndexedDB
   ↓
Detect connectivity
   ↓
Start sync scheduler
   ↓
Process queue
   ↓
Persist result
   ↓
Emit UI notification
```

The sync worker must survive browser refresh/restart.

## 31. Multi-Tab / Multi-Window Safety

Only one active POS transaction owner should operate the terminal.

Use browser coordination mechanisms such as BroadcastChannel/Web Locks where available.

Prevent two browser tabs from simultaneously owning the same terminal session.

## 32. Frontend Authorization

Frontend permissions are for UX control only.

Example:

```text
if permission('sales.void')
    show Void button
```

But backend must still enforce:

```text
sales.void
+ tenant scope
+ store scope
+ session state
+ workflow rule
```

Never trust frontend permission checks as security controls.

## 33. Error Handling

Map backend errors into UX categories:

```text
NETWORK_ERROR
AUTH_ERROR
PERMISSION_ERROR
VALIDATION_ERROR
BUSINESS_RULE_ERROR
CONFLICT_ERROR
RATE_LIMIT
SERVER_ERROR
```

Business errors should display a useful human message while preserving machine-readable error code.

## 34. Observability

Every transaction should expose:

```text
clientTransactionId
idempotencyKey
requestId
serverTransactionId
syncAttempt
terminalId
deviceId
```

Frontend telemetry must avoid sensitive customer/payment data.

## 35. Performance Targets

Initial target for normal online POS:

- barcode/product lookup: < 300 ms perceived target
- cart update: < 100 ms local interaction
- cached product search: < 100 ms target
- checkout API: < 1.5 s target excluding external provider latency
- UI should remain responsive during sync

Offline transaction creation should not wait for the network.

## 36. Testing Strategy

### Unit

- cart calculation
- promotion calculation
- offline eligibility
- sync state machine
- retry classification
- conflict mapping

### Integration

- IndexedDB persistence
- API sync
- duplicate retry
- browser restart
- network interruption

### E2E

- online sale
- offline sale
- reconnect
- duplicate sync
- payment failure
- server conflict
- printer failure
- terminal restart

### Chaos scenarios

- network drops during checkout
- network drops after payment request
- browser crashes after local commit
- backend returns 500 after receiving command
- duplicate webhook/payment result
- clock drift on terminal

## 37. Critical Rule: Payment Ambiguity

The most dangerous case is:

```text
POS → Payment Provider
       ↓
provider succeeds
       ↓
network drops
       ↓
POS does not receive response
```

Frontend must NOT simply retry with a new payment transaction.

The system must use provider-side idempotency/reference lookup/reconciliation.

Payment state must be resolved from authoritative provider/backend state.

## 38. Critical Rule: Sale vs Payment

Do not make the local POS assume:

```text
payment success = sale success
```

The backend must define the authoritative transaction state.

Examples:

```text
SALE_PENDING_PAYMENT
PAYMENT_SUCCESS
SALE_COMPLETED
PAYMENT_FAILED
SALE_CANCELLED
```

## 39. Offline Quota Recommendation

For the first production version:

- offline sales enabled per store;
- offline quota configurable by store/terminal;
- offline quota assigned only to approved SKUs/categories;
- no offline voucher redemption;
- no offline loyalty redemption;
- no offline refund;
- no offline stock adjustment;
- no offline promotion creation/editing;
- no offline privileged administration.

## 40. Recommended Frontend Domain Ownership

| Domain | Frontend Owner |
|---|---|
| Cart | cart |
| Product | product-search |
| Pricing | pricing |
| Promotion | promotion |
| Sales | sales |
| Payment | payment |
| POS Session | pos-session |
| Offline | offline |
| Sync | sync |
| Receipt | receipt |
| Customer | customer |
| Device | device |

## 41. Anti-Patterns

Do not:

- use localStorage as transaction database;
- generate a new idempotency key on retry;
- treat `navigator.onLine === true` as backend availability;
- store card secrets locally;
- let frontend calculate authoritative inventory;
- silently overwrite offline conflicts;
- retry payment blindly;
- hard-delete failed sync records;
- put all state into one global Zustand store;
- make Service Worker the source of truth;
- let UI components directly call arbitrary API endpoints.

## 42. Implementation Phases

### Phase 1 — Online POS

- authentication
- terminal/session
- product search
- cart
- pricing
- promotion
- checkout
- payment
- receipt

### Phase 2 — Offline Foundation

- IndexedDB
- durable queue
- connectivity state
- idempotency
- sync worker
- local audit

### Phase 3 — Controlled Offline Sale

- offline SKU policy
- offline quota
- cash sale
- reconciliation
- conflict workflow

### Phase 4 — Hardware

- barcode scanner
- printer
- cash drawer

### Phase 5 — Advanced Offline

- stronger device identity
- signed local transaction envelope
- richer conflict resolution
- offline operational dashboards

## 43. Architecture Acceptance Criteria

Before production:

- application survives browser restart without losing queued transactions;
- duplicate sync does not duplicate sales;
- same idempotency key returns the same logical result;
- offline sales are clearly identifiable;
- online server remains authoritative;
- stock conflicts are not silently overwritten;
- payment ambiguity has a reconciliation flow;
- offline privileges are restricted;
- local storage contains no prohibited payment secrets;
- terminal cannot be simultaneously controlled by multiple tabs;
- sync retry survives application restart;
- audit correlation exists from client transaction to server transaction;
- printer failure does not duplicate a sale;
- network interruption tests pass;
- backend security remains authoritative even when frontend is modified.

## 44. Final Architecture Decision

Adopt **Offline-Capable POS with Server Authority**.

The architecture deliberately optimizes for operational continuity without sacrificing financial and inventory correctness.

The core principle is:

> **Offline may create a pending operational transaction; only the server can establish authoritative business state.**

This decision must be reflected in the API Contract, OpenAPI schemas, Go backend sync endpoints, database reconciliation model, and POS frontend implementation.

## 45. Next Architecture Step

After this document, define:

**POS Offline Sync Contract v1**

before implementation.

It should specify:

- sync endpoint(s)
- batch size
- transaction envelope
- client/server IDs
- conflict codes
- retryability
- reconciliation response
- offline quota API
- device registration
- terminal heartbeat
- sync cursor/version
- clock synchronization
- audit correlation
- exactly-once business effect through idempotency.
