# NEXORA — Master Data Increment 3 Design Finalization

## Executive Summary

- **Document:** `docs/architecture/25-master-data-increment-design-finalization.md`
- **Target Increment:** Phase 2 / Batch 1 / Increment 3: Master Data & Catalog (`master`)
- **Review Date:** 2026-10-11
- **Status:** **DESIGN FINALIZATION (READ-ONLY — NO IMPLEMENTATION AUTHORIZED)**
- **Final Design Readiness Verdict:** **`READY FOR IMPLEMENTATION AUTHORIZATION`**

This artifact establishes the finalized, approved architectural design for NEXORA Master Data Increment 3. Following user approval of **Option A** across decisions `DEC-B1-008` through `DEC-B1-013`, this document provides complete technical alignment between the immutable baseline schema (`000001_baseline_hardened.sql`), approved forward migrations (`000002` through `000008`), OpenAPI v1.0.0 contracts, permission matrices, audit and event catalogs, and cross-domain boundaries.

No application code, migrations, tests, or Git state were modified during this task.

---

## 1. Approved Decision Register (DEC-B1-008 through DEC-B1-013)

The user has explicitly approved **Option A** for all six decisions formulated during pre-implementation review:

| Decision ID | Topic | Approved Option | Core Architectural Principle | Status |
|---|---|---|---|---|
| **DEC-B1-008** | Brand Audit Actions | **Option A:** Dedicated Audit Actions | Register `BRAND_CREATED` and `BRAND_UPDATED` in `audit.audit_action_catalog` via future forward migration. Never mutate audit records without catalog backing. | **APPROVED** |
| **DEC-B1-009** | Reference Data Permissions | **Option A:** Fine-Grained Permissions | Register `brand.create`, `brand.update`, `category.create`, `category.update`, `uom.create`, `uom.update` in `identity.permission` via future forward migration. Strict Segregation of Duties. | **APPROVED** |
| **DEC-B1-010** | Product Company Ownership | **Option A:** Hybrid Tenancy Ownership | `company_id IS NULL`: product is tenant-wide across all stores/warehouses of the tenant.<br>`company_id IS NOT NULL`: product is restricted to that company. Cross-tenant company bypass prohibited. | **APPROVED** |
| **DEC-B1-011** | Serial Number Scope | **Option A:** Catalog Definition Only | Increment 3 supports catalog lookup and serial definition where supported by schema. Physical inventory lifecycle (`RECEIVED`, `AVAILABLE`, `RESERVED`, `SOLD`) is deferred to Batch 2 (Inventory). | **APPROVED** |
| **DEC-B1-012** | Variant Pricing Boundary | **Option A:** Pure Catalog Metadata | Master Data responses expose strictly catalog metadata (SKU, attributes JSONB, UOM, barcode). No authoritative prices or placeholder price fields. Pricing owned by `pricing` domain. | **APPROVED** |
| **DEC-B1-013** | API Routing & Compatibility | **Option A:** Domain Mount with Alias | Canonical domain mount at `/api/v1/master/...`. Compatibility alias mounted at `/api/v1/products` for OpenAPI v1.0.0 client generator parity. | **APPROVED** |

---

## 2. Repository Sources Inspected

The following canonical sources were inspected and reconciled during design finalization:

| Source File | Path | Status / Evidence |
|---|---|---|
| Platform Architecture Lock | [`docs/architecture/09-architecture-lock.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/09-architecture-lock.md) | **CONFIRMED**: Section 19 Phase 2 locks Master Data as product, barcode, category, brand, UOM. |
| State Machine Catalog | [`docs/architecture/10-state-machine-catalog.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/10-state-machine-catalog.md) | **CONFIRMED**: Product lifecycle `ACTIVE`, `INACTIVE`, `DISCONTINUED`. |
| Permission Matrix | [`docs/architecture/11-permission-matrix.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/11-permission-matrix.md) | **CONFIRMED**: Section 8.3 Master Data permissions. |
| Event Catalog | [`docs/architecture/12-event-catalog.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/12-event-catalog.md) | **CONFIRMED**: Outbox event envelope, idempotency, versioning. |
| Audit Action Catalog | [`docs/architecture/13-audit-action-catalog.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/13-audit-action-catalog.md) | **CONFIRMED**: Section 13 Product / Master Data audit actions. |
| Offline Sync Contract | [`docs/architecture/08-offline-sync-contract.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/08-offline-sync-contract.md) | **CONFIRMED**: `PRODUCT_INACTIVE` conflict code; master data cached on POS. |
| Baseline Hardened Migration | [`db/migrations/000001_baseline_hardened.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000001_baseline_hardened.sql) | **CONFIRMED**: Tables `master.brand`, `master.category`, `master.uom`, `master.product`, `master.product_variant`, `master.product_barcode`, `master.serial_number`. |
| Audit Hardening Migration | [`db/migrations/000004_audit_hardening.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000004_audit_hardening.sql) | **CONFIRMED**: 14 master audit actions seeded in catalog; Brand audit actions absent. |
| Identity Permission Migration | [`db/migrations/000006_identity_permissions.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000006_identity_permissions.sql) | **CONFIRMED**: Master read/product permissions seeded; reference write permissions absent. |
| Organization Hardening Migration | [`db/migrations/000008_organization_hardening.sql`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000008_organization_hardening.sql) | **CONFIRMED**: Organization increment accepted. |
| OpenAPI Specification | [`contracts/openapi/openapi-v1.0.0.yaml`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/contracts/openapi/openapi-v1.0.0.yaml) | **CONFIRMED**: `/products`, `/products/{id}`, `/products/{id}/activate`, `/products/{id}/deactivate`, `/products/{id}/barcodes`, `/products/lookup/barcode/{barcode}`. |

---

## 3. Permission Matrix and Seed Reconciliation

### Canonical Permission Matrix vs. Existing Seed
Under approved decision **DEC-B1-009 (Option A)**, reference data operations receive fine-grained permissions.

| Permission Code | Name | Module | Current Status | Required Action in Future Migration |
|---|---|---|---|---|
| `product.read` | Read Product | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `product.create` | Create Product | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `product.update` | Update Product | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `product.activate` | Activate Product | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `product.deactivate` | Deactivate Product | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `product.barcode.read` | Read Product Barcode | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `product.barcode.manage` | Manage Product Barcodes | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `serial.read` | Read Serial Number | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `serial.manage` | Manage Serial Numbers | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `uom.read` | Read Unit of Measure | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `category.read` | Read Product Category | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `brand.read` | Read Product Brand | `MASTER` | **CONFIRMED SEEDED** (`000006`) | No change needed |
| `brand.create` | Create Product Brand | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |
| `brand.update` | Update Product Brand | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |
| `category.create` | Create Product Category | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |
| `category.update` | Update Product Category | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |
| `uom.create` | Create Unit of Measure | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |
| `uom.update` | Update Unit of Measure | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |

*Consistency Check:* Read and list operations for Brand, Category, and UOM are already fully covered by `brand.read`, `category.read`, and `uom.read`. No deletion permissions are introduced as master reference entities follow soft-deactivation only.

---

## 4. Audit Action Catalog Reconciliation

### Audit Catalog vs. Approved Brand Identifiers
Under approved decision **DEC-B1-008 (Option A)**, brand mutations require dedicated audit action catalog records.

| Audit Action | Severity | Target Module | Current Status | Future Migration Seed |
|---|---|---|---|---|
| `PRODUCT_CREATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `PRODUCT_UPDATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `PRODUCT_ACTIVATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `PRODUCT_DEACTIVATED` | `HIGH` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `PRODUCT_VARIANT_CREATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `PRODUCT_VARIANT_UPDATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `BARCODE_CREATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `BARCODE_CHANGED` | `HIGH` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `CATEGORY_CREATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `CATEGORY_UPDATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `UOM_CREATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `UOM_UPDATED` | `NOTICE` | `MASTER` | **CONFIRMED SEEDED** (`000004`) | Retain |
| `BRAND_CREATED` | `NOTICE` | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |
| `BRAND_UPDATED` | `NOTICE` | `MASTER` | **ABSENT** | **APPROVED SEED** (Candidate `000009`) |

*Consistency Check:* Direct writes to `audit.audit_log` require foreign key validation against `audit.audit_action_catalog(action)`. Seeding `BRAND_CREATED` and `BRAND_UPDATED` ensures that auditing brand operations will never violate database constraints.

---

## 5. Product Company Ownership & Tenant Security Rules

### Approved Hybrid Ownership Rules (DEC-B1-010)

1. **Tenant-Wide Products (`company_id IS NULL`):**
   - The product belongs to the owning tenant.
   - It is available across all companies, stores, warehouses, and sales channels within that tenant.
   - It is strictly invisible and inaccessible to any other tenant (enforced by PostgreSQL RLS).

2. **Company-Restricted Products (`company_id IS NOT NULL`):**
   - The product is bound to a specific legal entity (`organization.company`).
   - It is only tradable, orderable, and manageable by stores and users authorized for that specific company.
   - Foreign key validation requires: `company.tenant_id == current_tenant_id()`.

### Cross-Tenant Protection & Query Rules
- **RLS Policy:** Querying `master.product` always executes within a transaction where `SELECT set_config('app.tenant_id', $1, true)` has established the authenticated principal's tenant context.
- **Parent Validation:** When creating or updating a product:
  - If `company_id` is supplied: Must query `organization.company WHERE id = $company_id`. If row not found within caller's tenant $\rightarrow$ return HTTP 400 `ErrParentNotFound`.
  - If `brand_id` is supplied: Must query `master.brand WHERE id = $brand_id`. If not found $\rightarrow$ return HTTP 400 `ErrParentNotFound`.
  - If `category_id` is supplied: Must query `master.category WHERE id = $category_id`. If not found $\rightarrow$ return HTTP 400 `ErrParentNotFound`.
  - `base_uom_id` is mandatory (`NOT NULL`): Must query `master.uom WHERE id = $uom_id`. If not found $\rightarrow$ return HTTP 400 `ErrParentNotFound`.
- **Negative Security Invariant:** Supplying a valid UUID of another tenant's company, brand, category, or UOM must fail closed with HTTP 400 `ErrParentNotFound` (never 403), preventing cross-tenant existence enumeration.

---

## 6. Serial Number Scope Boundary (DEC-B1-011)

### Demarcation Between Increment 3 and Batch 2 (Inventory)

```
┌────────────────────────────────────────────────────────┐
│        INCREMENT 3 (Master Data & Catalog)             │
├────────────────────────────────────────────────────────┤
│ • Product Master flag: track_serial (boolean)          │
│ • Serial registry definition & lookup                  │
│ • Query serial by serial_no within tenant              │
└───────────────────────────┬────────────────────────────┘
                            │ Hand-off boundary
                            ▼
┌────────────────────────────────────────────────────────┐
│        BATCH 2 (Inventory, Warehouse, POS, Sales)      │
├────────────────────────────────────────────────────────┤
│ • Goods Receipt: status -> RECEIVED / AVAILABLE        │
│ • Stock Reservation: status -> RESERVED                │
│ • Point-of-Sale Checkout: status -> SOLD               │
│ • Return / RMA: status -> RETURNED                     │
│ • Damage / Service / Scrap: DAMAGED / SERVICE / SCRAPPED│
└────────────────────────────────────────────────────────┘
```

- **Scope in Increment 3:** Model `master.serial_number` as an entity; provide read/lookup capability for registered serial numbers.
- **Explicit Exclusion in Increment 3:** No inventory receipt workflows, stock movements, or status mutation endpoints (`SOLD`, `RESERVED`, etc.). These transitions are triggered by Inventory and Sales engines in Batch 2.

---

## 7. Pricing Domain Boundary (DEC-B1-012)

### Architecture Lock Compliance
- `master.product` and `master.product_variant` store **zero pricing fields**.
- In `contracts/openapi/openapi-v1.0.0.yaml`, schema `ProductVariant` lists optional fields `price` and `currency`. In accordance with Architecture Lock `09` and DEC-B1-012, Master Data **never calculates, owns, or mutates prices**.
- **Behavior in Increment 3:** Master Data APIs omit pricing fields or return them unpopulated.
- **Future Integration:** In Phase 2 Batch 2 (`pricing`), the Pricing service resolves applicable price rules (`pricing.price_rule`) and price lists (`pricing.price_list`) dynamically based on customer group, store location, and effective dates.

---

## 8. API Prefix and OpenAPI Compatibility Design (DEC-B1-013)

### Dual-Mount Routing Architecture

To simultaneously satisfy:
1. Modular monolith architectural symmetry (`/api/v1/identity`, `/api/v1/organization`, `/api/v1/master`).
2. Exact client contract parity with `contracts/openapi/openapi-v1.0.0.yaml` (`/products`, `/products/{id}`, etc.).

The HTTP transport adapter will mount routes using the dual-mount pattern:

```go
// 1. Canonical Domain Router mounted in backend/api.go
masterHandler := master.NewHandler(masterService, log)
srv.Router().Mount("/api/v1/master", masterHandler.Routes())

// 2. Compatibility Alias mounted at root API level
srv.Router().Mount("/api/v1/products", masterHandler.ProductAliasRoutes())
```

### Route Parity Mapping

| OpenAPI v1.0.0 Route | Compatibility Alias (`/api/v1/products`) | Canonical Domain Route (`/api/v1/master`) | Handler Method | Required Permission |
|---|---|---|---|---|
| `GET /products` | `GET /api/v1/products` | `GET /api/v1/master/products` | `handleListProducts` | `product.read` |
| `POST /products` | `POST /api/v1/products` | `POST /api/v1/master/products` | `handleCreateProduct` | `product.create` |
| `GET /products/{id}` | `GET /api/v1/products/{id}` | `GET /api/v1/master/products/{id}` | `handleGetProduct` | `product.read` |
| `PATCH /products/{id}` | `PATCH /api/v1/products/{id}` | `PATCH /api/v1/master/products/{id}` | `handleUpdateProduct` | `product.update` |
| `POST /products/{id}/activate` | `POST /api/v1/products/{id}/activate` | `POST /api/v1/master/products/{id}/activate` | `handleActivateProduct` | `product.activate` |
| `POST /products/{id}/deactivate` | `POST /api/v1/products/{id}/deactivate` | `POST /api/v1/master/products/{id}/deactivate` | `handleDeactivateProduct` | `product.deactivate` |
| `GET /products/{id}/barcodes` | `GET /api/v1/products/{id}/barcodes` | `GET /api/v1/master/products/{id}/barcodes` | `handleListBarcodes` | `product.barcode.read` |
| `POST /products/{id}/barcodes` | `POST /api/v1/products/{id}/barcodes` | `POST /api/v1/master/products/{id}/barcodes` | `handleCreateBarcode` | `product.barcode.manage` |
| `GET /products/lookup/barcode/{barcode}` | `GET /api/v1/products/lookup/barcode/{barcode}` | `GET /api/v1/master/products/lookup/barcode/{barcode}` | `handleLookupBarcode` | `product.read` |
| *N/A (Reference)* | — | `GET /api/v1/master/brands` | `handleListBrands` | `brand.read` |
| *N/A (Reference)* | — | `POST /api/v1/master/brands` | `handleCreateBrand` | `brand.create` |
| *N/A (Reference)* | — | `GET /api/v1/master/categories` | `handleListCategories` | `category.read` |
| *N/A (Reference)* | — | `POST /api/v1/master/categories` | `handleCreateCategory` | `category.create` |
| *N/A (Reference)* | — | `GET /api/v1/master/uoms` | `handleListUOMs` | `uom.read` |
| *N/A (Reference)* | — | `POST /api/v1/master/uoms` | `handleCreateUOM` | `uom.create` |

*Lifecycle of Alias:* Long-lived. The compatibility alias guarantees that any frontend SDK or API consumer generated against OpenAPI v1.0.0 will operate without URL rewriting.

---

## 9. Schema and Migration Requirements

### Migration Sequencing Governance
- Migrations `000001` through `000006`: Immutable baseline.
- Migration `000007_crm_audit_actions.sql`: Reserved strictly for CRM (absent).
- Migration `000008_organization_hardening.sql`: Completed for Organization Increment 2.
- **Candidate Forward Migration:** `000009_master_hardening.sql`.

### Candidate Migration Specification (`000009_master_hardening.sql`)
Upon future authorization, this migration will perform:

```sql
-- Migration 000009: Master Data Hardening (Permissions & Audit Action Catalog Seed)
BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.master.hardening.v1'));

-- 1. Seed Brand Audit Actions
INSERT INTO audit.audit_action_catalog (action, module, description, severity, requires_reason, is_system)
VALUES
  ('BRAND_CREATED', 'MASTER', 'Product brand created', 'NOTICE', false, true),
  ('BRAND_UPDATED', 'MASTER', 'Product brand updated', 'NOTICE', false, true)
ON CONFLICT (action) DO NOTHING;

-- 2. Seed Reference Data Write Permissions
INSERT INTO identity.permission (code, name, module, description)
VALUES
  ('brand.create', 'Create Product Brand', 'MASTER', 'BRAND_CREATE'),
  ('brand.update', 'Update Product Brand', 'MASTER', 'BRAND_UPDATE'),
  ('category.create', 'Create Product Category', 'MASTER', 'CATEGORY_CREATE'),
  ('category.update', 'Update Product Category', 'MASTER', 'CATEGORY_UPDATE'),
  ('uom.create', 'Create Unit of Measure', 'MASTER', 'UOM_CREATE'),
  ('uom.update', 'Update Unit of Measure', 'MASTER', 'UOM_UPDATE')
ON CONFLICT (code) DO NOTHING;

COMMIT;
```

*Architectural Properties:*
- **Zero DDL Changes:** Tables `master.brand`, `category`, `uom`, `product`, `product_variant`, `product_barcode`, `serial_number` already exist and have RLS enabled from baseline `000001`.
- **Purely Additive & Idempotent:** Uses `ON CONFLICT DO NOTHING`.
- **Advisory Lock Protected:** Uses `hashtext('nexora.master.hardening.v1')`.

---

## 10. Proposed Implementation Work Packages

The future implementation of Increment 3 is planned in four strictly bounded work packages:

### WP1: Reference Data Management (`brand`, `category`, `uom`)
- **Domain Models:** `Brand`, `Category`, `UOM`.
- **Validation:**
  - Category tree cycle detection: A category cannot have itself or any of its descendants as `parent_id`.
  - UOM precision scale enforcement: $0 \le \text{scale} \le 6$.
  - Uniqueness: Code unique per tenant.
- **Audit / Events:** `BRAND_CREATED`, `BRAND_UPDATED`, `CATEGORY_CREATED`, `CATEGORY_UPDATED`, `UOM_CREATED`, `UOM_UPDATED`.
- **Unit & Store Tests:** Repository and service validations.

### WP2: Product Master & Lifecycle Aggregate
- **Domain Model:** `Product`.
- **Validation:**
  - SKU uniqueness per tenant.
  - Required base UOM; optional Brand, Category, Company.
  - Hybrid ownership verification: `company.tenant_id == current_tenant_id()`.
  - State machine transitions: `ACTIVE ⇄ INACTIVE`, `ACTIVE/INACTIVE → DISCONTINUED`. Discontinued products cannot be reactivated.
  - Optimistic concurrency: `version = version + 1 WHERE version = $v`.
- **Audit / Events:** `PRODUCT_CREATED`, `PRODUCT_UPDATED`, `PRODUCT_ACTIVATED`, `PRODUCT_DEACTIVATED`, `catalog.product.*.v1`.

### WP3: Product Variants & Barcode Registry
- **Domain Models:** `ProductVariant`, `ProductBarcode`.
- **Validation:**
  - Variant SKU unique per tenant.
  - Barcode unique per tenant.
  - Primary barcode management: Exactly one primary barcode per product/variant.
  - Full-text search and fast barcode scan lookup (`/products/lookup/barcode/{barcode}`).
- **Audit / Events:** `PRODUCT_VARIANT_CREATED`, `PRODUCT_VARIANT_UPDATED`, `BARCODE_CREATED`, `BARCODE_CHANGED`, `catalog.variant.created.v1`, `catalog.barcode.assigned.v1`.

### WP4: HTTP Handlers, Dual-Mount Routing, and Disposable Integration Tests
- **Components:** Chi router mounting under `/api/v1/master` and `/api/v1/products`, request validation, `apperr.ErrorResponse` mapping.
- **Integration Tests:** Live disposable PostgreSQL tests with `nexora_app` non-superuser (`NOBYPASSRLS`), cross-tenant isolation verification, full catalog CRUD lifecycle.

---

## 11. Security and Negative Test Requirements

During future implementation, the following security and negative test scenarios are mandatory:

1. **Cross-Tenant Access Rejection:**
   - Tenant A attempts to read Tenant B's products $\rightarrow$ HTTP 404 `ErrProductNotFound` (prevent SKU enumeration).
   - Tenant A attempts to update or deactivate Tenant B's product $\rightarrow$ HTTP 404 `ErrProductNotFound`.
   - Tenant A attempts to create a product referencing Tenant B's `company_id`, `brand_id`, `category_id`, or `base_uom_id` $\rightarrow$ HTTP 400 `ErrParentNotFound`.

2. **RBAC & Authorization Boundary:**
   - Unauthenticated request $\rightarrow$ HTTP 401 `UNAUTHORIZED`.
   - Authenticated principal without `product.create` attempting product creation $\rightarrow$ HTTP 403 `FORBIDDEN`.
   - Cashier without `product.update` attempting product update $\rightarrow$ HTTP 403 `FORBIDDEN`.
   - Principal without `brand.create` attempting brand creation $\rightarrow$ HTTP 403 `FORBIDDEN`.

3. **Concurrency & State Machine Invariants:**
   - Two concurrent updates to the same product with identical version $\rightarrow$ one succeeds, one fails with HTTP 409 `ErrOptimisticLockConflict`.
   - Attempt to activate a `DISCONTINUED` product $\rightarrow$ HTTP 409 `ErrInvalidStatusTransition`.
   - Attempt to assign a duplicate barcode to another product $\rightarrow$ HTTP 409 `ErrBarcodeExists`.

4. **Category Cycle Prevention:**
   - Attempt to set Category A's parent to Category B when B is already a child of A $\rightarrow$ HTTP 400 `ErrCategoryCycleDetected`.

5. **Transaction Rollback Atomicity:**
   - If an error occurs during product creation (e.g. outbox write failure), the product record, audit record, and outbox event roll back together atomically.

---

## 12. Acceptance Criteria for Future Implementation

Future implementation will be accepted only if:
1. All capabilities in WP1–WP4 are implemented and verified.
2. Forward migration `000009_master_hardening.sql` is formally allocated, checksum verified, and applied cleanly in disposable test databases.
3. Migrations `000001` through `000008` remain completely unchanged.
4. Migration `000007_crm_audit_actions.sql` remains absent.
5. All unit tests, disposable PostgreSQL integration tests, and full backend race tests (`-race -count=1`) pass with 0 failures and 0 races.
6. `golangci-lint` passes with 0 issues; `go vet` and `go build` pass cleanly.
7. Both canonical routes (`/api/v1/master/...`) and compatibility aliases (`/api/v1/products/...`) pass contract verification.
8. Master Data responses strictly omit authoritative pricing and inventory quantity balances.
9. No Git commands or persistent database connections are performed.

---

## 13. Remaining Unresolved Decisions

- **Status:** **NONE**.
- Decisions `DEC-B1-008` through `DEC-B1-013` have all been resolved with approved **Option A**.
- All dependencies, schema objects, permissions, audit actions, and route paths are settled.

---

## 14. Risks and Dependencies

1. **Category Tree Recursion Depth:** Hierarchical category trees could theoretically experience performance degradation on deeply nested queries. The service must enforce a maximum depth limit (e.g. max 5 levels) to guarantee fast catalog traversal.
2. **UOM Precision Formatting:** Downstream inventory and POS domains must format quantities according to `master.uom.precision_scale`. Increment 3 models and validates the scale ($0 \le \text{scale} \le 6$).
3. **Migration Sequence Verification:** When authorization is granted, verify that `000009_master_hardening.sql` is allocated cleanly without conflicting with future migration slots.

---

## 15. Final Design Readiness Verdict

**VERDICT: `READY FOR IMPLEMENTATION AUTHORIZATION`**

### Verdict Justification
- All six architectural decisions (`DEC-B1-008` through `DEC-B1-013`) are approved and documented.
- The baseline database schema (`master.*`) is already present in `000001_baseline_hardened.sql` with forced RLS.
- Forward migration requirements are minimal, purely additive, and clearly specified (candidate `000009_master_hardening.sql`).
- Domain boundaries between Master Data, Pricing, Inventory, and CRM are strictly drawn.
- Route mapping preserves 100% compatibility with OpenAPI v1.0.0 via the dual-mount alias pattern.
- The design is complete, consistent, and ready for future implementation when authorized.

---

## 16. Governance Confirmation

- No application source code was modified.
- No database migrations were created or executed.
- No Git commands were executed.
- No persistent shared databases were accessed.
- Implementation remains strictly gated awaiting explicit user authorization.
