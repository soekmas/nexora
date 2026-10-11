# POS Requirement & Module Blueprint

**Version:** 1.0  
**Status:** Product Blueprint  
**Target:** Modern Retail / Home Appliance POS  
**Architecture Direction:** Web-based, API-driven, Multi-store, Multi-terminal

---

# 1. Product Vision

POS bukan hanya aplikasi untuk mencatat transaksi penjualan.

Sistem dirancang sebagai **Retail Transaction Platform** yang menghubungkan:

```text
Customer
    ↓
Product & Pricing
    ↓
Promotion Engine
    ↓
POS Transaction
    ↓
Payment
    ↓
Inventory
    ↓
Accounting / ERP
    ↓
Reporting & Analytics
```

Tujuan utama:

1. Mempercepat proses transaksi.
2. Mengurangi kesalahan cashier.
3. Menjamin harga dan promo dihitung secara konsisten.
4. Menjaga inventory secara real-time.
5. Mendukung multi-store dan multi-terminal.
6. Mendukung berbagai metode pembayaran.
7. Menyediakan audit trail seluruh transaksi.
8. Memungkinkan integrasi ERP/SAP.
9. Mendukung pengembangan omnichannel di masa depan.
10. Memisahkan business engine dari UI POS sehingga sistem dapat digunakan oleh Web POS, Mobile POS, Ecommerce, atau channel lainnya.

---

# 2. Target Business Model

Blueprint ini cocok untuk:

- Retail store
- Home appliance
- Electronics
- Distributor retail
- Brand store
- Multi-store
- Dealer/store network
- Omnichannel retail

Contoh struktur:

```text
Company
│
├── Store Jakarta
│   ├── POS Terminal 01
│   ├── POS Terminal 02
│   └── Warehouse
│
├── Store Bandung
│   ├── POS Terminal 01
│   └── Warehouse
│
└── Store Surabaya
    ├── POS Terminal 01
    └── Warehouse
```

---

# 3. High-Level Module Architecture

Sistem dibagi menjadi beberapa domain.

```text
┌───────────────────────────────────────────────┐
│                 POS PLATFORM                  │
├───────────────────────────────────────────────┤
│                                               │
│  MASTER DATA                                  │
│  ├── Product                                  │
│  ├── Customer                                 │
│  ├── Store                                    │
│  ├── Warehouse                                │
│  ├── Employee                                 │
│  └── Payment Method                            │
│                                               │
│  COMMERCIAL ENGINE                             │
│  ├── Pricing                                  │
│  ├── Promotion                                │
│  ├── Discount                                 │
│  ├── Voucher                                  │
│  └── Loyalty                                  │
│                                               │
│  POS ENGINE                                   │
│  ├── Cart                                     │
│  ├── Order                                    │
│  ├── Payment                                  │
│  ├── Receipt                                  │
│  └── Refund                                   │
│                                               │
│  INVENTORY                                    │
│  ├── Stock                                    │
│  ├── Reservation                              │
│  ├── Transfer                                 │
│  ├── Adjustment                                │
│  └── Stock Opname                             │
│                                               │
│  FINANCE                                      │
│  ├── Tax                                      │
│  ├── Settlement                               │
│  ├── Accounting Mapping                       │
│  └── ERP Integration                          │
│                                               │
│  CUSTOMER                                     │
│  ├── CRM                                      │
│  ├── Membership                               │
│  ├── Loyalty                                  │
│  └── Customer History                         │
│                                               │
│  REPORTING                                    │
│  ├── Sales                                    │
│  ├── Inventory                                │
│  ├── Payment                                  │
│  ├── Promotion                                │
│  └── Finance                                  │
│                                               │
└───────────────────────────────────────────────┘
```

---

# 4. Module 1 — Organization & Store Management

## 4.1 Company

Master company:

- Company code
- Company name
- Tax information
- Address
- Currency
- Timezone
- Fiscal period
- Accounting configuration

## 4.2 Store

Fields:

```text
store_id
store_code
store_name
store_type
company_id
address
city
province
postal_code
phone
email
warehouse_id
status
```

Store type:

```text
RETAIL
FLAGSHIP
OUTLET
DEALER
WAREHOUSE
POPUP
```

## 4.3 POS Terminal

Setiap terminal memiliki identitas unik.

```text
POS-001
POS-002
POS-003
```

Fields:

```text
terminal_id
terminal_code
store_id
terminal_name
device_identifier
status
```

---

# 5. Module 2 — User, Role & Permission

## Roles

Minimal:

```text
SUPER ADMIN
ADMIN
STORE MANAGER
SUPERVISOR
CASHIER
SALES
WAREHOUSE
FINANCE
AUDITOR
```

Permission menggunakan RBAC.

Contoh:

```text
POS.SALE.CREATE
POS.SALE.VOID
POS.SALE.REFUND
POS.DISCOUNT.APPLY
POS.SESSION.OPEN
POS.SESSION.CLOSE

INVENTORY.VIEW
INVENTORY.ADJUST
INVENTORY.TRANSFER

PROMO.VIEW
PROMO.CREATE
PROMO.APPROVE

REPORT.SALES
REPORT.FINANCE
```

Sensitive action harus membutuhkan authorization.

Contoh:

```text
Cashier
    ↓
Request Void
    ↓
Supervisor Approval
    ↓
Void Approved
```

---

# 6. Module 3 — Product Master

## Product

Core attributes:

```text
SKU
Product Code
Product Name
Brand
Category
Sub Category
UOM
Product Type
Tax Category
Cost Price
Selling Price
Status
```

Product type:

```text
STOCK
SERVICE
BUNDLE
FREE_ITEM
NON_STOCK
```

## 6.1 Product Variant

Untuk produk yang memiliki variasi:

```text
Product
 ├── Color
 ├── Size
 ├── Model
 └── Variant
```

## 6.2 Barcode

Satu produk dapat memiliki beberapa barcode:

```text
SKU
 ├── EAN
 ├── UPC
 ├── Internal Barcode
 └── Serial Barcode
```

Barcode harus mendukung pencarian cepat di POS.

---

# 7. Module 4 — Serial Number & Warranty

Untuk home appliance, modul ini sangat direkomendasikan.

```text
Product
    ↓
Serial Number
    ↓
Inventory
    ↓
POS Sale
    ↓
Customer
    ↓
Warranty
```

Data:

```text
serial_number
product_id
warehouse_id
status
purchase_order
sales_order
customer_id
warranty_start
warranty_end
```

Status:

```text
AVAILABLE
RESERVED
SOLD
RETURNED
DAMAGED
SERVICE
```

---

# 8. Module 5 — Customer / CRM

Customer master:

```text
Customer ID
Customer Code
Name
Phone
Email
Address
Customer Type
Customer Group
Tax ID
Membership
```

Customer type:

```text
RETAIL
MEMBER
CORPORATE
DEALER
EMPLOYEE
VIP
```

Customer transaction history:

```text
Customer
   ↓
Orders
   ↓
Payments
   ↓
Products
   ↓
Promotions
   ↓
Returns
```

---

# 9. Module 6 — Pricing Engine

Pricing harus dipisahkan dari POS UI.

Konsep:

```text
Product
   ↓
Base Price
   ↓
Customer Price
   ↓
Store Price
   ↓
Period Price
   ↓
Promotion
   ↓
Final Price
```

Pricing source:

```text
STANDARD_PRICE
STORE_PRICE
CUSTOMER_PRICE
MEMBER_PRICE
SPECIAL_PRICE
CAMPAIGN_PRICE
```

Priority contoh:

```text
Customer Specific Price
        ↓
Member Price
        ↓
Store Price
        ↓
Standard Price
```

Semua rule harus mempunyai:

```text
start_date
end_date
priority
status
```

---

# 10. Module 7 — Promotion Engine

Ini adalah salah satu modul paling penting.

Promotion engine harus berdiri sendiri.

```text
                    PROMOTION ENGINE
                           │
             ┌─────────────┴─────────────┐
             │                           │
          Criteria                    Reward
             │                           │
       ┌─────┼─────┐              ┌─────┼─────┐
       ▼     ▼     ▼              ▼     ▼     ▼
      Item   Qty  Amount        Disc   Free  Cashback
```

## 10.1 Promotion Criteria

Support:

```text
Product
SKU
Category
Sub Category
Brand
Quantity
Gross Amount
Net Amount
Customer
Customer Group
Store
Payment Method
Day
Time
Campaign
```

Contoh:

```text
Product Category = Cooker
AND
Minimum Qty = 2
AND
Store = Jakarta
```

---

# 11. Promotion Formula

Formula engine:

```text
TOTAL_QTY
TOTAL_AMOUNT_GROSS
TOTAL_AMOUNT_NET
MINIMUM_QTY
MINIMUM_AMOUNT
```

Reward:

```text
DISCOUNT_PERCENT
DISCOUNT_AMOUNT
FIXED_PRICE
FREE_ITEM
CASHBACK
VOUCHER
BUNDLE_PRICE
```

Contoh:

```text
Buy 2 items
→ Discount 10%

Buy ≥ Rp10.000.000
→ Discount 2%

Buy Product A
→ Free Product B
```

---

# 12. Promotion Stacking

Wajib mempunyai konsep:

```text
STACKABLE
EXCLUSIVE
PRIORITY
```

Contoh:

```text
Promo A
Discount 10%
Stackable = YES

Promo B
Free Item
Stackable = YES
```

Result:

```text
Base Price
 ↓
Promo A
 ↓
Promo B
 ↓
Final Price
```

Tetapi:

```text
Promo C
Exclusive = YES
```

maka promo lain tidak boleh diterapkan.

---

# 13. Promotion Calculation Context

Engine menerima context:

```json
{
  "store": "JKT01",
  "customer": "CUS001",
  "items": [],
  "payment_method": "QRIS",
  "transaction_date": "2026-10-05"
}
```

Output:

```json
{
  "eligible_promotions": [],
  "applied_promotions": [],
  "discount_total": 0,
  "free_items": [],
  "final_amount": 0
}
```

Dengan desain ini, promotion engine dapat digunakan oleh:

```text
POS
Ecommerce
Mobile App
Dealer App
Marketplace
```

---

# 14. Module 8 — POS Cart

Cart harus mendukung:

- Add item
- Remove item
- Update quantity
- Change price
- Discount
- Customer selection
- Barcode scanning
- Serial selection
- Promo recalculation
- Tax calculation
- Notes

Contoh:

```text
Cart
 ├── Product A x2
 ├── Product B x1
 └── Product C x3

Subtotal
Discount
Tax
Shipping
Grand Total
```

---

# 15. Module 9 — Sales Order / POS Transaction

Transaction lifecycle:

```text
DRAFT
   ↓
PENDING_PAYMENT
   ↓
PAID
   ↓
COMPLETED
```

Alternative:

```text
DRAFT
CANCELLED
VOIDED
REFUNDED
PARTIALLY_REFUNDED
```

Transaction header:

```text
order_id
order_number
store_id
terminal_id
cashier_id
customer_id
order_date
subtotal
discount
tax
grand_total
status
```

Transaction detail:

```text
order_item_id
order_id
product_id
sku
qty
unit_price
gross_amount
discount_amount
tax_amount
net_amount
```

---

# 16. Module 10 — Payment Engine

Payment harus menjadi domain tersendiri.

Supported:

```text
CASH
BANK_TRANSFER
QRIS
DEBIT_CARD
CREDIT_CARD
E_WALLET
VIRTUAL_ACCOUNT
PAYMENT_GATEWAY
```

Payment status:

```text
PENDING
PROCESSING
PAID
FAILED
EXPIRED
REFUNDED
PARTIAL_REFUND
```

Payment reference:

```text
payment_id
order_id
payment_method
amount
provider
provider_transaction_id
payment_status
paid_at
```

---

# 17. Split Payment

POS sebaiknya mendukung:

```text
Total = Rp1.000.000

Cash        Rp400.000
QRIS        Rp600.000
```

Maka:

```text
Order
 ├── Payment #1 CASH
 └── Payment #2 QRIS
```

---

# 18. Module 11 — POS Session / Cash Management

Session:

```text
OPEN
   ↓
ACTIVE
   ↓
CLOSING
   ↓
CLOSED
```

Opening:

```text
Opening Cash
Opening Time
Cashier
Terminal
```

During session:

```text
Sales
Refund
Cash In
Cash Out
Cash Drop
```

Closing:

```text
Expected Cash
Actual Cash
Variance
```

Formula:

```text
Expected Cash =
Opening Cash
+ Cash Sales
+ Cash In
- Cash Refund
- Cash Out
- Cash Drop
```

---

# 19. Module 12 — Cash In / Cash Out

Cash In:

```text
Petty Cash
Additional Float
```

Cash Out:

```text
Expense
Bank Deposit
Cash Drop
Operational Expense
```

Setiap cash movement harus memiliki:

```text
reference
reason
amount
user
timestamp
approval
```

---

# 20. Module 13 — Receipt

Receipt dapat berupa:

```text
Thermal Printer
PDF
Email
WhatsApp
Digital Receipt
```

Isi:

```text
Company
Store
Transaction Number
Date
Cashier

Items
Qty
Price
Discount

Subtotal
Discount
Tax
Grand Total

Payment
Change

Footer
Warranty information
```

---

# 21. Module 14 — Refund & Return

Flow:

```text
Original Transaction
       ↓
Return Request
       ↓
Select Item
       ↓
Quantity
       ↓
Reason
       ↓
Approval
       ↓
Inventory Return
       ↓
Refund
```

Return reason:

```text
WRONG_ITEM
DAMAGED
CUSTOMER_CHANGE_MIND
DEFECT
WRONG_QTY
OTHER
```

---

# 22. Module 15 — Inventory

Inventory engine harus mencatat **stock movement**, bukan hanya update `qty`.

Contoh:

```text
Stock In        +10
Sale             -2
Return           +1
Transfer Out     -3
Adjustment       -1
```

Ledger:

```text
inventory_movement
```

Setiap movement memiliki:

```text
product
warehouse
qty
movement_type
reference_type
reference_id
timestamp
```

---

# 23. Stock Reservation

Untuk mencegah overselling:

```text
Available Stock
      ↓
Reserve
      ↓
Payment
      ↓
Deduct
```

Contoh:

```text
On Hand       10
Reserved       3
Available     7
```

Formula:

```text
Available = On Hand - Reserved
```

Ini juga berguna jika POS terhubung dengan ecommerce.

---

# 24. Module 16 — Stock Transfer

Flow:

```text
Store A
   ↓
Transfer Request
   ↓
Approval
   ↓
Shipment
   ↓
Store B
   ↓
Receive
```

Status:

```text
DRAFT
REQUESTED
APPROVED
IN_TRANSIT
RECEIVED
CANCELLED
```

---

# 25. Module 17 — Stock Opname

Flow:

```text
Create Opname
      ↓
Freeze / Snapshot
      ↓
Count Physical
      ↓
System Qty
      ↓
Variance
      ↓
Approval
      ↓
Adjustment
```

Report:

```text
System Qty
Physical Qty
Variance Qty
Variance Value
```

---

# 26. Module 18 — Tax

Tax engine:

```text
Product
   ↓
Tax Category
   ↓
Tax Rule
   ↓
Tax Calculation
```

Support:

```text
VAT
Non-VAT
Tax Exempt
Other Tax
```

Tax harus configurable, bukan hard-coded.

---

# 27. Module 19 — Accounting Integration

POS tidak harus menjadi full accounting system.

Lebih baik menjadi **transaction source**.

```text
POS
 ↓
Accounting Mapping
 ↓
JE / AR / Revenue
 ↓
SAP / ERP
```

Contoh mapping:

```text
Sales
    ↓
Revenue Account

VAT
    ↓
VAT Payable

Cash
    ↓
Cash Account

QRIS
    ↓
Clearing Account

Discount
    ↓
Sales Discount Account
```

---

# 28. Module 20 — ERP / SAP Integration

Recommended architecture:

```text
POS
 ↓
Integration Queue
 ↓
Integration Service
 ↓
SAP API
```

Jangan:

```text
POS → SAP synchronous setiap transaksi
```

Lebih aman:

```text
POS
 ↓
Transaction Completed
 ↓
Create Integration Event
 ↓
Queue
 ↓
SAP
```

Status:

```text
PENDING
PROCESSING
SUCCESS
FAILED
RETRY
```

Dengan:

```text
idempotency_key
```

agar transaksi tidak terkirim dua kali.

---

# 29. Module 21 — Loyalty

Optional untuk MVP, tetapi architecture sebaiknya sudah siap.

Support:

```text
Points
Membership Tier
Voucher
Birthday Reward
Purchase Reward
Campaign Reward
```

Contoh:

```text
Rp10.000 = 1 point
```

Tier:

```text
REGULAR
SILVER
GOLD
PLATINUM
```

---

# 30. Module 22 — Voucher

Voucher:

```text
Voucher Code
Campaign
Discount Type
Discount Value
Minimum Purchase
Maximum Discount
Start Date
End Date
Usage Limit
Customer Limit
Store Limit
```

Contoh:

```text
MODENA100K

Min Purchase : Rp2.000.000
Discount     : Rp100.000
Max Usage    : 1/customer
```

---

# 31. Module 23 — Approval Workflow

Sensitive action:

```text
Cashier
 ↓
Request
 ↓
Supervisor
 ↓
Approve / Reject
```

Applicable untuk:

- Void transaction
- Large discount
- Price override
- Refund
- Stock adjustment
- Cash adjustment
- Manual payment
- Backdate transaction

---

# 32. Module 24 — Audit Trail

Setiap perubahan penting harus dicatat.

```text
audit_log
```

Data:

```text
user
action
module
record_id
old_value
new_value
ip_address
device
timestamp
```

Contoh:

```text
Cashier Budi
changed quantity
Product ABC
1 → 3
14:32:15
```

---

# 33. Module 25 — Notification

Notification event:

```text
Payment Success
Payment Failed
Refund Completed
Stock Low
Stock Out
Approval Request
Integration Failed
Session Closing
```

Channel:

```text
In-App
Email
WhatsApp
Push Notification
```

---

# 34. Module 26 — Reporting

## Sales Dashboard

```text
Today's Sales
MTD Sales
Transaction Count
Average Basket Size
Average Transaction Value
Units Sold
Discount
Net Sales
```

## Sales Analysis

```text
Sales by Store
Sales by Product
Sales by Category
Sales by Brand
Sales by Cashier
Sales by Customer
Sales by Payment
Sales by Hour
```

---

# 35. Promotion Reporting

Sangat penting untuk bisnis retail.

```text
Promo Usage
Promo Transaction
Discount Amount
Promo Cost
Sales Before Promo
Sales After Promo
Units Sold
Revenue Contribution
```

---

# 36. Inventory Reporting

```text
Stock On Hand
Available Stock
Reserved Stock
Stock Movement
Stock Aging
Fast Moving Product
Slow Moving Product
Stock Valuation
Stock Adjustment
Stock Opname
```

---

# 37. Finance Reporting

```text
Gross Sales
Discount
Net Sales
Tax
Payment
Refund
Outstanding
Cash Variance
Settlement
```

---

# 38. Dashboard Hierarchy

Recommended dashboard:

```text
Executive Dashboard
       ↓
Company
       ↓
Region
       ↓
Store
       ↓
Terminal
       ↓
Transaction
```

Filter:

```text
Date
Store
Product
Category
Brand
Customer
Cashier
Promotion
Payment Method
```

---

# 39. Offline POS

Untuk retail, offline capability sangat disarankan.

Architecture:

```text
              CLOUD
                │
        PostgreSQL / API
                │
          Sync Service
                │
        ┌───────┴───────┐
        │               │
      POS 01          POS 02
        │               │
     Local Cache     Local Cache
```

Ketika internet mati:

```text
POS
 ↓
Local transaction
 ↓
Queue
 ↓
Internet kembali
 ↓
Sync
 ↓
Server
```

Offline mode membutuhkan conflict resolution dan idempotency.

---

# 40. API Architecture

API domain:

```text
/api/auth
/api/products
/api/customers
/api/pricing
/api/promotions
/api/cart
/api/orders
/api/payments
/api/refunds
/api/inventory
/api/stores
/api/terminals
/api/sessions
/api/reports
/api/integrations
```

Contoh:

```text
POST /api/orders
POST /api/orders/{id}/payment
POST /api/orders/{id}/complete
POST /api/orders/{id}/refund
```

---

# 41. Recommended Core Database Domains

PostgreSQL schema dapat dibagi:

```text
master
 ├── product
 ├── customer
 ├── store
 ├── warehouse
 └── user

pricing
 ├── price_list
 ├── price_rule
 └── price_history

promotion
 ├── promotion
 ├── criteria
 ├── formula
 ├── reward
 └── application

sales
 ├── order
 ├── order_item
 ├── payment
 ├── refund
 └── receipt

inventory
 ├── stock
 ├── stock_ledger
 ├── reservation
 ├── transfer
 └── adjustment

finance
 ├── tax
 ├── settlement
 └── accounting_mapping

loyalty
 ├── member
 ├── point
 └── voucher

integration
 ├── outbox
 ├── integration_log
 └── retry_queue

audit
 └── audit_log
```

---

# 42. Core Entity Relationship

```text
COMPANY
   │
   ├── STORE
   │      │
   │      ├── POS TERMINAL
   │      │       │
   │      │       └── POS SESSION
   │      │               │
   │      │               └── ORDER
   │      │                    │
   │      │                    ├── ORDER ITEM
   │      │                    │      │
   │      │                    │      └── PRODUCT
   │      │                    │
   │      │                    ├── PAYMENT
   │      │                    │
   │      │                    ├── PROMOTION
   │      │                    │
   │      │                    └── REFUND
   │      │
   │      └── WAREHOUSE
   │              │
   │              └── STOCK
   │
   └── CUSTOMER
```

---

# 43. Critical Business Rules

## Rule 1 — Price

POS tidak boleh menentukan harga sendiri.

```text
POS
 ↓
Pricing Engine
 ↓
Price
```

## Rule 2 — Promotion

POS tidak boleh menghitung promo dengan hard-coded logic.

```text
POS
 ↓
Promotion Engine
 ↓
Applied Promotion
```

## Rule 3 — Inventory

Jangan hanya:

```sql
UPDATE stock SET qty = qty - 1;
```

Gunakan:

```text
Stock Ledger
+
Current Balance
```

## Rule 4 — Payment

Order dianggap completed hanya jika:

```text
Paid Amount >= Grand Total
```

atau sesuai rule pembayaran yang ditentukan.

## Rule 5 — Idempotency

Payment, order completion dan ERP integration harus idempotent.

Contoh:

```text
idempotency_key =
STORE + TERMINAL + ORDER_NUMBER
```

---

# 44. Security Requirement

Minimal:

```text
JWT / Session Authentication
RBAC
Password Hashing
MFA untuk admin
Rate Limiting
API Validation
Audit Trail
Encryption
HTTPS
Secret Management
```

Untuk payment:

```text
Jangan menyimpan
raw card number
CVV
atau sensitive payment credential.
```

---

# 45. Performance Requirement

Target awal:

```text
Product search        < 300 ms
Barcode lookup        < 200 ms
Cart calculation      < 300 ms
Promotion calculation < 500 ms
Payment confirmation  < 2 sec
```

Untuk high traffic:

```text
Redis
Database indexing
Connection pooling
Async queue
Caching
Read optimization
```

---

# 46. Recommended Technology Stack

```text
Frontend
Next.js
React
TypeScript

Backend
Go

Database
PostgreSQL

Cache
Redis

Queue
Redis / RabbitMQ / NATS

Container
Docker

Reverse Proxy
Nginx / Caddy / Cloudflare

Storage
Cloudflare R2 / Object Storage

Monitoring
Prometheus
Grafana
Sentry / equivalent

CI/CD
GitHub Actions
```

Alternative sederhana untuk MVP:

```text
Next.js
+
Go
+
PostgreSQL
+
Redis
+
Docker
```

---

# 47. Recommended Backend Architecture

Saya tidak menyarankan langsung microservices.

Mulai dengan:

```text
                    POS Backend
                        │
        ┌───────────────┼───────────────┐
        │               │               │
      Sales          Promotion       Inventory
        │               │               │
        └───────────────┼───────────────┘
                        │
                    PostgreSQL
```

Bentuknya:

**Modular Monolith**

Setelah scale meningkat, domain yang paling berat dapat dipisahkan.

---

# 48. Recommended Project Structure

Contoh Go:

```text
/internal

  auth/
  customer/
  product/
  pricing/
  promotion/
  sales/
  payment/
  inventory/
  warehouse/
  store/
  session/
  refund/
  loyalty/
  voucher/
  reporting/
  integration/
  audit/
```

Setiap module memiliki:

```text
handler
service
repository
model
dto
```

---

# 49. POS Frontend

Layout utama:

```text
┌─────────────────────────────────────────────┐
│ Store       Cashier       Session           │
├───────────────────────┬─────────────────────┤
│                       │                     │
│ Product Search        │ Current Cart       │
│                       │                     │
│ [Search] [Barcode]    │ Product A   2      │
│                       │ Product B   1      │
│ Categories            │ Product C   3      │
│                       │                     │
│ Product Grid          │---------------------│
│                       │ Subtotal            │
│ [A] [B] [C] [D]       │ Discount            │
│ [E] [F] [G] [H]       │ Tax                 │
│                       │ Grand Total         │
│                       │                     │
│                       │ [PAYMENT]           │
└───────────────────────┴─────────────────────┘
```

UX harus mengutamakan:

```text
Keyboard shortcut
Barcode scanner
Touch screen
Fast search
Minimal click
Large payment button
```

---

# 50. POS Transaction UX

Target:

```text
Scan
 ↓
Scan
 ↓
Scan
 ↓
PAY
 ↓
Payment
 ↓
Receipt
```

Cashier tidak boleh dipaksa melewati banyak form.

---

# 51. MVP Scope

Untuk versi pertama saya **tidak akan membangun semua module sekaligus**.

MVP:

```text
1. Authentication
2. User & Role
3. Store
4. Terminal
5. Product
6. Customer
7. Price
8. Basic Promotion
9. Cart
10. Sales Order
11. Payment
12. POS Session
13. Receipt
14. Basic Inventory
15. Refund
16. Basic Reporting
17. Audit Log
```

---

# 52. Phase 2

Setelah MVP stabil:

```text
Advanced Promotion
Loyalty
Voucher
Serial Number
Stock Transfer
Stock Opname
Split Payment
Approval Workflow
Advanced Reporting
Settlement
ERP Integration
```

---

# 53. Phase 3

Advanced platform:

```text
Offline POS
Omnichannel
Ecommerce Integration
Mobile POS
Customer Loyalty
Campaign Management
Advanced Analytics
AI Sales Analytics
Recommendation Engine
Dynamic Pricing
```

---

# 54. Suggested Development Sprint

## Sprint 1 — Foundation

```text
Project setup
Authentication
RBAC
Company
Store
Terminal
User
```

## Sprint 2 — Product

```text
Product
Category
Brand
Barcode
UOM
Warehouse
```

## Sprint 3 — Pricing

```text
Price List
Price Rule
Customer Price
Store Price
Pricing Engine
```

## Sprint 4 — POS

```text
Cart
Order
Order Item
Customer
Checkout
```

## Sprint 5 — Payment

```text
Cash
Transfer
QRIS
Payment abstraction
Payment status
Receipt
```

## Sprint 6 — Inventory

```text
Stock
Stock Ledger
Reservation
Stock Deduction
Return
```

## Sprint 7 — Promotion

```text
Criteria
Formula
Reward
Priority
Stacking
Promotion Calculation
```

## Sprint 8 — Session

```text
Open Session
Cash In
Cash Out
Closing
Cash Reconciliation
```

## Sprint 9 — Reporting

```text
Sales
Payment
Product
Cashier
Store
Promotion
```

## Sprint 10 — Hardening

```text
Security
Audit
Performance
Load Testing
Error Handling
Monitoring
Backup
Deployment
```

---

# 55. Acceptance Criteria — Core POS

Transaction berhasil apabila:

```text
Product valid
        AND
Price valid
        AND
Promotion calculated
        AND
Stock available
        AND
Payment successful
        AND
Order committed
        AND
Inventory updated
```

Semua proses transaksi utama harus bersifat **atomic**.

Jika payment berhasil tetapi inventory gagal, sistem harus memiliki mekanisme recovery/reconciliation.

---

# 56. Non-Functional Requirements

## Availability

Target:

```text
99.9%
```

## Performance

```text
P95 API < 500 ms
```

untuk transaksi normal.

## Scalability

Initial target:

```text
10–50 stores
100–500 POS terminals
```

Architecture tetap harus memungkinkan scale lebih tinggi.

## Reliability

Tidak boleh terjadi:

```text
Duplicate Order
Duplicate Payment
Duplicate Stock Deduction
Duplicate ERP Posting
```

---

# 57. Observability

Setiap request penting memiliki:

```text
request_id
trace_id
user_id
store_id
terminal_id
order_id
```

Log:

```text
INFO
WARN
ERROR
AUDIT
INTEGRATION
PAYMENT
```

---

# 58. Backup & Disaster Recovery

PostgreSQL:

```text
Daily Full Backup
Point-in-Time Recovery
Backup Verification
Offsite Backup
```

Recommended:

```text
Primary DB
    ↓
Backup
    ↓
Object Storage
```

---

# 59. Future Architecture

Jika platform sudah besar:

```text
                     API Gateway
                         │
        ┌────────────────┼────────────────┐
        │                │                │
      POS API        Ecommerce API    Mobile API
        │                │                │
        └────────────────┼────────────────┘
                         │
                  Domain Services
                         │
        ┌────────────────┼────────────────┐
        │                │                │
      Sales          Promotion        Inventory
        │                │                │
        └────────────────┼────────────────┘
                         │
                       Event Bus
                         │
        ┌────────────────┼────────────────┐
        │                │                │
       ERP            Analytics        Notification
```

Tetapi ini **fase scale-up**, bukan MVP.

---

# 60. Recommended Product Principle

Ada lima prinsip yang disarankan menjadi dasar desain:

### 1. Transaction First

POS harus selalu memastikan transaksi konsisten.

### 2. Business Rule Centralized

Pricing dan promotion tidak boleh tersebar di frontend/backend.

### 3. Inventory Ledger

Semua perubahan stock harus dapat ditelusuri.

### 4. Event Driven Integration

ERP/payment/external system menggunakan event/queue jika memungkinkan.

### 5. API First

POS UI bukan pusat business logic.

---

# 61. Final Module Map

```text
POS PLATFORM
│
├── Identity & Access
│   ├── User
│   ├── Role
│   └── Permission
│
├── Organization
│   ├── Company
│   ├── Store
│   ├── Terminal
│   └── Warehouse
│
├── Product
│   ├── Product
│   ├── Category
│   ├── Brand
│   ├── Barcode
│   └── Serial Number
│
├── Customer
│   ├── Customer
│   ├── Membership
│   └── Loyalty
│
├── Commercial
│   ├── Pricing
│   ├── Promotion
│   ├── Voucher
│   └── Discount
│
├── Sales
│   ├── Cart
│   ├── Order
│   ├── Payment
│   ├── Receipt
│   └── Refund
│
├── POS Operation
│   ├── Session
│   ├── Cash In
│   ├── Cash Out
│   └── Reconciliation
│
├── Inventory
│   ├── Stock
│   ├── Reservation
│   ├── Transfer
│   ├── Adjustment
│   └── Stock Opname
│
├── Finance
│   ├── Tax
│   ├── Settlement
│   └── Accounting Mapping
│
├── Integration
│   ├── Payment Gateway
│   ├── SAP / ERP
│   ├── Ecommerce
│   └── Notification
│
├── Reporting
│   ├── Sales
│   ├── Inventory
│   ├── Finance
│   ├── Promotion
│   └── Customer
│
└── Platform
    ├── Audit Log
    ├── Notification
    ├── Queue
    ├── Monitoring
    └── Configuration
```

---

# 62. Recommended Build Order

```text
                 MASTER DATA
                     │
                     ▼
                  PRICING
                     │
                     ▼
              PROMOTION ENGINE
                     │
                     ▼
                 POS CART
                     │
                     ▼
                SALES ORDER
                     │
                     ▼
                 PAYMENT
                     │
                     ▼
              INVENTORY LEDGER
                     │
                     ▼
              POS SESSION
                     │
                     ▼
                  REFUND
                     │
                     ▼
                 REPORTING
                     │
                     ▼
              ERP INTEGRATION
```

## Final Recommendation

Untuk POS ini, bagian paling strategis bukan halaman kasirnya. Tiga engine yang sebaiknya didesain paling matang sejak awal adalah:

```text
┌─────────────────────────────┐
│       PRICING ENGINE        │
├─────────────────────────────┤
│     PROMOTION ENGINE        │
├─────────────────────────────┤
│      INVENTORY ENGINE       │
└─────────────────────────────┘
             │
             ▼
       POS TRANSACTION
```

Jika ketiga engine ini benar, UI POS, ecommerce, dealer app, dan channel lain nantinya dapat menggunakan **business engine yang sama**.
