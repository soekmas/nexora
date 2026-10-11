# POS Go Backend Architecture v1

**Status:** Proposed / Architecture Baseline  
**Version:** 1.0  
**Date:** 2026-10-06  
**Position in sequence:** Blueprint v2.1 → ERD v1 → Database v1.1 → Domain/Security Review → API Contract v1 → **Go Backend Architecture v1**

---

## 1. Executive Summary

The POS backend will be implemented as a **modular monolith in Go**, designed so that domains have explicit ownership, dependencies are controlled, transactions are consistent, and future extraction into services is possible without redesigning the business model.

The architecture is:

- Domain-first
- API-first
- Modular monolith
- Transaction-safe
- Secure-by-default
- Idempotent for retried commands
- Event-ready through transactional outbox
- ERP/payment-provider agnostic through ports/adapters
- PostgreSQL-first
- Observable through request/trace correlation
- Suitable for POS, ecommerce, mobile, dealer, marketplace, and sales-rep channels

The most important rule is:

> **HTTP handlers, workers, and integrations may invoke application use cases, but they must not bypass domain rules or write directly to another domain's tables.**

---

## 2. Architecture Goals

### 2.1 Primary goals

1. Keep business rules independent from HTTP and external providers.
2. Make each domain responsible for its own invariants.
3. Make critical commands transactional.
4. Prevent tenant and scope leakage.
5. Make duplicate requests safe.
6. Make payment webhooks safe to replay.
7. Make inventory operations concurrency-safe.
8. Make audit and outbox part of the transaction boundary.
9. Keep external API failures outside the core database transaction where possible.
10. Allow future extraction of high-volume domains without changing public contracts.

### 2.2 Non-goals for v1

- Microservices from day one.
- Distributed transactions across services.
- Event sourcing as the primary persistence model.
- CQRS infrastructure for every query.
- Kubernetes as a prerequisite.
- Generic repository abstractions for every table.
- Reflection-heavy framework magic.

---

## 3. Architectural Style

```text
                         ┌─────────────────────┐
                         │ POS / Admin / Ecom   │
                         │ Mobile / Dealer      │
                         └──────────┬──────────┘
                                    │ HTTPS
                                    ▼
                         ┌─────────────────────┐
                         │ API / Middleware     │
                         │ Auth / Context /     │
                         │ Rate Limit / Trace   │
                         └──────────┬──────────┘
                                    ▼
                    ┌─────────────────────────────────┐
                    │ Application / Use Cases          │
                    │ Commands + Queries               │
                    └──────────────┬──────────────────┘
                                   ▼
                    ┌─────────────────────────────────┐
                    │ Domain Modules                   │
                    │ Sales / Inventory / Payment ...  │
                    └──────────────┬──────────────────┘
                                   │ ports
                 ┌─────────────────┼─────────────────────┐
                 ▼                 ▼                     ▼
          PostgreSQL          Outbox/Worker       External Adapters
          Repositories        Integration         Xendit/Nicepay/SAP/etc.
```

The application is one deployable binary initially, but its internal modules must behave as if they were independently owned components.

---

## 4. Dependency Rule

### 4.1 Allowed direction

```text
Transport
   ↓
Application
   ↓
Domain
   ↓
Ports / Interfaces
   ↓
Infrastructure
```

Infrastructure may implement interfaces defined by application/domain layers. The reverse dependency is forbidden.

### 4.2 Forbidden examples

A Sales handler must not:

```text
handler → SQL
handler → Xendit SDK
handler → SAP Service Layer
handler → inventory.stock table
```

Instead:

```text
handler
  → sales application service
     → inventory reservation port
     → payment port
     → sales repository
```

### 4.3 Domain-to-domain rule

A domain should not import another domain's internal package.

Preferred:

```text
sales → inventory application port
sales → pricing application port
sales → promotion application port
```

Avoid:

```text
sales → inventory/internal/repository
sales → promotion/internal/model
```

This prevents architectural coupling and makes later service extraction realistic.

---

## 5. Recommended Repository Structure

```text
nexora/
├── cmd/
│   ├── api/
│   │   └── main.go
│   ├── worker/
│   │   └── main.go
│   └── migrate/
│       └── main.go
│
├── internal/
│   ├── platform/
│   │   ├── config/
│   │   ├── database/
│   │   ├── transaction/
│   │   ├── httpx/
│   │   ├── authx/
│   │   ├── contextx/
│   │   ├── idempotency/
│   │   ├── numbering/
│   │   ├── clock/
│   │   ├── money/
│   │   ├── pagination/
│   │   ├── observability/
│   │   ├── cryptox/
│   │   └── errors/
│   │
│   ├── identity/
│   ├── organization/
│   ├── master/
│   ├── crm/
│   ├── pricing/
│   ├── promotion/
│   ├── sales/
│   ├── pos/
│   ├── payment/
│   ├── inventory/
│   ├── warehouse/
│   ├── purchasing/
│   ├── finance/
│   ├── loyalty/
│   ├── workflow/
│   ├── integration/
│   └── audit/
│
├── migrations/
├── docs/
├── api/
│   └── openapi/
├── tests/
│   ├── integration/
│   ├── contract/
│   └── concurrency/
├── go.mod
└── go.sum
```

Each business module should have a consistent internal structure, but only where useful.

Example:

```text
internal/sales/
├── domain/
│   ├── order.go
│   ├── item.go
│   ├── status.go
│   └── errors.go
├── application/
│   ├── create_order.go
│   ├── confirm_order.go
│   ├── cancel_order.go
│   └── void_order.go
├── ports/
│   ├── repository.go
│   ├── pricing.go
│   ├── promotion.go
│   ├── inventory.go
│   └── payment.go
├── infrastructure/
│   └── postgres/
│       └── repository.go
└── transport/
    └── http/
        ├── handler.go
        └── dto.go
```

Not every domain needs identical folders. Simpler read-only modules may have fewer layers.

---

## 6. Domain Ownership

| Domain | Owns | Must not be owned elsewhere |
|---|---|---|
| identity | users, roles, permissions, sessions, MFA | authentication state |
| organization | company/store/terminal/warehouse scope | organization hierarchy |
| master | product, variant, barcode, category, UOM | product identity |
| crm | customer and addresses | customer identity |
| pricing | price lists/rules | selling price resolution |
| promotion | eligibility/reward/stacking | promotion calculation |
| sales | orders/items/status | sales lifecycle |
| pos | terminal/session/cash movement | cash session lifecycle |
| payment | payment attempts/refunds/provider state | payment lifecycle |
| inventory | stock/reservation/ledger | stock quantity truth |
| warehouse | locations/transfers/opname | warehouse movement workflow |
| purchasing | supplier/PO/goods receipt | purchasing lifecycle |
| finance | tax/accounting mapping/settlement | financial mapping |
| loyalty | points/voucher ledger | loyalty balances |
| workflow | approval/state transition | approval lifecycle |
| integration | outbox/webhook/external refs | external delivery state |
| audit | audit/security/data-change logs | audit history |

---

## 7. Application Layer

Application services represent business use cases rather than database CRUD.

Examples:

```text
CreateSalesOrder
ConfirmSalesOrder
CancelSalesOrder
VoidSalesOrder
CreatePayment
ProcessPayment
RefundPayment
ReserveStock
ReleaseStock
AdjustStock
ApprovePromotion
OpenPOSSession
ClosePOSSession
RedeemVoucher
ApproveRefund
```

Each command should:

1. Validate request shape.
2. Resolve authenticated context.
3. Check permission and scope.
4. Check idempotency where required.
5. Start a transaction when the use case requires atomicity.
6. Load aggregates/data needed for the decision.
7. Apply domain rules.
8. Persist changes.
9. Write audit record.
10. Write outbox event.
11. Commit.
12. Return a deterministic response.

---

## 8. Domain Layer

The domain layer contains business invariants, not HTTP concerns.

Example sales state machine:

```text
DRAFT
  ↓
CONFIRMED
  ↓
COMPLETED

DRAFT → CANCELLED
CONFIRMED → CANCELLED
COMPLETED → VOIDED
COMPLETED → REFUND_PENDING
REFUND_PENDING → REFUNDED
```

Invalid transitions must be rejected by the domain/application service, not only by frontend UI.

Example:

```go
func (o *Order) Confirm() error {
    if o.Status != StatusDraft {
        return ErrInvalidStateTransition
    }
    o.Status = StatusConfirmed
    return nil
}
```

---

## 9. Repository Pattern

Use repositories for aggregate/domain persistence, not a generic `Repository[T]` for every table.

Preferred:

```go
type OrderRepository interface {
    GetByID(ctx context.Context, id uuid.UUID) (*Order, error)
    Save(ctx context.Context, order *Order) error
}
```

For specialized queries, use explicit query interfaces:

```go
type OrderQuery interface {
    Search(ctx context.Context, q SearchOrdersQuery) ([]OrderSummary, error)
}
```

This keeps write-side invariants separate from read optimization.

---

## 10. Transaction Manager

The transaction manager is a core platform component.

```go
type TxManager interface {
    WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Every transaction must establish request security context before queries execute.

Conceptually:

```sql
BEGIN;
SET LOCAL app.tenant_id = '...';
SET LOCAL app.user_id = '...';
SET LOCAL app.company_id = '...';
SET LOCAL app.store_id = '...';
SET LOCAL app.warehouse_id = '...';
SET LOCAL app.terminal_id = '...';

-- business operations

COMMIT;
```

Never rely on session-level mutable tenant state when using connection pooling.

---

## 11. Security Context

Every authenticated request should produce a typed context:

```text
TenantID
UserID
CompanyID
RegionID
StoreID
WarehouseID
TerminalID
Roles
Permissions
RequestID
TraceID
ClientType
```

The application layer must treat the context as untrusted input until authentication and authorization middleware have validated it.

A client must never be able to override:

```text
tenant_id
user_id
permission
company scope
store scope
warehouse scope
```

from request JSON.

---

## 12. Authentication Flow

```text
Request
  ↓
TLS termination
  ↓
Request ID / Trace ID
  ↓
Rate limiting
  ↓
Authentication
  ↓
Session/token validation
  ↓
Load permission/scope context
  ↓
Authorization middleware/use case
  ↓
Application service
```

Privileged operations should require stronger authentication where configured, including MFA.

Passwords must never be logged.

Tokens must not be stored in raw form in database session records.

---

## 13. Authorization Model

Authorization has three layers:

### Layer 1 — Authentication

Who is calling?

### Layer 2 — Permission

What operation can they perform?

Example:

```text
sales.create
sales.void
sales.refund.request
sales.refund.approve
inventory.adjust.create
inventory.adjust.approve
promotion.approve
user.role.assign
audit.read
```

### Layer 3 — Scope

Where may they perform it?

```text
tenant
company
region
store
warehouse
terminal
```

A valid permission without valid scope is still denied.

---

## 14. HTTP Handler Rules

Handlers should be thin.

Responsibilities:

- parse request
- validate DTO shape
- extract context
- call application service
- map result to HTTP response
- map typed errors

Handlers should not:

- perform SQL
- calculate promotion
- modify stock
- call payment provider directly
- call SAP directly
- create audit rows manually outside application use case

---

## 15. Error Architecture

Use typed application/domain errors.

Example categories:

```text
VALIDATION_ERROR
UNAUTHORIZED
FORBIDDEN
NOT_FOUND
CONFLICT
INVALID_STATE
IDEMPOTENCY_CONFLICT
INSUFFICIENT_STOCK
PAYMENT_FAILED
EXTERNAL_DEPENDENCY_ERROR
RATE_LIMITED
INTERNAL_ERROR
```

HTTP mapping should be deterministic:

```text
400 validation
401 authentication
403 authorization
404 resource not found
409 state/concurrency/idempotency conflict
422 business validation
429 rate limit
500 internal
502/503 external dependency availability
```

Never expose SQL errors or stack traces to clients.

---

## 16. Idempotency Architecture

Commands that can create financial/business side effects must support idempotency.

Examples:

```text
POST /sales/orders
POST /payments
POST /payments/{id}/refund
POST /inventory/reservations
POST /loyalty/vouchers/{id}/redeem
```

Flow:

```text
Request
  ↓
Idempotency key
  ↓
Hash canonical request
  ↓
Lookup tenant + key + operation
  ↓
┌─────────────────────────────┐
│ absent → execute             │
│ same hash → return result    │
│ different hash → 409        │
└─────────────────────────────┘
```

Idempotency must be transactionally coordinated with the business operation.

---

## 17. Sales Transaction Architecture

The sale is the primary orchestration use case.

```text
Create Order
   ↓
Validate customer/store/terminal
   ↓
Resolve product
   ↓
Resolve price
   ↓
Evaluate promotion
   ↓
Calculate tax
   ↓
Validate stock
   ↓
Reserve stock
   ↓
Persist order/items
   ↓
Audit
   ↓
Outbox
   ↓
Commit
```

Payment may be completed in the same business workflow, but external provider calls must not be held inside a long database transaction.

For payment-provider operations requiring an external call, use a state machine:

```text
PENDING
  ↓
PROCESSING
  ↓
SUCCESS
```

or:

```text
PROCESSING → FAILED
PROCESSING → EXPIRED
```

---

## 18. Inventory Concurrency

Inventory is one of the highest-risk domains.

For a deduction/reservation:

```text
BEGIN
 ↓
SELECT stock row FOR UPDATE
 ↓
Check available quantity
 ↓
Update stock/reservation
 ↓
Insert stock ledger
 ↓
Insert reservation history if applicable
 ↓
Insert audit
 ↓
Insert outbox
 ↓
COMMIT
```

Never perform:

```text
SELECT available
-- application thinks it is available
UPDATE later
```

without locking or an equivalent atomic concurrency mechanism.

The application must also define retry behavior for PostgreSQL serialization/deadlock errors.

---

## 19. Promotion Engine Boundary

Promotion calculation should be a domain service.

```text
Cart
 ↓
Normalize items
 ↓
Resolve base price
 ↓
Evaluate eligibility
 ↓
Order by priority
 ↓
Apply stacking policy
 ↓
Calculate reward
 ↓
Calculate net-after-promo
 ↓
Calculate tax
 ↓
Grand total
```

Sales stores the resulting promotion application/snapshot so historical transactions remain reproducible even if promotion configuration later changes.

Promotion must not directly modify inventory or payment state.

---

## 20. Payment Provider Architecture

Define an internal payment port:

```go
type PaymentProvider interface {
    CreatePayment(ctx context.Context, req CreatePaymentRequest) (PaymentResult, error)
    GetPayment(ctx context.Context, externalID string) (PaymentResult, error)
    RefundPayment(ctx context.Context, req RefundRequest) (RefundResult, error)
    VerifyWebhook(ctx context.Context, payload []byte, headers http.Header) (WebhookEvent, error)
}
```

Adapters:

```text
payment/provider/
├── xendit
├── nicepay
├── midtrans
└── bank_transfer
```

The payment domain owns internal state. Providers own external transport details.

The Sales domain must never import an Xendit/Nicepay SDK directly.

---

## 21. Webhook Architecture

```text
Provider
   ↓
Webhook endpoint
   ↓
Signature verification
   ↓
Persist provider event / dedupe key
   ↓
Return provider acknowledgment quickly
   ↓
Worker
   ↓
Payment application service
   ↓
State transition
   ↓
Audit + outbox
```

Webhook processing must be replay-safe.

Provider event IDs must be deduplicated.

Do not perform large downstream workflows before acknowledging the webhook if provider timeout behavior makes that unsafe.

---

## 22. Transactional Outbox

Every critical domain transaction that needs asynchronous integration should write its event to `integration.outbox` in the same database transaction.

```text
Business Transaction
 ├─ Order
 ├─ Stock
 ├─ Payment state
 ├─ Audit
 └─ Outbox Event
       ↓ COMMIT

Worker
  ↓
Delivery
  ↓
External system
```

This prevents the classic failure:

```text
DB committed
External API failed
Event lost
```

Outbox delivery must support retry, exponential backoff, dead-letter state, and observability.

---

## 23. ERP Integration Architecture

ERP integration must use adapters.

```text
Integration Application
       ↓
ERP Port
       ↓
SAP B1 Adapter
SAP S4 Adapter
Odoo Adapter
```

For SAP B1, the adapter translates internal business concepts into Service Layer payloads such as:

```text
Sales Order
AR Invoice
Journal Entry
Incoming Payment
Inventory Movement
Business Partner
```

The core sales domain must not contain SAP document field mappings.

External document identifiers belong in integration/external-reference storage.

---

## 24. Audit Architecture

Audit should be generated by application services as part of the business command.

Example:

```text
VoidSale
  ↓
validate permission
  ↓
validate state
  ↓
change state
  ↓
audit SALE_VOIDED
  ↓
outbox SaleVoided
  ↓
commit
```

Audit records must contain enough correlation to answer:

```text
Who?
What?
When?
Where?
Why?
Which record?
Which request?
Which trace?
What changed?
```

Sensitive secrets must never enter audit metadata.

---

## 25. Workflow / Approval Architecture

Approval should be invoked through application commands.

Example:

```text
Cashier
  ↓
Refund Requested
  ↓
Workflow
  ↓
Manager Approval
  ↓
Refund Completed
```

The approval service verifies:

- requester identity
- approver permission
- segregation of duties
- scope
- current state
- approval rule

A user should not approve their own restricted action when policy prohibits it.

---

## 26. Worker Architecture

Worker processes should be separate from the HTTP request path, even if deployed from the same repository.

Worker responsibilities:

- outbox delivery
- webhook processing
- payment reconciliation
- ERP synchronization
- notification dispatch
- retry processing
- scheduled jobs

Worker jobs must be idempotent.

A worker crash must not corrupt business state.

---

## 27. Retry Policy

Retries must distinguish between:

### Safe transient failures

- connection timeout
- temporary DNS failure
- HTTP 429
- HTTP 502/503/504
- PostgreSQL serialization failure

### Usually non-retryable

- invalid request
- authentication failure
- business rejection
- insufficient stock
- invalid payment method
- invalid SAP payload

Use exponential backoff with jitter.

Do not retry indefinitely.

Recommended initial pattern:

```text
attempt 1: immediate
attempt 2: 2s
attempt 3: 5s
attempt 4: 15s
attempt 5: 30s
then → dead-letter / manual reconciliation
```

Exact values should be configurable.

---

## 28. PostgreSQL Access

Recommended approach:

- `pgx`
- explicit SQL or generated SQL
- PostgreSQL-native features where valuable
- migrations tracked in source control
- prepared statements where appropriate
- context-aware queries

Avoid putting an ORM between every business rule and PostgreSQL merely for convenience.

Given the complexity of RLS, locking, partial indexes, JSONB, transactional outbox, and ledger semantics, direct PostgreSQL capabilities are important.

---

## 29. Database Connection Pool

The pool must be configured conservatively and based on measured workload.

Important:

```text
application pool
      ↓
PostgreSQL
```

The application must not assume a connection retains tenant context between requests.

Every transaction sets its own security context using `SET LOCAL`.

Pool metrics should expose:

- active connections
- idle connections
- wait count
- wait duration
- max connections
- transaction duration

---

## 30. Observability

Every request should have:

```text
request_id
trace_id
user_id
tenant_id
company_id
store_id
terminal_id
```

Structured logs should include:

```json
{
  "level": "INFO",
  "event": "sales.order.completed",
  "request_id": "...",
  "trace_id": "...",
  "tenant_id": "...",
  "store_id": "...",
  "order_id": "..."
}
```

Do not log:

- password
- password hash
- JWT
- refresh token
- API secret
- MFA secret
- CVV
- full card number
- encryption keys

---

## 31. Metrics

### Business metrics

- sales orders/minute
- completed sales
- cancelled sales
- voided sales
- refunds
- payment success rate
- payment failure rate
- stock reservation failure
- promotion application rate

### Technical metrics

- request latency p50/p95/p99
- HTTP error rate
- DB latency
- transaction duration
- DB pool wait
- outbox backlog
- worker failure rate
- webhook processing latency
- external provider latency
- dead-letter count

---

## 32. Concurrency and Locking Rules

### Inventory

Use row locks / atomic updates as appropriate.

### POS session

Database constraint guarantees only one active session per terminal.

### Payment

Lock or atomically update payment state to prevent duplicate processing.

### Voucher

Atomic redemption with affected-row verification.

### Loyalty

Ledger insert is authoritative; balance is derived/maintained transactionally.

### Document numbering

Use PostgreSQL sequence/counter locking. Never `MAX(number)+1`.

---

## 33. Money Handling

Never represent monetary business values as floating point.

Use:

```text
numeric/decimal in PostgreSQL
integer minor units or decimal type in Go
```

Currency must be explicit.

Example:

```text
amount = 125000.00
currency = IDR
```

Rounding rules must be centralized and deterministic.

---

## 34. Time Handling

Store business timestamps consistently, preferably as `timestamptz`.

Application layer should use UTC internally for timestamps while preserving store/business timezone where required for business-date calculations.

For POS:

```text
transaction timestamp
business date
store timezone
```

must not be conflated.

---

## 35. API Transport

Recommended stack:

```text
net/http
router: chi or equivalent lightweight router
JSON encoding: standard encoding/json initially
OpenTelemetry instrumentation
pgx PostgreSQL driver
```

Framework choice should remain lightweight. Avoid introducing a large framework that hides transaction and context behavior.

---

## 36. Configuration

Configuration must be environment-driven and validated at startup.

Examples:

```text
DATABASE_URL
DATABASE_MAX_CONNS
DATABASE_MIN_CONNS
JWT_ISSUER
JWT_AUDIENCE
SESSION_TTL
REDIS_URL
XENDIT_SECRET_REF
NICEPAY_SECRET_REF
SAP_SERVICE_URL
OUTBOX_BATCH_SIZE
```

Secrets should come from a secret manager/environment mechanism, not Git.

---

## 37. Caching

Redis may be used for:

- short-lived cache
- rate limiting
- distributed locks only where justified
- session/cache acceleration if architecture requires it

Redis must not become the source of truth for:

- stock quantity
- payment state
- sales orders
- loyalty ledger
- financial ledger

PostgreSQL remains authoritative.

---

## 38. Read Optimization

Queries that serve dashboards/searches may use optimized read models or SQL projections.

Do not force aggregate loading for every query.

Example:

```text
GET /sales/orders
```

can use a projection/query model rather than loading the full SalesOrder aggregate.

This is pragmatic CQRS, not full event sourcing.

---

## 39. Offline POS Strategy

Offline POS is not automatically enabled in v1.

If enabled later, it requires explicit conflict rules for:

- document numbering
- stock
- promotion validity
- payment
- customer updates
- loyalty
- synchronization

The online architecture must not pretend that offline sync is trivial.

Recommended initial strategy: online-first with controlled retry.

---

## 40. Testing Architecture

Testing levels:

### Unit tests

- domain state transitions
- pricing
- promotion rules
- tax
- money rounding
- permission decisions

### Integration tests

- PostgreSQL
- RLS
- transactions
- repository behavior
- outbox
- webhook dedupe

### Concurrency tests

- last-stock purchase
- duplicate payment
- duplicate voucher redemption
- simultaneous POS session opening
- sequence generation

### Contract tests

- API responses
- provider adapters
- webhook contracts
- ERP adapters

### End-to-end tests

```text
Login → POS session → Sale → Payment → Stock → Audit → Outbox
```

---

## 41. Critical Concurrency Test Example

For stock quantity = 1:

```text
Request A ─────┐
               ├── reserve item
Request B ─────┘
```

Expected:

```text
A = success
B = insufficient stock
```

Never:

```text
A = success
B = success
stock = -1
```

This test is mandatory before UAT.

---

## 42. Deployment Model

Initial deployment:

```text
                 ┌───────────────┐
Internet ───────►│ Reverse Proxy │
                 └───────┬───────┘
                         │
                 ┌───────▼───────┐
                 │ Go API         │
                 └───────┬───────┘
                         │
             ┌───────────┼───────────┐
             ▼           ▼           ▼
        PostgreSQL     Redis       Worker
```

Docker is recommended.

The API and worker may initially use the same image with different commands.

---

## 43. Horizontal Scaling

The API should be stateless except for externalized state.

Therefore:

```text
              Load Balancer
               /    |    \
             API   API   API
               \    |    /
                PostgreSQL
```

Session state, idempotency, outbox, and business state remain externalized.

Do not rely on in-memory state for correctness.

---

## 44. Graceful Shutdown

On SIGTERM:

1. stop accepting new requests
2. allow active requests to finish within timeout
3. stop worker intake
4. finish/mark in-flight jobs safely
5. close database pool
6. close telemetry exporters
7. exit

Shutdown must not acknowledge work that was never durably committed.

---

## 45. Security Baseline

Mandatory:

- TLS
- secure authentication
- MFA for privileged roles
- password hashing with modern adaptive algorithm
- secure session/token handling
- authorization + scope
- RLS defense in depth
- input validation
- output encoding where applicable
- rate limiting
- webhook signature verification
- secret management
- audit trail
- security event logging
- dependency scanning
- SQL injection prevention
- no sensitive data in logs

---

## 46. Domain Dependency Matrix

```text
                 Depends on
Sales       → Master, CRM, Pricing, Promotion, Inventory, Payment, POS
POS         → Organization, Identity
Payment     → Organization, Integration
Inventory   → Master, Organization, Warehouse
Promotion   → Master, Pricing
Pricing     → Master, Organization
Purchasing  → Master, Organization, Warehouse, Finance
Finance     → Organization, Master
Loyalty     → CRM, Sales
Workflow    → Identity, Organization
Integration → all domains through events/ports
Audit       → platform/security context
```

Dependency direction must not create cycles.

If a cycle appears, introduce an application port/event instead of importing internals.

---

## 47. Anti-Patterns Explicitly Forbidden

### Generic service god object

```text
POSService
  ├─ sales
  ├─ inventory
  ├─ payment
  ├─ finance
  └─ everything
```

### Generic repository for all tables

```text
Repository[T]
```

when it hides business semantics.

### Controller business logic

### Direct SQL from handlers

### Direct provider SDK calls from Sales

### Cross-domain table writes

### Long-running DB transactions around HTTP provider calls

### In-memory stock truth

### `MAX(number)+1`

### Trusting client-supplied tenant/store/user IDs

### Logging secrets

### Catch-all retry loops

---

## 48. Application Command Example

Conceptually:

```go
type CreateOrderCommand struct {
    CustomerID *uuid.UUID
    StoreID    uuid.UUID
    TerminalID uuid.UUID
    Items      []OrderItemCommand
    Payments   []PaymentCommand
}
```

Application service:

```go
func (s *Service) CreateOrder(
    ctx context.Context,
    cmd CreateOrderCommand,
) (*OrderResult, error) {
    // authorization
    // idempotency
    // transaction
    // pricing
    // promotion
    // inventory
    // persistence
    // audit
    // outbox
}
```

The service should orchestrate; detailed invariants stay in domain objects/services.

---

## 49. API-to-Use-Case Mapping

| API | Application use case |
|---|---|
| POST /sales/orders | CreateOrder |
| POST /sales/orders/{id}/confirm | ConfirmOrder |
| POST /sales/orders/{id}/cancel | CancelOrder |
| POST /sales/orders/{id}/void | VoidOrder |
| POST /payments | CreatePayment |
| POST /payments/{id}/process | ProcessPayment |
| POST /payments/{id}/refund | RequestRefund / RefundPayment |
| POST /inventory/reservations | ReserveStock |
| DELETE /inventory/reservations/{id} | ReleaseStock |
| POST /inventory/adjustments | CreateAdjustment |
| POST /inventory/adjustments/{id}/approve | ApproveAdjustment |
| POST /promotions/{id}/approve | ApprovePromotion |
| POST /pos/sessions | OpenSession |
| POST /pos/sessions/{id}/close | CloseSession |
| POST /vouchers/{id}/redeem | RedeemVoucher |

The API contract remains stable while implementation can evolve internally.

---

## 50. ADR Requirements

Architecture decisions that can materially affect future modules must be recorded as ADRs.

Initial ADR candidates:

```text
ADR-0001 Modular Monolith
ADR-0002 PostgreSQL as System of Record
ADR-0003 RLS + Application Authorization
ADR-0004 Transactional Outbox
ADR-0005 Payment Provider Abstraction
ADR-0006 ERP Adapter Architecture
ADR-0007 Inventory Concurrency Strategy
ADR-0008 Idempotency Strategy
ADR-0009 Audit Architecture
ADR-0010 Offline POS Scope
```

Format:

```text
Title
Context
Decision
Alternatives
Consequences
Status
```

---

## 51. Implementation Order

### Phase 1 — Platform foundation

- configuration
- logging
- tracing
- HTTP server
- PostgreSQL pool
- migrations
- transaction manager
- security context
- error model
- idempotency

### Phase 2 — Identity / organization

- authentication
- authorization
- tenant/store scope
- session
- MFA foundations

### Phase 3 — Master / CRM

- product
- barcode
- customer
- organization references

### Phase 4 — Commercial

- pricing
- promotion
- tax

### Phase 5 — POS / Sales

- POS session
- cart
- sales order
- stock reservation
- payment orchestration

### Phase 6 — Inventory

- stock
- ledger
- transfer
- adjustment
- opname

### Phase 7 — Integration

- outbox
- worker
- webhook
- payment adapters
- ERP adapter

### Phase 8 — Loyalty / Workflow / Finance expansion

- loyalty
- vouchers
- approvals
- settlement
- accounting integration

---

## 52. Definition of Done for Backend Foundation

Backend foundation is not considered complete until:

- [ ] API can authenticate a user.
- [ ] Tenant context is established.
- [ ] Store/warehouse/terminal scope is enforced.
- [ ] PostgreSQL transaction helper works.
- [ ] RLS context is set using `SET LOCAL`.
- [ ] Audit is written transactionally.
- [ ] Outbox is written transactionally.
- [ ] Idempotency works for commands.
- [ ] Typed errors map correctly to HTTP.
- [ ] Structured request/trace logging works.
- [ ] Sensitive data is excluded from logs.
- [ ] Database connection pool is observable.
- [ ] Graceful shutdown works.
- [ ] Migration process is repeatable.
- [ ] Integration test database is reproducible.

---

## 53. Definition of Done for Sales

- [ ] Create sale works.
- [ ] Duplicate request does not create duplicate sale.
- [ ] Price snapshot is stored.
- [ ] Promotion result is reproducible.
- [ ] Stock reservation is atomic.
- [ ] Payment state is deterministic.
- [ ] Sale state transitions are enforced.
- [ ] Audit is recorded.
- [ ] Outbox event is recorded.
- [ ] Concurrent checkout test passes.
- [ ] Failed external provider does not corrupt sale state.

---

## 54. Architecture Acceptance Criteria

The architecture is accepted when:

1. No domain requires direct access to another domain's repository internals.
2. Critical commands have explicit application services.
3. State transitions are enforced outside the frontend.
4. Tenant and scope are enforced at application and database levels.
5. Critical writes are idempotent.
6. Inventory is concurrency-safe.
7. Payment provider calls are isolated behind ports/adapters.
8. ERP integration is isolated behind adapters.
9. Outbox events are transactionally coupled to business changes.
10. Audit is transactionally coupled to critical actions.
11. Worker processing is replay-safe.
12. API handlers remain thin.
13. PostgreSQL remains the source of truth.
14. The architecture can later extract a domain without redesigning the public business contract.

---

## 55. Next Artifact

The next artifact after this architecture baseline is:

**OpenAPI Specification v1**

It should translate the existing API Contract into machine-readable OpenAPI YAML/JSON, including:

- security schemes
- request schemas
- response schemas
- error schemas
- pagination
- idempotency headers
- correlation headers
- endpoint tags
- examples
- webhook schemas
- versioning

Only after OpenAPI v1 should the frontend contract and implementation begin.

---

# Architecture Lock

```text
BLUEPRINT v2.1
      ↓
ERD v1
      ↓
DATABASE v1.1 HARDENED
      ↓
DOMAIN + SECURITY REVIEW v1.1
      ↓
API CONTRACT v1
      ↓
GO BACKEND ARCHITECTURE v1   ← CURRENT
      ↓
OPENAPI v1
      ↓
POS FRONTEND CONTRACT
      ↓
IMPLEMENTATION
```

**Status:** Proposed for review.  
**Next decision gate:** approve Go backend architecture and create OpenAPI Specification v1.
