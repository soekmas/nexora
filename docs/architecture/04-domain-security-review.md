# POS Platform — Domain & Security Review v1.1

**Status:** Architecture Hardening Review  
**Baseline:** POS PostgreSQL Database Design v1  
**Target:** Enterprise POS / Retail Platform / future Odoo-ready ERP integration  
**Database:** PostgreSQL 16+  
**Architecture:** Modular Monolith, API-first, Event-ready

---

## 1. Executive Decision

The v1 database is structurally strong enough to continue the architecture sequence, but **must not yet be treated as production/UAT-ready**.

The main reason is not missing business tables. The main risk is **security and consistency at the database boundary**:

1. Tenant IDs exist, but ordinary foreign keys can still create cross-tenant relationships.
2. RLS is enabled only on a subset of tenant-aware tables.
3. Company/store/warehouse/terminal scope is represented but not sufficiently enforced at database level.
4. Critical state transitions need explicit history and backend command ownership.
5. Payment idempotency needs request-hash semantics and webhook deduplication.
6. Inventory concurrency needs explicit reservation/ledger rules.
7. Audit needs a controlled action catalog and stronger correlation semantics.
8. Document numbering needs a first-class sequence abstraction.
9. Promotion history needs versioning/snapshot semantics so historical transactions remain reproducible.

**Architecture decision:** Continue with modular monolith. Do not introduce microservices yet. Harden the database and domain boundaries first.

---

## 2. Severity Model

| Level | Meaning |
|---|---|
| P0 | Must fix before API contract/UAT because it can cause security, financial, inventory, or data-integrity failure |
| P1 | Must harden before production; can be implemented alongside service layer |
| P2 | Valuable later; does not block current architecture |

---

## 3. P0 Findings — Must Fix Now

### P0-01 — Cross-tenant foreign-key leakage

**Finding:** A row can contain `tenant_id = A` while a normal FK points to a row belonging to tenant B.

Example:

```text
sales.sales_order.tenant_id = A
sales.sales_order.customer_id = customer(B)
```

A normal FK validates only that the customer exists. It does not validate ownership by tenant.

**Impact:** data leakage, incorrect pricing, inventory corruption, financial posting to the wrong company, and security boundary failure.

**v1.1 control:**
- Add database-level same-tenant validation for tenant-aware foreign-key relationships.
- Where practical, use composite `(tenant_id, id)` unique keys and composite foreign keys.
- Keep application authorization as a second layer.

---

### P0-02 — Incomplete RLS coverage

v1 enables RLS for selected tables but many tenant-aware transactional and master tables remain outside RLS.

**Required policy:** every table carrying `tenant_id` must either:
- have tenant RLS, or
- be explicitly classified as global/reference data with documented reason.

v1.1 therefore applies RLS consistently to tenant-aware tables and uses `FORCE ROW LEVEL SECURITY` so table ownership does not accidentally bypass policy in application contexts.

---

### P0-03 — Connection-pool tenant context

RLS depends on session settings such as:

```sql
SET LOCAL app.tenant_id = '...';
SET LOCAL app.user_id = '...';
SET LOCAL app.store_id = '...';
```

The application must set these **inside every transaction**. Never set tenant context once and assume it survives safely across pooled connections.

**Acceptance test:** tenant A request followed by tenant B request on the same pooled DB connection must never expose A data to B.

---

### P0-04 — Authorization scope is not only tenant scope

A user can belong to a tenant but still be restricted to:
- company
- region
- store
- warehouse
- terminal

`identity.user_scope` therefore must be interpreted by the authorization service, not treated as informational metadata.

The backend must deny access when the requested resource falls outside the user's effective scope.

---

### P0-05 — Critical state transitions

Do not expose generic CRUD updates for:
- sales order status
- payment status
- POS session status
- promotion lifecycle
- stock adjustment
- purchase order approval
- refund approval
- workflow approval

Use explicit domain commands/services such as:

```text
ConfirmSale
VoidSale
RequestRefund
ApproveRefund
OpenPOSSession
ClosePOSSession
ApproveStockAdjustment
CompletePayment
```

Each command creates state history and audit evidence.

---

### P0-06 — Inventory concurrency

Stock is not simply a mutable number.

The authoritative model is:

```text
stock balance
+ stock reservation
+ immutable stock ledger
```

Critical deduction flow:

1. begin transaction
2. lock stock row with `FOR UPDATE`
3. calculate available quantity
4. validate availability
5. update balance/version
6. write immutable ledger
7. write audit/event/outbox
8. commit

Never trust an application-side `SELECT available` followed later by `UPDATE` without row locking or an equivalent atomic concurrency control.

---

### P0-07 — Payment idempotency

`payment.payment` has an idempotency key, but production semantics need:

```text
same tenant + operation + idempotency key + same request hash
=> return original result

same key + different request hash
=> reject as idempotency conflict
```

Provider webhooks also need deduplication by provider/event ID and signature verification.

---

### P0-08 — Promotion reproducibility

Promotion calculation must be deterministic and historical.

Required flow:

```text
normalize cart
→ resolve price
→ eligibility
→ priority
→ stacking
→ reward
→ net-after-promo
→ next promotion
→ tax
→ grand total
```

The transaction must preserve the promotion version/configuration snapshot used at checkout.

Changing a promotion tomorrow must never change the historical result of yesterday's sale.

---

### P0-09 — Audit is a security control, not a reporting feature

Audit must cover:
- business actions
- data changes
- authentication
- authorization failures
- privileged actions
- approvals
- integration actions

Sensitive secrets must never be logged, including passwords, password hashes, MFA secrets, CVV, full card numbers, access tokens, refresh tokens, API secrets, or encryption keys.

Audit records must be immutable for the application role.

---

### P0-10 — Document numbering

Never generate business numbers using `MAX(number) + 1`.

Use PostgreSQL sequences or a locked counter abstraction with a unique constraint.

The recommended v1.1 abstraction is `platform.document_sequence`.

---

### P0-11 — Workflow/approval enforcement

Approval must be enforced by backend/domain logic, not only UI buttons.

Examples:

```text
Cashier != refund approver
Promotion creator != promotion approver
Stock adjustment creator != stock adjustment approver
User role assigner != self-escalation
```

Privileged actions require stronger authentication and security logging.

---

### P0-12 — Transaction lifecycle / deletion

Transactional records should not be hard deleted.

Use:

```text
cancel
void
refund
reversal
adjustment
```

Master records may be deactivated/discontinued. Financial and inventory ledgers remain immutable.

---

## 4. P1 Findings — Production Hardening

### P1-01 — State history

Add explicit history tables for important aggregates:
- sales order
- payment
- workflow transition
- stock reservation

This avoids depending on generic audit records to reconstruct business state.

### P1-02 — Promotion versioning

Add `promotion.promotion_version` so a promotion can have immutable executable versions.

The active transaction references the version and stores the calculation snapshot.

### P1-03 — Voucher atomic redemption

Voucher redemption must be atomic. Use a conditional update or row lock so concurrent checkout cannot exceed redemption limits.

### P1-04 — Loyalty ledger

`loyalty.transaction` remains the source of truth. Account balance is a derived/current balance and must be reconciled against the immutable ledger.

### P1-05 — Integration / Outbox

External calls must not execute inside the main database transaction.

Required pattern:

```text
business transaction
→ database commit + outbox event
→ worker
→ external API
→ retry/backoff
→ dead-letter
→ reconciliation
```

ERP failure must not silently undo a successful POS sale.

### P1-06 — ERP duplicate protection

Every outbound integration should use an internal idempotency key and maintain an external reference.

For SAP/Odoo/other ERP adapters, retries must be safe.

### P1-07 — Optimistic locking

Mutable aggregate tables should use `version` and update semantics equivalent to:

```sql
UPDATE ...
SET ..., version = version + 1
WHERE id = $1 AND version = $2;
```

Zero affected rows means a concurrency conflict.

### P1-08 — Data classification

Classify fields as public/internal/confidential/restricted. Encryption and masking requirements should follow the classification.

### P1-09 — Backup and disaster recovery

Production requires:
- encrypted backup
- offsite copy
- WAL/PITR where appropriate
- documented RPO/RTO
- scheduled restore test

A successful backup job without a successful restore test is not sufficient evidence of recoverability.

### P1-10 — Observability

Every business request should be correlatable using:

```text
request_id
trace_id
user_id
tenant_id
aggregate_id
business_number
```

The same identifiers should flow into application logs, audit, outbox, and integration attempts.

---

## 5. P2 — Later Enhancements

These should not delay API v1:

- reporting warehouse
- advanced table partitioning after workload measurement
- dedicated search engine
- automated reconciliation platform
- microservice extraction
- advanced event streaming infrastructure
- sophisticated offline conflict resolution

---

## 6. Domain-by-Domain Review

| Domain | v1 Assessment | v1.1 Decision |
|---|---|---|
| Organization | Good foundation | Add tenant relationship enforcement |
| Identity/RBAC | Good foundation | Add scope/SoD enforcement |
| Master Data | Good | Strengthen tenant ownership |
| CRM | Good | Strengthen tenant ownership |
| Pricing | Good foundation | Versioning and effective-date rules |
| Promotion | Good concept | Add immutable version/snapshot |
| Sales | Good | Add state history and command-only transitions |
| POS Session | Good | Keep unique active-session constraint; add history |
| Payment | Good foundation | Strengthen idempotency/webhooks/state history |
| Inventory | Strong model | Enforce concurrency/ledger rules |
| Purchasing | Foundation | Add approval/history controls |
| Finance | Foundation | Expand posting/reconciliation later |
| Loyalty | Good foundation | Ledger-first and atomic redemption |
| Workflow | Good foundation | Add transition history and SoD |
| Integration | Good | Outbox + retries + external refs |
| Audit | Strong concept | Add action catalog and immutable policy |
| RLS | Incomplete | Apply consistently |
| Performance | Reasonable | Index from measured query patterns |

---

## 7. Recommended v1.1 Additions

### Platform
- `platform.document_sequence`
- `platform.system_setting`

### Promotion
- `promotion.promotion_version`

### Sales
- `sales.sales_order_status_history`

### Payment
- `payment.payment_status_history`

### Inventory
- `inventory.stock_reservation_history`

### Workflow
- `workflow.transition_history`

### Audit
- `audit.audit_action_catalog`

### Identity
- `identity.permission_scope`

---

## 8. Baseline Permission Model

| Role | Sales | Void | Refund | Stock Adj. | Promo Approval | User Admin |
|---|---|---|---|---|---|---|
| Cashier | Create | No | No | No | No | No |
| Supervisor | Create | Yes | Limited | Limited | No | No |
| Store Manager | Full store sales | Yes | Yes | Yes | Limited | No |
| Finance | View | No | Approve | No | No | No |
| Warehouse | View | No | No | Yes | No | No |
| Promo Admin | View | No | No | No | Yes | No |
| System Admin | Technical | Policy-based | Policy-based | Policy-based | Policy-based | Yes |

Granular permissions should include at least:

```text
sales.create
sales.cancel
sales.void
sales.refund.request
sales.refund.approve
inventory.adjust.create
inventory.adjust.approve
promotion.create
promotion.approve
user.role.assign
audit.read
audit.export
```

---

## 9. Domain Event Baseline

```text
SalesOrderCreated
SalesOrderConfirmed
SalesOrderCompleted
SalesOrderCancelled
SalesOrderVoided
PaymentCreated
PaymentProcessing
PaymentCompleted
PaymentFailed
PaymentRefunded
StockReserved
StockReleased
StockDeducted
StockAdjusted
StockTransferred
PromotionApplied
PromotionApproved
POSSessionOpened
POSSessionClosed
PurchaseReceived
LoyaltyEarned
LoyaltyRedeemed
RefundRequested
RefundApproved
RefundCompleted
```

Event envelope:

```text
event_type
event_version
aggregate_type
aggregate_id
occurred_at
payload
```

---

## 10. Audit Action Catalog Baseline

```text
SALE_CREATED
SALE_COMPLETED
SALE_VOIDED
SALE_CANCELLED
SALE_REFUNDED
PAYMENT_CREATED
PAYMENT_SUCCESS
PAYMENT_FAILED
PAYMENT_REFUNDED
STOCK_ADJUSTED
STOCK_TRANSFERRED
STOCK_OPNAME_POSTED
PRICE_CREATED
PRICE_CHANGED
PROMOTION_CREATED
PROMOTION_APPROVED
PROMOTION_ACTIVATED
USER_CREATED
USER_DISABLED
USER_ROLE_CHANGED
LOGIN_SUCCESS
LOGIN_FAILURE
MFA_FAILURE
PERMISSION_DENIED
```

---

## 11. Acceptance Criteria Before API v1

- [ ] Cross-tenant relationship cannot be accidentally created.
- [ ] RLS prevents tenant leakage.
- [ ] Store/warehouse/terminal scope is enforced.
- [ ] Cashier cannot execute manager-only commands.
- [ ] Critical state transitions are controlled by domain commands.
- [ ] Stock deduction is concurrency-safe.
- [ ] Duplicate payment cannot create duplicate financial results.
- [ ] Duplicate webhook is harmless.
- [ ] Promotion historical result is reproducible.
- [ ] Voucher redemption is atomic.
- [ ] Loyalty ledger is immutable.
- [ ] Audit records cannot be updated/deleted by the application role.
- [ ] Document numbering is collision-free.
- [ ] ERP retry cannot create duplicate external documents.
- [ ] RLS is safe with DB pooling.
- [ ] Sensitive data is excluded/masked from logs.
- [ ] Backup restore is tested.
- [ ] Critical business actions are traceable by user + request + trace.

---

## 12. Architecture Lock After v1.1

```text
BLUEPRINT v2.1
      ↓
ERD v1
      ↓
DATABASE v1.1 HARDENED
      ↓
DOMAIN + SECURITY REVIEW
      ↓
API CONTRACT v1
      ↓
GO BACKEND DOMAIN ARCHITECTURE
      ↓
POS FRONTEND CONTRACT
      ↓
IMPLEMENTATION
```

Do not skip directly from ERD to UI coding. The API contract should be the boundary that locks domain behavior before implementation spreads across frontend and backend.

---

## 13. Next Deliverable

The next architecture artifact should be **API Contract v1**.

It should define:
- authentication/session contract
- RBAC and scope behavior
- error envelope
- pagination/filter/sort
- idempotency headers
- request/response correlation IDs
- sales commands
- payment commands/webhooks
- inventory commands
- pricing/promotion calculation contract
- POS session commands
- audit access
- workflow/approval commands
- integration endpoints

After API Contract v1, the Go backend package/domain architecture can be designed without prematurely creating microservices.
