# NEXORA — Phase 1 Foundation Implementation Plan

**Document Version:** 1.0  
**Status:** DRAFT — Architecture & Planning Baseline  
**Target Repository:** `nexora/`  
**Date:** 2026-10-10  
**Scope:** Planning & documentation only for Phase 1 (Foundation Implementation). No code modification, migration execution, or business-domain development.

---

## 1. Purpose, Scope, Exclusions, Assumptions & Dependencies

### 1.1 Purpose
This document establishes the definitive, testable implementation blueprint for **Phase 1 — Foundation Implementation** of the **NEXORA Retail Business Platform**. It translates the approved architectural locks (`09-architecture-lock.md`, `06-go-backend-architecture.md`, `04-domain-security-review.md`, `11-permission-matrix.md`, `12-event-catalog.md`, `13-audit-action-catalog.md`) into an ordered, dependency-aware engineering plan before any Phase 1 code is written.

### 1.2 Scope
Phase 1 implements the core non-functional runtime and platform capabilities required to host future business domains safely:
1. **Application Lifecycle & Dependency Wiring**: Deterministic boot, config validation, DI wiring, and graceful SIGINT/SIGTERM termination for `apps/api` and `apps/worker`.
2. **HTTP Transport & Error Envelope**: Chi router, middleware stack, context propagation, and canonical `ErrorResponse` serialization (no RFC 7807).
3. **Database & Transaction Management**: `pgxpool` management, transaction manager abstraction (`WithinTx`), and transaction-local PostgreSQL Row-Level Security (RLS) context establishment.
4. **Authentication & Authorization**: Principal resolution, token hashing, session management (`identity.session`), RBAC (`identity.permission`), and scope enforcement (`identity.user_scope`).
5. **Database-Backed Idempotency**: Atomic request hashing, replay of cached responses, concurrent in-flight locking, and payload mismatch rejection (`platform.idempotency_key`).
6. **Audit Trail**: Transactional persistence of audit logs (`audit.audit_log`, `audit.data_change`), actor/request correlation, and secret scrubbing.
7. **Transactional Outbox & Worker**: Outbox event insertion inside the business transaction (`integration.outbox_event`), worker polling/claiming loop, exponential backoff retries, and dead-letter handling.
8. **Observability & Health**: Structured `log/slog` logging, correlation IDs (`request_id`, `trace_id`), and robust `/healthz` and `/readyz` probes.

### 1.3 Exclusions
The following areas are **strictly excluded** from Phase 1:
- **Business Domains**: No sales orders, POS sessions, cash registers, pricing calculation, promotion engines, inventory stock ledger mutations, purchasing, loyalty calculations, or finance ledgers.
- **Offline POS Synchronization**: No client-side sync queue, conflict resolution engine, or Dexie IndexedDB sync logic (deferred to offline sync implementation phase).
- **External Third-Party Integrations**: No direct external network calls to payment gateways (Xendit, Midtrans) or ERPs (Odoo, SAP). Only internal ports and mock adapters are configured.
- **Database Schema Alteration**: `db/migrations/000001_baseline_hardened.sql` remains completely frozen and immutable.

### 1.4 Assumptions
1. PostgreSQL 16+ is the sole authoritative system of record.
2. Redis 7+ is used strictly for non-authoritative caching, ephemeral rate-limiting, and worker coordination; it is **never** the source of truth.
3. Every tenant-scoped operation executes within an explicit database transaction that establishes session context via parameterized `SELECT set_config('app.tenant_id', $1, true)`.
4. Network transport delivers messages *at-least-once*; business effect achieves *exactly-once* via database constraints and idempotency keys.

### 1.5 Architecture Dependencies
- `AGENTS.md`: Mandatory domain boundaries and non-negotiable rules.
- `docs/architecture/09-architecture-lock.md`: Locked decisions ADR-0001 through ADR-0015.
- `docs/architecture/04-domain-security-review.md`: P0/P1 security findings.
- `docs/architecture/06-go-backend-architecture.md`: Clean architecture and transaction flow.
- `docs/architecture/11-permission-matrix.md`: RBAC and scope permissions.
- `docs/architecture/12-event-catalog.md`: Outbox event envelope and status lifecycle.
- `docs/architecture/13-audit-action-catalog.md`: Audit action taxonomy and metadata schema.
- `contracts/openapi/openapi-v1.0.0.yaml`: Canonical API contracts and error schemas.
- `db/migrations/000001_baseline_hardened.sql`: System of record database DDL baseline.

---

## 2. Phase 0 Baseline & Immutability Constraints

### 2.1 Current Operational Baseline
Phase 0 successfully bootstrapped:
- Root monorepo Go module (`example.com/nexora`, `go 1.22`).
- Binary entrypoints: `apps/api/cmd/api/main.go` and `apps/worker/cmd/worker/main.go`.
- Backend root bridges: `backend/api.go` and `backend/worker.go` enabling compilation under Go's internal package rules.
- Platform scaffolding: `backend/internal/platform/{config,logger,apperr,transport/http}`.
- POS Web Next.js 14 / TypeScript workspace: `apps/pos-web/`.
- Local Docker environment: `docker-compose.yml` (PostgreSQL 16 Alpine, Redis 7 Alpine).
- Environment templates: `.env.example` adhering to `NEXORA_*` prefixes.
- CI pipeline: `.github/workflows/ci.yml` configuring Go 1.27, `golangci-lint` v2.14.0, and Redocly OpenAPI linting.

### 2.2 Non-Negotiable Constraints for Phase 1
1. **Baseline SQL Immutability**:
   - `db/migrations/000001_baseline_hardened.sql` SHA-256 (corrected in Batch 2 for B1-DISC-01):
     `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`.
   - No lines added, removed, or edited in the baseline SQL file.
2. **Migration Execution Ban in Planning**:
   - Zero migrations executed, seeded, or automatically wired into application boot during this planning task.
3. **API Error Contract Invariance**:
   - All HTTP error responses must strictly conform to `ErrorResponse` (`error.code`, `error.message`, `error.field`, `error.details`, `request_id`). No RFC 7807 structure allowed.
4. **Git Metadata Protection**:
   - No git repository initialization, commit creation, or remote setup without explicit user authorization.

---

## 3. Work Breakdown in Dependency Order

The Phase 1 foundation workstreams are ordered strictly by technical dependency:

```
[Workstream 1: Lifecycle & Config]
               │
               ▼
[Workstream 2: Database Pool, TxManager & RLS Context]
               │
               ├────────────────────────────────────────┬────────────────────────┐
               ▼                                        ▼                        ▼
[Workstream 3: HTTP Transport & Errors]   [Workstream 5: Idempotency]  [Workstream 6: Audit Trail]
               │                                        │                        │
               ▼                                        │                        │
[Workstream 4: AuthN & AuthZ (RBAC/Scope)]              │                        │
               │                                        │                        │
               └───────────────────┬────────────────────┴────────────────────────┘
                                   │
                                   ▼
                    [Workstream 7: Transactional Outbox & Worker]
                                   │
                                   ▼
                    [Workstream 8: Observability & Operational Readiness]
```

---

## 4. Workstream Specifications

### Workstream 1: Application Lifecycle & Dependency Wiring
- **Objective**: Establish deterministic startup, strongly-typed environment validation, dependency injection wiring, and graceful shutdown handling.
- **Dependencies**: None.
- **Proposed Files**:
  - `backend/internal/platform/config/config.go` (expand with validation rules)
  - `backend/internal/platform/lifecycle/app.go` (new: application lifecycle supervisor)
  - `apps/api/cmd/api/main.go` (wire dependencies via lifecycle supervisor)
  - `apps/worker/cmd/worker/main.go` (wire worker lifecycle)
- **Implementation Tasks**:
  1. Add strict config validation returning typed errors for missing credentials, invalid ports, or negative timeouts.
  2. Implement an application container orchestrating dependency boot order: Config -> Logger -> DB Pool -> Redis Client -> Services -> HTTP Server.
  3. Wire signal trap capturing `SIGINT` and `SIGTERM` with configurable context timeout (`NEXORA_HTTP_SHUTDOWN_TIMEOUT`).
  4. Ensure active database connections, HTTP listeners, and worker routines drain cleanly upon shutdown.
- **Security & Failure Scenarios**:
  - Invalid environment values fail fast on startup with human-readable error messages.
  - Shutdown timeout triggers force-kill after configured duration to avoid zombie processes.
- **Testing**:
  - Unit test for config validation with invalid/missing variables.
  - Lifecycle test verifying cleanup functions are executed in reverse order of initialization.
- **Acceptance Criteria**:
  - Applications fail to start if database or Redis configuration is malformed.
  - Receiving `SIGTERM` initiates orderly shutdown within configured timeout.
- **Definition of Done**: Unit tests pass; zero goroutine leaks on shutdown.

---

### Workstream 2: PostgreSQL Connection Pool, Transactions & RLS Integration
- **Objective**: Provide connection pooling, safe transaction boundaries (`WithinTx`), and automatic transaction-local RLS session context using parameterized `SELECT set_config(...)`.
- **Dependencies**: Workstream 1.
- **Proposed Files**:
  - `backend/internal/platform/postgres/pool.go` (new: pgxpool initialization, health probe, and pool metrics)
  - `backend/internal/platform/postgres/tx.go` (new: `TxManager` interface and implementation)
  - `backend/internal/platform/postgres/context.go` (new: SQL session variable builder executing parameterized `set_config`)
  - `backend/internal/platform/postgres/errors.go` (new: pgx error mapping, foreign key / unique violation parsing)
  - `backend/internal/platform/testfixture/` (new: synthetic platform test entities and operations for infrastructure testing without business domain pollution)
- **Implementation Tasks**:
  1. Initialize `pgxpool.Pool` with connection limits, idle timeout, and health verification.
  2. Implement `TxManager` exposing:
     ```go
     type TxManager interface {
         WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error
     }
     ```
  3. Extract security principal from `context.Context` and apply session variables immediately upon transaction start:
     ```sql
     SELECT set_config('app.tenant_id', $1, true);
     SELECT set_config('app.user_id', $2, true);
     SELECT set_config('app.company_id', $3, true);
     SELECT set_config('app.store_id', $4, true);
     SELECT set_config('app.warehouse_id', $5, true);
     SELECT set_config('app.terminal_id', $6, true);
     ```
     *Technical Parameterization Requirement*: PostgreSQL utility statement `SET LOCAL app.tenant_id = $1` does not accept prepared query bind parameters and causes a syntax error. `SELECT set_config($name, $val, true)` is a regular SQL function that accepts bind parameters natively. The third argument (`is_local = true`) guarantees that the configuration setting is strictly transaction-local and automatically reverts upon `COMMIT` or `ROLLBACK`.
  4. Ensure that all tenant-scoped queries within the unit of work execute on the exact same `pgx.Tx` / database connection where `set_config` was invoked.
  5. Enforce fail-closed semantics: If `tenant_id` is missing (nil or zero UUID) in the execution context, `WithinTx` immediately returns `ErrMissingTenantContext` before executing any SQL queries.
  6. Implement safe rollback on panic or error; commit only when `fn` returns nil.
  7. Map PostgreSQL error codes (e.g., `23505` unique violation, `23503` foreign key violation) to domain platform errors.
- **Security & Failure Scenarios**:
  - Pooled connection reuse leakage: Transaction-local `is_local = true` parameters automatically expire upon transaction completion or rollback. When the connection returns to `pgxpool`, no residual tenant context remains.
  - Cross-tenant data isolation: RLS policies in `db/migrations/000001_baseline_hardened.sql` evaluate `tenant_id = platform.current_tenant_id()`. If context is missing, `platform.current_tenant_id()` evaluates to `NULL`, resulting in zero rows returned or write policy violation.
  - Transaction rollback: If an error or panic occurs, all transactional operations roll back cleanly; subsequent queries on a new transaction cannot observe partial writes.
- **Testing**:
  - *Sequential Pooled Connection Test*: Acquire a physical connection from the pool, execute `set_config('app.tenant_id', TenantA, true)`, insert a test fixture row. Commit transaction and release connection to pool. Re-acquire the same physical connection from the pool, execute `set_config('app.tenant_id', TenantB, true)`. Query the test fixture table; assert Tenant B observes 0 rows of Tenant A data.
  - *Concurrent Cross-Tenant Isolation Test*: Spawn 20 concurrent goroutines executing simultaneous read/write operations against a shared 5-connection pool with alternating Tenant A and Tenant B contexts. Assert zero row leakage across tenants under high concurrency.
  - *Rollback State Cleanup Test*: Execute a transaction for Tenant A, insert a test fixture row, and trigger a simulated error/panic. Verify transaction rolls back and subsequent queries observe no inserted data.
  - *Fail-Closed Missing Context Test*: Attempt to execute a tenant-scoped query without providing tenant context; assert operation fails closed with `ErrMissingTenantContext`.
- **Acceptance Criteria**:
  - Transactions establish RLS context via parameterized `SELECT set_config(..., true)`.
  - Pooled connections never retain previous transaction state across concurrent or sequential queries.
  - Missing or invalid tenant context fails closed immediately before executing SQL.
- **Definition of Done**: RLS isolation and pool reuse integration tests pass against PostgreSQL 16 test container.

---

### Workstream 3: HTTP Transport & Canonical Error Handling
- **Objective**: Implement the standard HTTP middleware pipeline, router mounting, and strictly enforced `ErrorResponse` formatting.
- **Dependencies**: Workstream 1.
- **Proposed Files**:
  - `backend/internal/platform/transport/http/server.go` (refine router mounting)
  - `backend/internal/platform/transport/http/middleware/request_id.go` (new)
  - `backend/internal/platform/transport/http/middleware/recovery.go` (new)
  - `backend/internal/platform/transport/http/middleware/logging.go` (new)
  - `backend/internal/platform/transport/http/middleware/timeout.go` (new)
  - `backend/internal/platform/transport/http/responder.go` (new: standardized JSON responses)
  - `backend/internal/platform/apperr/errors.go` (expand error code taxonomy)
- **Implementation Tasks**:
  1. Build chi middleware chain: `RequestID` -> `RealIP` -> `Recovery` -> `StructuredLogger` -> `Timeout`.
  2. Implement panic recovery middleware that catches unhandled panics, logs the stack trace internally, and writes a sanitized HTTP 500 `SERVER_ERROR` envelope with `request_id`.
  3. Implement JSON error responder writing:
     ```json
     {
       "error": {
         "code": "ERROR_CODE",
         "message": "Descriptive message",
         "field": "optional_field",
         "details": {}
       },
       "request_id": "req_uuid"
     }
     ```
  4. Define standard error constructors: `ErrNotFound`, `ErrConflict`, `ErrUnauthorized`, `ErrForbidden`, `ErrValidation`, `ErrIdempotencyConflict`.
- **Security & Failure Scenarios**:
  - Internal database errors, SQL syntax failures, and connection pool timeouts must **never** leak raw SQL or stack traces to the HTTP caller.
  - Client timeout returns HTTP 504 `TIMEOUT_ERROR` with structured envelope.
- **Testing**:
  - Test panic recovery produces valid `ErrorResponse` and HTTP 500 status.
  - Test request timeout produces HTTP 504 and cancels downstream context.
  - Validate response payloads against OpenAPI `ErrorResponse` schema.
- **Acceptance Criteria**:
  - 100% of non-2xx responses serialize to `ErrorResponse`.
  - Stack traces and internal credentials are never exposed in responses.
- **Definition of Done**: Unit tests for all error constructors and middleware pass.

---

### Workstream 4: Authentication & Authorization Foundation
- **Objective**: Implement token verification, session tracking (`identity.session`), RBAC checks (`identity.permission`), and scope authorization (`identity.user_scope`).
- **Dependencies**: Workstreams 2 and 3.
- **Architectural Token Selection**:
  - **Selected Model**: **Opaque Bearer Session Tokens** (`nex_at_<random_bytes>`) backed by PostgreSQL `identity.session`.
  - **Verification Mechanism**: The raw token is hashed with SHA-256 (`token_hash`) and matched against `identity.session`. A session is valid if and only if `revoked_at IS NULL` and `expires_at > now()`. This ensures immediate, zero-lag revocation upon logout or security compromise without relying on volatile distributed cache states.
  - **JWT Distinction**: Purely stateless JWTs are explicitly **not** the foundation access token model. Because stateless JWTs cannot be revoked immediately without an authoritative central blacklist (violating the architectural principle that Redis is not the source of truth), JWT access tokens are deferred. Any future adoption of JWTs requires an approved ADR detailing signature algorithms (e.g. Ed25519) and revocation synchronization.
- **Proposed Files**:
  - `backend/internal/platform/auth/principal.go` (new: typed authenticated principal context)
  - `backend/internal/platform/auth/token.go` (new: high-entropy token generator and SHA-256 hasher)
  - `backend/internal/platform/auth/session_store.go` (new: PostgreSQL repository for `identity.session`)
  - `backend/internal/platform/auth/authorizer.go` (new: RBAC permission and scope evaluator)
  - `backend/internal/platform/transport/http/middleware/auth.go` (new: AuthN/AuthZ middleware)
- **Implementation Tasks**:
  1. Implement `AuthenticatedPrincipal` struct carrying: `TenantID`, `UserID`, `CompanyID`, `StoreID`, `WarehouseID`, `TerminalID`, `Roles`, `Permissions`, `Scopes`.
  2. Implement token authentication middleware:
     - Extracts `Bearer <token>` from `Authorization` header.
     - Computes SHA-256 hash of token and checks `identity.session` for active, non-expired, non-revoked session.
     - Resolves user roles and permissions from `identity.user_role` and `identity.role_permission`.
     - Resolves user scopes from `identity.user_scope`.
     - Injects principal into `context.Context`.
  3. Implement authorization helper: `RequirePermission(perm string)` and `RequireScope(scopeType string, id uuid.UUID)`.
  4. Implement Segregation of Duties (SoD) hook: Verify actor cannot approve their own submitted entities.
  5. Implement refresh-token lifecycle specifications:
     - Rotation: Exchanging a refresh token issues a new token pair and revokes the old token.
     - Reuse Detection: Attempting to use an already-revoked refresh token indicates theft; immediately invalidates all active sessions belonging to that user's session family and writes a high-priority audit security event.
- **Security & Failure Scenarios**:
  - Client cannot forge or override `tenant_id`, `user_id`, or `permissions` via request body or query params.
  - Revoked sessions (`revoked_at IS NOT NULL`) return HTTP 401 `SESSION_REVOKED` immediately.
  - Expired tokens return HTTP 401 `TOKEN_EXPIRED`.
  - Missing permissions return HTTP 403 `FORBIDDEN` with `required_permission` detail.
  - Scope mismatch (e.g. store manager attempting to access another store) returns HTTP 403 `SCOPE_ACCESS_DENIED`.
  - Refresh-token reuse returns HTTP 401 `TOKEN_REUSED` and revokes all active sibling sessions.
- **Testing (Explicit Test Cases)**:
  - *Token Expiration Test*: Request with token where `expires_at < now()` returns HTTP 401 `TOKEN_EXPIRED`.
  - *Session Revocation Test*: User calls logout or admin revokes session (`revoked_at = now()`); subsequent request returns HTTP 401 `SESSION_REVOKED` with zero delay.
  - *Refresh-Token Rotation Test*: Refresh request issues new token pair and marks prior token revoked.
  - *Refresh-Token Reuse Detection Test*: Re-submitting an already rotated refresh token fails with HTTP 401 `TOKEN_REUSED` and triggers session family revocation.
  - *Invalid Scope Test*: User with Store A scope attempts to query Store B resource; returns HTTP 403 `SCOPE_ACCESS_DENIED`.
  - *Permission Denial Test*: User with `order:read` attempts `order:cancel`; returns HTTP 403 `FORBIDDEN` with `required_permission: "order:cancel"`.
- **Acceptance Criteria**:
  - Unauthenticated or malformed requests rejected with HTTP 401.
  - Revoked or expired sessions rejected with HTTP 401.
  - Requests exceeding role or scope boundaries rejected with HTTP 403.
- **Definition of Done**: RBAC, scope authorization, expiration, and revocation integration tests pass.

---

### Workstream 5: Database-Backed Idempotency Engine
- **Objective**: Ensure critical command endpoints guarantee exactly-once business effects using `platform.idempotency_key`.
- **Dependencies**: Workstreams 2, 3, and 4.
- **Proposed Files**:
  - `backend/internal/platform/idempotency/store.go` (new: repository for `platform.idempotency_key`)
  - `backend/internal/platform/idempotency/hasher.go` (new: SHA-256 request payload hasher)
  - `backend/internal/platform/idempotency/middleware.go` (new: HTTP idempotency middleware)
- **Implementation Tasks**:
  1. Implement `IdempotencyStore` operating on `platform.idempotency_key`.
  2. Calculate deterministic SHA-256 hash of request payload (`request_hash`).
  3. On incoming request with `Idempotency-Key` header:
     - Check if key exists for `(tenant_id, key, operation)`.
     - **Replay Match**: If key exists and `request_hash` matches:
       - If completed: return cached HTTP status and response body immediately (header `X-Cache: HIT`).
       - If in-flight: return HTTP 409 `IDEMPOTENCY_IN_PROGRESS` or wait on lock.
     - **Payload Mismatch**: If key exists but `request_hash` differs: return HTTP 409 `IDEMPOTENCY_KEY_REUSED`.
     - **New Request**: Insert record with status `IN_PROGRESS` or acquire advisory row lock.
  4. Upon transaction success, update record with HTTP status and response body.
  5. Provide configurable TTL (`expires_at`, default 24h).
- **Security & Failure Scenarios**:
  - Concurrent requests with identical key: Database unique constraint on `(tenant_id, key, operation)` guarantees only one thread proceeds; concurrent requests receive `409 IDEMPOTENCY_IN_PROGRESS`.
  - Server restart: Pending or committed idempotency records survive in PostgreSQL.
- **Testing (Explicit Test Cases & Measurable Criteria)**:
  - *Concurrent Idempotency Requests*: 20 simultaneous goroutines firing the exact same `(tenant_id, Idempotency-Key, operation)` with payload P. Exactly 1 goroutine executes the operation and commits; 19 receive either 409 `IDEMPOTENCY_IN_PROGRESS` or the cached completed response (200/201 with `X-Cache: HIT`). Exactly 1 row in `platform.idempotency_key` and 1 execution of the underlying handler. Zero database constraint violation errors (`23505`) leaked to callers.
  - *Same Key with Different Payload*: Request 1 with Key K and body `{"qty": 5}` returns 200. Request 2 with Key K and body `{"qty": 10}` returns HTTP 409 `IDEMPOTENCY_KEY_REUSED` with error details explaining hash mismatch. No handler execution occurs on Request 2.
  - *Retry After Process Restart*: Complete cold restart of the API daemon after Request 1 succeeds. Re-sending Request 1 with Key K and original payload returns the cached response with `X-Cache: HIT` and identical payload/status from PostgreSQL.
  - *Transaction Rollback Before Commit*: Handler encounters an unhandled panic or business error midway through `WithinTx`. Transaction rolls back: idempotency record is NOT committed in `COMPLETED` state; subsequent request with same key is permitted to re-attempt.
- **Acceptance Criteria**:
  - Same key + same payload = same cached result.
  - Same key + different payload = HTTP 409 conflict.
  - Idempotency state survives application process restart.
- **Definition of Done**: Concurrency, mismatch, restart, and rollback tests pass against PostgreSQL.

---

### Workstream 6: Audit Trail & Data Change Tracking
- **Objective**: Implement immutable audit logging (`audit.audit_log` and `audit.data_change`) committing atomically with business operations.
- **Dependencies**: Workstreams 2, 3, and 4.
- **Proposed Files**:
  - `backend/internal/platform/audit/writer.go` (new: audit logging port and repository)
  - `backend/internal/platform/audit/scrubber.go` (new: sensitive data / PII redaction engine)
  - `backend/internal/platform/audit/context.go` (new: audit metadata extractor)
- **Implementation Tasks**:
  1. Implement `AuditWriter` interface:
     ```go
     type AuditWriter interface {
         Record(ctx context.Context, entry AuditEntry) error
     }
     ```
  2. Implement PostgreSQL repository inserting into `audit.audit_log`:
     - Fields: `tenant_id`, `company_id`, `store_id`, `terminal_id`, `user_id`, `module`, `action`, `resource_type`, `resource_id`, `business_number`, `status`, `request_id`, `trace_id`, `ip_address`, `user_agent`, `reason`, `metadata`.
  3. Implement field-level delta recorder inserting into `audit.data_change` (`table_schema`, `table_name`, `row_id`, `field_name`, `old_value`, `new_value`).
  4. Implement data scrubber masking passwords, MFA tokens, API keys, CVV, card numbers, and bearer tokens.
- **Security & Failure Scenarios**:
  - Immutability enforcement: Application database user must have `INSERT` and `SELECT` only on `audit.*`; `UPDATE` and `DELETE` must be denied by PostgreSQL engine privilege grants.
  - Failure atomicity: If writing audit log fails within a business transaction, the entire business transaction rolls back.
- **Testing (Explicit Test Cases & Measurable Criteria)**:
  - *Atomicity of Data Changes, Audit, Outbox, and Idempotency*: Handled inside `WithinTx` on synthetic platform test fixture. If any write fails (e.g. audit write fails or outbox fails), all mutations roll back together: 0 rows in test entity, 0 rows in `audit.audit_log`, 0 rows in `audit.data_change`, 0 rows in `integration.outbox_event`, and 0 rows in `platform.idempotency_key`.
  - *Audit Immutability*: Integration test attempts `UPDATE audit.audit_log` and `DELETE FROM audit.audit_log` as the application role; PostgreSQL engine rejects both with error code `42501` (insufficient privilege).
  - *Sensitive-Data Redaction*: Payloads containing `password`, `token`, `mfa_code`, `cvv`, `pan`, `secret` are scrubbed before persistence in `audit.audit_log.metadata` and `audit.data_change.new_value`. Assert string `"[REDACTED]"` replaces the sensitive values in database records.
- **Acceptance Criteria**:
  - Critical actions produce immutable audit entries with actor and scope metadata.
  - No secrets or sensitive payment data logged in metadata.
  - Audit records commit atomically with business operations.
- **Definition of Done**: Immutability and scrubber unit/integration tests pass.

---

### Workstream 7: Transactional Outbox & Background Worker Processing
- **Objective**: Provide atomic outbox event publishing (`integration.outbox_event`) and asynchronous delivery worker (`apps/worker`) with at-least-once delivery semantics.
- **Dependencies**: Workstreams 1, 2, and 6.
- **Proposed Files**:
  - `backend/internal/platform/outbox/writer.go` (new: outbox insertion port)
  - `backend/internal/platform/outbox/event.go` (new: versioned event envelope model)
  - `backend/internal/platform/outbox/poller.go` (new: worker claiming and delivery loop)
  - `backend/internal/platform/outbox/dispatcher.go` (new: internal event consumer router)
  - `apps/worker/cmd/worker/main.go` (wire outbox poller daemon)
- **Implementation Tasks**:
  1. Implement `OutboxWriter` inserting into `integration.outbox_event` inside the active business transaction:
     - Fields: `id`, `tenant_id`, `aggregate_type`, `aggregate_id`, `event_type`, `event_version`, `payload`, `status` (`PENDING`), `occurred_at`.
  2. Implement polling consumer in `apps/worker`:
     - Select pending events using `SELECT ... FOR UPDATE SKIP LOCKED` in batches (e.g. 50 items).
     - Update status to `PUBLISHED` upon successful dispatch.
  3. Implement delivery attempt recorder (`integration.delivery_attempt`).
  4. Implement retry mechanism with exponential backoff (`retry_count`, `next_attempt_at`).
  5. Transition events to `DEAD_LETTER` after maximum retries (default 5 attempts).
  6. Ensure internal consumer handlers are idempotent.
- **Security & Failure Scenarios**:
  - Crash after commit but before dispatch: Worker claims event on next polling cycle; no data lost.
  - Consumer timeout: Worker catches context cancellation and records failure attempt.
  - Malformed payload: Moves to `DEAD_LETTER` without blocking subsequent events.
- **Testing (Explicit Test Cases & Measurable Criteria)**:
  - *Outbox Duplicate Delivery & Consumer Idempotency*: Worker dispatches event E to consumer twice; consumer deduplication logic / idempotency check ensures underlying effect executes strictly once.
  - *Retry & Exponential Backoff*: Handler fails with transient network error. Worker records `integration.delivery_attempt`, increments `retry_count` (e.g. 1 -> 2), and schedules `next_attempt_at` with exponential backoff ($2^n \times \text{base}$).
  - *Worker Crash & Restart*: Send `SIGKILL` to worker daemon while processing in-flight events. Start a new worker daemon instance; events left in `PENDING` status are claimed via `SELECT ... FOR UPDATE SKIP LOCKED` and processed without data loss or stuck locks.
  - *Dead-Letter Behavior*: After 5 consecutive failed delivery attempts, event status transitions to `DEAD_LETTER`; worker ceases polling it and logs a high-severity alert.
- **Acceptance Criteria**:
  - Events committed atomically with database changes.
  - Worker processes pending events asynchronously with `SKIP LOCKED`.
  - Failed events retry up to threshold before entering dead-letter queue.
- **Definition of Done**: Outbox atomicity and dead-letter worker tests pass.

---

### Workstream 8: Observability, Metrics & Operational Readiness
- **Objective**: Ensure comprehensive structured logging, correlation propagation, and operational health probes.
- **Dependencies**: Workstreams 1, 2, and 3.
- **Proposed Files**:
  - `backend/internal/platform/observability/metrics.go` (new: Prometheus/OpenTelemetry metric counters)
  - `backend/internal/platform/observability/tracing.go` (new: W3C TraceContext propagation)
  - `backend/internal/platform/transport/http/server.go` (refine `/healthz` and `/readyz`)
- **Implementation Tasks**:
  1. Standardize log fields on `log/slog`: `request_id`, `trace_id`, `tenant_id`, `user_id`, `duration_ms`, `http_status`.
  2. Implement Prometheus metrics endpoint (`GET /metrics`):
     - `http_requests_total` (method, path, status)
     - `http_request_duration_seconds` (histogram)
     - `db_pool_conns` (total, idle, acquired)
     - `outbox_events_total` (status, event_type)
  3. Refine `/readyz` probe to perform deep dependency health checks (PostgreSQL pool ping, Redis ping, outbox backlog threshold).
- **Security & Failure Scenarios**:
  - `/metrics` restricted to internal network / management port.
  - High DB latency reflects in `/readyz` as degraded/failing.
- **Testing**:
  - Unit test verifying log attributes contain `request_id` and `trace_id`.
  - Health check test asserting `/readyz` returns 503 when PostgreSQL pool is closed.
- **Acceptance Criteria**:
  - Structured logs consistently carry correlation IDs.
  - `/healthz` and `/readyz` accurately reflect daemon status.
- **Definition of Done**: Observability test suite passes.

---

## 5. Database & Migration Strategy

### 5.1 System of Record Separation
- **Baseline Schema**: `db/migrations/000001_baseline_hardened.sql` is the frozen foundational schema (1,563 lines, approved Batch 2 SHA-256 `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`).
- **Phase 1 Database Scope**:
  - Phase 1 tests execute directly against the baseline schema loaded into a disposable PostgreSQL test instance.
  - **Zero migrations executed in Phase 1 planning**.
  - Any future schema additions (e.g. Phase 2+) will be authored as discrete, sequential files (`db/migrations/000002_*.sql`) adhering to strict expand/contract rules.

### 5.2 RLS Session Parameters
The following transaction-local session variables are established by `TxManager` using parameterized function calls inside an explicit transaction, and consumed by RLS policies:
```sql
SELECT set_config('app.tenant_id', $1, true);    -- Current tenant UUID (mandatory for tenant isolation)
SELECT set_config('app.user_id', $2, true);      -- Current actor UUID
SELECT set_config('app.company_id', $3, true);   -- Current company boundary
SELECT set_config('app.store_id', $4, true);     -- Current store boundary
SELECT set_config('app.warehouse_id', $5, true); -- Current warehouse boundary
SELECT set_config('app.terminal_id', $6, true);  -- Current terminal device
```

*Parameterization Mechanics*:
- PostgreSQL `SET LOCAL app.tenant_id = $1` is a utility statement that rejects prepared query parameters with a syntax error.
- `SELECT set_config($name, $val, true)` executes as a parameterized query with safe bind values.
- Setting `is_local = true` binds configuration parameters exclusively to the current transaction. When the transaction finishes (`COMMIT` or `ROLLBACK`), the connection automatically reverts all variables to default, preventing cross-tenant leakage across pooled connections.

---

## 6. API Error Contract Consistency

Every error returned by `apps/api` must serialize through `backend/internal/platform/apperr.RespondError` conforming to `ErrorResponse`:

```json
{
  "error": {
    "code": "STRING_ERROR_CODE",
    "message": "Human readable error description",
    "field": "optional_violating_field",
    "details": {
      "key": "value"
    }
  },
  "request_id": "req_01HVABCXYZ12345"
}
```

### Prohibited Patterns
- Prohibited: Returning RFC 7807 (`type`, `title`, `status`).
- Prohibited: Returning unstructured plain text or HTML error pages.
- Prohibited: Leaking Go stack traces or PostgreSQL syntax errors.

---

## 7. Tenant Isolation & RLS Verification Strategy

### Isolation Mechanism & Parameterized Setup
1. **Application Boundary**: Authentication middleware extracts `tenant_id` from the authenticated session and injects it into Go `context.Context`.
2. **Database Transaction Boundary**: `TxManager.WithinTx` opens a transaction on a dedicated connection acquired from `pgxpool.Pool` and immediately invokes:
   ```sql
   SELECT set_config('app.tenant_id', $1, true);
   ```
   If additional scopes exist on the principal, it also executes:
   ```sql
   SELECT set_config('app.user_id', $2, true);
   SELECT set_config('app.company_id', $3, true);
   SELECT set_config('app.store_id', $4, true);
   SELECT set_config('app.warehouse_id', $5, true);
   SELECT set_config('app.terminal_id', $6, true);
   ```
3. **Connection Isolation & Leak Prevention**:
   - The third parameter `true` sets `is_local = true`, binding settings strictly to the current transaction.
   - When the transaction finishes (commit or rollback), PostgreSQL automatically discards the setting.
   - The connection returned to the pool is completely sanitized without residual session context.
   - All subsequent queries in the block MUST execute on this exact `pgx.Tx` / connection handle.
4. **Fail-Closed Policy**:
   - If `tenant_id` is missing (nil or zero UUID) in the execution context, `TxManager` immediately halts and returns `ErrMissingTenantContext` before executing any SQL.
   - If an un-scoped query runs against an RLS-enforced table without context, RLS policies evaluate `current_tenant_id()` to `NULL`, resulting in zero rows returned or write policy violation.
5. **Database Engine Enforcement**: PostgreSQL RLS policies enforce `tenant_id = platform.current_tenant_id()` on all tenant-scoped tables with `FORCE ROW LEVEL SECURITY`.

### Verification Test Suite (Platform Test Fixture)
Tests execute against `backend/internal/platform/testfixture/` using synthetic test tables without introducing business domains:
- **Cross-Tenant Read Isolation**: Insert fixture row for Tenant 1. In a separate transaction, set context for Tenant 2 via `SELECT set_config('app.tenant_id', Tenant2, true)`. Query fixture table; assert 0 rows returned.
- **Cross-Tenant Write Rejection**: With context set to Tenant 1, attempt to insert a fixture row with Tenant 2's `tenant_id`. Assert PostgreSQL RLS policy rejects the write with error code `42501` / policy violation.
- **Sequential Pooled Connection Reuse**:
  1. Acquire physical connection from pool.
  2. Set context to Tenant 1, insert row, commit.
  3. Release connection to pool.
  4. Acquire same connection, set context to Tenant 2.
  5. Query table; assert Tenant 2 sees 0 rows of Tenant 1 data.
- **Concurrent Multi-Tenant Pool Contention**:
  - Run 20 concurrent goroutines querying across 5 pooled connections with alternating Tenant 1 and Tenant 2 contexts.
  - Assert zero cross-tenant row leakage or corrupted session variables under maximum concurrency.
- **Rollback State Cleanup**:
  - Open transaction, set Tenant 1 context, insert test row, force error/rollback.
  - Acquire connection without setting context; verify no row exists and `current_setting('app.tenant_id', true)` is empty/null.

---

## 8. Authentication Token Lifecycle & Revocation

### 8.1 Selected Token Architecture
- **Authoritative Model**: **Opaque Bearer Session Tokens** backed by PostgreSQL `identity.session`.
- **Token Format**: High-entropy cryptographically secure random string (256-bit entropy, prefixed `nex_at_...`).
- **Storage & Hashing**: Tokens are **never** stored in plaintext. The raw token is hashed via SHA-256 (`token_hash = sha256(raw_token)`).
- **Session Lookup & Instant Revocation**:
  - Every authenticated request queries `identity.session` by `token_hash`.
  - Condition for validity: `revoked_at IS NULL AND expires_at > now()`.
  - Logout or administrative revocation sets `revoked_at = now()`.
  - Instant effect: Revocation has 0-second latency. No distributed cache propagation or stale token window.
- **Explicit JWT Distinction**:
  - Purely stateless JWTs are rejected for the foundation because they cannot provide immediate revocation without an authoritative central blacklist (which would violate the rule that Redis is not the system of record).
  - Any future adoption of JWTs (e.g. for edge POS terminals or microservices) is deferred to Phase 2+ and requires an approved Architecture Decision Record (ADR).

### 8.2 Refresh-Token Lifecycle & Replay Defense
- **Refresh Token Format**: High-entropy opaque token (prefixed `nex_rt_...`), stored as SHA-256 hash.
- **Refresh-Token Rotation (RTR)**:
  - Every refresh token exchange invalidates the supplied refresh token (`revoked_at = now()`) and issues a newly generated access token and refresh token pair.
- **Reuse Detection (Theft / Replay Defense)**:
  - If a refresh token that was already marked as revoked is submitted for exchange, the server detects token reuse / credential compromise.
  - Mitigation: Immediately invalidates all active sessions belonging to the user's session family, logs a high-severity security audit event (`action = 'SECURITY_ALERT_TOKEN_REUSE'`), and returns HTTP 401 `TOKEN_REUSED`.

### 8.3 Unresolved Security Decisions Requiring Approval
1. **Refresh Token Schema Storage**: Baseline schema `identity.session` currently contains a single `token_hash` column and no `refresh_token_hash` column. Whether refresh tokens are tracked via a dedicated future table (`identity.refresh_token`), session family relations, or sliding expiration on `identity.session` remains an open architecture decision requiring approval before implementing refresh endpoints.
2. **Asymmetric vs Symmetric JWT Specification**: If JWT access tokens are approved for future edge/offline use, deciding between EdDSA (Ed25519) vs ECDSA (ES256) and public JWKS distribution (`/.well-known/jwks.json`) is deferred.
3. **MFA Step-Up State Representation**: How partial/unverified sessions (pre-TOTP/WebAuthn) are isolated from fully authenticated sessions in `identity.session` without altering the immutable baseline SQL schema.

### 8.4 Explicit Test Suite
- **Token Expiration**: Request with expired token (`expires_at < now()`) returns HTTP 401 `TOKEN_EXPIRED`.
- **Immediate Revocation**: Logout sets `revoked_at = now()`; subsequent request with same token returns HTTP 401 `SESSION_REVOKED`.
- **Refresh-Token Rotation**: Refresh request successfully rotates token and invalidates the previous token.
- **Reuse Detection**: Presenting an already-revoked refresh token triggers session family revocation and security audit logging.
- **Invalid Scope Access**: Authenticated principal with Store 1 scope attempting to read Store 2 data returns HTTP 403 `SCOPE_ACCESS_DENIED`.
- **Permission Denial**: Principal missing required permission returns HTTP 403 `FORBIDDEN` with `required_permission` detail.

---

## 9. Idempotency Concurrency & Mismatch Semantics

### State Flow on `POST /commands/*` with `Idempotency-Key`
```
Client Request (Key: K, Payload: P)
  │
  ▼
Compute Hash H = SHA256(P)
  │
  ▼
Query platform.idempotency_key for (tenant_id, K, operation)
  │
  ├─► Not Found ───────► Insert IN_PROGRESS ──► Execute Command ──► Update Result ──► Return 200/201
  │
  ├─► Found + Same Hash (H)
  │     │
  │     ├─► Status = COMPLETED ───► Return Cached Status & Body (X-Cache: HIT)
  │     │
  │     └─► Status = IN_PROGRESS ─► Return HTTP 409 (IDEMPOTENCY_IN_PROGRESS)
  │
  └─► Found + Different Hash ─────► Return HTTP 409 (IDEMPOTENCY_KEY_REUSED)
```

---

## 10. Audit Immutability & Data Redaction

### Immutability Controls
- Database permissions restrict application user to `INSERT` and `SELECT` only on `audit.audit_log` and `audit.data_change`.
- Application layer provides no update or delete ports for audit entries.

### Sensitive Data Scrubbing
The audit scrubber applies recursive JSON masking on the following key patterns:
- `password`, `secret`, `token`, `mfa_code`, `cvv`, `pan`, `card_number`, `auth_header`.
- Replaces values with string `"[REDACTED]"`.

---

## 11. Transactional Outbox Delivery Semantics

### Delivery Guarantee
- **Persistence**: `integration.outbox_event` written in same DB transaction as business mutations.
- **Network Dispatch**: *At-least-once*. Consumers must be idempotent.
- **Status Lifecycle**: `PENDING` -> `PUBLISHED` (or `FAILED` with retry backoff -> `DEAD_LETTER`).

### Polling Algorithm
```sql
SELECT id, event_type, payload
FROM integration.outbox_event
WHERE status IN ('PENDING', 'FAILED')
  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
ORDER BY occurred_at ASC
LIMIT 50
FOR UPDATE SKIP LOCKED;
```

---

## 12. Observability, Health & Lifecycle Management

- **Liveness (`GET /healthz`)**: Returns 200 OK as long as the HTTP listener is operational.
- **Readiness (`GET /readyz`)**: Returns 200 OK only when:
  1. PostgreSQL pool ping succeeds within 2 seconds.
  2. Redis ping succeeds within 2 seconds.
  3. Returns HTTP 503 `SERVICE_UNAVAILABLE` with `ErrorResponse` if any check fails.
- **Graceful Termination**: On `SIGTERM`:
  1. Stop accepting new HTTP requests.
  2. Allow in-flight requests up to `NEXORA_HTTP_SHUTDOWN_TIMEOUT` (10s) to finish.
  3. Stop background outbox poller.
  4. Close database pool and Redis connections.

---

## 13. Local Verification Commands & Testing Strategy

All verification executes locally without requiring remote GitHub Actions:

```bash
# 1. Format & Lint
make fmt
make lint

# 2. Unit Tests
go test -v -race ./backend/... ./apps/api/... ./apps/worker/...

# 3. Integration Tests (Disposable PostgreSQL Container)
docker compose up -d
NEXORA_TEST_DB_URL="postgres://nexora_admin:nexora_secret_dev@localhost:5432/nexora?sslmode=disable" \
go test -v -tags=integration -race ./backend/internal/platform/...

# 4. OpenAPI Contract Lint
npx --yes @redocly/cli lint contracts/openapi/openapi-v1.0.0.yaml

# 5. Build Verification
make build
pnpm --filter @nexora/pos-web build

# 6. Baseline SQL Checksum Check
shasum -a 256 db/migrations/000001_baseline_hardened.sql
# Must output: e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f
```

---

## 14. Ordered Milestones & Phase Gates

### Milestone 1: Platform Core & Database Foundation
- Workstream 1 (Lifecycle) + Workstream 2 (Pool, TxManager, RLS context via `set_config`) + Workstream 3 (HTTP transport, errors).
- **Gate 1.1**:
  - Clean compilation of API and worker skeletons (`make build`).
  - Unit tests for canonical error serialization pass conforming to `ErrorResponse`.
  - Parameterized `SELECT set_config('app.tenant_id', $1, true)` RLS context setting, sequential pool reuse, rollback residue cleanup, and concurrent cross-tenant isolation tests pass against PostgreSQL test container.
  - Missing tenant context fails closed immediately with `ErrMissingTenantContext`.

### Milestone 2: Security & Identity Foundation
- Workstream 4 (AuthN, Session Store, RBAC, Scope Enforcement).
- **Gate 1.2**:
  - Unauthenticated requests rejected with HTTP 401.
  - Token expiration (`expires_at < now()`) returns HTTP 401 `TOKEN_EXPIRED`.
  - Immediate session revocation (`revoked_at IS NOT NULL`) returns HTTP 401 `SESSION_REVOKED` with zero delay.
  - Refresh-token rotation succeeds; presenting a previously revoked refresh token triggers session family revocation.
  - Cross-tenant and cross-store scope checks return HTTP 403 `SCOPE_ACCESS_DENIED`.
  - Missing permissions return HTTP 403 `FORBIDDEN`.

### Milestone 3: Command Safety & Durability
- Workstream 5 (Idempotency Engine) + Workstream 6 (Audit Trail) tested via synthetic platform test fixture (`testfixture`).
- **Gate 1.3**:
  - Concurrent idempotency: 20 simultaneous goroutines with same key execute exactly once; zero duplicate writes, zero database constraint errors (`23505`) leaked.
  - Request payload hash mismatches return HTTP 409 `IDEMPOTENCY_KEY_REUSED`.
  - Cold daemon restart replays cached completed response from PostgreSQL with `X-Cache: HIT`.
  - Rollback before commit leaves zero orphan rows across test entity, audit, outbox, and idempotency tables.
  - Audit logs persist atomically with transactions; scrubbing masks credentials (`"[REDACTED]"`); `UPDATE`/`DELETE` on `audit.*` rejected with code `42501` by PostgreSQL engine.

### Milestone 4: Async Dispatch & Integration Foundation
- Workstream 7 (Transactional Outbox & Worker Poller) + Workstream 8 (Observability).
- **Gate 1.4**:
  - Outbox events persist atomically in database transactions alongside business mutations.
  - Background worker claims events using `SELECT ... FOR UPDATE SKIP LOCKED` without duplicates across multiple worker processes.
  - Worker crash (`SIGKILL`) and restart safely reclaims in-flight `PENDING` events without data loss or stuck locks.
  - Consumer idempotency deduplicates re-delivered events.
  - Retries apply exponential backoff; dead-letter queue catches persistent errors after 5 attempts.

### Final Phase Gate: Gate 1 (Foundation Lock)
- All gates (1.1, 1.2, 1.3, 1.4) passed.
- `make fmt`, `make lint`, `make test`, and `make build` pass with zero errors.
- OpenAPI specification lints with 0 errors via Redocly.
- `db/migrations/000001_baseline_hardened.sql` SHA-256 confirmed unchanged.

---

## 15. Risks, Unresolved Decisions & Open Items

| Item | Description | Mitigation / Architectural Position | Status |
|---|---|---|---|
| **1. Access Token Architecture** | Opaque bearer session token vs stateless JWT access token. | **Resolved for Phase 1**: Opaque session token backed by `identity.session` (`token_hash`) selected as foundation default to guarantee immediate revocation without relying on cache states. JWT model deferred. | Resolved for Foundation |
| **2. Refresh Token Persistence Schema** | Baseline schema `identity.session` currently contains a single `token_hash` column and no `refresh_token_hash` column. | Open architectural decision: Whether refresh tokens will be tracked in a dedicated future table (`identity.refresh_token`), session family relations, or sliding expiration on `identity.session`. Requires schema approval before refresh endpoints are implemented. | Open Decision (Needs Approval) |
| **3. Asymmetric JWT Support** | RS256/EdDSA JWT access tokens for external integrations or edge POS offline validation. | Deferred to Phase 2+. Any adoption requires an approved ADR detailing signature algorithms and revocation synchronization. | Open Decision (Deferred) |
| **4. MFA Step-Up Lifecycle** | How partial/unverified sessions (pre-TOTP/WebAuthn) are isolated in `identity.session`. | Documented requirement: Pre-MFA tokens carry restricted intermediate state; full session issued only upon MFA factor verification. Schema representation details open. | Open Decision (Needs Approval) |
| **5. Sequence Numbering** | Concurrency safety of document number generation (`platform.document_sequence`). | Locked in ADR-0010: Use PostgreSQL sequence / locked row counter; never use `MAX(id)+1`. | Resolved in architecture lock |
| **6. Redis Cluster Mode** | Single-node Redis vs Sentinel / Cluster mode for production. | Phase 1 targets Redis 7 standalone for local development; interface abstracts cluster client. | Open for infrastructure team |

---

## 16. Traceability Matrix

| Architectural Contract | Target Workstream | Implemented Package | Key Verification Test |
|---|---|---|---|
| ADR-0001 (Modular Monolith) | All | `backend/internal/platform/...` | Package boundary linting & zero cross-domain imports |
| ADR-0004 (Tenant Isolation / RLS) | Workstream 2 | `platform/postgres` | Parameterized `set_config` RLS integration test; sequential pool reuse; concurrent isolation test |
| ADR-0005 (Scope Authorization) | Workstream 4 | `platform/auth` | Cross-store scope denial test; RBAC permission check; SoD approval hook |
| ADR-0006 (Idempotency) | Workstream 5 | `platform/idempotency` | Concurrent replay (20 goroutines), 409 mismatch, cold restart replay, and rollback test |
| ADR-0009 (Transactional Outbox) | Workstream 7 | `platform/outbox` | Atomic commit, worker `SKIP LOCKED` polling, crash/restart recovery, retry backoff & dead-letter test |
| ADR-0011 (Audit By Default) | Workstream 6 | `platform/audit` | Atomic audit write, DB engine immutability check (UPDATE/DELETE denied), and PII scrubber test |
| P0-03 (Pooled RLS Context) | Workstream 2 | `platform/postgres` | Sequential pooled connection test & fail-closed missing context test |
| OpenAPI Error Envelope | Workstream 3 | `platform/apperr` | Redocly lint, JSON schema conformance, and panic recovery test |

---

## 17. Proposed Implementation Backlog

| Task ID | Workstream | Summary | Dependencies | Priority | Measurable Acceptance Criteria |
|---|---|---|---|---|---|
| **TASK-P1-001** | WS 1 | Strongly-typed config validation | None | P0 | Rejects missing mandatory DB/Redis env vars on boot |
| **TASK-P1-002** | WS 1 | Application lifecycle & graceful shutdown | TASK-P1-001 | P0 | SIGTERM shuts down HTTP server within 10s |
| **TASK-P1-003** | WS 2 | PostgreSQL connection pool wrapper | TASK-P1-001 | P0 | `pgxpool` connects with connection limits and health probe |
| **TASK-P1-004** | WS 2 | Transaction manager (`WithinTx`) | TASK-P1-003 | P0 | Automatic rollback on panic or returned error |
| **TASK-P1-005** | WS 2 | Transaction-local RLS session context (`set_config`) | TASK-P1-004 | P0 | Parameterized `SELECT set_config('app.tenant_id', $1, true)` executed upon tx start; fails closed on missing context; zero leakage on pool reuse |
| **TASK-P1-006** | WS 3 | Standard HTTP middleware stack | TASK-P1-002 | P0 | RequestID, Recoverer, Logger, Timeout wired in chi |
| **TASK-P1-007** | WS 3 | Canonical NEXORA error responder | TASK-P1-006 | P0 | 100% of errors match `ErrorResponse` envelope |
| **TASK-P1-008** | WS 4 | Authenticated principal context model | TASK-P1-006 | P0 | Context carries typed tenant, user, company, store, warehouse, terminal, and scopes |
| **TASK-P1-009** | WS 4 | Opaque token hashing & session repository | TASK-P1-005, P1-008 | P0 | SHA-256 token hash verified against `identity.session`; instant revocation and rotation reuse detection pass |
| **TASK-P1-010** | WS 4 | RBAC permission & scope authorizer | TASK-P1-009 | P0 | Missing permission returns 403 `FORBIDDEN`; store mismatch returns 403 `SCOPE_ACCESS_DENIED` |
| **TASK-P1-011** | WS 5 | Request payload hashing engine | TASK-P1-007 | P0 | Deterministic SHA-256 computation on JSON bodies |
| **TASK-P1-012** | WS 5 | PostgreSQL idempotency store | TASK-P1-005, P1-011 | P0 | Atomic insertion into `platform.idempotency_key` with advisory lock / in-progress status |
| **TASK-P1-013** | WS 5 | Idempotency replay & conflict middleware | TASK-P1-012 | P0 | 20 concurrent goroutines execute exactly once; returns cached response on replay; returns 409 on body mismatch; survives daemon restart |
| **TASK-P1-014** | WS 6 | Sensitive data scrubbing filter | TASK-P1-001 | P0 | Masks passwords, tokens, and payment credentials with `"[REDACTED]"` |
| **TASK-P1-015** | WS 6 | Transactional audit logger | TASK-P1-005, P1-014 | P0 | Commits `audit.audit_log` and `audit.data_change` atomically with business data; UPDATE/DELETE rejected by DB engine with 42501 |
| **TASK-P1-016** | WS 7 | Outbox event model & writer | TASK-P1-005 | P0 | Inserts `integration.outbox_event` in active transaction; rolls back completely on tx error |
| **TASK-P1-017** | WS 7 | Worker claiming poller (`SKIP LOCKED`) | TASK-P1-016 | P0 | `apps/worker` claims pending events without duplicates across multiple instances |
| **TASK-P1-018** | WS 7 | Exponential backoff, crash recovery & dead-letter queue | TASK-P1-017 | P0 | Survives worker crash/restart; retries 5 times with exponential backoff before moving to `DEAD_LETTER` |
| **TASK-P1-019** | WS 8 | Correlation logging & Prometheus metrics | TASK-P1-006 | P1 | Requests export duration histograms and correlation IDs (`request_id`, `trace_id`) |
| **TASK-P1-020** | WS 8 | Operational `/healthz` & `/readyz` probes | TASK-P1-003, P1-019 | P0 | `/readyz` fails safe on DB/Redis connection disruption |
