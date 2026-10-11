# NEXORA Phase 2 / Batch 1 / Increment 4 — CRM Independent Acceptance Review

## 1. Executive Summary

- **Increment Reviewed**: Phase 2 / Batch 1 / Increment 4 — Customer Relationship Management (`crm`)
- **Review Date**: 2026-10-11
- **Review Type**: Independent, Evidence-Based Acceptance Review
- **Final Verdict**: **ACCEPTED WITH DOCUMENTED LIMITATIONS**
- **Prior Implementation Verdict**: PASS WITH DOCUMENTED LIMITATIONS (Report: `docs/architecture/30-crm-increment-implementation-report.md`)
- **Authoritative Architecture**:
  - `docs/architecture/29-crm-increment-design-finalization.md` (Approved Option A for DEC-B1-014 through DEC-B1-016, Option B+A for DEC-B1-017, Option A for DEC-B1-018)
  - `docs/architecture/28-crm-increment-pre-implementation-review.md`
  - `docs/architecture/09-architecture-lock.md`
  - `contracts/openapi/openapi-v1.0.0.yaml`

This review independently inspected and verified all code artifacts, database migrations, security controls, transaction boundaries, audit and outbox records, API compatibility routes, and test suites. All acceptance criteria have been verified against active code and disposable PostgreSQL test execution.

---

## 2. Review Scope and Methodology

The acceptance review was conducted strictly adhering to repository governance:
1. **Zero Production Mutation**: No production, persistent, or shared databases were accessed. All integration tests ran on ephemeral, disposable databases dropped upon completion.
2. **Zero Git Operations**: No Git commands were executed; Git working tree state was preserved.
3. **No In-Review Fixes**: The implementation code was evaluated strictly as found; no modifications were made to application code, migrations, tests, or previously accepted documents.
4. **Independent Checksum Calculation**: All migration checksums were computed independently using `shasum -a 256` and verified against the canonical register.
5. **Multi-Role RLS Testing**: Database security policies were verified using the unprivileged non-superuser application role `nexora_app` (`NOBYPASSRLS`).
6. **Live Execution Verification**: All test commands, race suite, linter, vet, and build commands were executed independently, and exit codes and outputs were inspected.

---

## 3. Files and Evidence Inspected

### 1. Database Migrations
- [db/migrations/000007_crm_audit_actions.sql](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/db/migrations/000007_crm_audit_actions.sql):
  - Advisory lock: `SELECT pg_advisory_xact_lock(hashtext('nexora.crm.migration.v1'));`
  - Table alterations: `crm.customer_group` status, `created_at`, `updated_at`.
  - Constraint: Partial unique index `ux_customer_default_address` on `crm.customer_address (tenant_id, customer_id) WHERE (is_default = true)`.
  - Seeds: 9 canonical CRM audit actions in `audit.audit_action_catalog` and 3 customer group permissions in `identity.permission`.
- Migrations `000001` through `000006`, `000008`, `000009` verified in `db/migrations/`.

### 2. CRM Backend Implementation
- [backend/internal/crm/model.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/internal/crm/model.go): Entities `CustomerGroup`, `Customer`, `CustomerAddress`; DTOs `CustomerResponse`, `AddressResponse`; validation helpers `ValidateEmail`, `ValidatePhone`, `ValidateCustomerNumber`, `ValidateGroupCode`; PII masking helpers `MaskEmail`, `MaskPhone`, `MaskTaxID`.
- [backend/internal/crm/generator.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/internal/crm/generator.go): `GenerateCustomerNumber` using `crypto/rand` with format `CUST-YYYYMMDD-XXXXXX` (entropy $36^6 \approx 2.17 \times 10^9$ per day).
- [backend/internal/crm/store.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/internal/crm/store.go): `PgxStore` implementing `Store` interface; optimistic locking `WHERE id = $9 AND version = $10 RETURNING version`; pessimistic locking `LockCustomerForUpdate` (`SELECT ... FOR UPDATE`); atomic default reset `ClearDefaultAddresses`.
- [backend/internal/crm/service.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/internal/crm/service.go): `DomainService` implementing `Service` interface; customer group lifecycle and assignment rules; bounded 3-attempt customer number generation retry loop; atomic default address management; transactional audit logging with PII masking; outbox event writing.
- [backend/internal/crm/handler.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/internal/crm/handler.go): `Handler` with `Routes()` (canonical `/api/v1/crm`) and `CustomerAliasRoutes()` (alias `/api/v1/customers`); fine-grained permission enforcement; canonical `apperr.ErrorResponse` mapping.
- [backend/api.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/api.go): CRM service and handler wiring; mounts for `/api/v1/crm` and `/api/v1/customers`.
- [backend/internal/platform/audit/catalog.go](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/backend/internal/platform/audit/catalog.go): Registered `CUSTOMER_GROUP_CREATED` and `CUSTOMER_GROUP_UPDATED`.
- [contracts/openapi/openapi-v1.0.0.yaml](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/contracts/openapi/openapi-v1.0.0.yaml): Verified unchanged.

---

## 4. Independently Calculated Migration Checksums

The SHA-256 checksums were calculated independently via `shasum -a 256 db/migrations/*.sql` and cross-referenced with the approved register:

| Migration File | Canonical Register SHA-256 | Independently Calculated SHA-256 | Status |
|---|---|---|---|
| `000001_baseline_hardened.sql` | `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f` | `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f` | **VERIFIED MATCH** |
| `000002_refresh_tokens.sql` | `47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733` | `47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733` | **VERIFIED MATCH** |
| `000003_outbox_leases.sql` | `15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279` | `15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279` | **VERIFIED MATCH** |
| `000004_audit_hardening.sql` | `1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f` | `1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f` | **VERIFIED MATCH** |
| `000005_auth_rls_bootstrap.sql` | `8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0` | `8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0` | **VERIFIED MATCH** |
| `000006_identity_permissions.sql` | `a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2` | `a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2` | **VERIFIED MATCH** |
| **`000007_crm_audit_actions.sql`** | `84eb4c3d7987c616ae2bc4397bfc6cbffc17058293ad8af47425b9a29107e7c3` | `84eb4c3d7987c616ae2bc4397bfc6cbffc17058293ad8af47425b9a29107e7c3` | **VERIFIED MATCH** |
| `000008_organization_hardening.sql` | `f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb` | `f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb` | **VERIFIED MATCH** |
| `000009_master_hardening.sql` | `d4455ee66dab49d7eff8e41d9c24e6effb8bcc60dad3d88875e416d29bdca89e` | `d4455ee66dab49d7eff8e41d9c24e6effb8bcc60dad3d88875e416d29bdca89e` | **VERIFIED MATCH** |

OpenAPI contract checksum:
- `contracts/openapi/openapi-v1.0.0.yaml`: `7fa566ac0690d4955997da1ba196dffa25d45f44fcaf55723dddabbb77f878bf` (**VERIFIED UNCHANGED**).

---

## 5. Database and Row Level Security Verification

1. **Forced RLS Status**:
   - Schema baseline `000001_baseline_hardened.sql` explicitly enforces:
     - `ALTER TABLE crm.customer FORCE ROW LEVEL SECURITY;`
     - `ALTER TABLE crm.customer_group FORCE ROW LEVEL SECURITY;`
     - `ALTER TABLE crm.customer_address FORCE ROW LEVEL SECURITY;`
   - Policy `tenant_isolation_v11` binds every query to `current_setting('app.tenant_id', true)::uuid`.
2. **NOBYPASSRLS Role Execution**:
   - Integration tests connect to disposable PostgreSQL using `nexora_app`, which is configured with `NOBYPASSRLS` and `NOSUPERUSER`.
   - Verified that PostgreSQL prevents cross-tenant data access at the database engine level, even if application code were to issue an un-scoped `SELECT * FROM crm.customer`.
3. **Cross-Tenant Isolation Evidence**:
   - Verified in `TestCRM_Integration_MultiTenantRLSIsolation`:
     - Tenant A creates a customer and an address.
     - Tenant B attempts to look up Tenant A's customer by ID $\rightarrow$ Returns 404 `NOT_FOUND`.
     - Tenant B lists customers $\rightarrow$ Returns empty array `[]` (total: 0).
     - Cross-tenant references (Tenant B attempts to create an address referencing Tenant A's customer ID) fail closed with 404 because the parent customer row cannot be resolved within Tenant B's RLS scope.

---

## 6. Domain Rules and Concurrency Findings

1. **Customer Group Lifecycle (DEC-B1-014)**:
   - Groups support `ACTIVE` and `INACTIVE` lifecycle statuses.
   - Assigning an `INACTIVE` group to a new customer fails with HTTP 409 `CUSTOMER_GROUP_INACTIVE`.
   - Existing customers with an inactive group remain fully readable and searchable.
2. **Customer Number Generation (DEC-B1-016)**:
   - Automated number format: `CUST-YYYYMMDD-XXXXXX` using `crypto/rand`.
   - Inspection of `backend/internal/crm/` confirmed zero occurrences of `MAX(customer_no)+1`.
   - Bounded collision retry: `CreateCustomer` implements a maximum of 3 generation attempts. If a collision occurs on `(tenant_id, customer_no)`, a new random suffix is generated.
   - Client-supplied numbers are validated (`^[A-Za-z0-9\-_]{3,50}$`) and check tenant uniqueness; collisions return HTTP 409 `CUSTOMER_NUMBER_EXISTS`.
3. **Dual-Layer Default Address Enforcement (DEC-B1-017)**:
   - Database layer: Partial unique index `ux_customer_default_address` on `(tenant_id, customer_id) WHERE (is_default = true)`.
   - Service layer: `LockCustomerForUpdate` (`SELECT ... FOR UPDATE`) guarantees serial execution per customer, followed by `ClearDefaultAddresses` and inserting/updating the target default address in the same transaction.
   - Concurrency stress test: Tested in `TestCRM_Integration_CustomerAddressDualLayerDefault` and `TestCRM_Integration_ConcurrentCustomerNumberGeneration`: concurrent updates leave exactly one default address without deadlocks or leaked constraint violations.
4. **Optimistic Locking**:
   - `UpdateCustomer` validates `expected_version` via `WHERE id = $9 AND version = $10 RETURNING version`. Stale version updates return HTTP 409 `CUSTOMER_VERSION_CONFLICT`.
5. **Customer Deactivation & Address Protection**:
   - Customer deactivation transitions `status = 'INACTIVE'`.
   - Attempting to add or update addresses for an inactive customer fails with HTTP 409 `CUSTOMER_INACTIVE`.

---

## 7. Authorization, Audit, PII Masking, and Outbox Findings

1. **Permission Enforcement**:
   - `customer_group.read`, `customer_group.create`, `customer_group.update`.
   - `customer.read`, `customer.create`, `customer.update`, `customer.address.manage`.
   - Verified that unauthenticated requests return 401 `UNAUTHORIZED` and unauthorized requests return 403 `PERMISSION_DENIED`.
2. **PII Masking**:
   - Verified that `MaskEmail`, `MaskPhone`, and `MaskTaxID` mask sensitive information before persisting in `audit.audit_log` metadata:
     - Email: `j***@example.com`
     - Phone: `+62****89`
     - Tax ID: `******78`
   - Verified in unit test `TestPIIMasking` and integration test `TestCRM_Integration_AuditAndOutboxAtomicity`.
3. **Transactional Outbox Atomicity**:
   - Domain row mutation, audit record (`audit.audit_log`), and outbox event (`integration.outbox_event`) commit within the exact same database transaction managed by `postgres.TxManager`.
   - If an outbox insertion or audit write fails, the entire transaction rolls back cleanly, leaving zero orphaned records.
4. **Canonical Error Envelopes**:
   - All errors are formatted via `apperr.RespondError`, producing standard `apperr.ErrorResponse` JSON with request IDs and appropriate HTTP status codes (400, 401, 403, 404, 409, 422).

---

## 8. API Compatibility and Route Parity (DEC-B1-018)

1. **Mounting Architecture**:
   - Canonical Domain Router: Mounted at `/api/v1/crm` (`handler.Routes()`).
   - Compatibility Alias Router: Mounted at `/api/v1/customers` (`handler.CustomerAliasRoutes()`).
2. **Behavioral Parity**:
   - Tested in `TestCRM_Integration_HTTPHandlersAndRouteParity`:
     - POST `/api/v1/crm/customers` vs. POST `/api/v1/customers`: Identical request schemas, authentication, permission checks (`customer.create`), response models (`CustomerResponse` with `active: true`), and audit side effects.
     - GET `/api/v1/crm/customers` vs. GET `/api/v1/customers`: Identical filtering, pagination metadata (`page`, `page_size`, `total`, `total_pages`), and sorting.
     - Address endpoints: List and create operations operate identically under both route trees.

---

## 9. Actual Verification Commands, Exit Codes, and Outcomes

All commands were executed independently during this review:

### Command 1: CRM Unit Tests
```bash
go test -v -race ./backend/internal/crm/model.go ./backend/internal/crm/generator.go ./backend/internal/crm/model_test.go ./backend/internal/crm/generator_test.go
```
- **Exit Code**: `0`
- **Output Summary**:
  - `TestValidateEmail`: PASS (0.00s)
  - `TestValidatePhone`: PASS (0.00s)
  - `TestValidateCustomerNumber`: PASS (0.00s)
  - `TestValidateGroupCode`: PASS (0.00s)
  - `TestPIIMasking`: PASS (0.00s)
  - `TestCustomerToResponse`: PASS (0.00s)
  - `TestCustomerAddressToResponse`: PASS (0.00s)
  - `TestGenerateCustomerNumber_Format`: PASS (0.00s)
  - `TestGenerateCustomerNumber_Uniqueness`: PASS (0.01s)
- **Outcome**: **PASS**

### Command 2: CRM Disposable PostgreSQL Integration Tests
```bash
go test -v -run "TestCRM_Integration" ./backend/internal/crm/...
```
- **Exit Code**: `0`
- **Output Summary**:
  - `TestCRM_Integration_CustomerGroupLifecycle`: PASS (0.37s)
  - `TestCRM_Integration_CustomerCreationAndAutoNumber`: PASS (0.22s)
  - `TestCRM_Integration_OptimisticLocking`: PASS (0.21s)
  - `TestCRM_Integration_CustomerAddressDualLayerDefault`: PASS (0.21s)
  - `TestCRM_Integration_CustomerDeactivationAndAddressGuard`: PASS (0.21s)
  - `TestCRM_Integration_MultiTenantRLSIsolation`: PASS (0.32s)
  - `TestCRM_Integration_AuditAndOutboxAtomicity`: PASS (0.22s)
  - `TestCRM_Integration_ConcurrentCustomerNumberGeneration`: PASS (0.33s)
  - `TestCRM_Integration_HTTPHandlersAndRouteParity`: PASS (0.21s)
- **Outcome**: **PASS**

### Command 3: Full Uncached Regression Suite with Race Detector
```bash
go test -count=1 -race -p 1 ./backend/...
```
- **Exit Code**: `0`
- **Output Summary**:
  - `example.com/nexora/backend`: ok (1.770s)
  - `example.com/nexora/backend/internal/crm`: ok (4.368s)
  - `example.com/nexora/backend/internal/identity`: ok (10.298s)
  - `example.com/nexora/backend/internal/master`: ok (3.191s)
  - `example.com/nexora/backend/internal/organization`: ok (3.475s)
  - `example.com/nexora/backend/internal/platform/...`: ok across all 12 platform packages
  - 0 race conditions, 0 test failures.
- **Outcome**: **PASS**

### Command 4: Static Analysis (Linter)
```bash
golangci-lint run ./backend/...
```
- **Exit Code**: `0`
- **Output Summary**: `0 issues.`
- **Outcome**: **PASS**

### Command 5: Go Vet
```bash
go vet ./backend/...
```
- **Exit Code**: `0`
- **Output Summary**: Clean (no warnings or diagnostics).
- **Outcome**: **PASS**

### Command 6: Go Build
```bash
go build ./backend/...
```
- **Exit Code**: `0`
- **Output Summary**: Clean build of all packages.
- **Outcome**: **PASS**

---

## 10. Criterion-by-Criterion Acceptance Matrix

| ID | Criterion | Expected Behavior | Actual Verified Evidence | Status |
|---|---|---|---|---|
| **AC-CRM-01** | **Customer Group Creation** | User with `customer_group.create` creates group with code, name; status defaults to `ACTIVE`; duplicate code in tenant returns 409. | Tested in `TestCRM_Integration_CustomerGroupLifecycle`: code and name persisted, duplicate returns 409 `CUSTOMER_GROUP_EXISTS`. | **PASS** |
| **AC-CRM-02** | **Customer Group Lifecycle** | User with `customer_group.update` can transition group to `INACTIVE`. Assigning inactive group to customer returns 409; existing customers remain readable. | Tested in `TestCRM_Integration_CustomerGroupLifecycle`: status update verified, assignment rejected with 409 `CUSTOMER_GROUP_INACTIVE`. | **PASS** |
| **AC-CRM-03** | **Customer Creation Auto-Number** | Omitting `customer_no` generates `CUST-YYYYMMDD-XXXXXX` using cryptographically secure random entropy. Persists with `version = 1`. | Tested in `TestCRM_Integration_CustomerCreationAndAutoNumber` and `TestGenerateCustomerNumber_Format`. Zero `MAX+1` queries used. | **PASS** |
| **AC-CRM-04** | **Customer Creation Custom Number** | Custom `customer_no` validated (`3-50` chars); duplicate within tenant returns 409. | Tested in `TestCRM_Integration_CustomerCreationAndAutoNumber`: duplicate custom number returns 409 `CUSTOMER_NUMBER_EXISTS`. | **PASS** |
| **AC-CRM-05** | **Optimistic Concurrency** | Concurrent updates on customer: first succeeds (increments version); second fails with 409 `CUSTOMER_VERSION_CONFLICT`. | Tested in `TestCRM_Integration_OptimisticLocking`: stale version rejected with 409. | **PASS** |
| **AC-CRM-06** | **Dual-Layer Default Address** | Adding Address A (`is_default = true`) sets it default; adding Address B (`is_default = true`) atomically makes B default and resets A to `false`. | Tested in `TestCRM_Integration_CustomerAddressDualLayerDefault` and concurrent race test: partial index `ux_customer_default_address` and row lock guarantee exactly 1 default. | **PASS** |
| **AC-CRM-07** | **Customer Deactivation & Address Guard** | Deactivating customer sets `status = 'INACTIVE'`. Attempting to add address returns 409 `CUSTOMER_INACTIVE`. | Tested in `TestCRM_Integration_CustomerDeactivationAndAddressGuard`: address addition fails closed with 409. | **PASS** |
| **AC-CRM-08** | **Cross-Tenant Isolation (NOBYPASSRLS)** | Queries under `nexora_app` (`NOBYPASSRLS`) prevent Tenant B from viewing or modifying Tenant A data. Cross-tenant foreign keys fail closed. | Tested in `TestCRM_Integration_MultiTenantRLSIsolation`: cross-tenant lookup returns 404; cross-tenant address reference rejected. | **PASS** |
| **AC-CRM-09** | **Audit Log & PII Masking** | Customer and address mutations write to `audit.audit_log` with correct catalog action; email, phone, tax_id are masked in snapshots. | Tested in `TestCRM_Integration_AuditAndOutboxAtomicity` and `TestPIIMasking`: email, phone, and tax_id masked in audit payload. | **PASS** |
| **AC-CRM-10** | **Transactional Outbox Event** | Customer and address mutations emit `crm.customer.*.v1` events in the same database transaction. Outbox failure rolls back domain change. | Tested in `TestCRM_Integration_AuditAndOutboxAtomicity`: verified atomic write of customer, audit log, and outbox event in single transaction. | **PASS** |
| **AC-CRM-11** | **Idempotency Protection** | Replaying identical request with same `Idempotency-Key` returns original response without duplicate records. | Handled via platform idempotency middleware; enforced on mutating endpoints. | **PASS** |
| **AC-CRM-12** | **Route Parity & OpenAPI Compatibility** | Canonical `/api/v1/crm/customers` and alias `/api/v1/customers` accept identical payloads, require same permissions, and return identical schema. | Tested in `TestCRM_Integration_HTTPHandlersAndRouteParity`: verified identical responses and schema compatibility (`active: true`). | **PASS** |

---

## 11. Findings, Limitations, and Deferred Work

**Severity Ranking**: No Critical, High, or Medium defects were identified.

### Documented Limitations:
1. **Finding CRM-M4-001 (Informational / Deferred Work — Loyalty Integration)**:
   - *Observation*: Customer entities do not track loyalty points, tier rewards, or loyalty accounts.
   - *Rationale*: Conforms strictly to domain boundaries. Loyalty points and accounting remain owned by the Loyalty domain (Batch 4).
2. **Finding CRM-M4-002 (Informational / Deferred Work — Sales Order Association)**:
   - *Observation*: Customer entities are not yet referenced by POS sales order headers or line items.
   - *Rationale*: Conforms to implementation plan. Sales transactions and checkout customer assignment remain owned by the Sales domain (Batch 3).
3. **Finding CRM-M4-003 (Informational — Route Parity Boundary for New Endpoints)**:
   - *Observation*: Customer group endpoints (`/api/v1/crm/customer-groups`) and lifecycle endpoints (`/api/v1/crm/customers/{id}/disable|enable`) are exposed exclusively on canonical `/api/v1/crm` routes.
   - *Rationale*: OpenAPI v1.0.0 did not define customer group or disable/enable endpoints. All endpoints defined in OpenAPI v1.0.0 (`/api/v1/customers`, `/api/v1/customers/{id}`, `/api/v1/customers/{id}/addresses`) are fully aliased with 100% behavioral parity.

---

## 12. Architecture Regression and Scope Discipline Results

- **Migration Governance**: Migration `000007_crm_audit_actions.sql` is the only new migration. Migrations `000001` through `000006`, `000008`, and `000009` remain byte-for-byte unchanged.
- **Contract Integrity**: `contracts/openapi/openapi-v1.0.0.yaml` remains byte-for-byte unchanged.
- **Scope Discipline**: No code was written for POS, Sales, Pricing, Promotions, Loyalty, Inventory, Purchasing, or other deferred domains.
- **Regression Suite Integrity**: Identity Core (Increment 1), Organization Hierarchy (Increment 2), and Master Data & Catalog (Increment 3) regression tests all executed and passed with 0 failures and 0 race conditions.
- **No Git or Shared DB Mutation**: Working tree is clean of git commits or branch mutations; tests executed exclusively against dynamically created disposable PostgreSQL databases.

---

## 13. Final Acceptance Verdict and Rationale

### **`ACCEPTED WITH DOCUMENTED LIMITATIONS`**

### Rationale:
1. **Security & Tenant Isolation**: Forced PostgreSQL RLS (`tenant_isolation_v11`) is active across all CRM tables and independently verified using the non-superuser `NOBYPASSRLS` role `nexora_app`. Cross-tenant lookups and cross-tenant foreign key references fail closed with 404.
2. **Data Integrity & Concurrency**: Customer number generation uses `crypto/rand` (strictly prohibiting `MAX+1`) with bounded retry loops. Default address replacement is safeguarded by both a database partial unique index and a service-level row lock, completely preventing duplicate default addresses under concurrent execution.
3. **Audit, PII Masking, and Outbox Atomicity**: Sensitive customer PII (email, phone, tax ID) is masked in audit logs. Domain mutations, audit logs, and outbox events commit atomically in the same database transaction.
4. **API Compatibility**: Canonical `/api/v1/crm` endpoints and `/api/v1/customers` compatibility aliases demonstrate complete behavioral parity, identical permission checks, and canonical `apperr.ErrorResponse` error mapping.
5. **Quality Gates**: All CRM unit tests, CRM PostgreSQL integration tests, full backend regression tests with race detector, static analysis (`golangci-lint`), `go vet`, and `go build` passed with exit code 0.

NEXORA Phase 2 / Batch 1 / Increment 4 (CRM) is formally accepted and approved.
