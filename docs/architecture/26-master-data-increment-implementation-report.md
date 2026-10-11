# NEXORA Phase 2 / Batch 1 / Increment 3 — Master Data & Catalog Implementation Report

## 1. Executive Status and Scope

- **Status**: Completed & Verified — Awaiting Independent Acceptance Review
- **Verdict**: **PASS WITH DOCUMENTED LIMITATIONS**
- **Date**: 2026-10-11
- **Domain**: Master Data & Catalog (`master`)
- **Governing Architecture & Approvals**:
  - `docs/architecture/25-master-data-increment-design-finalization.md` (Approved Option A across `DEC-B1-008` through `DEC-B1-013`)
  - `docs/architecture/24-master-data-increment-pre-implementation-review.md`
  - `docs/architecture/09-architecture-lock.md`
  - `contracts/openapi/openapi-v1.0.0.yaml`
- **Authorized Scope**:
  1. Product Master (`master.product`)
  2. Product Variant (`master.product_variant`)
  3. Product Barcode (`master.product_barcode`)
  4. Brand Reference Data (`master.brand`)
  5. Category Reference Data (`master.category`)
  6. Unit of Measure Reference Data (`master.uom`)
  7. Serial Number Catalog Definition & Lookup (`master.serial_number`, DEC-B1-011)
  8. Forward Migration `000009_master_hardening.sql` (Audit action and permission catalog seeds)
  9. Canonical Domain Router mounted at `/api/v1/master` with Compatibility Alias mounted at `/api/v1/products` (DEC-B1-013)
- **Explicit Exclusions**:
  - Customer, Customer Group, Customer Address — CRM domain (Increment 4).
  - Price Lists, Unit Prices, Price Rules — Pricing domain (Batch 2).
  - Stock Quantities, Warehouse Stock Balances — Inventory domain (Batch 2).
  - Physical Serial Inventory State Transitions (Receiving, Reservation, Allocation, Sale) — Inventory domain (DEC-B1-011, Batch 2).
  - POS Sessions, Sales Orders, Payments, Promotions, Loyalty, Workflows.

---

## 2. Preflight Evidence and Verification

Before executing any code or migration additions, repository preflight checks verified:

1. **Migration Sequence and Immutability (`000001` through `000008`)**:
   - `000001_baseline_hardened.sql`: SHA-256 matches approved register (`e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`).
   - `000002_refresh_tokens.sql`: SHA-256 matches approved register (`47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733`).
   - `000003_outbox_leases.sql`: SHA-256 matches approved register (`15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279`).
   - `000004_audit_hardening.sql`: SHA-256 matches approved register (`1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f`).
   - `000005_auth_rls_bootstrap.sql`: SHA-256 matches approved register (`8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0`).
   - `000006_identity_permissions.sql`: SHA-256 matches approved register (`a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2`).
   - `000007_crm_audit_actions.sql`: Verified absent and reserved exclusively for CRM domain.
   - `000008_organization_hardening.sql`: SHA-256 matches approved register (`f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb`).
   - Forward migration `000009` verified available.
2. **Schema Baseline Inspection**:
   - Baseline tables `master.brand`, `master.category`, `master.uom`, `master.product`, `master.product_variant`, `master.product_barcode`, and `master.serial_number` verified in `000001_baseline_hardened.sql`.
   - Verified that RLS is enabled and forced (`FORCE ROW LEVEL SECURITY`) with `tenant_isolation_v11` across all 7 master tables.
   - Verified exact column names and constraints:
     - `audit.audit_action_catalog`: `(code, module, description, severity, requires_reason, active)`.
     - `identity.permission`: `(code, name, module, action)`.
     - `master.uom`: `precision_scale smallint CHECK (precision_scale BETWEEN 0 AND 6)`.
     - `master.product`: `company_id uuid REFERENCES organization.company(id)` (nullable, supporting hybrid company ownership).
3. **No Blockers Encountered**: All baseline assumptions verified without architecture drift.

---

## 3. Files Created and Modified

### Files Created:
1. `db/migrations/000009_master_hardening.sql`: Forward migration seeding Brand audit actions and reference data permissions.
2. `backend/internal/master/model.go`: Domain entities, lifecycle status constants, domain events, audit action codes, request DTOs, and OpenAPI response DTOs.
3. `backend/internal/master/errors.go`: Sentinel domain errors and `MapToAppError` mapping to canonical HTTP error envelopes.
4. `backend/internal/master/store.go`: PostgreSQL persistence layer implementing `Store` interface with RLS and optimistic concurrency.
5. `backend/internal/master/service.go`: Master domain service implementing business rules, state machines, category cycle detection, and transactional audit/outbox.
6. `backend/internal/master/handler.go`: Chi HTTP router implementing canonical `/api/v1/master` routes and `/api/v1/products` compatibility aliases.
7. `backend/internal/master/service_test.go`: Unit tests for domain logic, validation, state transitions, category DAG cycles, UOM scale, and serials.
8. `backend/internal/master/handler_test.go`: Unit tests for HTTP transport, authorization, error mapping, and OpenAPI alias parity.
9. `backend/internal/master/master_integration_test.go`: Disposable PostgreSQL integration tests with forced RLS and role `nexora_app`.
10. `docs/architecture/26-master-data-increment-implementation-report.md`: This comprehensive implementation report.

### Files Modified:
1. `backend/internal/platform/auth/rls_migration_integration_test.go`: Registered migration `000009` checksum in integration test harness.
2. `backend/internal/platform/audit/catalog.go`: Registered `BRAND_CREATED` and `BRAND_UPDATED` in memory catalog registry.
3. `backend/api.go`: Initialized Master Data store, service, and handler; mounted `/api/v1/master` and `/api/v1/products`.
4. `docs/architecture/IMPLEMENTATION_STATUS.md`: Updated Phase 2 / Batch 1 / Increment 3 status.
5. `TASKS.md`: Checked off completed Phase 4 Master Data tasks.

---

## 4. API Routes and OpenAPI Compatibility (DEC-B1-013)

To ensure modular monolith symmetry while maintaining exact parity with `contracts/openapi/openapi-v1.0.0.yaml`, a dual-mount architecture is implemented in `backend/api.go`:

```go
masterStore := master.NewPgxStore(dbPool)
masterAudit := audit.NewPgxStore(dbPool, tm, log)
masterOutbox := outbox.NewPgxWriter(log)
masterService := master.NewService(masterStore, tm, masterAudit, masterOutbox, log)
masterHandler := master.NewHandler(masterService, log)

// 1. Canonical Domain Router
srv.Router().Mount("/api/v1/master", masterHandler.Routes())

// 2. OpenAPI v1.0.0 Compatibility Alias Router
srv.Router().Mount("/api/v1/products", masterHandler.ProductAliasRoutes())
```

### Route Parity Table

| OpenAPI Operation | Compatibility Alias Route | Canonical Domain Route | Handler Method | Required Permission |
|---|---|---|---|---|
| `getProducts` | `GET /api/v1/products` | `GET /api/v1/master/products` | `handleListProducts` | `product.read` |
| `postProducts` | `POST /api/v1/products` | `POST /api/v1/master/products` | `handleCreateProduct` | `product.create` |
| `getProductsId` | `GET /api/v1/products/{id}` | `GET /api/v1/master/products/{id}` | `handleGetProduct` | `product.read` |
| `patchProductsId` | `PATCH /api/v1/products/{id}` | `PATCH /api/v1/master/products/{id}` | `handleUpdateProduct` | `product.update` |
| `postProductsIdActivate` | `POST /api/v1/products/{id}/activate` | `POST /api/v1/master/products/{id}/activate` | `handleActivateProduct` | `product.activate` |
| `postProductsIdDeactivate` | `POST /api/v1/products/{id}/deactivate` | `POST /api/v1/master/products/{id}/deactivate` | `handleDeactivateProduct` | `product.deactivate` |
| `getProductsIdBarcodes` | `GET /api/v1/products/{id}/barcodes` | `GET /api/v1/master/products/{id}/barcodes` | `handleListBarcodes` | `product.barcode.read` |
| `postProductsIdBarcodes` | `POST /api/v1/products/{id}/barcodes` | `POST /api/v1/master/products/{id}/barcodes` | `handleCreateBarcode` | `product.barcode.manage` |
| `getProductsLookupBarcodeBarcode` | `GET /api/v1/products/lookup/barcode/{barcode}` | `GET /api/v1/master/products/lookup/barcode/{barcode}` | `handleLookupBarcode` | `product.read` |
| *Product Variants* | — | `POST /api/v1/master/products/{id}/variants` | `handleCreateVariant` | `product.update` |
| *Product Variants* | — | `GET /api/v1/master/products/{id}/variants` | `handleListVariants` | `product.read` |
| *Serial Definition* | — | `POST /api/v1/master/products/{id}/serials` | `handleCreateSerialNumber` | `product.update` |
| *Serial List* | — | `GET /api/v1/master/products/{id}/serials` | `handleListSerialNumbers` | `product.read` |
| *Serial Lookup* | — | `GET /api/v1/master/serials/{serial_no}` | `handleGetSerialNumber` | `product.read` |
| *Brand Management* | — | `POST /api/v1/master/brands` | `handleCreateBrand` | `brand.create` |
| *Brand Management* | — | `GET /api/v1/master/brands` | `handleListBrands` | `brand.read` |
| *Brand Management* | — | `GET /api/v1/master/brands/{id}` | `handleGetBrand` | `brand.read` |
| *Brand Management* | — | `PATCH /api/v1/master/brands/{id}` | `handleUpdateBrand` | `brand.update` |
| *Category Management* | — | `POST /api/v1/master/categories` | `handleCreateCategory` | `category.create` |
| *Category Management* | — | `GET /api/v1/master/categories` | `handleListCategories` | `category.read` |
| *Category Management* | — | `GET /api/v1/master/categories/{id}` | `handleGetCategory` | `category.read` |
| *Category Management* | — | `PATCH /api/v1/master/categories/{id}` | `handleUpdateCategory` | `category.update` |
| *UOM Management* | — | `POST /api/v1/master/uoms` | `handleCreateUOM` | `uom.create` |
| *UOM Management* | — | `GET /api/v1/master/uoms` | `handleListUOMs` | `uom.read` |
| *UOM Management* | — | `GET /api/v1/master/uoms/{id}` | `handleGetUOM` | `uom.read` |

Both routers invoke identical underlying handler logic and share identical tenant context, transaction boundaries, audit trails, and canonical error envelopes.

---

## 5. Database and Migration Changes (Migration 000009)

Forward migration `000009_master_hardening.sql` was created and applied.
- Protected by transaction-scoped advisory lock: `SELECT pg_advisory_xact_lock(hashtext('nexora.master.hardening.v1'));`.
- Contains forward-only `INSERT ... ON CONFLICT DO NOTHING` statements:
  - Seeds Brand audit actions into `audit.audit_action_catalog`:
    - `BRAND_CREATED` (Module: `MASTER`, Severity: `NOTICE`, RequiresReason: `false`)
    - `BRAND_UPDATED` (Module: `MASTER`, Severity: `NOTICE`, RequiresReason: `false`)
  - Seeds Reference Data permissions into `identity.permission`:
    - `brand.create` ("Create Product Brand", `MASTER`, `BRAND_CREATE`)
    - `brand.update` ("Update Product Brand", `MASTER`, `BRAND_UPDATE`)
    - `category.create` ("Create Product Category", `MASTER`, `CATEGORY_CREATE`)
    - `category.update` ("Update Product Category", `MASTER`, `CATEGORY_UPDATE`)
    - `uom.create` ("Create Unit of Measure", `MASTER`, `UOM_CREATE`)
    - `uom.update` ("Update Unit of Measure", `MASTER`, `UOM_UPDATE`)
- SHA-256 Checksum: `d4455ee66dab49d7eff8e41d9c24e6effb8bcc60dad3d88875e416d29bdca89e`.
- Fully idempotent: re-executing against disposable PostgreSQL produces no errors and duplicate entries are ignored.

---

## 6. Migration Checksum Register Verification

Executed `shasum -a 256 db/migrations/*.sql`:

| Migration File | SHA-256 Checksum | Status |
|---|---|---|
| `000001_baseline_hardened.sql` | `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f` | **MATCH (Unchanged)** |
| `000002_refresh_tokens.sql` | `47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733` | **MATCH (Unchanged)** |
| `000003_outbox_leases.sql` | `15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279` | **MATCH (Unchanged)** |
| `000004_audit_hardening.sql` | `1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f` | **MATCH (Unchanged)** |
| `000005_auth_rls_bootstrap.sql` | `8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0` | **MATCH (Unchanged)** |
| `000006_identity_permissions.sql` | `a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2` | **MATCH (Unchanged)** |
| `000007_crm_audit_actions.sql` | *N/A (Reserved for CRM)* | **ABSENT (Reserved)** |
| `000008_organization_hardening.sql` | `f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb` | **MATCH (Unchanged)** |
| `000009_master_hardening.sql` | `d4455ee66dab49d7eff8e41d9c24e6effb8bcc60dad3d88875e416d29bdca89e` | **NEW (Verified)** |

---

## 7. Permission and Audit Catalog Governance

- **DEC-B1-008 (Brand Audit Actions)**:
  - Canonical identifiers: `BRAND_CREATED`, `BRAND_UPDATED`.
  - Registered in both database catalog seed (`000009_master_hardening.sql`) and Go runtime catalog registry (`backend/internal/platform/audit/catalog.go`).
  - Audited on brand create and update with resource type `brand` and ID.
- **DEC-B1-009 (Reference Data Permissions)**:
  - Canonical permission codes: `brand.create`, `brand.update`, `category.create`, `category.update`, `uom.create`, `uom.update`.
  - Enforced in HTTP handler via `checkPermission(principal, "...")`.
  - Read operations enforce existing approved codes: `brand.read`, `category.read`, `uom.read`, `product.read`, `product.barcode.read`.

---

## 8. Tenant and Company Ownership Enforcement (DEC-B1-010)

- **Ownership Invariants**:
  - `company_id IS NULL`: Tenant-wide product available to all operating units of the owning tenant.
  - `company_id IS NOT NULL`: Product scoped to the specified operating company within the tenant.
- **Security & Integrity Enforcement**:
  - The caller's tenant is established strictly from the authenticated principal via `postgres.WithTenantID`. Client-supplied headers cannot override the security context.
  - When `company_id` is specified on `CreateProduct`, the service validates that the company exists in `organization.company` under the authenticated tenant context (`ValidateCompany`).
  - If a client supplies a `company_id` belonging to a different tenant or non-existent company, the validation fails closed and returns `ErrCompanyCrossTenant` (HTTP 400 Bad Request).
  - Database row-level security (`FORCE ROW LEVEL SECURITY`) with `tenant_isolation_v11` prevents any cross-tenant data leakage or mutation across all master tables.

---

## 9. Domain Rules and Data Integrity

1. **Category DAG Integrity & Cycle Prevention**:
   - Creating or updating a category with `parent_id` traverses the ancestor hierarchy.
   - If `parent_id == id`, returns `ErrCategorySelfParent`.
   - If any ancestor in the chain matches `id`, returns `ErrCategoryCycleDetected`.
2. **UOM Precision Scale Constraints**:
   - `precision_scale` validated to fall strictly within `0 <= scale <= 6`.
   - Out-of-bounds scales return `ErrInvalidUOMPrecision`.
3. **Product State Machine**:
   - Permitted transitions: `ACTIVE` $\leftrightarrow$ `INACTIVE`.
   - Terminal state: `DISCONTINUED`.
   - Attempting to activate or update a discontinued product is strictly forbidden and returns `ErrProductDiscontinued` (HTTP 409 Conflict).
4. **Primary Barcode Exclusivity**:
   - When a barcode is created with `is_primary = true`, existing primary barcodes for that product (or specific variant) are atomically updated to `is_primary = false` via `store.ClearPrimaryBarcode`.
5. **Serial Number Catalog Definition & Lookup (DEC-B1-011)**:
   - Defining a serial number requires `track_serial = true` on the product master; otherwise returns `ErrSerialTrackingNotEnabled`.
   - Physical lifecycle transitions (received, reserved, sold, returned) are deferred to Inventory domain.

---

## 10. Audit and Outbox Transaction Guarantees

Every mutating domain operation executes inside a single database transaction via `txManager.WithinTx`:
1. `store.Create*` / `store.Update*` executes within the transaction.
2. `auditStore.Record` writes the immutable audit log entry into `audit.audit_log` within the transaction.
3. `outboxWriter.Write` writes the versioned event into `integration.outbox_event` within the transaction.
4. If either audit or outbox writes fail, the transaction returns an error and PostgreSQL rolls back the entire operation atomically.
5. In case of unexpected server failure, partial mutations cannot leak into PostgreSQL.

---

## 11. Test Execution Evidence

All test executions were run with macOS build cache conventions (`GOCACHE=/private/tmp/nexora-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/nexora-golangci-cache`).

### 1. Unit Tests (`backend/internal/master`)
- Command: `go test -v ./backend/internal/master/...`
- Result: **PASS** (10 test suites, exit code 0)
- Suites tested:
  - `TestHandler_AuthenticationAndAuthorization` (401 on missing principal, 403 on missing permission, 200 on authorized)
  - `TestHandler_DualMountCompatibilityAliasParity` (parity between `/products/{id}` and `/master/products/{id}`)
  - `TestHandler_ErrorMappingEnvelopes` (404 Not Found, 409 Conflict, 409 Optimistic Lock Conflict)
  - `TestMaster_BrandLifecycle` (happy path, duplicate code, validation, update)
  - `TestMaster_CategoryHierarchyAndCyclePrevention` (self-parenting, ancestor loop cycle detection)
  - `TestMaster_UOMPrecisionScale` (0..6 range validation, negative and >6 rejections)
  - `TestMaster_HybridCompanyOwnership` (tenant-wide nil company, valid company, cross-tenant rejection)
  - `TestMaster_ProductStateTransitions` (ACTIVE $\leftrightarrow$ INACTIVE, DISCONTINUED terminal state)
  - `TestMaster_BarcodePrimaryRulesAndLookup` (primary exclusivity, barcode scan lookup)
  - `TestMaster_SerialDefinitionAndLookup` (track_serial enforcement, serial lookup)

### 2. Disposable PostgreSQL Integration Tests (`master_integration_test.go`)
- Command: `go test -v -run "TestMaster_Integration" ./backend/internal/master/...`
- Result: **PASS** (5 integration suites, exit code 0)
- Execution characteristics:
  - Database: Disposable database `nexora_master_test_<timestamp>` created on PostgreSQL `127.0.0.1:15432`.
  - Roles: Migrations applied via admin pool; application tests executed via role `nexora_app` (`NOBYPASSRLS`, non-superuser).
  - Cleanup: Disposable database dropped `WITH (FORCE)` in `t.Cleanup`.
- Suites tested:
  - `TestMaster_Integration_Migration000009_IdempotencyAndSeeds`: Verified audit action catalog seeds (`BRAND_CREATED`, `BRAND_UPDATED`), permission seeds (`brand.create`, `category.create`, etc.), and idempotency of re-applying migration 000009.
  - `TestMaster_Integration_RLSTenantIsolation`: Verified Tenant B cannot access Tenant A's brands, categories, UOMs, products, variants, or barcodes under forced database RLS.
  - `TestMaster_Integration_HybridCompanyOwnership`: Verified Tenant A company access, tenant-wide access, and fail-closed rejection when attempting to reference Tenant B's company.
  - `TestMaster_Integration_OptimisticLockingAndLifecycle`: Verified version incrementing and lifecycle status transitions.
  - `TestMaster_Integration_AuditAndOutboxAtomicity`: Verified atomic write of product, audit log (`audit.audit_log`), and outbox event (`integration.outbox_event`).

### 3. Full Repository Regression with Race Detector
- Command: `go test -count=1 -race -p 1 ./backend/...`
- Result: **PASS** (Exit code 0, 0 race conditions across all packages)
  - `example.com/nexora/backend`
  - `example.com/nexora/backend/internal/identity`
  - `example.com/nexora/backend/internal/master`
  - `example.com/nexora/backend/internal/organization`
  - `example.com/nexora/backend/internal/platform/...` (apperr, audit, auth, config, idempotency, lifecycle, logger, observability, outbox, postgres, transport/http)

### 4. Static Analysis and Linter Verification
- Command: `golangci-lint run ./backend/...`
  - Result: **PASS** (0 issues, exit code 0)
- Command: `go vet ./backend/...`
  - Result: **PASS** (0 issues, exit code 0)
- Command: `go build ./backend/...`
  - Result: **PASS** (Exit code 0)

---

## 12. Disposable Database & Role Details

- **Test Host**: `127.0.0.1:15432`
- **Application Role**: `nexora_app`
  - Characteristics: `NOSUPERUSER`, `NOCREATEDB`, `NOCREATEROLE`, `NOINHERIT`, `NOBYPASSRLS`.
  - Grants: `SELECT`, `INSERT`, `UPDATE`, `DELETE` on schemas `identity`, `organization`, `master`, `platform`, `audit`, `integration`.
- **Database Lifecycle**: Ephemeral, dynamically created per test suite run, and dropped with force on completion.

---

## 13. Known Limitations and Deferred Work

1. **Pricing Isolation (DEC-B1-012)**:
   - Master Data endpoints return catalog metadata only (SKU, attributes JSONB, UOM, barcodes).
   - In accordance with DEC-B1-012, Master Data responses do not own or return authoritative prices. Unit prices and price list calculations remain strictly deferred to the Pricing domain (Batch 2).
2. **Serial Inventory Lifecycle (DEC-B1-011)**:
   - Serial number records in Master Data represent catalog definitions and lookup only.
   - Physical inventory lifecycle transitions (`AVAILABLE`, `RESERVED`, `SOLD`, `RETURNED`, `DAMAGED`) remain strictly deferred to the Inventory domain (Batch 2).
3. **Customer & CRM (Increment 4)**:
   - Customer entities, customer groups, and customer addresses remain un-implemented in this increment and are scheduled for Increment 4.
4. **Migration 000007**:
   - `000007_crm_audit_actions.sql` remains uncreated and reserved for Increment 4.

---

## 14. Governance Affirmations

- [x] Migrations `000001` through `000006` remain byte-for-byte unchanged.
- [x] Migration `000007_crm_audit_actions.sql` remains reserved for CRM and was NOT created or modified.
- [x] Migration `000008_organization_hardening.sql` remains byte-for-byte unchanged.
- [x] Migration `000009_master_hardening.sql` is forward-only, idempotent, and contains only approved catalog seeds.
- [x] No Git operations were performed (`git status`, `git commit`, `git push`, etc.).
- [x] No persistent or shared databases were accessed or modified. All integration tests ran on ephemeral, disposable databases.
- [x] Domain boundaries, forced RLS, audit/outbox atomicity, and API envelope contracts were fully preserved.

---

## 15. Final Verdict

**PASS WITH DOCUMENTED LIMITATIONS**

Master Data Increment 3 implementation is complete, fully verified against repository test gates and database security invariants, and is now **AWAITING INDEPENDENT ACCEPTANCE REVIEW**.
