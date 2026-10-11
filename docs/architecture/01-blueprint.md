# POS Platform Blueprint v2.1 — Enterprise Odoo-Ready Architecture

**Version:** 2.1  
**Status:** Architecture Blueprint  
**Target:** Enterprise Retail / Home Appliance POS Platform  
**Architecture:** API-first, Modular Monolith, Event-ready, Multi-store, ERP-ready, Security-first

## 1. Architecture Vision

Platform tidak dirancang sebagai "aplikasi kasir", tetapi sebagai **Retail Business Platform**. POS adalah salah satu channel yang menggunakan business engine.

```text
                    BUSINESS PLATFORM
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
       POS              ECOMMERCE          MOBILE
        │                  │                  │
        └──────────────────┼──────────────────┘
                           │
                    BUSINESS ENGINE
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
      SALES            INVENTORY         COMMERCIAL
        │                  │                  │
        │                  │           ┌──────┴──────┐
        │                  │        PRICING       PROMO
        │                  │
        └──────────────────┼──────────────────┘
                           │
                      FINANCE LAYER
                           │
                    INTEGRATION LAYER
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
             SAP          ODOO        OTHER ERP
```

**Prinsip utama:** POS adalah channel; business logic berada di domain engine.

## 2. Strategic Goals

1. POS retail modern.
2. Multi-company dan multi-store.
3. Centralized Pricing Engine.
4. Centralized Promotion Engine.
5. Inventory berbasis ledger.
6. Payment abstraction.
7. Offline POS.
8. Serial number dan warranty.
9. Loyalty, voucher, gift card, eWallet.
10. Integrasi SAP/ERP.
11. Evolusi menuju Retail ERP.
12. Tidak bergantung pada satu payment provider atau ERP.
13. Business rules tidak berada di frontend.

## 2.1 Enterprise Audit & Traceability Requirement

Audit Trail adalah **core platform capability**, bukan fitur tambahan. Setiap modul yang menghasilkan perubahan bisnis atau security-sensitive action wajib menghasilkan audit event yang dapat ditelusuri berdasarkan user, waktu, resource, request, dan transaction context.

### Audit scope

Audit berlaku lintas modul: Identity & Access, Organization, Master Data, CRM, Pricing, Promotion, Sales, POS, Payment, Inventory, Warehouse, Purchasing, Finance, Loyalty, Workflow, Integration, Configuration, dan Reporting.

Contoh business actions:

```text
USER_CREATED / ROLE_CHANGED / LOGIN_SUCCESS / LOGIN_FAILURE
PRODUCT_CREATED / PRODUCT_UPDATED / PRICE_CHANGED
PROMOTION_APPROVED / PROMOTION_ACTIVATED
SALE_CREATED / SALE_COMPLETED / SALE_VOIDED / SALE_REFUNDED
PAYMENT_SUCCESS / PAYMENT_FAILED / PAYMENT_REFUNDED
STOCK_RECEIVED / STOCK_ADJUSTED / STOCK_TRANSFERRED
SESSION_OPENED / SESSION_CLOSED / CASH_ADJUSTED
PURCHASE_APPROVED / GOODS_RECEIVED
ERP_SYNC_SUCCESS / ERP_SYNC_FAILED / WEBHOOK_RECEIVED
PERMISSION_DENIED / MFA_FAILURE / SESSION_REVOKED
```

### Audit domains

```text
audit
├── audit_log       # business activity / actor / resource / context
├── data_change     # field-level before/after changes
├── security_event  # authentication and security events
└── login_history   # login/session history
```

### Audit context

Setiap critical request membawa minimal `tenant_id`, `company_id`, `store_id`, `terminal_id`, `user_id`, `request_id`, `trace_id`, `resource_type`, `resource_id`, timestamp, status, IP address, dan user agent sesuai kebutuhan. Sensitive data wajib dimasking. Password, CVV, full card number, access token, refresh token, API secret, MFA secret, dan encryption key tidak boleh disimpan di audit log.

### Immutability & access

Audit log bersifat append-only. Application role tidak boleh melakukan UPDATE/DELETE terhadap audit records. Koreksi histori dilakukan melalui event baru, bukan mengubah histori lama. Akses audit dibatasi dengan RBAC dan user scope; security/audit administrator dipisahkan dari operational cashier permission.

### Audit architecture

```text
User / POS / Admin
       ↓
     Go API
       ↓
 Domain Service
       ├── Business Transaction
       ├── Audit Event
       └── Outbox Event
              ↓
          PostgreSQL
              ↓
      Async Publisher / SIEM / Reporting
```

Application layer bertanggung jawab terhadap business audit context; PostgreSQL trigger dapat digunakan secara selektif untuk critical data-change evidence. Audit harus tetap memiliki request/trace correlation dengan domain event dan integration event.

## 3. Architecture Principles

- **Domain First** — domain business menjadi pusat desain.
- **Channel Agnostic** — POS, ecommerce, mobile, dealer, dan marketplace menggunakan business engine yang sama.
- **API First** — frontend memanggil API.
- **Event Ready** — event dapat diterbitkan tanpa langsung menjadi full microservices.
- **Modular Monolith First** — mulai sederhana, pisahkan service hanya ketika dibutuhkan.
- **Idempotent** — operasi penting aman terhadap duplicate request.

## 4. Capability Map

```text
PLATFORM
├── Identity & Access
├── Organization
│   ├── Company
│   ├── Store
│   ├── Warehouse
│   └── Terminal
├── Master Data
│   ├── Product
│   ├── Customer
│   ├── Supplier
│   ├── UOM
│   ├── Category
│   └── Brand
├── Commercial
│   ├── Pricing
│   ├── Promotion
│   ├── Discount
│   ├── Voucher
│   ├── Loyalty
│   ├── Gift Card
│   └── eWallet
├── Sales
│   ├── Quotation
│   ├── Sales Order
│   ├── POS Order
│   ├── Delivery
│   ├── Return
│   └── Invoice
├── Inventory
│   ├── Stock
│   ├── Reservation
│   ├── Warehouse
│   ├── Transfer
│   ├── Picking
│   ├── Adjustment
│   └── Stock Opname
├── Purchasing
│   ├── Purchase Request
│   ├── Purchase Order
│   ├── Goods Receipt
│   └── Supplier Return
├── Finance
│   ├── Tax
│   ├── Payment
│   ├── Settlement
│   ├── AR
│   ├── Journal
│   └── Accounting Mapping
├── Integration
│   ├── ERP
│   ├── Payment Gateway
│   ├── Ecommerce
│   ├── Shipping
│   └── Notification
└── Platform Services
    ├── Workflow
    ├── Audit
    ├── Notification
    ├── Queue
    ├── Search
    └── Configuration
```

## 5. Module Lifecycle

- **M0:** Future
- **M1:** MVP
- **M2:** Retail
- **M3:** Enterprise
- **M4:** ERP

### MVP
Identity, Company, Store, Terminal, Product, Customer, Pricing, Promotion, Sales, Payment, Inventory, POS Session, Receipt, Refund, Audit, Reporting.

### Retail
Barcode, Serial, Warranty, Advanced Inventory, Loyalty, Voucher, Gift Card, eWallet, Offline POS, Settlement.

### Enterprise
Sales Order, Quotation, Delivery, Purchase, Warehouse, Approval, Tax, Advanced Reporting, ERP Integration.

### ERP
Accounting, AR, AP, Journal, Bank, Reconciliation, Manufacturing, CRM, Asset, HR, Expense, Maintenance, Helpdesk.

## 6. Core Domain Model

```text
Company
Store
Warehouse
Location
Terminal
User
Customer
Supplier
Product
ProductVariant
ProductCategory
Brand
UOM
Barcode
SerialNumber
Lot
PriceList
PriceRule
Promotion
PromotionCondition
PromotionFormula
PromotionReward
PromotionApplication
Cart
SalesOrder
SalesOrderItem
Payment
PaymentTransaction
Refund
Stock
StockLedger
StockReservation
StockTransfer
StockAdjustment
StockOpname
POSSession
CashMovement
Settlement
Voucher
LoyaltyAccount
LoyaltyTransaction
GiftCard
Wallet
PurchaseRequest
PurchaseOrder
GoodsReceipt
Invoice
Tax
JournalEntry
AccountingMapping
IntegrationEvent
IntegrationLog
AuditLog
```

## 7. Domain Ownership

| Entity | Domain |
|---|---|
| Product | Master |
| Customer | CRM |
| PriceList | Pricing |
| Promotion | Promotion |
| SalesOrder | Sales |
| Payment | Payment |
| Stock | Inventory |
| POSSession | POS |
| PurchaseOrder | Purchasing |
| Invoice | Finance |
| JournalEntry | Finance |
| IntegrationEvent | Platform |

Satu domain memiliki ownership terhadap state entity-nya.

## 8. Sales Domain

Sales menjadi domain generik. POS membuat Sales Order dengan `source = POS`; ecommerce dan mobile menggunakan Sales Engine yang sama.

```text
                 SALES ENGINE
                       │
       ┌───────────────┼───────────────┐
       ▼               ▼               ▼
      POS          ECOMMERCE         MOBILE
       │               │               │
       └───────────────┼───────────────┘
                       ▼
                  SALES ORDER
```

Sales channel dapat berupa POS, ECOMMERCE, MOBILE, DEALER, MARKETPLACE, SALES_REP.

## 9. Pricing Engine

Input: Product, Customer, Customer Group, Store, Channel, Quantity, Date, Time, Currency.

Output: Base Price, Applied Price Rule, Unit Price.

Priority contoh:

```text
Customer Specific
      ↓
Customer Group
      ↓
Store
      ↓
Channel
      ↓
Standard
```

Semua rule memiliki `effective_from`, `effective_to`, `priority`, dan `status`.

## 10. Promotion Engine

Promotion engine reusable untuk POS, ecommerce, mobile, dealer, dan marketplace.

```text
Cart
 ↓
Normalize Items
 ↓
Resolve Base Price
 ↓
Evaluate Eligibility
 ↓
Sort Promotion Priority
 ↓
Apply Promotion
 ↓
Check Stackability
 ↓
Recalculate Net Amount
 ↓
Evaluate Next Promotion
 ↓
Tax
 ↓
Final Total
```

Promotion dapat dimodelkan sebagai data, misalnya:

```json
{
  "condition": {"type": "MINIMUM_AMOUNT", "value": 10000000},
  "reward": {"type": "DISCOUNT_PERCENT", "value": 2},
  "stackable": true,
  "priority": 10
}
```

Ini memungkinkan penambahan promo tanpa selalu melakukan deployment backend.

## 11. Inventory Domain

Gunakan tiga komponen utama:

```text
Stock Balance
+
Stock Ledger
+
Reservation
```

`Available = On Hand - Reserved`

Ledger types dapat mencakup `PURCHASE_RECEIPT`, `SALE`, `RETURN`, `TRANSFER_IN`, `TRANSFER_OUT`, `ADJUSTMENT`, `OPNAME`, `DAMAGE`, `SERVICE`.

## 12. Warehouse & Serial/Warranty

Warehouse memiliki Location, misalnya Main, Display, Damaged. Untuk home appliance:

```text
Product
 ↓
Serial Number
 ↓
Stock
 ↓
Sales
 ↓
Customer
 ↓
Warranty
```

Lifecycle serial: `RECEIVED → AVAILABLE → RESERVED → SOLD → RETURNED/DAMAGED/SERVICE/SCRAPPED`.

## 13. Purchase & Delivery

Purchase:

```text
Purchase Request
 ↓
Purchase Order
 ↓
Goods Receipt
 ↓
Inventory
 ↓
Supplier Invoice
 ↓
Accounting
```

Delivery:

```text
Sales Order
 ↓
Delivery Order
 ↓
Picking
 ↓
Shipment
 ↓
Stock Deduction
```

POS dapat menggunakan immediate delivery atau ship-later.

## 14. Finance Boundary

MVP: Tax, Payment, Settlement, Accounting Mapping.

Future: Invoice, AR, AP, Journal, Bank, Reconciliation.

POS tidak perlu menjadi full accounting system pada fase awal.

## 15. Payment Abstraction

```text
PaymentProvider
├── CreatePayment
├── GetStatus
├── Cancel
├── Refund
└── Verify
```

Provider dapat berupa Xendit, Midtrans, Nicepay, Bank, QRIS, EDC, Cash, dan provider lain.

## 16. ERP Adapter Architecture

```text
                 ERP Integration
                       │
          ┌────────────┼────────────┐
          ▼            ▼            ▼
       SAP B1       SAP S/4      Odoo ERP
       Adapter       Adapter       Adapter
```

Core platform tidak mengetahui detail ERP.

## 17. Event Architecture

Gunakan transactional outbox:

```text
Business Transaction
        ↓
Database Transaction
        ↓
Outbox Event
        ↓
Event Publisher
        ↓
Queue
        ↓
Integration / Async Worker
```

Contoh event: `SalesOrderCreated`, `SalesOrderCompleted`, `PaymentCompleted`, `StockDeducted`, `RefundCompleted`, `PromotionApplied`, `PurchaseReceived`.

## 18. Idempotency

Critical operation harus memiliki idempotency key seperti `order_id`, `payment_id`, `integration_id`, dan `event_id`.

Duplicate request harus mengembalikan hasil yang sudah ada, bukan membuat transaksi kedua.

## 19. Workflow & Approval

Generic workflow:

```text
Document
 ↓
State
 ↓
Action
 ↓
Permission
 ↓
Approval
 ↓
Next State
```

Contoh refund: `DRAFT → REQUESTED → APPROVED → REFUNDED`.

## 20. Database Architecture

PostgreSQL dengan logical schema boundary:

```text
identity
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
integration
workflow
audit
reporting
```

Gunakan ID yang scalable (UUID/ULID sesuai kebutuhan), timestamps, actor fields, version, dan status. Transactional data sebaiknya immutable jika memungkinkan.

## 21. API Boundary

```text
/api/v1/auth
/api/v1/master
/api/v1/products
/api/v1/customers
/api/v1/pricing
/api/v1/promotions
/api/v1/sales
/api/v1/pos
/api/v1/payments
/api/v1/inventory
/api/v1/warehouse
/api/v1/purchasing
/api/v1/finance
/api/v1/loyalty
/api/v1/integrations
/api/v1/reports
```

API versioning disiapkan dari awal.

## 22. Backend & Frontend Architecture

Recommended backend:

```text
Go
 ├── HTTP / REST
 ├── Domain Services
 ├── Repository
 ├── Event Publisher
 ├── Worker
 └── Integration Adapter
```

Frontend:

```text
/apps/pos
/apps/admin
/apps/backoffice
/packages/ui
/packages/api-client
/packages/types
```

Future: `/apps/ecommerce` dan `/apps/mobile`.

## 23. Offline Architecture

```text
Next.js POS
     │
Service Worker
     │
IndexedDB
     │
Offline Queue
     │
Sync Engine
     │
API
```

Critical entities seperti payment dan stock tidak boleh menggunakan blind last-write-wins untuk conflict resolution.

## 24. Reporting & Observability

OLTP: PostgreSQL. Untuk laporan dapat digunakan read replica, materialized view, atau reporting schema. Future: ETL/CDC ke data warehouse.

Setiap request penting membawa `request_id`, `trace_id`, `user_id`, `company_id`, `store_id`, `terminal_id`, dan `order_id`.

## 25. Security

Authentication → Authorization → Tenant/Company Context → Store Context → Permission → Business Validation.

Gunakan HTTPS, secure session/token handling, password hashing, RBAC, MFA untuk privileged user, tenant/company/store scope, PostgreSQL RLS untuk defense-in-depth, rate limiting, validation, secret management, centralized immutable audit trail, security event logging, encryption sesuai kebutuhan, API key rotation, session revocation, dan least-privilege database roles.

Security requirement: application database role tidak menggunakan superuser/postgres dan tidak boleh mengubah atau menghapus audit/ledger records. Critical transactional data seperti payment, stock ledger, loyalty ledger, completed sales, integration event, dan audit log menggunakan append-only/reversal pattern.

## 26. Multi-Company / Multi-Store

```text
Company
  ↓
Region
  ↓
Store
  ↓
Warehouse
  ↓
Terminal
```

User scope dapat Company-wide, Region, Store, atau Terminal.

## 27. Tax & Currency Readiness

Tax menggunakan `Tax Category → Tax Rule → Tax Rate → Effective Date`. Jangan hard-code tarif.

Currency disiapkan dengan `currency`, `exchange_rate`, `price_currency`, dan `transaction_currency`, meskipun fase awal hanya IDR.

## 28. Loyalty, Gift Card & eWallet

Loyalty menggunakan ledger:

```text
EARN
REDEEM
EXPIRE
ADJUST
REFUND_REVERSAL
```

Gift Card dan eWallet juga menggunakan ledger, bukan hanya balance field.

## 29. Settlement

```text
POS Payment
      ↓
Gateway
      ↓
Settlement
      ↓
Bank
      ↓
Reconciliation
```

Simpan gross, fee, tax, net, settlement date, dan provider reference.

## 30. Odoo Capability Mapping

| Odoo-like Capability | Our Module | Phase |
|---|---|---|
| POS | POS | M1 |
| Pricelist | Pricing | M1 |
| Promotions | Promotion | M1/M2 |
| Loyalty | Loyalty | M2 |
| Coupons | Voucher | M2 |
| Gift Card | Gift Card | M2 |
| eWallet | Wallet | M2 |
| Barcode | Barcode | M2 |
| Serial | Serial | M2 |
| Inventory | Inventory | M1/M2 |
| Warehouse | Warehouse | M2 |
| Delivery | Sales/Delivery | M3 |
| Purchase | Purchasing | M3 |
| Accounting | Finance | M4 |
| CRM | CRM | M4 |
| Manufacturing | Manufacturing | Future |
| HR | HR | Future |

## 31. What Not to Build Initially

Jangan membuat full Accounting, HR, Payroll, Manufacturing, CRM, Project Management, atau Helpdesk sebelum POS dan retail engine stabil.

## 32. MVP Architecture

```text
Next.js POS
      │
      ▼
     Go API
      │
 ┌────┼───────────────┐
 │    │               │
Sales Pricing      Promotion
 │    │               │
 └────┼───────────────┘
      │
 Inventory
      │
 Payment
      │
PostgreSQL
      │
 Redis
```

Docker services awal: `nginx`, `app`, `worker`, `postgres`, `redis`.

## 33. MVP Modules

Identity, Organization, Product, Customer, Pricing, Promotion, POS, Sales, Payment, Inventory, Session, Receipt, Refund, Audit, Reporting.

## 34. Roadmap

### Phase 2 — Retail Platform

Barcode, Serial, Warranty, Advanced Inventory, Warehouse, Transfer, Stock Opname, Loyalty, Voucher, Gift Card, eWallet, Settlement, Offline.

### Phase 3 — Distribution Platform

Quotation, Sales Order, Delivery, Purchase, Supplier, Goods Receipt, Replenishment, Commission, Advanced Approval.

### Phase 4 — ERP Platform

Accounting, AR, AP, GL, Tax, Bank, Reconciliation, CRM, Asset, Maintenance.

### Phase 5 — Advanced ERP

Manufacturing, MRP, BOM, Work Order, Quality, HR, Payroll, Expense, Project, Helpdesk.

## 35. Sprint Sequence

1. Foundation
2. Master Data
3. Pricing Engine
4. Promotion Engine
5. POS Cart & Sales
6. Payment
7. Inventory
8. POS Session & Cash
9. Refund & Return
10. Audit, Security & Reporting
11. Barcode / Serial
12. Advanced Inventory
13. Loyalty / Voucher
14. Offline POS
15. Settlement
16. ERP Integration

## 36. Core Transaction Flow

```text
Cashier
   ↓
POS
   ↓
Create Cart
   ↓
Pricing Engine
   ↓
Promotion Engine
   ↓
Stock Validation
   ↓
Create Sales Order
   ↓
Payment
   ↓
Complete Order
   ↓
Inventory Event
   ↓
Stock Ledger
   ↓
Accounting Event
   ↓
ERP Queue
```

## 37. Database Transaction Boundary

Critical POS completion should be atomic where possible:

```text
BEGIN
  Create Sales Order
  Create Order Items
  Create Payment Record
  Create Stock Movement
  Create Outbox Event
COMMIT
```

External APIs tidak dilakukan di dalam database transaction. Setelah commit, worker memanggil payment provider atau SAP sesuai kebutuhan.

## 38. Error Recovery

```text
POS Order Completed
        ↓
Outbox Event
        ↓
SAP Integration
        ↓
FAILED
        ↓
Retry Queue
        ↓
SUCCESS
```

Cashier tidak perlu mengulang transaksi hanya karena SAP sedang unavailable.

## 39. Performance Targets

```text
Product Search        P95 < 300ms
Barcode Lookup        P95 < 200ms
Cart Calculation      P95 < 300ms
Promotion Calculation P95 < 500ms
Order Completion      P95 < 1s
API General           P95 < 500ms
```

Initial scale target: 10–50 stores dan 100–500 POS terminals.

## 40. Testing Strategy

- Unit test: pricing, promotion, tax, inventory, payment, loyalty.
- Integration test: PostgreSQL, Redis, payment gateway, ERP.
- Contract test: external API, payment provider, SAP.
- E2E: login → session → scan → promo → payment → receipt → stock.
- Load test menggunakan k6 untuk search, barcode, cart, promotion, checkout, dan concurrent payment.

## 41. Deployment Architecture

```text
                    Cloudflare
                        │
                        ▼
                  Reverse Proxy
                        │
             ┌──────────┴──────────┐
             ▼                     ▼
          Next.js                Go API
                                   │
                         ┌─────────┼─────────┐
                         ▼         ▼         ▼
                      Worker    PostgreSQL Redis
                         │
                         ▼
                    Integration
```

Docker Compose cukup untuk MVP. Container orchestration baru dipertimbangkan ketika scale membutuhkan.

## 42. Backup & DR

PostgreSQL: full backup, WAL/incremental strategy, Point-in-Time Recovery, offsite backup, dan restore test.

Critical business data: Sales, Payment, Inventory, Promotion, Finance.

## 43. Future Omnichannel

```text
                    BUSINESS ENGINE
                          │
       ┌──────────────────┼──────────────────┐
       ▼                  ▼                  ▼
      POS             ECOMMERCE           MOBILE
       │                  │                  │
       └──────────────────┼──────────────────┘
                          ▼
                    SALES ORDER
                          │
                 ┌────────┼────────┐
                 ▼        ▼        ▼
              Inventory Payment  Loyalty
```

## 44. Future Headless Commerce

```text
Frontend
  │
  ├── POS
  ├── Ecommerce
  ├── Mobile
  └── Dealer
          │
          ▼
       API Layer
          │
          ▼
     Business Engine
```

## 45. Long-Term Stack

```text
Frontend: Next.js + TypeScript
Backend: Go
Database: PostgreSQL
Cache: Redis
Queue: Redis Streams / NATS / RabbitMQ
Storage: Cloudflare R2
Container: Docker
Proxy: Cloudflare + Nginx/Caddy
Monitoring: Prometheus + Grafana
Error Tracking: Sentry
Load Testing: k6
CI/CD: GitHub Actions
```

## 45.1 Audit Trail Acceptance Criteria

Sistem dianggap memenuhi enterprise audit requirement apabila:

1. Semua critical business action dari seluruh modul menghasilkan audit event.
2. Audit dapat ditelusuri berdasarkan user, company, store, terminal, resource, request ID, dan trace ID.
3. UPDATE critical data menyimpan before/after atau field-level changes sesuai klasifikasi data.
4. Security event dan login history dapat dibedakan dari business audit.
5. Audit records immutable bagi application roles.
6. Sensitive credentials/payment secrets tidak pernah masuk audit.
7. Audit access mengikuti RBAC dan organizational scope.
8. Audit event memiliki retention policy, backup strategy, dan monitoring.
9. Domain event, outbox event, dan audit event dapat dikorelasikan untuk troubleshooting end-to-end.
10. Audit failure untuk transaksi kritis memiliki defined failure policy sehingga tidak menyebabkan transaksi sukses tanpa evidence audit, kecuali secara eksplisit ditetapkan sebagai non-blocking event dengan recovery mechanism.

## 46. Final Strategic Position

Target platform:

> **Retail Business Platform dengan POS sebagai channel utama dan architecture yang siap berkembang menuju ERP.**

Bukan:

> **POS yang nanti dipaksa menjadi ERP.**

Dengan architecture ini, platform dapat berkembang:

```text
POS
 ↓
Retail Platform
 ↓
Distribution + Finance
 ↓
ERP / Omnichannel Platform
```

tanpa rewrite total.

## 47. Immediate Next Steps

```text
1. Finalize Business Requirement
        ↓
2. Finalize Domain Model
        ↓
3. Design PostgreSQL ERD
        ↓
4. Define Module Boundaries
        ↓
4a. Define Audit Events & Security Controls
        ↓
5. Define API Contract
        ↓
6. Design Pricing Engine
        ↓
7. Design Promotion Engine
        ↓
8. Design Inventory Ledger
        ↓
9. Design POS Transaction Flow
        ↓
10. Create Development Backlog
        ↓
11. Build MVP
```

### Prioritas tertinggi

```text
┌──────────────────────────────┐
│       PRICING ENGINE         │
├──────────────────────────────┤
│      PROMOTION ENGINE        │
├──────────────────────────────┤
│       INVENTORY ENGINE       │
└──────────────────────────────┘
               │
               ▼
        SALES / POS ENGINE
               │
               ▼
        PAYMENT / FINANCE
               │
               ▼
         ERP INTEGRATION
```

**Jika tiga engine tersebut didesain dengan benar, pengembangan menuju capability Odoo-style di masa depan tidak membutuhkan redesign fundamental.**
