# NEXORA — Master Data Increment 3 Pre-Implementation Review

## Executive Summary

- **Target Increment:** Phase 2 / Batch 1 / Increment 3: Master Data & Catalog (`master`)
- **Review Date:** 2026-10-11
- **Status:** **READ-ONLY REVIEW & PLANNING (NO IMPLEMENTATION AUTHORIZED)**
- **Deliverable:** `docs/architecture/24-master-data-increment-pre-implementation-review.md`
- **Readiness Verdict:** **`READY WITH DOCUMENTED DECISIONS`**

This document provides a comprehensive, read-only pre-implementation review of the Master Data domain for the NEXORA Retail Business Platform. It reconciles the immutable baseline schema (`000001_baseline_hardened.sql`), approved forward migrations (`000002` through `000008`), canonical OpenAPI v1.0.0 specification, permission catalog, audit action catalog, event catalog, and architecture locks.

Six architectural decisions (`DEC-B1-008` through `DEC-B1-013`) are identified and formulated for user approval prior to implementation.

---

## 1. Review Scope and Governance Boundaries

### Strict Governance Mandate
1. **Read-Only Inspection:** No application code, migrations, tests, API contracts, or existing architecture documentation are modified by this review.
2. **Migration Immutability:**
   - Migrations `000001` through `000006` remain immutable.
   - Migration `000007_crm_audit_actions.sql` is strictly reserved for CRM and remains absent.
   - Migration `000008_organization_hardening.sql` belongs to Organization Increment 2 and is untouched.
   - No database migrations are created or executed.
3. **Database Security:** No operations against production, shared, or persistent databases were performed.
4. **Git State:** No Git commands or working tree commits were executed.
5. **Accepted Foundation Preservation:** Foundation Phase 1, Identity Increment 1, and Organization Increment 2 remain accepted baselines.

---

## 2. Sources Inspected

All canonical architectural and source documents specified in the review mandate were inspected:

| Document / Source | Path | Inspection Status |
|---|---|---|
| Platform Architecture Lock | [`docs/architecture/09-architecture-lock.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/09-architecture-lock.md) | Confirmed |
| State Machine Catalog | [`docs/architecture/10-state-machine-catalog.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/10-state-machine-catalog.md) | Confirmed |
| Canonical Permission Matrix | [`docs/architecture/11-permission-matrix.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/11-permission-matrix.md) | Confirmed |
| Canonical Event Catalog | [`docs/architecture/12-event-catalog.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/12-event-catalog.md) | Confirmed |
| Audit Action Catalog | [`docs/architecture/13-audit-action-catalog.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/13-audit-action-catalog.md) | Confirmed |
| Offline Sync Contract | [`docs/architecture/08-offline-sync-contract.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/08-offline-sync-contract.md) | Confirmed |
| Domain Security Review | [`docs/architecture/04-domain-security-review.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/04-domain-security-review.md) | Confirmed |
| Phase 2 Domain Plan | [`docs/architecture/19-phase2-domain-implementation-plan.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/19-phase2-domain-implementation-plan.md) | Confirmed |
| Batch 1 Pre-Implementation Review | [`docs/architecture/20-phase2-batch1-pre-implementation-review.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/20-phase2-batch1-pre-implementation-review.md) | Confirmed |
| Organization Increment 2 Acceptance Review | [`docs/architecture/23-organization-increment-acceptance-review.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/23-organization-increment-acceptance-review.md) | Confirmed |
| Current Implementation Status | [`docs/architecture/IMPLEMENTATION_STATUS.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/IMPLEMENTATION_STATUS.md) | Confirmed |
| Tasks Register | [`TASKS.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/TASKS.md) | Confirmed |
| OpenAPI v1.0.0 Specification | [`contracts/openapi/openapi-v1.0.0.yaml`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/contracts/openapi/openapi-v1.0.0.yaml) | Confirmed |
| Hardened Baseline Migration | [`db/migrations/000001_baseline_hardened.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000001_baseline_hardened.sql) | Confirmed |
| Audit Hardening Migration | [`db/migrations/000004_audit_hardening.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000004_audit_hardening.sql) | Confirmed |
| Permission Seeding Migration | [`db/migrations/000006_identity_permissions.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000006_identity_permissions.sql) | Confirmed |
| Organization Hardening Migration | [`db/migrations/000008_organization_hardening.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000008_organization_hardening.sql) | Confirmed |

*Note on requested paths:* `contracts/openapi/openapi.yaml` is located at `contracts/openapi/openapi-v1.0.0.yaml`. All references have been reconciled against the actual file.

---

## 3. Confirmed Master Data Scope vs. Exclusions

Based on `19-phase2-domain-implementation-plan.md` Section 4.3, `20-phase2-batch1-pre-implementation-review.md` Section 2.3, and `000001_baseline_hardened.sql`, the candidate master entities are classified as follows:

| Candidate Entity | Classification | Evidence / Rationale |
|---|---|---|
| **Product Master (`master.product`)** | **Confirmed in Scope** | Defined in baseline lines 326–348; OpenAPI `/products`; permissions `product.read`, `product.create`, `product.update`, `product.activate`, `product.deactivate`. |
| **Product Variant (`master.product_variant`)** | **Confirmed in Scope** | Defined in baseline lines 350–362; sub-entity of product; audit actions `PRODUCT_VARIANT_CREATED`, `PRODUCT_VARIANT_UPDATED`. |
| **Product Barcode (`master.product_barcode`)** | **Confirmed in Scope** | Defined in baseline lines 364–374; OpenAPI `/products/{id}/barcodes`, `/products/lookup/barcode/{barcode}`; permissions `product.barcode.read`, `product.barcode.manage`. |
| **Unit of Measure (`master.uom`)** | **Confirmed in Scope** | Defined in baseline lines 317–324; required foreign key on `master.product(base_uom_id)` (NOT NULL); permission `uom.read`. |
| **Product Category (`master.category`)** | **Confirmed in Scope** | Defined in baseline lines 304–315; foreign key on `master.product(category_id)`; hierarchical tree (`parent_id`); permission `category.read`. |
| **Brand (`master.brand`)** | **Confirmed in Scope** | Defined in baseline lines 292–302; foreign key on `master.product(brand_id)`; permission `brand.read`. |
| **Serial Number (`master.serial_number`)** | **Proposed for Partial / Read Scope** | Defined in baseline lines 376–386. Physical serial lifecycle (`RECEIVED`, `AVAILABLE`, `RESERVED`, `SOLD`) belongs to Inventory / Sales. Increment 3 should support serial definition/read, while inventory state transitions are deferred to Batch 2. |
| **Customer (`crm.customer`)** | **Explicitly Out of Scope** | Belongs to Phase 2 / Batch 1 / Increment 4 (`crm`). |
| **Customer Group (`crm.customer_group`)** | **Explicitly Out of Scope** | Belongs to Increment 4 (`crm`). |
| **Customer Address (`crm.customer_address`)** | **Explicitly Out of Scope** | Belongs to Increment 4 (`crm`). |
| **Price Rules & Lists (`pricing.*`)** | **Explicitly Out of Scope** | Belongs to Phase 2 / Batch 2 (`pricing`). Master Data stores zero pricing information per architecture lock. |
| **Stock Balances (`inventory.stock`)** | **Explicitly Out of Scope** | Belongs to Phase 2 / Batch 2 (`inventory`). Master Data stores zero inventory quantity state. |
| **UOM Conversion Matrix** | **Not Defined by Available Evidence** | Baseline schema does not contain a `master.uom_conversion` table; UOM conversions are not present in baseline. |

---

## 4. Existing Database and Schema Analysis

All 7 Master Data tables are already defined in `db/migrations/000001_baseline_hardened.sql`:

```
┌────────────────────────────────────────────────────────────────────────┐
│                          master.product                                │
├────────────────────────────────────────────────────────────────────────┤
│ id: uuid (PK)                                                          │
│ tenant_id: uuid (NOT NULL)                                             │
│ company_id: uuid (FK organization.company, NULLABLE)                   │
│ sku: varchar(80) (UNIQUE with tenant_id)                               │
│ name: varchar(200)                                                     │
│ brand_id: uuid (FK master.brand, NULLABLE)                             │
│ category_id: uuid (FK master.category, NULLABLE)                       │
│ base_uom_id: uuid (FK master.uom, NOT NULL)                            │
│ product_type: varchar(30) ('STOCKABLE','SERVICE','CONSUMABLE','BUNDLE')│
│ track_serial: boolean (DEFAULT false)                                  │
│ track_lot: boolean (DEFAULT false)                                     │
│ tax_category: varchar(50)                                              │
│ status: varchar(20) ('ACTIVE','INACTIVE','DISCONTINUED')               │
│ created_at / updated_at: timestamptz                                   │
│ created_by / updated_by: uuid                                          │
│ version: bigint (DEFAULT 1, CHECK version > 0)                         │
└────────────────┬──────────────────────┬────────────────────────────────┘
                 │ 1:N                  │ 1:N
                 ▼                      ▼
┌─────────────────────────────────┐   ┌──────────────────────────────────┐
│     master.product_variant      │   │      master.product_barcode      │
├─────────────────────────────────┤   ├──────────────────────────────────┤
│ id: uuid (PK)                   │   │ id: uuid (PK)                    │
│ tenant_id: uuid (NOT NULL)      │   │ tenant_id: uuid (NOT NULL)       │
│ product_id: uuid (FK product)   │   │ product_id: uuid (FK product)    │
│ sku: varchar(80) (UNIQUE/tenant)│   │ variant_id: uuid (FK variant, opt│
│ name: varchar(200)              │   │ barcode: varchar(100) (UNIQUE/ten│
│ attributes: jsonb (DEFAULT '{}')│   │ barcode_type: varchar(30) ('EAN')│
│ status: varchar(20)             │   │ is_primary: boolean              │
│ created_at / updated_at         │   │ status: varchar(20)              │
└─────────────────────────────────┘   └──────────────────────────────────┘
```

### Reference Entities

1. **`master.brand`**:
   - Primary Key: `id uuid`
   - Tenant Scoped: `tenant_id uuid NOT NULL`, `UNIQUE (tenant_id, code)`
   - Status: `CHECK (status IN ('ACTIVE','INACTIVE'))`
   - Concurrency / Audit: No `version` column in baseline; no `created_by`/`updated_by`.
2. **`master.category`**:
   - Primary Key: `id uuid`
   - Tenant Scoped: `tenant_id uuid NOT NULL`, `UNIQUE (tenant_id, code)`
   - Hierarchy: `parent_id uuid REFERENCES master.category(id)` (self-referencing tree).
   - Status: `CHECK (status IN ('ACTIVE','INACTIVE'))`
   - Concurrency / Audit: No `version` column in baseline.
3. **`master.uom`**:
   - Primary Key: `id uuid`
   - Tenant Scoped: `tenant_id uuid NOT NULL`, `UNIQUE (tenant_id, code)`
   - Precision: `precision_scale smallint NOT NULL DEFAULT 0 CHECK (precision_scale BETWEEN 0 AND 6)`
   - Status / Timestamps: Baseline has no `status` column (all rows active by presence) and no timestamps.

### Database Constraints and Indexes
- **Optimistic Concurrency:** `master.product.version` is protected by `CHECK (version > 0)`.
- **Search Indexes:** `ix_product_name ON master.product USING gin (to_tsvector('simple', name))` provides full-text product catalog search.
- **Foreign Key Indexes:**
  - `ix_product_category ON master.product(category_id)`
  - `ix_barcode_product ON master.product_barcode(product_id)`
  - `ix_serial_product ON master.serial_number(product_id)`
- **RLS Status in Baseline:** All 7 tables in `master` received `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, and policy `tenant_isolation_v11` (`USING (tenant_id = platform.current_tenant_id()) WITH CHECK (tenant_id = platform.current_tenant_id())`) in `000001_baseline_hardened.sql` lines 1536–1552.

---

## 5. Identity and Organization Dependencies

### Dependency Direction
`transport → application → domain → ports`

The Master Data domain depends on:
1. **Identity:** Authenticated principal (`auth.Principal`), tenant context extraction, user ID attribution for `created_by`/`updated_by`, and permission enforcement.
2. **Organization:** Company referential validation. `master.product.company_id` is an optional foreign key to `organization.company(id)`. When specified, the service must verify that `company.tenant_id == principal.tenant_id`.
3. **Platform Infrastructure:** Transaction Manager (`tm.WithinTx`), append-only audit store (`audit.Store`), transactional outbox writer (`outbox.Writer`), and error envelope mapping (`apperr.AppError`).

### Tenant & Scope Ownership Model
- **Product Ownership:** `tenant_id` is mandatory on all master records. `company_id` is optional.
  - If `company_id IS NULL`: The product is tenant-wide (available to all companies, stores, and channels within the tenant).
  - If `company_id IS NOT NULL`: The product is company-restricted (visible and tradable only within that specific company).
- **Cross-Tenant Leakage Prevention:** Database foreign keys and forced RLS prevent associating a product with a company, brand, category, or UOM from a different tenant.

---

## 6. Permissions, Audit, and Events Analysis

### Permissions (Seeded in `000006_identity_permissions.sql`)
The following canonical Master Data permissions were seeded in migration `000006`:
- `product.read` — Read Product catalog
- `product.create` — Create Product
- `product.update` — Update Product
- `product.activate` — Activate Product
- `product.deactivate` — Deactivate Product
- `product.barcode.read` — Read Barcodes
- `product.barcode.manage` — Manage / assign Barcodes
- `serial.read` — Read Serial Numbers
- `serial.manage` — Manage Serial Numbers
- `uom.read` — Read Units of Measure
- `category.read` — Read Categories
- `brand.read` — Read Brands

*Gap Identified:* No dedicated write permissions exist for Brand (`brand.create`), Category (`category.create`), or UOM (`uom.create`). See Decision `DEC-B1-008` & `DEC-B1-009`.

### Audit Actions (Seeded in `000004_audit_hardening.sql`)
The following 14 audit actions were seeded in `audit.audit_action_catalog`:
- `PRODUCT_CREATED`
- `PRODUCT_UPDATED`
- `PRODUCT_ACTIVATED`
- `PRODUCT_DEACTIVATED`
- `PRODUCT_VARIANT_CREATED`
- `PRODUCT_VARIANT_UPDATED`
- `BARCODE_CREATED`
- `BARCODE_CHANGED`
- `SERIAL_REGISTERED`
- `SERIAL_STATUS_CHANGED`
- `CATEGORY_CREATED`
- `CATEGORY_UPDATED`
- `UOM_CREATED`
- `UOM_UPDATED`

*Gap Identified:* `BRAND_CREATED` and `BRAND_UPDATED` were NOT seeded in `000004`. Direct insertion of `BRAND_CREATED` into `audit.audit_log` would fail foreign key constraint `fk_audit_log_action`. See Decision `DEC-B1-008`.

### Domain Events (Per `19-phase2-domain-implementation-plan.md`)
The canonical transactional outbox events to be emitted:
- `catalog.product.created.v1`
- `catalog.product.updated.v1`
- `catalog.product.activated.v1`
- `catalog.product.deactivated.v1`
- `catalog.variant.created.v1`
- `catalog.variant.updated.v1`
- `catalog.barcode.assigned.v1`

---

## 7. Proposed API Surface

Conforming to OpenAPI v1.0.0 and `/api/v1` canonical routing conventions:

### Product Endpoints
| Method | Path | Required Permission | Description |
|---|---|---|---|
| `GET` | `/api/v1/master/products` | `product.read` | List products with pagination, search, category/brand filter |
| `POST` | `/api/v1/master/products` | `product.create` | Create product (with optional initial variants & barcodes) |
| `GET` | `/api/v1/master/products/{id}` | `product.read` | Get product details with variants, barcodes, UOM |
| `PUT` | `/api/v1/master/products/{id}` | `product.update` | Update product details with optimistic locking (`version`) |
| `POST` | `/api/v1/master/products/{id}/activate` | `product.activate` | Transition product from `INACTIVE` to `ACTIVE` |
| `POST` | `/api/v1/master/products/{id}/deactivate` | `product.deactivate` | Transition product from `ACTIVE` to `INACTIVE` |

### Barcode Endpoints
| Method | Path | Required Permission | Description |
|---|---|---|---|
| `GET` | `/api/v1/master/products/{id}/barcodes` | `product.barcode.read` | List barcodes assigned to product / variants |
| `POST` | `/api/v1/master/products/{id}/barcodes` | `product.barcode.manage` | Assign new barcode to product / variant |
| `GET` | `/api/v1/master/products/lookup/barcode/{barcode}` | `product.read` | Fast barcode scan lookup for POS and inventory |

### Reference Data Endpoints
| Method | Path | Required Permission | Description |
|---|---|---|---|
| `GET` | `/api/v1/master/brands` | `brand.read` | List brands |
| `POST` | `/api/v1/master/brands` | `product.create` (or `brand.create`) | Create brand |
| `GET` | `/api/v1/master/categories` | `category.read` | List category tree |
| `POST` | `/api/v1/master/categories` | `product.create` (or `category.create`) | Create category |
| `GET` | `/api/v1/master/uoms` | `uom.read` | List units of measure |
| `POST` | `/api/v1/master/uoms` | `product.create` (or `uom.create`) | Create unit of measure |

*OpenAPI Compatibility Note:* Root `/api/v1/products` route will be aliased to `/api/v1/master/products` to preserve strict compatibility with client generators targeting `contracts/openapi/openapi-v1.0.0.yaml`.

---

## 8. Business Rules and State-Machine Analysis

### Product Lifecycle State Machine

```
              ┌─────────────┐
   Create ───►│   ACTIVE    │◄─────────────┐
              └──────┬──────┘              │
                     │                     │
          Deactivate │                     │ Activate
                     ▼                     │
              ┌─────────────┐              │
              │  INACTIVE   │──────────────┘
              └──────┬──────┘
                     │
         Discontinue │
                     ▼
              ┌─────────────┐
              │DISCONTINUED │ (Terminal state)
              └─────────────┘
```

1. **State Invariants:**
   - A product in `INACTIVE` status cannot be added to new Sales Orders, POs, or POS carts (`PRODUCT_INACTIVE` error per `08-offline-sync-contract.md`).
   - `DISCONTINUED` is a terminal lifecycle state. A discontinued product cannot be reactivated.
   - Products are never hard-deleted once created.

2. **Uniqueness Constraints:**
   - Product SKU is strictly unique per tenant: `UNIQUE (tenant_id, sku)`.
   - Variant SKU is strictly unique per tenant: `UNIQUE (tenant_id, sku)`.
   - Barcode is strictly unique per tenant: `UNIQUE (tenant_id, barcode)`.
   - Brand Code is strictly unique per tenant: `UNIQUE (tenant_id, code)`.
   - Category Code is strictly unique per tenant: `UNIQUE (tenant_id, code)`.
   - UOM Code is strictly unique per tenant: `UNIQUE (tenant_id, code)`.

3. **Referential Integrity:**
   - A product requires a valid `base_uom_id` belonging to the same tenant.
   - If `brand_id` is specified, it must belong to the same tenant.
   - If `category_id` is specified, it must belong to the same tenant.
   - If `company_id` is specified, it must belong to the same tenant.
   - Category `parent_id` cannot introduce cyclical references (e.g., A $\rightarrow$ B $\rightarrow$ A).

---

## 9. Security and Operational Architecture

1. **Forced RLS:** Every query against `master.*` tables operates under `SELECT set_config('app.tenant_id', $1, true)`. Application role `nexora_app` (`NOBYPASSRLS`) cannot read or mutate another tenant's catalog data.
2. **Transaction Atomicity:** Domain mutations, audit entries (`audit.Store`), and outbox events (`outbox.Writer`) are committed in the same database transaction.
3. **Optimistic Locking:** Product mutations enforce `WHERE id = $id AND version = $version` and increment `version = version + 1`. Stale requests receive HTTP 409 `ErrOptimisticLockConflict`.
4. **Information Leakage Prevention:** Cross-tenant product lookups return HTTP 404 `ErrProductNotFound` rather than 403, preventing enumeration of product SKUs across tenants.

---

## 10. Proposed Implementation Work Packages

The implementation of Increment 3 is broken down into 4 bounded, reviewable packages:

### Work Package 1: Reference Data (Brand, Category, UOM)
- **Entities:** `master.brand`, `master.category`, `master.uom`.
- **Capabilities:** Persistence, category tree traversal, decimal precision validation on UOM ($0 \le \text{scale} \le 6$), CRUD operations.
- **Audit / Events:** `CATEGORY_CREATED`, `CATEGORY_UPDATED`, `UOM_CREATED`, `UOM_UPDATED`.
- **Tests:** Tree cycle rejection, uniqueness constraints, scale validation.

### Work Package 2: Product Core Aggregate & Lifecycle
- **Entities:** `master.product`.
- **Capabilities:** Product CRUD, state transitions (`ACTIVE`, `INACTIVE`, `DISCONTINUED`), optimistic locking, parent entity validation (Brand, Category, UOM, Company).
- **Audit / Events:** `PRODUCT_CREATED`, `PRODUCT_UPDATED`, `PRODUCT_ACTIVATED`, `PRODUCT_DEACTIVATED`, `catalog.product.*.v1`.
- **Tests:** Uniqueness, invalid state transitions, optimistic lock conflict, audit/outbox atomicity.

### Work Package 3: Product Variants & Barcodes
- **Entities:** `master.product_variant`, `master.product_barcode`.
- **Capabilities:** Variant creation with JSONB attributes, barcode assignment (EAN/UPC), primary barcode selection, fast barcode lookup endpoint.
- **Audit / Events:** `PRODUCT_VARIANT_CREATED`, `PRODUCT_VARIANT_UPDATED`, `BARCODE_CREATED`, `BARCODE_CHANGED`, `catalog.variant.created.v1`, `catalog.barcode.assigned.v1`.
- **Tests:** Duplicate barcode rejection, variant SKU uniqueness, fast barcode scan lookup.

### Work Package 4: HTTP Transport, API Aliasing & Integration Test Suite
- **Components:** Chi router mounting under `/api/v1/master` and `/api/v1/products`, DTO validation, canonical JSON error envelopes.
- **Integration Tests:** Live disposable PostgreSQL tests with `nexora_app` role, cross-tenant denial, full catalog lifecycle.

---

## 11. Decision Register

| Decision ID | Area | Architectural Question | Available Options | Trade-offs | Recommended Option | Status |
|---|---|---|---|---|---|---|
| **DEC-B1-008** | Audit / Migration | `master.brand` audit action catalog coverage | **Option A:** Allocate forward migration `000009_master_hardening.sql` to add `BRAND_CREATED` and `BRAND_UPDATED` to `audit.audit_action_catalog`.<br>**Option B:** Audit brand changes under existing action `PRODUCT_UPDATED` with resource type `brand`.<br>**Option C:** Do not record audit entries for brand mutations. | Option A maintains strict audit catalog purity and foreign key validation. Option B reuses existing actions. Option C loses auditability. | **Option A (Forward migration 000009)** | **NEEDS USER DECISION** |
| **DEC-B1-009** | RBAC / Migration | Reference Data (Brand, Category, UOM) Write Permissions | **Option A:** Allocate forward migration `000009_master_hardening.sql` to seed `brand.create`, `brand.update`, `category.create`, `category.update`, `uom.create`, `uom.update`.<br>**Option B:** Protect Brand/Category/UOM creation under existing canonical permission `product.create` and updates under `product.update`. | Option A allows fine-grained separation of reference data management from product cataloging. Option B avoids a migration by using coarse-grained permissions. | **Option B (Recommended for zero-migration)** or **Option A (if migration 000009 is approved)** | **NEEDS USER DECISION** |
| **DEC-B1-010** | Tenancy | Product Company Ownership (`company_id`) | **Option A:** Hybrid ownership: If `company_id` is NULL, product is tenant-wide; if set, product is restricted to that company.<br>**Option B:** Mandatory `company_id` on all products.<br>**Option C:** Strictly tenant-global; ignore `company_id`. | Option A matches the baseline schema where `company_id` is nullable and supports multi-company retail groups sharing catalog SKUs while permitting company-exclusive products. | **Option A (Hybrid ownership)** | **PROPOSED** |
| **DEC-B1-011** | Scope Boundary | Serial Number Tracking (`master.serial_number`) | **Option A:** Exclude serial lifecycle mutations from Increment 3; defer to Batch 2 (Inventory & Warehouse).<br>**Option B:** Implement full serial lifecycle in Increment 3. | Option A keeps Master Data strictly catalog-focused and prevents premature coupling to warehouse receipts/stock ledgers. Option B bloats master data scope. | **Option A (Defer to Batch 2 Inventory)** | **PROPOSED** |
| **DEC-B1-012** | Domain Boundary | Variant Pricing in Master Data Responses | **Option A:** Master data responses strictly return catalog metadata (SKU, attributes, UOM, barcode) without price fields.<br>**Option B:** Introduce placeholder price fields in Master Data. | Option A faithfully obeys Architecture Lock `09` (Pricing lives strictly in `pricing`). Option B violates domain boundaries. | **Option A (Strict catalog metadata only)** | **PROPOSED** |
| **DEC-B1-013** | Routing | API Endpoint Prefix Convention | **Option A:** Mount at `/api/v1/master/...` with `/api/v1/products` alias for OpenAPI compatibility.<br>**Option B:** Mount exclusively at `/api/v1/products`. | Option A follows both the modular monolith domain path convention (`/api/v1/organization`, `/api/v1/identity`) and OpenAPI v1.0.0 client contracts. | **Option A (Domain mount with alias)** | **PROPOSED** |

---

## 12. Acceptance Criteria for Future Increment

Upon future authorization, Master Data Increment 3 must satisfy:
1. **Catalog CRUD:** Brand, Category, UOM, Product, Product Variant, and Barcode records can be created, retrieved, updated, and listed.
2. **Category Hierarchy:** Hierarchical categories enforce parent-child integrity and reject cyclical parent assignments.
3. **Product Lifecycle:** State transitions conform to `ACTIVE → INACTIVE → DISCONTINUED`; invalid transitions return structured HTTP 409 conflict errors.
4. **Barcode Operations:** Primary barcode selection enforced; barcode scan lookup returns product and variant within single query.
5. **Tenant Isolation:** Verified with `nexora_app` (`NOBYPASSRLS`); cross-tenant reads return 404; cross-tenant writes fail closed.
6. **Optimistic Locking:** Version conflict on product update returns HTTP 409 `ErrOptimisticLockConflict`.
7. **Audit & Outbox Atomicity:** Mutations atomically commit audit records and outbox events in the same transaction; rollback on error verified.
8. **Test Execution:** 100% pass on unit tests, disposable PostgreSQL integration tests, race detector suite (`-race -count=1`), `golangci-lint` (0 issues), and `go vet`.
9. **Migration Governance:** Checksums of `000001`–`000006` remain immutable; `000007` remains absent; `000008` remains unmodified.

---

## 13. Risks, Assumptions, and Unresolved Questions

1. **UOM Scale Constraint:** Baseline schema defines `CHECK (precision_scale BETWEEN 0 AND 6)`. Service must strictly validate quantity decimal places against UOM precision.
2. **Full-Text Search Index:** Baseline creates `ix_product_name USING gin (to_tsvector('simple', name))`. Product search query should use `to_tsvector` match for performant prefix/full-text filtering.
3. **Migration Sequence Decision:** If the user approves Option A for `DEC-B1-008` and `DEC-B1-009`, migration `000009_master_hardening.sql` must be formally allocated and governed. If Option B is approved, Increment 3 requires **zero new database migrations**.

---

## 14. Final Readiness Verdict

**VERDICT: `READY WITH DOCUMENTED DECISIONS`**

- The Master Data domain schema, RLS policies, indexes, and basic permissions are already established in the repository baseline.
- Scope boundaries are clearly demarcated from Pricing, Inventory, POS, and CRM.
- Pre-implementation planning is complete. Implementation should proceed only after user resolution of Decisions `DEC-B1-008` through `DEC-B1-013`.

---

## 15. Governance Confirmation

- No application code was created or modified.
- No migrations were created or executed.
- No Git operations were performed.
- No persistent databases were accessed.
- Implementation remains gated and awaiting explicit user authorization.
