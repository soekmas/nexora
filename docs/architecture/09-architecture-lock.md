# POS Platform — ADR / Architecture Lock v1

**Status:** ACCEPTED — Architecture Baseline  
**Version:** 1.0  
**Date:** 2026-10-06  
**Scope:** Enterprise POS / Retail Platform  
**Backend:** Go  
**Frontend:** Next.js + React + TypeScript  
**Database:** PostgreSQL  
**Cache:** Redis  
**Deployment:** Docker  
**Architecture Style:** Modular Monolith, API-first, Event-ready, Offline-capable  
**Primary Authority:** PostgreSQL + domain application services

---

# 1. Purpose

Dokumen ini menjadi **Architecture Lock v1** untuk POS Platform.

Tujuannya:

1. Mengunci keputusan arsitektur yang sudah disepakati.
2. Menghilangkan ambiguity antar dokumen.
3. Mencegah implementation detail berubah menjadi architecture decision tanpa review.
4. Menjadi baseline sebelum production implementation dimulai.
5. Menjadi referensi utama untuk ADR lanjutan apabila arsitektur berubah.

Dokumen ini tidak menggantikan Blueprint, ERD, Database Design, API Contract, OpenAPI, Go Backend Architecture, Frontend Architecture, atau Offline Sync Contract.

Sebaliknya, dokumen ini mengikat keputusan lintas artifact tersebut.

---

# 2. Architecture Baseline

Urutan artifact yang menjadi baseline:

```text
BLUEPRINT v2.1
       ↓
ERD v1 Secure / Odoo Ready
       ↓
DATABASE v1.1 HARDENED
       ↓
DOMAIN + SECURITY REVIEW v1.1
       ↓
API CONTRACT v1
       ↓
GO BACKEND ARCHITECTURE v1
       ↓
OPENAPI v1
       ↓
POS FRONTEND ARCHITECTURE v1
       ↓
POS OFFLINE SYNC CONTRACT v1
       ↓
ARCHITECTURE LOCK v1
       ↓
IMPLEMENTATION
```

Tidak boleh ada implementation decision penting yang bertentangan dengan baseline ini tanpa ADR.

---

# 3. Executive Architecture Decision

POS Platform menggunakan:

```text
Modular Monolith
+
PostgreSQL System of Record
+
API-first
+
Domain-oriented boundaries
+
Transactional Outbox
+
Idempotent Commands
+
RLS + Application Authorization
+
Audit-by-default
+
Provider / ERP Adapters
+
Offline-capable POS
```

Kita **tidak** memulai dengan microservices.

Kita juga **tidak** menggunakan event sourcing sebagai primary persistence model.

---

# 4. ADR-0001 — Modular Monolith

## Context

Platform memiliki banyak domain:

- Identity
- Organization
- Master
- CRM
- Pricing
- Promotion
- Sales
- POS
- Payment
- Inventory
- Purchasing
- Finance
- Loyalty
- Workflow
- Integration
- Audit
- Reporting

Membuat microservices sejak awal akan menambah:

- distributed transactions
- service discovery
- deployment complexity
- observability complexity
- network failure modes
- eventual consistency
- operational overhead

tanpa bukti bahwa pemisahan fisik sudah diperlukan.

## Decision

Gunakan **modular monolith**.

Setiap domain mempunyai:

- ownership
- application service
- domain rules
- repository/ports
- API ownership
- test boundary

Cross-domain access dilakukan melalui:

- application port
- domain service
- explicit interface
- domain event

bukan direct table/repository access.

## Alternatives

### Microservices from day one

Rejected karena terlalu kompleks untuk tahap awal.

### Big monolith without boundaries

Rejected karena akan menghasilkan coupling dan sulit diekstrak.

## Consequences

Positive:

- simpler deployment
- simpler transaction boundary
- strong consistency where needed
- easier development
- future service extraction remains possible

Negative:

- codebase discipline becomes critical
- module boundaries must be enforced by review/testing

## Status

**ACCEPTED**

---

# 5. ADR-0002 — PostgreSQL as System of Record

## Decision

PostgreSQL adalah authoritative system of record untuk:

- sales
- payment state
- inventory
- loyalty ledger
- POS session
- master data
- workflow
- audit
- idempotency
- outbox

Redis bukan source of truth.

Frontend IndexedDB bukan source of truth.

External provider bukan source of truth internal.

## Consequence

Cache failure tidak boleh mengubah business truth.

---

# 6. ADR-0003 — Tenant Isolation

## Decision

Tenant isolation menggunakan defense-in-depth:

```text
JWT / Identity
      ↓
Application Authorization
      ↓
Tenant / Company / Store / Warehouse / Terminal Scope
      ↓
Domain Authorization
      ↓
PostgreSQL RLS
```

Database tenant context menggunakan transaction-local setting:

```sql
SET LOCAL app.tenant_id = '...';
SET LOCAL app.user_id = '...';
SET LOCAL app.store_id = '...';
```

Connection pool tidak boleh membawa context transaksi sebelumnya.

## Tenant Header Correction

Dokumen API sebelumnya menggunakan `X-Tenant-ID` sebagai required header.

Architecture Lock menetapkan:

> **Tenant authorization tidak boleh bergantung pada header client.**

Jika `X-Tenant-ID` digunakan untuk explicit tenant selection pada user yang memiliki akses multi-tenant, server harus memvalidasi bahwa tenant tersebut termasuk dalam authorized scope.

Untuk POS normal, tenant context sebaiknya diturunkan dari authenticated identity + terminal registration.

## Consequence

Client tidak dapat memilih tenant arbitrary hanya dengan mengubah header.

---

# 7. ADR-0004 — Authorization Model

Authorization dilakukan berlapis:

```text
Authentication
 ↓
Tenant
 ↓
Company
 ↓
Region
 ↓
Store
 ↓
Warehouse / Terminal
 ↓
Permission
 ↓
Business Rule
 ↓
Workflow / Approval
```

Contoh:

User memiliki:

```text
sales.void
```

tetapi tetap tidak boleh melakukan void pada store di luar scope.

## Separation of Duties

Contoh:

```text
Cashier
 ≠
Refund Approver
```

dan:

```text
Promotion Creator
 ≠
Promotion Approver
```

Privileged operation memerlukan MFA sesuai policy.

---

# 8. ADR-0005 — Command-oriented API

## Decision

Business mutation menggunakan explicit commands.

Contoh:

```text
POST /sales/orders/{id}/confirm
POST /sales/orders/{id}/void
POST /payments/{id}/process
POST /payments/{id}/refund
POST /inventory/adjustments/{id}/approve
```

Bukan generic:

```text
PATCH /sales/orders/{id}
```

untuk critical state changes.

## Consequence

State transition tetap berada di domain/application service.

---

# 9. ADR-0006 — Idempotency Standard

## Decision

Header canonical:

```http
Idempotency-Key: <stable-key>
```

Bukan:

```http
X-Idempotency-Key
```

sebagai standard utama.

`X-Idempotency-Key` dapat diterima hanya sebagai compatibility alias jika diperlukan selama migration.

## Scope

Idempotency record:

```text
tenant_id
operation
idempotency_key
request_hash
status
response
resource_type
resource_id
created_at
completed_at
```

Unique logical key:

```text
(tenant_id, operation, idempotency_key)
```

Same key + same request:

```text
return original result
```

Same key + different request:

```text
409 IDEMPOTENCY_KEY_REUSED
```

## Exactly-once

Network:

```text
at-least-once
```

Business effect:

```text
exactly-once
```

through database idempotency and unique constraints.

---

# 10. ADR-0007 — Inventory Concurrency

Inventory uses:

```text
stock balance
+
immutable stock ledger
+
reservation
```

Authoritative mutation uses database transaction and row locking/versioning.

Example:

```text
SELECT stock
FOR UPDATE
```

Then:

```text
validate available
→ update balance
→ insert ledger
→ audit
→ outbox
→ commit
```

Two concurrent purchases of the last unit must result in only one successful authoritative deduction.

Redis cannot replace database locking for inventory truth.

---

# 11. ADR-0008 — Payment Architecture

Core Sales/Payment domain does not directly call:

- Xendit SDK
- Nicepay SDK
- Midtrans SDK
- bank API
- provider-specific API

Instead:

```text
Payment Application Service
          ↓
Payment Provider Port
          ↓
Provider Adapter
```

Examples:

```text
XenditAdapter
NicepayAdapter
MidtransAdapter
BankTransferAdapter
CashAdapter
```

Provider-specific credentials and protocol remain outside core domain.

## Payment ambiguity

If a provider request may have succeeded but the response is unknown:

```text
PAYMENT_UNKNOWN
```

must enter reconciliation.

Never blindly create a second charge.

---

# 12. ADR-0009 — Transactional Outbox

Critical business transaction:

```text
Business Data
+
Audit
+
Outbox
+
Idempotency Result
```

commits atomically where applicable.

External calls happen after commit through workers.

```text
DB Commit
   ↓
Outbox
   ↓
Worker
   ↓
External System
```

ERP failure must not roll back an already completed POS transaction.

---

# 13. ADR-0010 — Audit Architecture

Audit is a **core platform capability**.

Audit records include:

```text
tenant
company
store
terminal
user
module
action
resource
business number
request_id
trace_id
IP
user agent
reason
metadata
timestamp
```

Audit categories:

```text
business action
data change
security event
login history
integration activity
approval history
```

Audit is immutable from the application role.

Sensitive information is prohibited:

```text
password
password hash
CVV
full PAN
JWT secret
refresh token
API secret
MFA secret
encryption key
```

---

# 14. ADR-0011 — Promotion Reproducibility

Promotion engine must be deterministic.

Calculation order:

```text
normalize cart
 ↓
resolve price
 ↓
eligibility
 ↓
priority
 ↓
stacking
 ↓
reward
 ↓
net amount
 ↓
next promotion
 ↓
tax
 ↓
grand total
```

Historical transaction stores enough snapshot information to reproduce the applied result:

```text
promotion_id
promotion_code
promotion_version
rule/reward snapshot
discount
price snapshot
tax snapshot
```

Promotion changes must not rewrite historical sales.

---

# 15. ADR-0012 — Offline POS

## Decision

POS is:

> **Offline-capable, server-authoritative**

Not unrestricted offline-first.

Offline capability is enabled through an explicit server-issued policy.

Offline transactions are temporary operational records until synchronized.

## Allowed by default

- product lookup from cache
- barcode
- cart
- controlled cash sale
- approved offline pricing
- explicitly cacheable promotion

## Restricted/default disabled

- online gateway payment
- refund
- loyalty redemption
- voucher redemption
- stock adjustment
- user administration
- promotion administration
- ERP operation

## Core principle

> Offline may create a pending operational transaction; only the server establishes authoritative business state.

---

# 16. ADR-0013 — Offline Sync

Offline delivery is:

```text
at-least-once
```

Business processing is idempotent.

Every syncable command has:

```text
client_transaction_id
idempotency_key
client_sequence
terminal_id
device_id
store_id
user_id
payload_version
created_at_device
```

Sync is:

- durable
- resumable
- ordered
- idempotent
- observable

Default batch:

```text
20
```

Maximum initial batch:

```text
100
```

---

# 17. ADR-0014 — Offline Conflict Handling

Conflicts are never silently resolved.

Examples:

```text
STOCK_CONFLICT
PRICE_VERSION_CONFLICT
PROMOTION_VERSION_CONFLICT
SESSION_STATE_CONFLICT
PAYMENT_UNKNOWN
DEVICE_REVOKED
OFFLINE_POLICY_EXPIRED
CLOCK_DRIFT_EXCEEDED
```

Conflict states:

```text
CONFLICT
 ↓
RECONCILIATION_REQUIRED
 ↓
RESOLVED
```

Resolution requires:

```text
resolved_by
resolved_at
resolution_code
resolution_reason
```

---

# 18. ADR-0015 — Device Identity

Device identity is separate from user identity.

```text
terminal_id
device_id
installation_id
user_id
```

A cashier may change while terminal identity remains stable.

Browser profile reset cannot automatically impersonate an existing device.

Device registration and revocation are server-controlled.

---

# 19. ADR-0016 — Offline Inventory Quota

Offline stock cannot be globally authoritative.

Use controlled quota:

```text
server stock
 ↓
offline exposure limit
 ↓
terminal quota
```

Quota is:

- versioned
- scoped
- auditable
- expirable

When synchronized, authoritative server stock determines the final outcome.

If two terminals consume the same final stock offline, one may enter:

```text
STOCK_CONFLICT
```

without silently modifying historical customer transaction data.

---

# 20. ADR-0017 — Clock Authority

Client clock is not authoritative.

Server provides:

```text
server_time
```

Client timestamps are diagnostic.

Authoritative transaction timestamps are server-generated.

Suggested:

```text
warning > 2 minutes
block > configurable threshold
```

Clock drift policy is configurable.

---

# 21. ADR-0018 — Data Deletion

Transactional business data is not physically deleted through normal APIs.

Use:

```text
cancel
void
refund
reversal
adjustment
deactivation
```

Master data can be deactivated where appropriate.

Ledgers are immutable.

Audit records are immutable.

---

# 22. ADR-0019 — ERP Integration

Core platform is ERP-agnostic.

ERP adapters:

```text
SAP B1
SAP S/4
Odoo
Future ERP
```

ERP-specific fields do not leak into core Sales/Inventory entities unless they represent genuine business concepts.

External references are maintained through integration domain.

Retry is idempotent and reconciliation-aware.

---

# 23. ADR-0020 — Reporting

Operational transactions remain normalized and authoritative.

Reporting may use:

- optimized SQL projections
- read models
- reporting schema
- future analytical warehouse

Do not introduce full CQRS/event sourcing only for reporting.

Reporting workloads must not degrade transactional POS workloads.

---

# 24. ADR-0021 — Redis

Redis may be used for:

- short-lived cache
- rate limiting
- temporary coordination
- selected distributed locks

Redis must not be authoritative for:

```text
stock
sales
payment
loyalty
finance
audit
```

---

# 25. ADR-0022 — Event Architecture

Domain/integration events use transactional outbox.

Baseline event envelope:

```text
event_type
event_version
aggregate_type
aggregate_id
occurred_at
payload
metadata
```

Event contracts are versioned.

Changing event meaning requires ADR or explicit version migration.

Internal domain events are not automatically public integration contracts.

---

# 26. ADR-0023 — API Versioning

Public API:

```text
/api/v1
```

Major breaking changes require:

```text
/api/v2
```

Backward-compatible additions remain within v1.

OpenAPI is the machine-readable public contract.

---

# 27. ADR-0024 — OpenAPI Source of Truth

OpenAPI defines:

- public paths
- HTTP methods
- request schemas
- response schemas
- headers
- security
- error contract
- examples

Implementation must conform to OpenAPI.

Internal Go interfaces do not become public API automatically.

---

# 28. ADR-0025 — Security Context

Every request has a security context:

```text
tenant_id
company_id
region_id
store_id
warehouse_id
terminal_id
user_id
request_id
trace_id
```

Context is derived and validated server-side.

For database transactions:

```sql
SET LOCAL ...
```

must occur before tenant-aware queries.

---

# 29. ADR-0026 — Error Contract

Canonical error envelope:

```json
{
  "error": {
    "code": "STOCK_CONFLICT",
    "message": "Requested quantity is no longer available",
    "field": null,
    "details": {}
  },
  "request_id": "req_123"
}
```

Error categories:

```text
AUTH_*
PERMISSION_*
TENANT_*
VALIDATION_*
*_NOT_FOUND
*_STATE_INVALID
*_CONFLICT
IDEMPOTENCY_*
INVENTORY_*
PAYMENT_*
PROMOTION_*
WORKFLOW_*
INTEGRATION_*
```

Business errors must be machine-readable.

---

# 30. ADR-0027 — Money

Money is never represented using binary floating point in persistence or core business calculations.

Use:

```text
NUMERIC / decimal semantics
```

Every monetary value has explicit:

```text
amount
currency
```

Rounding rules are centralized.

---

# 31. ADR-0028 — Time

Internal timestamps use UTC.

Store-local timezone comes from organization configuration.

Example:

```text
UTC:
2026-10-06T03:40:00Z

Store timezone:
Asia/Jakarta
```

Client timezone is not authoritative for server transaction time.

---

# 32. ADR-0029 — Database Ownership

Each domain owns its database tables logically.

Cross-domain direct writes are prohibited.

Example:

```text
Sales → Inventory Application Port
```

not:

```text
Sales → INSERT inventory.stock
```

Database schema separation is a boundary aid, not a substitute for application architecture.

---

# 33. ADR-0030 — Transaction Boundary

Transaction boundaries belong to application use cases.

Typical:

```text
BEGIN
  authorize
  set RLS context
  idempotency check
  domain command
  persist
  audit
  outbox
  idempotency result
COMMIT
```

External HTTP calls must not be held inside the core transaction unless a future ADR explicitly justifies it.

---

# 34. ADR-0031 — Background Workers

Workers handle:

- outbox delivery
- ERP integration
- notifications
- provider reconciliation
- sync support
- cleanup/retention
- reporting projection where required

Workers must be idempotent and retryable.

Poison messages require dead-letter/review behavior.

---

# 35. ADR-0032 — Observability

Every request should be correlated through:

```text
request_id
trace_id
```

Critical transaction correlation:

```text
client_transaction_id
server_transaction_id
business_number
idempotency_key
```

Metrics include:

```text
request latency
error rate
DB pool
outbox lag
sync queue
payment failures
inventory conflicts
```

Secrets and payment-sensitive information must not appear in logs.

---

# 36. ADR-0033 — Deployment

Initial deployment:

```text
Reverse Proxy
      ↓
Go API
      ↓
PostgreSQL
      +
Redis
      +
Worker
```

Docker is the baseline deployment mechanism.

API should be stateless so multiple replicas can be introduced later.

---

# 37. ADR-0034 — Scaling

Scale only after measurements.

Initial scaling order:

```text
application replicas
 ↓
DB query/index optimization
 ↓
read optimization
 ↓
worker scaling
 ↓
cache optimization
 ↓
partitioning if measured necessary
 ↓
service extraction if justified
```

Do not introduce microservices merely because traffic increases.

---

# 38. ADR-0035 — Testing Gates

Before UAT:

### Security

- tenant isolation
- RLS
- RBAC
- scope
- privileged operation
- secret leakage

### Concurrency

- last stock
- duplicate payment
- voucher redemption
- terminal session
- document numbering

### Reliability

- provider timeout
- webhook replay
- outbox retry
- duplicate command
- DB restart
- API restart

### Offline

- browser restart
- network interruption
- duplicate sync
- conflict
- payment unknown
- policy expiration
- clock drift
- queue recovery

### Data integrity

- audit
- ledger
- idempotency
- outbox
- reconciliation

---

# 39. Cross-Artifact Consistency Corrections

The following corrections are now authoritative.

## 39.1 Idempotency header

Canonical:

```http
Idempotency-Key
```

Any earlier mention of only:

```http
X-Idempotency-Key
```

is superseded.

## 39.2 Tenant header

`X-Tenant-ID` is not trusted for authorization.

It may be used only as an explicit tenant selector for an already-authorized multi-tenant identity.

For POS terminal requests, terminal/device registration and authenticated context are preferred.

## 39.3 Offline POS

Earlier Go Backend Architecture language suggesting:

> offline is not automatically enabled in v1

is superseded.

The architecture baseline is now:

> **Offline-capable is a supported architecture capability, but each terminal/store must explicitly receive an offline policy.**

Thus offline capability exists in the platform architecture, but it is not necessarily enabled for every deployment/terminal.

## 39.4 Sync capability

Offline sync is treated as a first-class platform capability.

Required endpoints are:

```text
POST /api/v1/pos/devices/register
GET  /api/v1/pos/offline-policy
GET  /api/v1/pos/offline-quota
POST /api/v1/pos/sync
GET  /api/v1/pos/sync/reconciliation/{client_transaction_id}
GET  /api/v1/pos/sync/master-data
POST /api/v1/pos/terminals/{terminal_id}/heartbeat
```

These must be incorporated into the next OpenAPI revision.

## 39.5 API/OpenAPI revision

OpenAPI v1 remains the baseline for existing endpoints.

Offline endpoints require:

```text
OpenAPI v1.1
```

before implementation.

This is a backward-compatible extension, not a breaking v2.

---

# 40. Architecture Decisions Still Intentionally Deferred

The following are **not** locked yet:

1. exact Go HTTP router package;
2. exact JWT library;
3. exact PostgreSQL migration tool;
4. exact Redis client;
5. exact IndexedDB wrapper;
6. exact Next.js state library implementation;
7. exact printer SDK;
8. exact barcode hardware;
9. exact payment provider implementation;
10. exact ERP payload mappings;
11. exact cloud/VPS topology;
12. Kubernetes adoption;
13. event broker selection such as Kafka/RabbitMQ/NATS.

These are implementation/operational decisions and may be chosen without changing the architecture baseline, unless they affect architectural invariants.

---

# 41. Architecture Changes Requiring ADR

An ADR is mandatory before:

1. changing modular monolith to microservices;
2. changing PostgreSQL as system of record;
3. removing RLS defense-in-depth;
4. bypassing domain ownership;
5. introducing distributed transactions;
6. making payment provider logic part of Sales;
7. removing idempotency from financial commands;
8. making offline stock authoritative;
9. allowing offline voucher/loyalty redemption without a new risk model;
10. replacing transactional outbox;
11. introducing event sourcing as primary persistence;
12. exposing database tables directly as public APIs;
13. changing API versioning strategy;
14. changing audit immutability;
15. allowing client-controlled business document numbers.

---

# 42. Implementation Rules

Implementation must follow:

```text
API
 ↓
Handler
 ↓
Application Use Case
 ↓
Domain
 ↓
Port
 ↓
Infrastructure
```

Forbidden:

```text
Handler → SQL
Handler → Provider SDK
Sales → Provider SDK
Sales → Inventory SQL
Frontend → Provider API
Frontend → ERP API
Worker → arbitrary table mutation
```

---

# 43. Implementation Phase Gate

Coding can begin only after:

- [x] Blueprint approved
- [x] ERD baseline
- [x] Database hardening review
- [x] API Contract
- [x] Go Backend Architecture
- [x] OpenAPI baseline
- [x] Frontend Architecture
- [x] Offline Sync Contract
- [x] Architecture Lock v1

Remaining before production implementation:

- [ ] OpenAPI v1.1 offline extension
- [ ] Offline database migration design
- [ ] final permission matrix
- [ ] final state machine definitions
- [ ] final event catalog
- [ ] final audit action catalog
- [ ] implementation repository setup
- [ ] CI/CD architecture
- [ ] test environment
- [ ] security test plan

---

# 44. Recommended Implementation Sequence

## Phase 0 — Architecture completion

1. OpenAPI v1.1 offline extension
2. Offline database design
3. state machines
4. permission matrix
5. event catalog
6. audit catalog
7. ADR index

## Phase 1 — Backend foundation

1. Go repository
2. configuration
3. HTTP server
4. PostgreSQL
5. migration
6. transaction manager
7. security context
8. RLS context
9. authentication
10. authorization
11. error model
12. idempotency
13. audit
14. outbox
15. observability

## Phase 2 — Master

1. organization
2. product
3. barcode
4. customer
5. terminal/device

## Phase 3 — Commercial

1. pricing
2. promotion
3. tax

## Phase 4 — POS core

1. POS session
2. sales
3. inventory reservation
4. cash payment
5. receipt
6. audit/outbox

## Phase 5 — Online payment

1. payment abstraction
2. provider adapter
3. webhook
4. reconciliation
5. refund

## Phase 6 — Offline POS

1. IndexedDB
2. device registration
3. offline policy
4. offline quota
5. transaction queue
6. sync API
7. conflict
8. reconciliation

## Phase 7 — Enterprise

1. purchasing
2. workflow
3. loyalty
4. voucher
5. finance
6. ERP
7. reporting

---

# 45. Definition of Architecture Done

Architecture v1 is considered locked when:

- every domain has clear ownership;
- tenant isolation is enforced;
- RLS is defense-in-depth;
- critical commands are explicit;
- idempotency is standardized;
- inventory concurrency is defined;
- payment ambiguity is defined;
- audit is immutable;
- outbox is transactional;
- ERP/provider adapters are isolated;
- offline capability has explicit policy;
- sync is idempotent;
- conflict is explicit;
- server remains authoritative;
- OpenAPI is the public contract;
- implementation boundaries are clear;
- architecture changes require ADR.

---

# 46. Final Architecture Diagram

```text
                         ┌─────────────────────────┐
                         │ POS / Admin / Ecommerce  │
                         │ Mobile / Dealer / SFA   │
                         └────────────┬────────────┘
                                      │
                                   HTTPS
                                      │
                         ┌────────────▼────────────┐
                         │      Go API Layer       │
                         │ Auth / Scope / RBAC     │
                         │ Request / Trace Context │
                         └────────────┬────────────┘
                                      │
                         ┌────────────▼────────────┐
                         │ Application Use Cases   │
                         └────────────┬────────────┘
                                      │
        ┌─────────────────────────────┼─────────────────────────────┐
        │                             │                             │
        ▼                             ▼                             ▼
     Sales                         Payment                       Inventory
        │                             │                             │
     Pricing                      Provider                       Stock
     Promotion                    Adapter                        Ledger
     Customer                     Webhook                        Reserve
        │                             │                             │
        └─────────────────────────────┼─────────────────────────────┘
                                      │
                         ┌────────────▼────────────┐
                         │      PostgreSQL         │
                         │ RLS / Transaction       │
                         │ Audit / Idempotency     │
                         │ Outbox / Ledger         │
                         └────────────┬────────────┘
                                      │
                              Transactional Outbox
                                      │
                         ┌────────────▼────────────┐
                         │       Workers           │
                         └──────┬─────────┬────────┘
                                │         │
                               ERP     Notification
                                │
                         ┌──────▼────────────┐
                         │ External Systems  │
                         │ SAP / Payment /   │
                         │ Other Integration │
                         └───────────────────┘


              OFFLINE POS PATH

       POS Terminal
            │
            ▼
       IndexedDB
            │
       Durable Queue
            │
            ▼
       Offline Sync
            │
            ▼
       Idempotent API
            │
            ▼
       Server Authority
```

---

# 47. Final Architecture Principles

The following principles are considered non-negotiable for v1:

1. **Domain owns business rules.**
2. **PostgreSQL owns authoritative transactional state.**
3. **Client never owns final financial truth.**
4. **Inventory is concurrency-controlled.**
5. **Payment is idempotent and reconcilable.**
6. **Audit is mandatory and immutable.**
7. **External calls are adapterized and asynchronous where appropriate.**
8. **Offline mode is controlled, not unrestricted.**
9. **Conflicts are explicit, never silently overwritten.**
10. **Tenant and scope authorization is server-side.**
11. **RLS is defense-in-depth.**
12. **Critical commands are explicit.**
13. **Every retryable financial operation has an idempotency strategy.**
14. **Outbox provides reliable integration delivery.**
15. **Architecture changes require ADR.**
16. **Implementation must not silently redefine architecture.**

---

# 48. Status

**ARCHITECTURE LOCK v1 — ACCEPTED**

The platform is now ready to move from architecture definition into controlled implementation planning.

The next artifacts should be:

```text
1. OpenAPI v1.1 Offline Extension
2. Offline Database Design v1
3. State Machine Catalog v1
4. Permission Matrix v1
5. Event Catalog v1
6. Audit Action Catalog v1
7. Implementation Repository / Monorepo Blueprint
8. CI/CD Architecture
9. Backend Foundation Implementation
```

No production business logic should be considered final until it conforms to this Architecture Lock.
