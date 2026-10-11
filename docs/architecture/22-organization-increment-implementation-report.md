# NEXORA — Organization Increment 2 Implementation Report

## Executive Summary

- **Increment:** Phase 2 / Batch 1 / Increment 2: Organization Hierarchy & Terminal Security
- **Date:** 2026-10-11
- **Status:** COMPLETED & VERIFIED
- **Verdict:** `PASS`

Organization Increment 2 implements the organizational hierarchy (Company, Region, Store, Warehouse, POS Terminal), POS terminal registration and token lifecycle management, parent-child referential authorization, transactional audit/outbox integration, and tenant RLS isolation.

Implementation conforms strictly to the approved decisions:
- **DEC-B1-006 (Option A):** Forward migration `000008_organization_hardening.sql` enables RLS on `organization.region` with policy `tenant_region_isolation`, and expands the terminal status check constraint to include `DISABLED` and `RETIRED`.
- **DEC-B1-007 (Option A):** Terminal device tokens are server-issued with high cryptographic entropy (`crypto/rand`, 32 bytes hex encoded). The database stores exclusively the SHA-256 hash in `organization.terminal.device_identifier`. The plaintext token is returned strictly once upon registration/rotation and is never logged or exposed.

---

## 1. Migration Changes & Checksum Verification

### Migration Allocation
- **New Migration:** `db/migrations/000008_organization_hardening.sql`
- **Advisory Lock:** `SELECT pg_advisory_xact_lock(hashtext('nexora.organization.hardening.v1'))`
- **Contents:**
  1. `ALTER TABLE organization.region ENABLE ROW LEVEL SECURITY;`
  2. `ALTER TABLE organization.region FORCE ROW LEVEL SECURITY;`
  3. `CREATE POLICY tenant_region_isolation ON organization.region AS RESTRICTIVE USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);`
  4. `ALTER TABLE organization.terminal DROP CONSTRAINT IF EXISTS terminal_status_check;`
  5. `ALTER TABLE organization.terminal ADD CONSTRAINT terminal_status_check CHECK (status IN ('ACTIVE', 'INACTIVE', 'MAINTENANCE', 'DISABLED', 'RETIRED'));`

### Checksum Verification Results
Every migration in `db/migrations/` was verified using SHA-256 (`shasum -a 256 db/migrations/*.sql`):

| Migration File | SHA-256 Checksum | Status |
|---|---|---|
| `000001_baseline_hardened.sql` | `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f` | UNCHANGED (Matches baseline lock) |
| `000002_refresh_tokens.sql` | `47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733` | UNCHANGED |
| `000003_outbox_leases.sql` | `15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279` | UNCHANGED |
| `000004_audit_hardening.sql` | `1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f` | UNCHANGED |
| `000005_auth_rls_bootstrap.sql` | `8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0` | UNCHANGED |
| `000006_identity_permissions.sql` | `a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2` | UNCHANGED |
| `000007_crm_audit_actions.sql` | *Absent* | RESERVED FOR CRM (Never created or executed) |
| `000008_organization_hardening.sql` | `f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb` | NEW FORWARD MIGRATION |

---

## 2. Implemented Files & Capabilities

All domain code is localized within `backend/internal/organization/` and route wiring in `backend/api.go`:

1. **`backend/internal/organization/model.go`**:
   - Entities: `Company`, `Region`, `Store`, `Warehouse`, `Terminal`.
   - Lifecycle Statuses:
     - General: `ACTIVE`, `INACTIVE`.
     - Terminal: `ACTIVE`, `INACTIVE`, `MAINTENANCE`, `DISABLED`, `RETIRED`.
     - Warehouse Types: `MAIN`, `STORE`, `TRANSIT`, `RETURN`.
   - Canonical Outbox Event Types:
     - `organization.company.created.v1`
     - `organization.region.created.v1`
     - `organization.store.created.v1`
     - `organization.warehouse.created.v1`
     - `organization.terminal.registered.v1`
     - `organization.terminal.updated.v1`
     - `organization.terminal.revoked.v1`
   - Canonical Audit Actions:
     - `COMPANY_CREATED`, `COMPANY_UPDATED`
     - `REGION_CREATED`, `REGION_UPDATED`
     - `STORE_CREATED`, `STORE_UPDATED`
     - `WAREHOUSE_CREATED`, `WAREHOUSE_UPDATED`
     - `TERMINAL_REGISTERED`, `TERMINAL_UPDATED`, `TERMINAL_REVOKED`
   - Request and response DTOs with validation rules.

2. **`backend/internal/organization/token.go`**:
   - `GenerateDeviceToken()`: Generates `dev_tok_` prefix followed by 32 cryptographically secure random bytes from `crypto/rand` (hex-encoded, 256-bit entropy).
   - `HashDeviceToken(token)`: Computes SHA-256 hash formatted as lowercase hex string.
   - `VerifyDeviceToken(providedToken, storedHash)`: Constant-time comparison using `crypto/subtle.ConstantTimeCompare` protecting against timing attacks.

3. **`backend/internal/organization/errors.go`**:
   - Sentinel domain errors: `ErrCompanyNotFound`, `ErrCompanyCodeExists`, `ErrRegionNotFound`, `ErrRegionCodeExists`, `ErrStoreNotFound`, `ErrStoreCodeExists`, `ErrWarehouseNotFound`, `ErrWarehouseCodeExists`, `ErrTerminalNotFound`, `ErrTerminalCodeExists`, `ErrDeviceTokenInvalid`, `ErrTerminalRevoked`, `ErrParentNotFound`, `ErrInvalidStatusTransition`, `ErrOptimisticLockConflict`.
   - `MapToAppError(err)`: Deterministic translation into canonical `apperr.AppError` envelopes with HTTP 400, 404, or 409 status codes.

4. **`backend/internal/organization/store.go`**:
   - Persistence layer implementing `OrgStore` interface on PostgreSQL with `pgxpool`.
   - Enforces optimistic concurrency via `version = version + 1 WHERE version = $N`.
   - Handles PostgreSQL unique constraint violations (`23505`) and foreign key violations (`23503`).
   - Supports device token lookup by SHA-256 hash (`GetTerminalByDeviceHash`) and atomic hash updates (`UpdateTerminalDeviceHash`).
   - Implements `ValidateScopeEntity` for cross-domain validation of Identity user-scope assignments.

5. **`backend/internal/organization/service.go`**:
   - Business service (`DomainService`) orchestrating transactional commands.
   - Verifies hierarchical integrity:
     - Region belongs to Company.
     - Store belongs to Company and optional Region.
     - Warehouse belongs to Company and optional Store.
     - Terminal belongs to Store and Company.
   - Enforces terminal lifecycle state transitions:
     - Disallows transitions from terminal terminal states (`RETIRED`).
     - Rotates and revokes device tokens atomically.
   - Writes atomic audit records via `audit.Store` and outbox events via `outbox.Writer` within the same database transaction.

6. **`backend/internal/organization/handler.go`**:
   - HTTP transport adapter using Chi router mounted under `/api/v1/organization`.
   - Enforces authentication (`auth.Principal`) and canonical permissions:
     - `company.create`, `company.update`, `company.read`
     - `region.create`, `region.update`, `region.read`
     - `store.create`, `store.update`, `store.read`
     - `warehouse.create`, `warehouse.update`, `warehouse.read`
     - `terminal.register`, `terminal.rotate`, `terminal.revoke`, `terminal.update`, `terminal.read`
   - Injects authenticated `tenant_id` into database transaction context via `tm.WithinTx`.
   - Single-exposure token return: plaintext token returned once in `TerminalRegistrationResult` and never persisted.

7. **`backend/api.go`**:
   - Wired `organization.NewHandler` and mounted routes under `/api/v1/organization`.

---

## 3. API Endpoints and Authorization Requirements

All endpoints require an authenticated principal (`Bearer` token) within a valid tenant.

| Method | Path | Required Permission | Description |
|---|---|---|---|
| `POST` | `/api/v1/organization/companies` | `company.create` | Create company |
| `GET` | `/api/v1/organization/companies` | `company.read` | List companies (paginated) |
| `GET` | `/api/v1/organization/companies/{id}` | `company.read` | Get company by ID |
| `PUT` | `/api/v1/organization/companies/{id}` | `company.update` | Update company |
| `POST` | `/api/v1/organization/regions` | `region.create` | Create region |
| `GET` | `/api/v1/organization/regions` | `region.read` | List regions (filter by `company_id`) |
| `GET` | `/api/v1/organization/regions/{id}` | `region.read` | Get region by ID |
| `PUT` | `/api/v1/organization/regions/{id}` | `region.update` | Update region |
| `POST` | `/api/v1/organization/stores` | `store.create` | Create store |
| `GET` | `/api/v1/organization/stores` | `store.read` | List stores (filter by `company_id`) |
| `GET` | `/api/v1/organization/stores/{id}` | `store.read` | Get store by ID |
| `PUT` | `/api/v1/organization/stores/{id}` | `store.update` | Update store |
| `POST` | `/api/v1/organization/warehouses` | `warehouse.create` | Create warehouse |
| `GET` | `/api/v1/organization/warehouses` | `warehouse.read` | List warehouses (filter by `company_id`, `store_id`) |
| `GET` | `/api/v1/organization/warehouses/{id}` | `warehouse.read` | Get warehouse by ID |
| `PUT` | `/api/v1/organization/warehouses/{id}` | `warehouse.update` | Update warehouse |
| `POST` | `/api/v1/organization/terminals` | `terminal.register` | Register terminal (issues device token) |
| `GET` | `/api/v1/organization/terminals` | `terminal.read` | List terminals (filter by `store_id`) |
| `GET` | `/api/v1/organization/terminals/{id}` | `terminal.read` | Get terminal by ID |
| `PUT` | `/api/v1/organization/terminals/{id}` | `terminal.update` | Update terminal details/status |
| `POST` | `/api/v1/organization/terminals/{id}/rotate-token` | `terminal.rotate` | Rotate device token (issues new token) |
| `POST` | `/api/v1/organization/terminals/{id}/revoke-token` | `terminal.revoke` | Revoke device token (clears hash, sets `DISABLED`) |

---

## 4. Token Lifecycle and Security Behavior

- **Entropy:** Tokens are constructed with prefix `dev_tok_` and 32 bytes from `crypto/rand` (64 hex characters, 256 bits entropy).
- **Storage:** Only `HashDeviceToken(token)` (lowercase hex of SHA-256) is persisted to `organization.terminal.device_identifier`. Plaintext is never stored in PostgreSQL.
- **Exposure:** The plaintext device token is returned strictly once in the response body of `POST /terminals` and `POST /terminals/{id}/rotate-token`. Subsequent `GET /terminals/{id}` queries never return device credentials.
- **Verification:** Presentation of the bearer device token is verified using `VerifyDeviceToken` which performs `subtle.ConstantTimeCompare` on the computed hash against the stored hash.
- **Rotation:** Supersedes previous hash immediately with new token hash, increments terminal version, logs audit action, and emits outbox event.
- **Revocation:** Sets `device_identifier = NULL`, status to `DISABLED`, increments terminal version, logs `TERMINAL_REVOKED` audit action, and emits `organization.terminal.revoked.v1` event.
- **Leakage Prevention:** Plaintext token is never passed to audit metadata, outbox payloads, log messages, or ordinary API responses.

---

## 5. Security & Isolation Verification (Disposable DB & Role)

### Disposable Database Environment
- **Host / Port:** `127.0.0.1:5432`
- **Disposable Database Name:** `nexora_test_org_*` (dynamically provisioned and dropped per test suite)
- **Application Role:** `nexora_app` (`NOBYPASSRLS`, non-superuser, non-owner)
- **Verification:**
  - `organization.region` row-level security: Tenant A cannot read or update Tenant B's regions.
  - Multi-tenant boundary: Cross-tenant company, store, warehouse, and terminal accesses return 404 `ErrNotFound`.
  - Application role verified to be constrained by RLS without bypass permissions.

---

## 6. Test Execution & Verification Evidence

All tests were executed and verified locally.

### 1. Organization Unit Tests
```bash
go test -count=1 -v ./backend/internal/organization -run "TestToken|TestService|TestHandler"
```
- `TestGenerateDeviceToken_FormatAndEntropy`: PASS
- `TestHashAndVerifyDeviceToken`: PASS
- `TestVerifyDeviceToken_Mismatch`: PASS
- `TestService_CreateStore_ParentValidation`: PASS
- `TestService_TerminalLifecycle`: PASS
- `TestHandler_AuthAndPermissions`: PASS
- `TestHandler_RegisterTerminal_And_TokenVerification`: PASS

### 2. Disposable PostgreSQL Integration Tests
```bash
go test -count=1 -v ./backend/internal/organization -run "TestOrganization_Integration"
```
- `TestOrganization_Integration_FullLifecycle`: PASS
  - Company $\rightarrow$ Region $\rightarrow$ Store $\rightarrow$ Warehouse $\rightarrow$ Terminal hierarchy creation.
  - Terminal device token registration: plaintext returned once; SHA-256 hash verified in PostgreSQL.
  - Device token constant-time verification against stored hash: PASS.
  - Device token rotation: new token generated; old token rejected: PASS.
  - Device token revocation: hash cleared, status set to `DISABLED`: PASS.
  - Audit logs recorded: PASS.
  - Outbox events emitted: PASS.
- `TestOrganization_Integration_RLS_RegionIsolation`: PASS
  - Region created by Tenant A.
  - Tenant B queries with Tenant B RLS context $\rightarrow$ 0 rows returned.
  - Direct update attempt by Tenant B $\rightarrow$ rejected with `ErrRegionNotFound`.
- `TestOrganization_Integration_OptimisticLocking`: PASS
  - Concurrent update on stale version $\rightarrow$ rejected with `ErrOptimisticLockConflict` (HTTP 409).
- `TestOrganization_Integration_ScopeEntityValidation`: PASS
  - Validates active status of Company, Region, Store, Warehouse, Terminal for Identity scope binding.
  - Rejects inactive or non-existent entities.

### 3. Full Repository Race & Concurrency Test
```bash
go test -count=1 -race -p 1 ./backend/...
```
- `example.com/nexora/backend`: ok (1.822s)
- `example.com/nexora/backend/internal/identity`: ok (9.994s)
- `example.com/nexora/backend/internal/organization`: ok (3.047s)
- `example.com/nexora/backend/internal/platform/apperr`: ok (1.229s)
- `example.com/nexora/backend/internal/platform/audit`: ok (1.701s)
- `example.com/nexora/backend/internal/platform/auth`: ok (2.129s)
- `example.com/nexora/backend/internal/platform/config`: ok (1.177s)
- `example.com/nexora/backend/internal/platform/idempotency`: ok (1.831s)
- `example.com/nexora/backend/internal/platform/lifecycle`: ok (1.273s)
- `example.com/nexora/backend/internal/platform/logger`: ok (1.202s)
- `example.com/nexora/backend/internal/platform/observability`: ok (1.285s)
- `example.com/nexora/backend/internal/platform/outbox`: ok (1.794s)
- `example.com/nexora/backend/internal/platform/postgres`: ok (1.398s)
- `example.com/nexora/backend/internal/platform/transport/http`: ok (1.280s)
- `example.com/nexora/backend/internal/platform/transport/http/middleware`: ok (1.981s)
- **Result:** 0 data races, 0 failures across all 15 backend packages.

### 4. Static Analysis & Build Verification
```bash
golangci-lint run ./backend/...
go vet ./backend/...
go build ./backend/...
```
- `golangci-lint`: 0 issues.
- `go vet`: 0 issues.
- `go build`: Build succeeded cleanly.

---

## 7. Governance and Operational Confirmations

1. **Migrations 000001–000006:** Verified unchanged. Checksums match baseline and previous increments byte-for-byte.
2. **Migration 000007:** Verified absent. Reserved exclusively for future CRM work.
3. **Migration 000008:** Verified allocated for Organization hardening.
4. **Git Operations:** ZERO Git commands or Git state mutations executed.
5. **Database Operations:** ZERO operations executed against production or persistent shared databases. All integration tests executed exclusively in disposable databases.
6. **Domain Boundaries:** Master Data, CRM, POS sessions, Sales, Payment, Inventory, and other domains remain strictly unimplemented.

---

## 8. Known Limitations and Deferred Items

1. **Host Build Cache Configuration:** macOS host build cache permissions require `GOCACHE=/private/tmp/nexora-go-build-cache` and `GOLANGCI_LINT_CACHE=/private/tmp/nexora-golangci-cache` when running locally outside CI container environments.
2. **POS Session Opening:** Actual POS session opening (`pos.session`) and cash drawer workflows are deferred to Phase 2 Batch 2 (`pos` domain). Terminal registration and token verification foundations are complete.

---

## 9. Final Verdict

**VERDICT: `PASS`**

Organization Increment 2 satisfies all approved architectural decisions, passes all unit, integration, and concurrency race tests, respects non-superuser PostgreSQL RLS isolation, enforces device token security, preserves migration governance, and maintains clean domain boundaries. Acceptance remains subject to formal review.
