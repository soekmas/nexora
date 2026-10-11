# POS Platform — PostgreSQL Database Design v1

**Version:** 1.0  
**Target:** Enterprise Retail / Home Appliance POS Platform  
**Database:** PostgreSQL 16+  
**Architecture:** Modular Monolith, API-first, Event-ready, Multi-company, Multi-store, ERP-ready

## 1. Purpose

Dokumen ini menerjemahkan POS Enterprise Blueprint v2.1 dan ERD secure menjadi database design yang implementable.

Fokus utama:

- modular PostgreSQL schemas
- multi-tenant isolation
- company/store/terminal scope
- RBAC
- PostgreSQL RLS
- immutable inventory & loyalty ledger
- centralized audit trail
- payment idempotency
- transactional outbox
- workflow/approval
- business document numbering
- optimistic concurrency dengan `version`
- ERP integration readiness

## 2. Logical Schemas

```text
identity
organization
master
crm
pricing
promotion
sales
pos
payment
inventory
warehouse
purchasing
finance
loyalty
workflow
integration
audit
platform
reporting
```

## 3. Security Model

Defense in depth:

```text
Authentication
      ↓
Authorization / RBAC
      ↓
Tenant Context
      ↓
Company / Store / Terminal Scope
      ↓
Business Validation
      ↓
Repository Authorization
      ↓
PostgreSQL RLS
```

Context request diset oleh API menggunakan:

```sql
SET LOCAL app.tenant_id = '...';
SET LOCAL app.user_id = '...';
SET LOCAL app.store_id = '...';
```

Application role tidak menggunakan PostgreSQL superuser.

## 4. Core Entity Groups

### Identity

- user_account
- role
- permission
- user_role
- role_permission
- user_scope
- session
- mfa_factor
- login_attempt

### Organization

- company
- region
- store
- warehouse
- terminal

### Master

- brand
- category
- uom
- product
- product_variant
- product_barcode
- serial_number

### Commercial

- price_list
- price_rule
- promotion
- promotion_condition
- promotion_reward
- promotion_application

### Sales / POS

- sales_order
- sales_order_item
- pos.session
- pos.cash_movement

### Payment

- provider
- payment
- payment_attempt
- refund

### Inventory

- location
- stock
- stock_ledger
- stock_reservation
- stock_transfer
- stock_adjustment

### Purchasing / Finance

- supplier
- purchase_order
- goods_receipt
- tax
- accounting_mapping
- settlement

### Loyalty

- account
- transaction
- voucher
- voucher_redemption

### Workflow

- definition
- state
- transition
- instance
- approval

### Integration

- outbox_event
- delivery_attempt
- external_reference
- webhook_event
- platform.idempotency_key

### Audit

- audit_log
- data_change
- security_event
- login_history

## 5. Enterprise Audit Trail

Audit berlaku lintas modul.

Contoh business actions:

```text
PRODUCT_CREATED
PRODUCT_UPDATED
PRICE_CHANGED
PROMOTION_APPROVED
PROMOTION_ACTIVATED
SALE_CREATED
SALE_COMPLETED
SALE_VOIDED
PAYMENT_SUCCESS
PAYMENT_FAILED
PAYMENT_REFUNDED
STOCK_ADJUSTED
STOCK_TRANSFERRED
SESSION_OPENED
SESSION_CLOSED
USER_ROLE_CHANGED
PERMISSION_DENIED
```

Audit context:

```text
tenant_id
company_id
store_id
terminal_id
user_id
module
action
resource_type
resource_id
business_number
status
request_id
trace_id
ip_address
user_agent
reason
metadata
created_at
```

`audit_log`, `data_change`, `security_event`, `stock_ledger`, dan `loyalty.transaction` diperlakukan immutable.

## 6. Transaction Boundaries

### POS completion

```text
BEGIN
  validate order
  calculate price
  calculate promotion
  create sales order
  create payment
  reserve/deduct stock
  create inventory ledger
  create audit event
  create outbox event
COMMIT
```

External provider/API tidak dipanggil di dalam DB transaction.

### Inventory

Untuk stock deduction gunakan:

```text
BEGIN
SELECT stock FOR UPDATE
validate available quantity
update stock + version
insert stock_ledger
insert audit/outbox
COMMIT
```

Gunakan optimistic concurrency `version` sebagai defense tambahan.

### Payment

```text
idempotency check
    ↓
payment record
    ↓
provider reference
    ↓
provider status
    ↓
finalize order
    ↓
audit + outbox
```

Raw card number, CVV, token, password, MFA secret, dan API secret tidak disimpan dalam audit.

## 7. Immutable Data Strategy

Transactional history tidak dihapus untuk memperbaiki kesalahan.

Gunakan:

```text
VOID
CANCEL
REVERSAL
REFUND
ADJUSTMENT
CORRECTION
```

bukan hard delete atau update history.

## 8. Index Strategy

Prioritas index:

- tenant + status
- FK columns
- business number
- barcode
- SKU
- product search
- order date
- payment status
- stock lookup
- stock ledger time
- audit resource
- audit user
- audit trace
- outbox pending queue

Partitioning kandidat setelah workload terukur:

```text
sales.sales_order
sales.sales_order_item
inventory.stock_ledger
audit.audit_log
payment.payment_attempt
integration.delivery_attempt
```

Jangan melakukan partitioning hanya karena tabel terlihat besar.

## 9. RLS Strategy

RLS diterapkan minimal pada:

```text
organization.company
organization.store
organization.warehouse
organization.terminal
master.product
crm.customer
pricing.price_rule
promotion.promotion
sales.sales_order
pos.session
payment.payment
inventory.stock
inventory.stock_ledger
audit.audit_log
```

Semua tabel tenant-aware yang expose data antar tenant sebaiknya mengikuti pola yang sama.

## 10. Database Roles

Recommended:

```text
pos_app
pos_worker
pos_reporting
pos_migration
pos_readonly
pos_admin
```

Tidak boleh:

```text
application → postgres superuser
```

## 11. Business Numbering

UUID digunakan sebagai technical PK.

Business number digunakan untuk user/business process.

Contoh:

```text
POS-JKT01-20261005-000123
PO-JKT01-202610-000001
ADJ-JKT01-202610-000001
```

Jangan pernah menggunakan:

```sql
SELECT MAX(number) + 1
```

untuk numbering concurrent.

## 12. Implementation Notes

DDL yang menyertai dokumen ini adalah baseline v1. Sebelum production:

1. review naming dengan tim engineering
2. finalisasi tenant model
3. finalisasi FK lintas schema
4. finalisasi permission matrix
5. implement RLS scope hierarchy
6. implement audit event service di Go
7. implement outbox worker
8. implement migration tooling
9. load test stock/payment concurrency
10. restore test PostgreSQL backup

## 13. Important Production Rule

Database design ini sengaja menggunakan **modular monolith**, bukan microservices.

Domain boundary berada di schema dan package Go terlebih dahulu. Service extraction hanya dilakukan jika scale, ownership, atau operational requirement benar-benar membutuhkannya.
