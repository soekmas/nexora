# POS Platform — Permission Matrix v1

**Version:** 1.0  
**Status:** Architecture Lock Candidate  
**Target:** Enterprise Retail / Home Appliance POS Platform  
**Depends on:** Blueprint v2.1, ERD Secure/Odoo Ready v1, PostgreSQL Design v1, Domain & Security Review v1.1, API Contract v1, Go Backend Architecture v1, Offline Sync Contract v1, ADR / Architecture Lock v1, State Machine Catalog v1

---

## 1. Purpose

Dokumen ini mendefinisikan authorization model untuk POS Platform secara implementable dan testable.

Permission tidak diperlakukan sebagai boolean sederhana. Authorization final merupakan kombinasi:

```text
Authentication
    ↓
Tenant Authorization
    ↓
Organization Scope
    ↓
Role / Permission
    ↓
State Transition Permission
    ↓
Business Rule
    ↓
Workflow / Approval
    ↓
Segregation of Duties
    ↓
Audit Requirement
    ↓
Allow / Deny
```

Tujuan utama:

- mencegah cross-tenant access;
- mencegah akses lintas company/store/warehouse/terminal;
- membatasi critical command berdasarkan permission;
- menerapkan state-machine transition secara eksplisit;
- menerapkan Segregation of Duties (SoD);
- mendukung workflow approval;
- membedakan human authorization dan service authorization;
- mendukung offline POS tanpa mempercayai client;
- memastikan setiap privileged action dapat diaudit;
- menyediakan dasar untuk API authorization middleware dan automated tests.

---

# 2. Authorization Principles

## 2.1 Deny by default

Jika tidak ada permission yang eksplisit, request ditolak.

```text
No matching permission = DENY
```

Tidak ada implicit `admin=true` di application layer.

---

## 2.2 Permission tidak menggantikan scope

Contoh:

```text
User A
Permission: sales.void
Scope: Store Jakarta-01
```

User A boleh void transaksi Store Jakarta-01 tetapi:

```text
Store Jakarta-02 → DENY
Store Bandung-01  → DENY
Tenant B          → DENY
```

---

## 2.3 Scope lebih spesifik membatasi akses

Hierarki organization:

```text
Tenant
  ↓
Company
  ↓
Region
  ↓
Store
  ├── Terminal
  └── Warehouse
```

Scope authorization harus menentukan apakah resource berada di dalam authorized scope user.

---

## 2.4 Permission tidak otomatis memberi approval

Permission berikut berbeda:

```text
inventory.adjust.create
inventory.adjust.approve
```

User dengan `inventory.adjust.create` tidak otomatis dapat approve.

Hal yang sama berlaku untuk:

```text
promotion.create
promotion.approve

sales.refund.request
sales.refund.approve
```

---

## 2.5 State transition harus permission-aware

Tidak boleh:

```http
PATCH /sales/orders/{id}
status = VOIDED
```

Authorization harus melekat pada command:

```http
POST /sales/orders/{id}/void
```

Kemudian application layer memvalidasi:

```text
current state
+ permission
+ scope
+ business rule
+ workflow
+ SoD
+ idempotency
```

---

# 3. Authorization Decision Model

## 3.1 Formal model

```text
ALLOW(action, resource, actor)
```

hanya jika seluruh kondisi berikut terpenuhi:

```text
authenticated
AND tenant_allowed
AND scope_allowed
AND permission_allowed
AND state_transition_allowed
AND business_rule_allowed
AND approval_allowed
AND sod_allowed
AND policy_allowed
```

Jika salah satu gagal:

```text
DENY
```

---

## 3.2 Evaluation order

Urutan canonical:

```text
1. Authenticate actor
2. Resolve tenant context
3. Resolve organization scope
4. Resolve device / terminal context if POS
5. Load effective permissions
6. Validate resource ownership/scope
7. Validate current state
8. Validate requested transition
9. Validate business policy
10. Validate workflow / approval
11. Validate SoD
12. Validate MFA / privileged policy
13. Execute command transaction
14. Write audit
15. Write outbox if applicable
```

---

# 4. Scope Model

## 4.1 Supported scope types

| Scope | Meaning | Typical user |
|---|---|---|
| TENANT | seluruh tenant | system administration |
| COMPANY | satu company | corporate admin |
| REGION | satu region | regional manager |
| STORE | satu store | store manager |
| WAREHOUSE | satu warehouse | warehouse staff |
| TERMINAL | satu POS terminal | cashier / terminal operator |

`TENANT` merupakan authorization context konseptual dan bukan row pada `identity.user_scope`.

---

## 4.2 Scope inheritance

Scope hierarchy boleh memperluas akses ke child resource, tetapi hanya bila permission memang mengizinkan resource tersebut.

Contoh:

```text
COMPANY scope
    ↓
REGION
    ↓
STORE
    ↓
TERMINAL
```

User dengan Company scope + `sales.read` dapat membaca sales pada store dalam company tersebut.

Namun implementasi tidak boleh mengasumsikan:

```text
Company scope = semua permission
```

Permission tetap wajib ada.

---

## 4.3 Warehouse scope

Warehouse berada di organization scope terpisah namun dapat memiliki hubungan dengan company/store.

User warehouse hanya dapat melakukan inventory command terhadap warehouse yang berada dalam scope-nya.

---

## 4.4 Terminal scope

Terminal scope digunakan terutama untuk POS operation.

Contoh:

```text
cashier01
permission: sales.create
scope: TERMINAL POS-JKT-01-01
```

Request dari terminal lain harus ditolak kecuali user memiliki broader authorized scope.

---

# 5. Permission Naming Convention

Format canonical:

```text
<domain>.<resource>.<action>
```

Contoh:

```text
sales.order.create
sales.order.confirm
sales.order.cancel
sales.order.void
sales.refund.request
sales.refund.approve
```

Permission yang sudah digunakan pada State Machine Catalog tetap valid sebagai compatibility alias bila lebih pendek, misalnya:

```text
sales.create
sales.confirm
sales.void
payment.process
```

Namun untuk implementation v1, disarankan menggunakan namespace yang lebih granular.

---

# 6. Human Role Catalog

## 6.1 Core roles

| Role | Primary responsibility | Risk |
|---|---|---:|
| Cashier | POS transaction execution | Medium |
| Supervisor | operational supervision | High |
| Store Manager | store-level business control | High |
| Finance | financial control and refund approval | High |
| Warehouse | stock operation | Medium |
| Promo Admin | pricing/promotion management | High |
| Regional Manager | regional oversight | High |
| Auditor | read/audit access | Low |
| Security Admin | identity/security administration | Critical |
| System Admin | technical platform administration | Critical |
| Integration Service | machine-to-machine integration | Critical |

---

# 7. Role Baseline

## 7.1 Cashier

Primary permissions:

```text
sales.order.create
sales.order.confirm
sales.payment.create
payment.process
pos.session.open
pos.session.close
customer.read
product.read
price.read
promotion.evaluate
```

Cannot by default:

```text
sales.order.void
sales.refund.approve
inventory.adjust.approve
promotion.approve
user.role.assign
audit.export
```

Cashier may create offline cash sale only if terminal/store offline policy permits.

---

## 7.2 Supervisor

Adds operational control:

```text
sales.order.cancel
sales.order.void
sales.refund.request
inventory.adjust.create
pos.cash_movement.create
pos.session.variance.review
```

Still cannot approve own high-risk transaction when SoD applies.

---

## 7.3 Store Manager

Adds store-level authority:

```text
sales.order.void
sales.refund.request
sales.refund.approve
inventory.adjust.create
inventory.adjust.approve
stock.transfer.request
stock.transfer.approve
price.read
promotion.read
```

Self-approval remains prohibited for operations configured with SoD.

---

## 7.4 Finance

Primary responsibilities:

```text
payment.read
payment.reconcile
sales.refund.read
sales.refund.approve
settlement.read
finance.read
finance.reconcile
audit.read
```

Finance should not automatically receive store operational permissions.

---

## 7.5 Warehouse

```text
product.read
inventory.read
inventory.reserve
inventory.adjust.create
stock.transfer.create
stock.transfer.receive
purchase.order.read
goods_receipt.create
goods_receipt.post
```

Approval permission for inventory adjustment is separate.

---

## 7.6 Promo Admin

```text
promotion.read
promotion.create
promotion.update
promotion.submit
promotion.approve
promotion.activate
promotion.pause
promotion.expire
price_rule.read
price_rule.create
price_rule.update
price_rule.approve
```

Promotion creator cannot approve the same promotion version when SoD policy is enabled.

---

## 7.7 Regional Manager

Primarily read/control within assigned region:

```text
sales.read
sales.refund.read
inventory.read
inventory.adjust.read
promotion.read
store.read
reporting.read
```

Approval permissions must be explicitly granted; region scope alone does not grant approval.

---

## 7.8 Auditor

Read-only:

```text
sales.read
payment.read
inventory.read
promotion.read
workflow.read
integration.read
audit.read
audit.export
security_event.read
login_history.read
```

Tidak boleh melakukan business mutation.

---

## 7.9 Security Admin

```text
user.read
user.create
user.disable
user.unlock
role.read
role.create
role.update
user.role.assign
mfa.manage
session.revoke
security_policy.manage
security_event.read
login_history.read
```

Security Admin tidak otomatis mendapatkan `sales.void`, `payment.refund.approve`, atau business approval.

---

## 7.10 System Admin

System administration:

```text
system.config.read
system.config.update
integration.config.read
integration.config.update
service_account.manage
technical_health.read
job.manage
```

System Admin tetap tunduk pada audit dan privileged access policy.

---

# 8. Master Permission Catalog

## 8.1 Identity

| Permission | Action | Risk |
|---|---|---:|
| user.read | read user | Low |
| user.create | create user | High |
| user.update | update user | High |
| user.disable | disable user | High |
| user.unlock | unlock user | High |
| user.role.read | read assignments | Medium |
| user.role.assign | assign role | Critical |
| user.role.revoke | revoke role | Critical |
| role.read | read role | Low |
| role.create | create role | Critical |
| role.update | update role | Critical |
| role.permission.assign | assign permission to role | Critical |
| mfa.manage | manage MFA | Critical |
| session.revoke | revoke session | High |
| security_event.read | read security event | High |
| login_history.read | read login history | Medium |

---

## 8.2 Organization

```text
company.read
company.create
company.update
region.read
region.create
region.update
store.read
store.create
store.update
store.disable
warehouse.read
warehouse.create
warehouse.update
terminal.read
terminal.register
terminal.update
terminal.disable
terminal.retire
```

---

## 8.3 Master Data

```text
product.read
product.create
product.update
product.activate
product.deactivate
product.barcode.read
product.barcode.manage
serial.read
serial.manage
customer.read
customer.create
customer.update
customer.address.manage
uom.read
category.read
brand.read
```

Master-data write access should normally be separate from POS cashier roles.

---

# 9. Sales Permission Matrix

| Permission | Cashier | Supervisor | Store Mgr | Finance | Auditor |
|---|:---:|:---:|:---:|:---:|:---:|
| sales.order.read | ✓ | ✓ | ✓ | ✓ | ✓ |
| sales.order.create | ✓ | ✓ | ✓ | - | - |
| sales.order.confirm | ✓ | ✓ | ✓ | - | - |
| sales.order.cancel | - | ✓ | ✓ | - | - |
| sales.order.void | - | ✓* | ✓* | - | - |
| sales.refund.request | - | ✓ | ✓ | ✓ | - |
| sales.refund.approve | - | - | ✓* | ✓* | - |
| sales.refund.read | ✓ | ✓ | ✓ | ✓ | ✓ |
| sales.export | - | ✓* | ✓* | ✓* | ✓ |

`*` subject to scope, policy, amount threshold, workflow and SoD.

---

# 10. Payment Permission Matrix

| Permission | Cashier | Supervisor | Store Mgr | Finance | Auditor |
|---|:---:|:---:|:---:|:---:|:---:|
| payment.read | ✓ | ✓ | ✓ | ✓ | ✓ |
| payment.create | ✓ | ✓ | ✓ | - | - |
| payment.process | ✓ | ✓ | ✓ | - | - |
| payment.reconcile | - | - | - | ✓ | ✓ |
| payment.refund.request | - | ✓ | ✓ | ✓ | - |
| payment.refund.approve | - | - | ✓* | ✓ | - |
| payment.provider.manage | - | - | - | - | - |

Provider configuration is reserved for system/integration administration and is not a store operational permission.

---

# 11. POS Session Permission Matrix

| Command | Cashier | Supervisor | Store Mgr |
|---|:---:|:---:|:---:|
| session.read | ✓ | ✓ | ✓ |
| session.open | ✓ | ✓ | ✓ |
| session.close | ✓ | ✓ | ✓ |
| session.variance.review | - | ✓ | ✓ |
| session.variance.approve | - | - | ✓* |
| cash_movement.create | ✓ | ✓ | ✓ |
| cash_movement.approve | - | ✓ | ✓ |

Database invariant remains:

```text
one active session per terminal
```

---

# 12. Inventory Permission Matrix

| Permission | Cashier | Supervisor | Store Mgr | Warehouse | Auditor |
|---|:---:|:---:|:---:|:---:|:---:|
| inventory.read | ✓ | ✓ | ✓ | ✓ | ✓ |
| inventory.reserve | ✓ | ✓ | ✓ | ✓ | - |
| inventory.adjust.read | ✓ | ✓ | ✓ | ✓ | ✓ |
| inventory.adjust.create | - | ✓ | ✓ | ✓ | - |
| inventory.adjust.submit | - | ✓ | ✓ | ✓ | - |
| inventory.adjust.approve | - | - | ✓* | ✓* | - |
| inventory.adjust.post | - | - | ✓* | ✓* | - |
| stock.transfer.create | - | ✓ | ✓ | ✓ | - |
| stock.transfer.approve | - | - | ✓* | ✓* | - |
| stock.transfer.receive | - | - | ✓ | ✓ | - |
| stock.opname.create | - | ✓ | ✓ | ✓ | - |
| stock.opname.post | - | - | ✓* | ✓* | - |

Stock ledger itself has no direct CRUD permission.

```text
NO inventory.stock_ledger.update
NO inventory.stock_ledger.delete
```

All changes happen through domain commands.

---

# 13. Pricing Permission Matrix

```text
price.read
price_rule.read
price_rule.create
price_rule.update
price_rule.submit
price_rule.approve
price_rule.activate
price_rule.deactivate
```

Suggested baseline:

| Action | Cashier | Supervisor | Store Mgr | Promo Admin |
|---|:---:|:---:|:---:|:---:|
| read | ✓ | ✓ | ✓ | ✓ |
| create | - | - | - | ✓ |
| update | - | - | - | ✓ |
| submit | - | - | - | ✓ |
| approve | - | - | policy | ✓* |
| activate | - | - | policy | ✓* |

Price activation must not be granted simply because a user can edit prices.

---

# 14. Promotion Permission Matrix

| Permission | Cashier | Supervisor | Store Mgr | Promo Admin |
|---|:---:|:---:|:---:|:---:|
| promotion.read | ✓ | ✓ | ✓ | ✓ |
| promotion.create | - | - | - | ✓ |
| promotion.update | - | - | - | ✓ |
| promotion.submit | - | - | - | ✓ |
| promotion.approve | - | - | policy | ✓* |
| promotion.activate | - | - | policy | ✓* |
| promotion.pause | - | ✓* | ✓ | ✓ |
| promotion.expire | - | - | ✓* | ✓ |
| promotion.evaluate | ✓ | ✓ | ✓ | ✓ |

Promotion application at checkout is not equivalent to promotion administration.

Cashier may evaluate applicable promotion but cannot modify promotion definition.

---

# 15. Purchasing Permission Matrix

```text
purchase.order.read
purchase.order.create
purchase.order.submit
purchase.order.approve
purchase.order.cancel
purchase.receipt.create
goods_receipt.post
goods_receipt.reverse
```

SoD recommendation:

```text
PO Creator ≠ PO Approver
```

Goods receipt posting should require warehouse scope.

---

# 16. Loyalty and Voucher Permission Matrix

```text
loyalty.account.read
loyalty.earn
loyalty.transaction.read
loyalty.reverse
loyalty.voucher.read
loyalty.voucher.issue
loyalty.voucher.redeem
loyalty.voucher.reverse
```

Default POS:

```text
loyalty.earn       → allowed according to policy
loyalty.redeem     → controlled / online default
voucher.redeem     → online default
```

Offline voucher redemption is disabled by default.

---

# 17. Workflow / Approval Permission Matrix

```text
workflow.read
workflow.start
workflow.approve
workflow.reject
workflow.cancel
workflow.delegate
workflow.admin
```

Approval engine must verify:

```text
actor has workflow.approve
AND actor has underlying approval permission
AND actor is in authorized scope
AND actor is not prohibited by SoD
AND approval state is PENDING
```

Approval alone does not bypass domain authorization.

---

# 18. Audit Permission Matrix

| Permission | Auditor | Store Mgr | Finance | Security Admin | System Admin |
|---|:---:|:---:|:---:|:---:|:---:|
| audit.read | ✓ | ✓* | ✓* | ✓ | ✓* |
| audit.export | ✓ | policy | policy | ✓* | policy |
| security_event.read | ✓ | - | - | ✓ | ✓* |
| login_history.read | ✓ | - | - | ✓ | ✓* |
| audit.config.manage | - | - | - | ✓* | ✓* |

Audit data is immutable to application roles.

Tidak ada:

```text
audit.delete
audit.update
```

---

# 19. Reporting Permission Matrix

Read access to reporting is separately controlled:

```text
reporting.dashboard.read
reporting.sales.read
reporting.inventory.read
reporting.finance.read
reporting.export
```

Export is intentionally separate from read because export can create a larger data exposure surface.

---

# 20. Integration Permission Matrix

```text
integration.read
integration.external_reference.read
integration.webhook.read
integration.webhook.replay
integration.outbox.read
integration.delivery.retry
integration.delivery.dead_letter
integration.config.read
integration.config.update
```

Operational staff should not automatically receive integration configuration permissions.

---

# 21. State Transition Authorization Matrix

## 21.1 Sales Order

| From | To | Command | Permission | Approval |
|---|---|---|---|---|
| DRAFT | CONFIRMED | ConfirmOrder | sales.order.confirm | No |
| DRAFT | CANCELLED | CancelOrder | sales.order.cancel | policy |
| CONFIRMED | PROCESSING | StartProcessing | sales.order.process | No |
| PROCESSING | COMPLETED | CompleteOrder | sales.order.complete | No |
| CONFIRMED | CANCELLED | CancelOrder | sales.order.cancel | policy |
| PROCESSING | CANCELLED | CancelOrder | sales.order.cancel | restricted |
| COMPLETED | VOIDED | VoidOrder | sales.order.void | Yes/policy |

`VOIDED` merupakan high-risk transition.

---

## 21.2 Payment

| From | To | Permission | Approval |
|---|---|---|---|
| PENDING | PROCESSING | payment.process | No |
| PROCESSING | SUCCESS | payment.process | No |
| PROCESSING | FAILED | payment.process | No |
| PROCESSING | UNKNOWN | payment.process | No |
| SUCCESS | REFUND_PENDING | payment.refund.request | policy |
| REFUND_PENDING | REFUND_APPROVED | payment.refund.approve | Yes |
| REFUND_APPROVED | PROCESSING | payment.refund.process | No |
| PROCESSING | SUCCESS | provider result | No |

Payment `UNKNOWN` tidak boleh diubah menjadi SUCCESS hanya berdasarkan client retry.

---

## 21.3 POS Session

| From | To | Permission |
|---|---|---|
| OPENING | OPEN | pos.session.open |
| OPEN | CLOSING | pos.session.close |
| CLOSING | CLOSED | pos.session.close |
| CLOSING | VARIANCE_REVIEW | pos.session.variance.review |
| VARIANCE_REVIEW | CLOSED | pos.session.variance.approve |

---

## 21.4 Inventory Adjustment

| From | To | Permission | SoD |
|---|---|---|---|
| DRAFT | SUBMITTED | inventory.adjust.submit | No |
| SUBMITTED | APPROVAL_PENDING | inventory.adjust.submit | No |
| APPROVAL_PENDING | APPROVED | inventory.adjust.approve | Creator ≠ Approver |
| APPROVED | POSTED | inventory.adjust.post | policy |
| APPROVAL_PENDING | REJECTED | inventory.adjust.approve | Creator ≠ Approver |
| DRAFT | CANCELLED | inventory.adjust.cancel | No |

---

## 21.5 Promotion

| From | To | Permission | SoD |
|---|---|---|---|
| DRAFT | PENDING_APPROVAL | promotion.submit | No |
| PENDING_APPROVAL | APPROVED | promotion.approve | Creator ≠ Approver |
| APPROVED | SCHEDULED | promotion.schedule | No |
| SCHEDULED | ACTIVE | promotion.activate | policy |
| ACTIVE | PAUSED | promotion.pause | policy |
| ACTIVE | EXPIRED | promotion.expire | system/policy |

---

## 21.6 Refund

| From | To | Permission | SoD |
|---|---|---|---|
| REQUESTED | APPROVAL_PENDING | sales.refund.request | No |
| APPROVAL_PENDING | APPROVED | sales.refund.approve | Requester ≠ Approver |
| APPROVED | PROCESSING | sales.refund.process | No |
| PROCESSING | SUCCESS | provider result | No |
| PROCESSING | FAILED | provider result | No |

---

# 22. Segregation of Duties

## 22.1 Mandatory SoD rules

### Refund

```text
Refund Requester != Refund Approver
```

### Promotion

```text
Promotion Creator != Promotion Approver
```

### Inventory Adjustment

```text
Adjustment Creator != Adjustment Approver
```

### Purchase Order

```text
PO Creator != PO Approver
```

### User Role Assignment

Role assignment to privileged roles should require:

```text
authorized security administrator
+ MFA
+ audit
```

---

## 22.2 SoD enforcement point

SoD harus diperiksa server-side tepat sebelum transition dijalankan.

Tidak cukup hanya pada frontend.

Contoh:

```text
requester_id = current_user
approver_id = current_user
```

Jika policy mensyaratkan berbeda:

```text
DENY
Code: SOD_VIOLATION
HTTP: 409
```

---

# 23. Amount-Based Approval

Critical operations dapat memiliki approval threshold.

Contoh konfigurasi:

```text
refund <= threshold_1
    → Supervisor approval

refund > threshold_1
    → Store Manager approval

refund > threshold_2
    → Finance approval
```

Threshold harus configurable melalui policy/configuration, bukan hard-coded di UI.

Authorization result harus mempertimbangkan:

```text
permission
+ scope
+ amount
+ policy
+ workflow
+ SoD
```

---

# 24. Privileged Access

Operation berikut dikategorikan privileged:

```text
user.role.assign
role.permission.assign
mfa.manage
payment.refund.approve
sales.order.void
inventory.adjust.approve
promotion.approve
promotion.activate
system.config.update
integration.config.update
```

Policy dapat mewajibkan:

```text
MFA
recent authentication
step-up authentication
reason/comment
audit
```

---

# 25. Break-Glass Access

Break-glass hanya boleh digunakan untuk emergency operational recovery.

Karakteristik:

```text
explicit activation
short-lived
MFA
reason required
full audit
high-severity security event
automatic expiration
post-event review
```

Break-glass tidak boleh menjadi cara normal untuk bypass RBAC.

Tidak boleh:

```text
permanent super-admin token
```

---

# 26. Service Account Authorization

Human role tidak digunakan untuk integration worker.

Service account menggunakan permission khusus:

```text
integration.sap.write
integration.sap.read
integration.payment.webhook.process
integration.outbox.consume
integration.erp.reconcile
```

Service account harus memiliki:

```text
service identity
scoped permissions
minimal scope
credential rotation
no interactive login
full audit
```

---

# 27. Offline Authorization

Offline client tidak boleh dianggap sebagai source of truth authorization.

Server tetap menentukan final authorization saat sync.

## 27.1 Offline-safe actions

```text
product.read
barcode.read
price.read_cached
promotion.evaluate_cached
sales.order.create_local
sales.cash_sale.queue
```

## 27.2 Offline-disabled by default

```text
payment.gateway.process
payment.refund
sales.order.void
inventory.adjust
inventory.adjust.approve
promotion.activate
voucher.redeem
loyalty.redeem
user.role.assign
```

## 27.3 Offline policy

Offline execution harus memvalidasi:

```text
terminal active
AND device active
AND user authorized
AND offline policy active
AND quota available
AND cached data valid
AND operation offline-safe
```

Queued transaction tetap harus direvalidasi ketika sync.

---

# 28. Device and Terminal Authorization

POS request context minimal:

```text
tenant_id
company_id
store_id
terminal_id
device_id
installation_id
user_id
session_id
```

Server tidak boleh menerima nilai ini sebagai trusted authorization claim hanya karena dikirim client.

Server harus resolve/verify melalui registered device and terminal context.

---

# 29. API Authorization Mapping

Contoh canonical mapping:

| API | Permission |
|---|---|
| POST /sales/orders | sales.order.create |
| POST /sales/orders/{id}/confirm | sales.order.confirm |
| POST /sales/orders/{id}/cancel | sales.order.cancel |
| POST /sales/orders/{id}/void | sales.order.void |
| POST /payments/{id}/process | payment.process |
| POST /payments/{id}/refund | payment.refund.request |
| POST /refunds/{id}/approve | sales.refund.approve |
| POST /pos/sessions | pos.session.open |
| POST /pos/sessions/{id}/close | pos.session.close |
| POST /inventory/adjustments | inventory.adjust.create |
| POST /inventory/adjustments/{id}/approve | inventory.adjust.approve |
| POST /promotions | promotion.create |
| POST /promotions/{id}/approve | promotion.approve |
| POST /promotions/{id}/activate | promotion.activate |
| POST /pos/sync | pos.sync |
| GET /audit/logs | audit.read |
| GET /audit/export | audit.export |

---

# 30. Permission Evaluation Pseudocode

```text
authorize(actor, command, resource):

    require actor.authenticated

    tenant = resolveTenant(actor, request)
    require actor.canAccessTenant(tenant)

    scope = resolveScope(actor, resource)
    require actor.canAccessScope(scope)

    permission = requiredPermission(command)
    require actor.hasPermission(permission)

    state = resource.currentState
    require transitionAllowed(command, state)

    require businessPolicyAllows(actor, command, resource)

    if requiresApproval(command, resource):
        require workflowAllows(command, resource)

    if violatesSoD(actor, command, resource):
        deny SOD_VIOLATION

    if requiresMFA(command):
        require recentMFA(actor)

    return ALLOW
```

Authorization failure must occur before mutation.

---

# 31. Error Codes

Recommended machine-readable errors:

```text
AUTHENTICATION_REQUIRED
TENANT_ACCESS_DENIED
SCOPE_ACCESS_DENIED
PERMISSION_DENIED
STATE_TRANSITION_NOT_ALLOWED
WORKFLOW_APPROVAL_REQUIRED
APPROVAL_NOT_AUTHORIZED
SOD_VIOLATION
MFA_REQUIRED
MFA_STEP_UP_REQUIRED
OFFLINE_OPERATION_NOT_ALLOWED
DEVICE_NOT_AUTHORIZED
TERMINAL_NOT_AUTHORIZED
POLICY_DENIED
```

Typical HTTP mapping:

```text
401 → unauthenticated
403 → authorization denied
409 → state / SoD / workflow conflict
422 → business validation failure
```

---

# 32. Database Model Recommendation

Current database already memiliki:

```text
identity.role
identity.permission
identity.user_role
identity.role_permission
identity.user_scope
```

Disarankan menambahkan explicit permission scope layer karena role permission saja tidak cukup untuk enterprise authorization.

Recommended conceptual model:

```text
role
  ↓
role_permission
  ↓
permission

user
  ↓
user_role
  ↓
role

user
  ↓
user_scope
```

Untuk permission yang memiliki scope-specific restriction, pertimbangkan:

```text
identity.permission_scope
```

dengan konsep:

```text
permission_id
scope_type
scope_id
effect
conditions
created_at
created_by
```

Namun implementasi v1 dapat tetap menggunakan `user_scope` sebagai baseline dan `permission_scope` ditambahkan jika kebutuhan ABAC menjadi nyata.

---

# 33. RBAC vs ABAC Boundary

Architecture v1 menggunakan:

```text
RBAC + Scope + Business Policy
```

Bukan full ABAC.

RBAC menjawab:

```text
WHO may perform WHAT?
```

Scope menjawab:

```text
WHERE?
```

Business policy menjawab:

```text
UNDER WHICH CONDITIONS?
```

Contoh:

```text
Store Manager
+ sales.refund.approve
+ Store Jakarta-01
+ refund amount <= configured threshold
+ requester != approver
+ MFA
```

baru:

```text
ALLOW
```

---

# 34. Permission Precedence

Untuk v1:

```text
Explicit deny policy
    >
Scope restriction
    >
Permission grant
```

Role inheritance tidak boleh digunakan untuk menghasilkan privilege yang sulit dilacak.

Disarankan tidak menggunakan nested role seperti:

```text
Admin
  inherits Manager
    inherits Supervisor
```

sebagai mekanisme utama.

Lebih mudah diaudit jika effective permission dapat dihitung secara eksplisit dari role assignments.

---

# 35. Permission Change Audit

Perubahan berikut wajib diaudit:

```text
ROLE_CREATED
ROLE_UPDATED
ROLE_PERMISSION_GRANTED
ROLE_PERMISSION_REVOKED
USER_ROLE_ASSIGNED
USER_ROLE_REVOKED
USER_SCOPE_GRANTED
USER_SCOPE_REVOKED
PRIVILEGED_ACCESS_GRANTED
PRIVILEGED_ACCESS_REVOKED
BREAK_GLASS_ACTIVATED
BREAK_GLASS_EXPIRED
```

Audit minimal:

```text
actor_id
subject_user_id
role_id
permission_id
scope
reason
request_id
trace_id
ip_address
user_agent
created_at
```

---

# 36. Read vs Write vs Export

Permission harus membedakan:

```text
read
create
update
submit
approve
execute
reverse
export
admin
```

Contoh:

```text
sales.read
sales.export
```

bukan satu permission:

```text
sales.access
```

Hal ini mengurangi over-privilege.

---

# 37. Sensitive Data Authorization

Field-level masking dapat diterapkan untuk:

```text
customer.phone
customer.email
payment.provider_reference
payment.card_last4
financial amounts
employee/security data
```

Contoh:

```text
payment.read
```

tidak otomatis berarti user boleh melihat seluruh sensitive provider metadata.

---

# 38. Authorization and Audit Transaction Boundary

Critical command transaction:

```text
BEGIN
 ↓
SET LOCAL tenant context
 ↓
SET LOCAL user context
 ↓
Idempotency check
 ↓
Authorization
 ↓
State validation
 ↓
Business mutation
 ↓
Audit record
 ↓
Outbox event
 ↓
Idempotency result
 ↓
COMMIT
```

Jika authorization gagal:

```text
NO business mutation
NO success audit
```

Security failure dapat tetap menghasilkan security event sesuai policy.

---

# 39. RLS Relationship

Application authorization dan RLS memiliki tanggung jawab berbeda.

Application layer:

```text
business authorization
permission
workflow
SoD
MFA
policy
```

RLS:

```text
tenant / row isolation defense-in-depth
```

RLS tidak menggantikan permission model.

Permission model juga tidak menggantikan RLS.

---

# 40. Connection Pool Safety

Setiap transaction yang mengakses tenant-scoped data wajib menetapkan context secara transaction-local:

```sql
SET LOCAL app.tenant_id = '...';
SET LOCAL app.user_id = '...';
SET LOCAL app.store_id = '...';
```

Tidak boleh mengandalkan session-level state yang dapat bocor antar request melalui connection pool.

---

# 41. High-Risk Operation Catalog

Minimal high-risk:

```text
sales.order.void
sales.refund.approve
payment.refund.approve
inventory.adjust.approve
inventory.stock_opname.post
promotion.approve
promotion.activate
price_rule.approve
user.role.assign
role.permission.assign
mfa.manage
system.config.update
integration.config.update
```

Semua high-risk operation:

```text
permission controlled
+ scope controlled
+ audited
+ policy controlled
```

MFA/approval requirements configurable berdasarkan risk class.

---

# 42. Emergency and Exceptional Operations

Exceptional command tidak boleh menggunakan direct database update.

Contoh salah:

```sql
UPDATE sales.sales_order
SET status = 'VOIDED'
WHERE id = ...;
```

Contoh benar:

```text
VoidOrder command
    ↓
authorization
    ↓
state validation
    ↓
workflow/policy
    ↓
audit
    ↓
outbox
    ↓
transaction
```

---

# 43. Permission Testing Matrix

## Positive tests

```text
Cashier can create sale within terminal scope
Supervisor can void within store scope
Finance can approve eligible refund
Promo Admin can create promotion
Warehouse can create stock adjustment
Auditor can read audit
```

## Negative tests

```text
Cashier cannot void
Cashier cannot approve refund
User cannot access another tenant
Store Manager cannot access another store outside scope
Promotion creator cannot approve own promotion
Adjustment creator cannot approve own adjustment
Auditor cannot mutate transaction
Security Admin cannot automatically void sale
```

---

# 44. Cross-Tenant Test Cases

Mandatory:

```text
User tenant A → resource tenant B = DENY

User tenant A + valid permission
→ tenant B resource
→ DENY

Client changes X-Tenant-ID
→ DENY unless authenticated identity has authorized tenant scope

Pooled connection reused
→ no tenant context leakage
```

---

# 45. Cross-Store Test Cases

```text
User Store A + sales.void
→ Sale Store A = ALLOW

User Store A + sales.void
→ Sale Store B = DENY

Regional user + sales.read
→ Store within Region = ALLOW

Regional user + sales.void without explicit void permission
→ DENY
```

---

# 46. SoD Test Cases

```text
Refund requester = approver
→ DENY SOD_VIOLATION

Promotion creator = approver
→ DENY SOD_VIOLATION

Adjustment creator = approver
→ DENY SOD_VIOLATION

PO creator = approver
→ DENY SOD_VIOLATION
```

---

# 47. Permission Revocation

Permission revocation must take effect server-side without relying on client cache.

For active sessions:

```text
new command
→ effective permission evaluated
```

Cached frontend permissions are UI hints only.

Critical privileged commands may additionally require:

```text
fresh authorization
or
recent authentication
```

---

# 48. Role Assignment Rules

Role assignment should validate:

```text
actor has user.role.assign
AND actor scope contains target user scope
AND target role is assignable by actor
AND privileged role policy passes
AND MFA policy passes
AND SoD policy passes
```

System roles should not be assignable by ordinary business managers.

---

# 49. Role and Permission Administration Boundary

Recommended separation:

```text
Business Admin
    → business roles

Security Admin
    → user/role/security

System Admin
    → technical configuration

Auditor
    → observation only
```

No single role should receive unlimited privileges merely for convenience.

---

# 50. Effective Permission Calculation

Conceptual:

```text
effective_permissions(user) =
    union(
        permissions(role)
        for role in active_user_roles(user)
    )
```

Then restrict with:

```text
user_scope
+ permission policy
+ state
+ business condition
+ SoD
```

Do not materialize a permanent superuser permission list in the browser.

---

# 51. Caching Authorization

Authorization cache may be used for performance, but:

```text
cache ≠ source of truth
```

After privileged role change, cache invalidation should be triggered.

For critical operations, stale permission cache must not create privilege escalation.

Recommended:

```text
short TTL
+ event-driven invalidation
+ deny on uncertain critical authorization
```

---

# 52. API Middleware Boundary

Go backend request pipeline:

```text
HTTP
 ↓
Request ID / Trace
 ↓
Authentication middleware
 ↓
Tenant resolver
 ↓
Device / Terminal resolver
 ↓
Authorization middleware
 ↓
Handler
 ↓
Application Use Case
 ↓
Domain
```

Namun command-specific authorization harus tetap berada di application/use-case layer karena permission dapat bergantung pada resource state dan business context.

---

# 53. Recommended Go Authorization Interfaces

Conceptual interfaces:

```go
type Authorizer interface {
    Authorize(ctx context.Context, action Action, resource Resource) error
}

type ScopeChecker interface {
    CanAccess(ctx context.Context, actor Actor, resource Resource) error
}

type PolicyEvaluator interface {
    Evaluate(ctx context.Context, policy Policy, input PolicyInput) error
}
```

Tidak disarankan membuat satu `GodAuthorizationService` yang mengetahui seluruh domain secara langsung.

Authorization policy dapat memiliki domain-specific evaluators.

---

# 54. Frontend Authorization

Frontend boleh menggunakan permission untuk:

```text
hide button
hide menu
disable action
show warning
```

Tetapi frontend authorization bukan security boundary.

Contoh:

```typescript
can("sales.order.void")
```

hanya UX control.

Server tetap wajib melakukan:

```text
permission
scope
state
policy
SoD
```

---

# 55. Offline Frontend Authorization

Offline client dapat menggunakan cached effective permissions untuk UX.

Namun queued command harus menyertakan:

```text
user_id
terminal_id
device_id
client_sequence
idempotency_key
```

Server akan memvalidasi ulang.

Client tidak boleh memalsukan:

```text
role
permission
scope
approval
```

---

# 56. Permission Auditability

Untuk setiap critical command, audit harus dapat menjawab:

```text
Who?
What?
When?
Where?
Which tenant?
Which store?
Which terminal?
Which permission?
Which role?
Which state transition?
Which approval?
Which policy?
Which request?
Which trace?
Was MFA used?
Was operation online/offline?
```

---

# 57. Audit Example

```json
{
  "action": "SALE_VOIDED",
  "actor_id": "user-123",
  "tenant_id": "tenant-001",
  "company_id": "company-001",
  "store_id": "store-jkt-01",
  "terminal_id": "pos-01",
  "resource_type": "sales_order",
  "resource_id": "order-123",
  "permission": "sales.order.void",
  "from_state": "COMPLETED",
  "to_state": "VOIDED",
  "reason": "Customer cancellation",
  "request_id": "req-123",
  "trace_id": "trace-123",
  "created_at": "..."
}
```

Sensitive credentials must never be placed in audit metadata.

---

# 58. Permission Matrix Summary

## Cashier

```text
READ:
  product
  price
  customer
  promotion
  sales
  payment

WRITE:
  sales.create
  sales.confirm
  payment.process
  session.open
  session.close

DENY BY DEFAULT:
  void
  refund approval
  stock adjustment approval
  promotion administration
  role administration
```

## Supervisor

```text
Cashier
+
operational cancellation
void
refund request
inventory adjustment create
variance review
```

## Store Manager

```text
Supervisor
+
store-level approvals
refund approval
inventory adjustment approval
stock transfer approval
```

subject to SoD.

## Finance

```text
payment reconciliation
refund approval
finance reporting
financial audit
```

## Warehouse

```text
inventory
transfer
receipt
stock opname
```

## Promo Admin

```text
promotion
pricing
approval
activation
```

## Auditor

```text
read + audit + export
no mutation
```

## Security Admin

```text
identity + role + MFA + session security
```

## System Admin

```text
technical configuration + integration administration
```

---

# 59. Architecture Decisions Locked by This Document

The following are now normative for Permission Matrix v1:

1. Authorization is deny-by-default.
2. Permission never bypasses scope.
3. Scope never grants permission by itself.
4. Critical state changes use explicit commands.
5. Approval is separate from permission to request/create.
6. SoD is enforced server-side.
7. Audit is mandatory for privileged actions.
8. RLS is defense-in-depth, not the entire authorization model.
9. Tenant header is not trusted authorization.
10. Frontend permission checks are UX only.
11. Offline authorization is provisional; server is authoritative.
12. Service accounts are separate from human roles.
13. Export is a separate permission from read.
14. Sensitive data visibility can be restricted separately from resource read.
15. Privileged operations may require MFA/step-up authentication.
16. Break-glass is exceptional, temporary, audited and MFA protected.
17. Role inheritance is not the primary authorization mechanism.
18. Direct database status mutation is prohibited.
19. Stock ledger and audit ledger have no generic update/delete permissions.
20. Permission changes themselves are audited.

---

# 60. Acceptance Criteria

Permission Matrix v1 is considered implemented only when:

### Authorization

- [ ] Every protected API endpoint maps to a permission.
- [ ] Every critical command validates permission server-side.
- [ ] Authorization is deny-by-default.
- [ ] No client-supplied tenant ID can bypass authorization.

### Scope

- [ ] Tenant scope is enforced.
- [ ] Company scope is enforced.
- [ ] Region scope is enforced where applicable.
- [ ] Store scope is enforced.
- [ ] Warehouse scope is enforced.
- [ ] Terminal scope is enforced for POS commands.
- [ ] Cross-scope access tests pass.

### State

- [ ] Every critical state transition has an explicit permission.
- [ ] Invalid transitions are rejected.
- [ ] Direct status PATCH is unavailable for critical state.

### Workflow

- [ ] Approval permissions are separate from create/request permissions.
- [ ] Approval state is validated.
- [ ] Approval scope is validated.

### SoD

- [ ] Refund requester cannot approve own refund where policy requires separation.
- [ ] Promotion creator cannot approve own promotion.
- [ ] Inventory adjustment creator cannot approve own adjustment.
- [ ] PO creator cannot approve own PO.

### Security

- [ ] Privileged operations enforce MFA where policy requires.
- [ ] Break-glass access is time limited.
- [ ] Role changes are audited.
- [ ] Permission changes are audited.

### Audit

- [ ] Critical authorization actions are auditable.
- [ ] Audit records are immutable.
- [ ] Sensitive credentials are never logged.

### Offline

- [ ] Offline-safe commands have explicit policy.
- [ ] Offline client permissions are not trusted as final authorization.
- [ ] Sync revalidates permission, scope, device and state.

---

# 61. Recommended Next Database Changes

The current database already has:

```text
identity.role
identity.permission
identity.user_role
identity.role_permission
identity.user_scope
```

Before implementation, review/finalize:

```text
1. tenant-aware composite foreign keys for authorization-sensitive entities
2. user_scope uniqueness / overlap rules
3. role assignment audit history
4. permission assignment audit history
5. optional identity.permission_scope
6. privileged access/session policy
7. MFA enforcement policy
8. service account model
9. explicit authorization policy configuration
```

`identity.permission_scope` should only be added when real use cases require permission-specific scope restrictions. Do not introduce unrestricted ABAC prematurely.

---

# 62. Next Artifact

Setelah Permission Matrix v1, urutan berikutnya tetap:

```text
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
Implementation Repository / Monorepo Blueprint
        ↓
CI/CD Architecture
        ↓
Go Backend Foundation
```

Permission Matrix v1 menjadi referensi authorization untuk Event Catalog, Audit Action Catalog, OpenAPI v1.1, dan implementation layer.
