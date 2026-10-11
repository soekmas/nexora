# NEXORA — Phase 2 Batch 1 Pre-Implementation Design & Readiness Review

**Document ID:** `docs/architecture/20-phase2-batch1-pre-implementation-review.md`  
**Version:** 1.0.0  
**Status:** DRAFT — Pre-Implementation Review  
**Target Modules:** `identity`, `organization`, `master`, `crm`  
**Governing Documents:**
- `AGENTS.md`
- `IMPLEMENTATION_GUIDE.md`
- `TASKS.md`
- `docs/architecture/09-architecture-lock.md`
- `docs/architecture/10-state-machine-catalog.md`
- `docs/architecture/11-permission-matrix.md`
- `docs/architecture/12-event-catalog.md`
- `docs/architecture/13-audit-action-catalog.md`
- `docs/architecture/17-foundation-implementation-plan.md`
- `docs/architecture/18-operational-readiness-guide.md`
- `docs/architecture/19-phase2-domain-implementation-plan.md`
- `docs/architecture/IMPLEMENTATION_STATUS.md`

---

## 1. Executive Summary & Verdict

### Readiness Verdict: **`READY WITH DOCUMENTED DECISIONS`**

Foundation Phase 1 is verified and accepted with documented limitations. The PostgreSQL schemas (`identity`, `organization`, `master`, `crm`) and forced RLS policies exist in the database baseline (`000001_baseline_hardened.sql` through `000005_auth_rls_bootstrap.sql`). All platform services (connection pool, RLS context, transaction manager, structured logger, RFC 8785 idempotency, append-only audit, transactional outbox, and OTLP tracing) are fully operational.

However, concrete pre-implementation design gaps must be approved before writing Go application code:
1. **Password Hashing Implementation:** Argon2id parameters must be locked, and `golang.org/x/crypto/argon2` approved for addition to `go.mod`.
2. **Missing CRM Audit Catalog Seeds:** In `db/migrations/000004_audit_hardening.sql`, CRM audit actions (`CUSTOMER_CREATED`, `CUSTOMER_UPDATED`, etc.) were omitted. Due to the `fk_audit_log_action` constraint on `audit.audit_log`, a forward migration (`000006_crm_audit_actions.sql`) is required before CRM audit logs can be written.
3. **Canonical Permission Seeds:** `identity.permission` has no baseline seed rows in migration `000001`; a canonical permission catalog seed strategy must be approved.
4. **Lockout Policy Parameters:** Failed login threshold and temporary lockout duration must be locked.

---

## 2. Deep-Dive Review of Batch 1 Modules

### 2.1 Identity & Access Management (`backend/internal/identity`)
- **Entity Ownership & Schema:**
  - Tables: `identity.user_account`, `identity.role`, `identity.permission`, `identity.user_role`, `identity.role_permission`, `identity.user_scope`, `identity.session`, `identity.mfa_factor`, `identity.login_attempt`, `identity.refresh_token`.
- **Tenant Boundaries & Uniqueness:**
  - `user_account`: `UNIQUE (tenant_id, username)`, `UNIQUE (tenant_id, email)` (case-insensitive via `citext`).
  - `role`: `UNIQUE (tenant_id, code)`.
  - `permission`: Global catalog `UNIQUE (code)`, `UNIQUE (module, action)`.
  - `user_scope`: Tenant-scoped mapping to `company_id`, `region_id`, `store_id`, `warehouse_id`, `terminal_id`.
- **Lifecycle & State Machine:**
  - Statuses: `ACTIVE`, `LOCKED`, `DISABLED`, `PENDING`.
  - Transitions:
    - `PENDING → ACTIVE` (account activation / credential set).
    - `ACTIVE → LOCKED` (automatic upon 5 failed attempts within 15 minutes; resets upon expiry).
    - `LOCKED → ACTIVE` (manual admin unlock or timeout expiration).
    - `ACTIVE → DISABLED` (admin deactivation).
    - `DISABLED → ACTIVE` (admin reactivation).
- **Audit Actions & Domain Events:**
  - Audit Actions: `USER_CREATED`, `USER_UPDATED`, `USER_DISABLED`, `USER_ENABLED`, `USER_LOCKED`, `USER_ROLE_ASSIGNED`, `USER_ROLE_REVOKED`, `USER_SCOPE_CHANGED`, `LOGIN_SUCCESS`, `LOGIN_FAILURE`.
  - Domain Events: `identity.user.created.v1`, `identity.user.updated.v1`, `identity.user.locked.v1`, `identity.role.assigned.v1`.
- **Concurrency & Failure Recovery:**
  - Password hashing via Argon2id. Atomic transaction combines user write, audit record, and outbox event.
  - Segregation of Duties (SoD) hook prevents a user from granting administrative roles to themselves.

### 2.2 Organization Hierarchy (`backend/internal/organization`)
- **Entity Ownership & Schema:**
  - Tables: `organization.company`, `organization.region`, `organization.store`, `organization.warehouse`, `organization.terminal`.
- **Tenant Boundaries & Uniqueness:**
  - `company`: `UNIQUE (tenant_id, code)`.
  - `region`: `UNIQUE (company_id, code)`.
  - `store`: `UNIQUE (company_id, code)`.
  - `warehouse`: `UNIQUE (company_id, code)`.
  - `terminal`: `UNIQUE (store_id, code)`, `UNIQUE (device_identifier)`.
- **Foreign Key Relationships & Deletion Policy:**
  - Cascading deletes are strictly disabled. Entities reference `company_id` and `store_id`.
  - Soft-deactivation only: `status` transitions (`ACTIVE → INACTIVE`).
- **Audit Actions & Domain Events:**
  - Audit Actions: `COMPANY_CREATED`, `COMPANY_UPDATED`, `REGION_CREATED`, `REGION_UPDATED`, `STORE_CREATED`, `STORE_UPDATED`, `STORE_DISABLED`, `WAREHOUSE_CREATED`, `WAREHOUSE_UPDATED`, `TERMINAL_REGISTERED`, `TERMINAL_DISABLED`, `TERMINAL_RETIRED`.
  - Domain Events: `org.company.created.v1`, `org.store.created.v1`, `org.terminal.registered.v1`.
- **Scope Enforcement:**
  - Any operation within a store requires the authenticated principal's scope to match `store_id` (or have parent `company_id` scope).

### 2.3 Master Data & Catalog (`backend/internal/master`)
- **Entity Ownership & Schema:**
  - Tables: `master.brand`, `master.category`, `master.uom`, `master.product`, `master.product_variant`, `master.product_barcode`, `master.serial_number`.
- **Tenant Boundaries & Uniqueness:**
  - `brand`: `UNIQUE (tenant_id, code)`.
  - `category`: `UNIQUE (tenant_id, code)` with hierarchical self-reference `parent_id REFERENCES master.category(id)`.
  - `uom`: `UNIQUE (tenant_id, code)` with decimal scale precision ($0 \le \text{scale} \le 6$).
  - `product`: `UNIQUE (tenant_id, sku)`. References `company_id`, `brand_id`, `category_id`, `base_uom_id`.
  - `product_variant`: `UNIQUE (tenant_id, sku)`. References `product_id`.
  - `product_barcode`: `UNIQUE (tenant_id, barcode)`.
  - `serial_number`: `UNIQUE (tenant_id, serial_no)`.
- **Lifecycle & State Machine:**
  - `product`: `ACTIVE`, `INACTIVE`, `DISCONTINUED`.
  - `serial_number`: `RECEIVED → AVAILABLE → RESERVED → SOLD (or RETURNED / DAMAGED / SERVICE / SCRAPPED)`.
- **Audit Actions & Domain Events:**
  - Audit Actions: `PRODUCT_CREATED`, `PRODUCT_UPDATED`, `PRODUCT_ACTIVATED`, `PRODUCT_DEACTIVATED`, `PRODUCT_VARIANT_CREATED`, `PRODUCT_VARIANT_UPDATED`, `BARCODE_CREATED`, `BARCODE_CHANGED`, `SERIAL_REGISTERED`, `SERIAL_STATUS_CHANGED`, `CATEGORY_CREATED`, `CATEGORY_UPDATED`, `UOM_CREATED`, `UOM_UPDATED`.
  - Domain Events: `catalog.product.created.v1`, `catalog.product.updated.v1`, `catalog.variant.created.v1`, `catalog.barcode.assigned.v1`.

### 2.4 Customer Relationship Management (`backend/internal/crm`)
- **Entity Ownership & Schema:**
  - Tables: `crm.customer_group`, `crm.customer`, `crm.customer_address`.
- **Tenant Boundaries & Uniqueness:**
  - `customer_group`: `UNIQUE (tenant_id, code)`.
  - `customer`: `UNIQUE (tenant_id, customer_no)`. Non-unique composite indexes on `(tenant_id, email)` and `(tenant_id, phone)`.
  - `customer_address`: References `customer_id`.
- **Lifecycle & State Machine:**
  - `customer`: `ACTIVE`, `INACTIVE`.
- **Audit Actions & Domain Events:**
  - Audit Actions: `CUSTOMER_CREATED`, `CUSTOMER_UPDATED`, `CUSTOMER_DISABLED`, `CUSTOMER_ADDRESS_CREATED`, `CUSTOMER_ADDRESS_UPDATED`, `CUSTOMER_ADDRESS_DELETED`, `CUSTOMER_GROUP_CHANGED`.
  - Domain Events: `crm.customer.created.v1`, `crm.customer.updated.v1`.
- **Critical Discovery (Audit Constraint Gap):**
  - None of the CRM audit actions are seeded in `audit.audit_action_catalog`. Writing any audit record for CRM will fail PostgreSQL foreign key validation `fk_audit_log_action`.
  - **Remediation Required:** Forward migration `000006_crm_audit_actions.sql` must seed these actions prior to CRM audit writing.

---

## 3. Cross-Module Circular Dependency Analysis

### Identified Coupling Points:
1. `identity.user_scope` references `organization.company(id)`, `organization.region(id)`, `organization.store(id)`, `organization.warehouse(id)`, `organization.terminal(id)`.
2. `master.product` references `organization.company(id)`.
3. `organization` tables have nullable `created_by` / `updated_by` UUID fields pointing to user identifiers.

### Dependency Resolution:
- `identity` core (Users, Roles, Permissions, Auth) has **zero outbound foreign keys** to other domains. It can be constructed independently.
- `organization` depends only on primitive user UUIDs for audit actor attribution.
- The cross-domain binding entity `identity.user_scope` must be wired **after** `organization` entities exist.
- Therefore, the strict unidirectional build sequence must be:
  ```text
  Identity (Core Auth/Users/Roles)
       ↓
  Organization (Company, Store, Terminal)
       ↓
  Identity Scopes (user_scope operational binding)
       ↓
  Master Data (Products referencing Company)
       ↓
  CRM (Customers & Addresses)
  ```

---

## 4. Decision Register for Batch 1 Pre-Implementation Gaps

| Decision ID | Area | Architectural Question | Options & Tradeoffs | Approved Decision | Status | Approver |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **DEC-B1-001** | Identity | Password Hashing Implementation & Library | **Option A:** Argon2id via `golang.org/x/crypto/argon2` ($m=64\text{MB}, t=3, p=4$).<br>**Option B:** Standard `bcrypt` via `golang.org/x/crypto/bcrypt` (cost 12). | **Option A (Argon2id)**: Memory 64 MiB, time cost 3, parallelism 4, salt 16B, key 32B. | **APPROVED** | User (2026-10-11) |
| **DEC-B1-002** | CRM / Audit | Missing CRM Actions in `audit.audit_action_catalog` | **Option A:** Forward migration `000007_crm_audit_actions.sql` (renumbered per user Option 1 approval). | Forward migration deferred to future CRM increment; NOT authorized in Increment 1. | **APPROVED (Deferred to 000007)** | User (2026-10-11) |
| **DEC-B1-003** | Identity | Canonical Permission Seed Mechanism | **Option A:** Forward migration `000006_identity_permissions.sql` seeding canonical permission table (renumbered per user Option 1 approval). | Forward migration `000006_identity_permissions.sql` seeding canonical permission catalog. | **APPROVED (Allocated to 000006)** | User (2026-10-11) |
| **DEC-B1-004** | Organization | Terminal Device Identification Protocol | **Option A:** SHA-256 fingerprint generated from client hardware/browser tokens.<br>**Option B:** Cryptographically random UUID token issued during terminal registration. | **Option B**: Server-issued device token stored securely in client storage. | **APPROVED** | User (2026-10-11) |
| **DEC-B1-005** | Identity | Account Lockout Security Policy | **Option A:** 5 consecutive failures = 15-minute lock.<br>**Option B:** 3 consecutive failures = indefinite lock requiring admin unlock. | **Option A**: 5 failed attempts = 15-minute temporary lockout. | **APPROVED** | User (2026-10-11) |

> **User Authorization Note (2026-10-11):**  
> Option 1 approved for migration sequencing: `000006_identity_permissions.sql` is allocated to Identity permission seeding in this increment. `000007_crm_audit_actions.sql` is reserved for future CRM work and remains uncreated/unauthorized. Identity Increment 1 is explicitly authorized for implementation.


---

## 5. Recommended Execution Roadmap for Batch 1

Batch 1 should be decomposed into 4 small, test-driven increments:

### Increment 1: Identity & Credentials (`backend/internal/identity`)
- Implement User Account, Role, and Permission domain models and Pgx repository.
- Implement Argon2id hasher and validator (`HashPassword`, `VerifyPassword`).
- Implement User registration, status lifecycle (`ACTIVE`, `LOCKED`, `DISABLED`), and login credential verification.
- Unit & integration tests for password hashing, lockout logic, and tenant RLS isolation.

### Increment 2: Organization Hierarchy (`backend/internal/organization`)
- Implement Company, Region, Store, Warehouse, and Terminal domain models and repositories.
- Implement soft-deactivation rules and unique code constraints.
- Wire `identity.user_scope` operational binding against created stores and warehouses.
- Multi-tenant RLS isolation tests across store/terminal boundaries.

### Increment 3: Master Data & Catalog (`backend/internal/master`)
- Implement Brand, Category, UOM, Product, Product Variant, and Barcode models and repositories.
- Implement SKU and Barcode uniqueness validation.
- Implement Product status transitions (`ACTIVE`, `INACTIVE`, `DISCONTINUED`).
- Search index verification (GIN index on product name).

### Increment 4: CRM & Audit Hardening (`backend/internal/crm`)
- Prepare forward migration `000006_crm_audit_actions.sql` and `000007_seed_permissions.sql`.
- Implement Customer Group, Customer, and Address models and repositories.
- Implement phone/email search and customer status management.
- Integration tests verifying atomic audit write, outbox publishing, and multi-tenant isolation.

---

## 6. Batch 1 Acceptance Gate (Gate 2.1 Criteria)

Batch 1 implementation will be considered complete and ready for Gate 2.1 acceptance only when:
1. `go test -race ./backend/internal/{identity,organization,master,crm}/...` passes with **0 failures and 0 data races**.
2. Multi-tenant RLS isolation verified under `nexora_app` (`NOBYPASSRLS`): Tenant B cannot view or mutate Tenant A users, stores, products, or customers.
3. User scope boundary enforcement verified: Cashier scoped to Store A cannot execute commands on Store B.
4. Mutation commands atomically commit state change, append-only audit log, and transactional outbox event.
5. All endpoints strictly adhere to canonical `ErrorResponse` envelope.
6. `golangci-lint run ./...` reports 0 issues.
7. Redocly OpenAPI linting passes with 0 errors.
8. Migration checksum baseline remains consistent and locked.
