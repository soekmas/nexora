# Implementation Status

Date: 2026-10-11

## Architecture

Architecture artifacts through CI/CD are prepared and treated as the current design baseline.

## Coding

Current phase: **Phase 2 — Domain Implementation (Batch 2: Increments 5, 6 & 7 Accepted; Awaiting Increment 8 Inventory Domain Authorization)**

## Completed repository preparation

- [x] Control files: `AGENTS.md`, `IMPLEMENTATION_GUIDE.md`, `TASKS.md`
- [x] Architecture documentation copied into repository
- [x] Contract directories created
- [x] Hardened DB baseline staged
- [x] OpenAPI v1 baseline staged
- [x] Application directories created
- [x] Deployment/test/documentation directories created
- [x] Phase 0 — Repository Bootstrap completed & accepted
- [x] Phase 1 — Workstream 1: Core Lifecycle, Configuration & Dependency Wiring completed & accepted
- [x] Phase 1 — Workstream 2: Database Connection Pool, Transactions & RLS Context completed & accepted
- [x] Phase 1 — Workstream 3: HTTP Transport & Canonical Error Handling completed & accepted
- [x] Phase 1 — Workstream 4: Authentication, Session Management & Scope Authorization completed & accepted
- [x] Phase 1 — Workstream 5: Database-Backed Idempotency Engine completed & accepted
- [x] Phase 1 — Workstream 6: Transactional Outbox Engine completed & accepted
- [x] Phase 1 — Workstream 7: Append-Only Audit Subsystem completed & hardened
- [x] Phase 1 — Workstream 8: Observability & Operational Readiness completed & accepted
- [x] Phase 1 — Remediation Batch 3: Reliability, Tenant Security & Transaction Integrity completed & accepted
- [x] Phase 1 — Remediation Batch 4: Auth RLS Resolver (WS4-RLS-01), CI Baseline Checksum (CI-BASELINE-01), Lint Cache (CI-LINT-01) completed & accepted
- [x] Phase 1 — Final Acceptance Gate (Gate 1): ACCEPTED WITH DOCUMENTED LIMITATIONS
- [x] Phase 2 — Domain Architecture & Implementation Planning completed (`19-phase2-domain-implementation-plan.md`)
- [x] Phase 2 — Batch 1 Pre-Implementation Review & Decisions Approved (`20-phase2-batch1-pre-implementation-review.md`)
- [x] Phase 2 — Batch 1 / Increment 1: Identity Core & Credentials accepted with documented limitations (Migration 000006, Argon2id, lockout, SoD, atomic audit/outbox, forced RLS; MFA foundation deferred; Gate 2 / Batch 1 acceptance)
- [x] Phase 2 — Batch 1 / Increment 2: Organization Hierarchy & Terminal Security accepted with documented limitations (Migration 000008, region RLS, terminal lifecycle DISABLED/RETIRED, device token SHA-256 hash security; POS sessions/cash operations deferred to POS domain; `docs/architecture/23-organization-increment-acceptance-review.md`)
- [x] Phase 2 — Batch 1 / Increment 3: Master Data & Catalog accepted with documented limitations (Migration 000009, DEC-B1-008 through DEC-B1-013, hybrid company ownership, forced RLS, dual-mount canonical & alias routes; authoritative pricing deferred to Pricing domain, physical serial inventory movements deferred to Inventory domain; `docs/architecture/27-master-data-increment-acceptance-review.md`)
- [x] Phase 2 — Batch 1 / Increment 4: Customer Relationship Management (`crm`) accepted with documented limitations (Migration 000007, DEC-B1-014 through DEC-B1-018, collision-resistant customer numbering, partial unique index & atomic default address, forced RLS, dual-mount canonical & alias routes; loyalty points deferred to Batch 4, checkout customer linking deferred to Batch 3, customer-group & lifecycle routes canonical-only; `docs/architecture/31-crm-increment-acceptance-review.md`)
- [x] Phase 2 — Batch 1 Closure Review: CLOSED WITH DOCUMENTED FOLLOW-UPS (`docs/architecture/32-phase2-batch1-closure-review.md`; all four increments formally accepted with documented limitations; administrative status synchronization completed)
- [x] Phase 2 — Batch 2 Pre-Implementation Review & Decisions Approved (`docs/architecture/33-phase2-batch2-pre-implementation-review.md`)
- [x] Phase 2 — Batch 2 / Increment 5: POS Session & Cash Management accepted (`db/migrations/000010_pos_hardening.sql`, terminal exclusivity, blind count confidentiality, supervisor variance approval; `docs/architecture/38-pos-stage2-acceptance-consistency-audit.md`)
- [x] Phase 2 — Batch 2 / Increment 6: Pricing Engine accepted (`db/migrations/000011_pricing_engine.up.sql`, pure rational arithmetic & rounding, 8-tier specificity hierarchy, quantity tiers, price list & rule CRUD, canonical state machine, audit/outbox events, Stage 5 OpenAPI parity; Final Acceptance: ACCEPTED)
- [x] Phase 2 — Batch 2 / Increment 7: Promotion Engine accepted (`db/migrations/000012_promotion_hardening.sql`, deterministic pure evaluator, `big.Rat` exact arithmetic, `BUY_X_GET_Y` reward with `BonusItem` output, immutable approved promotions, approval SoD, atomic audit/outbox events, forced RLS, canonical routes & backward-compatible aliases, Stage 5 OpenAPI parity & contract tests; Final Acceptance: ACCEPTED)

## Intentionally not completed yet

- [ ] Phase 2 — Batch 2: Store Operations, Rules & Inventory remaining increment (Increment 8: `inventory`)
- [ ] Phase 2 — Batch 3: Transaction Execution & Settlement (`sales`, `payment`)
- [ ] Phase 2 — Batch 4: Enterprise Extensions & Back-Office (`purchasing`, `loyalty`, `workflow`, `finance`/`reporting`)

These are intentionally left for Antigravity execution according to `17-foundation-implementation-plan.md` and `TASKS.md`.

## Workstream 5 Details (Database-Backed Idempotency Engine — Remediated & Hardened)

- **Status**: Remediated, Hardened & Verified (Accepted with Findings)
- **Remediations Completed**:
  - **Remediation A (Atomicity & Safe Uncertain Failure Handling)**:
    - Removed unsafe automatic `store.Release` calls on handler panics, 5xx server errors, or context aborts. When an operation outcome is uncertain, the in-progress reservation (`response_status IS NULL`) remains in PostgreSQL, causing client retries to fail closed with HTTP 409 `IDEMPOTENCY_IN_PROGRESS` rather than executing duplicate operations.
    - Deterministic responses (< 500, including 2xx, 3xx, and client 4xx errors) are completed and cached using `context.WithoutCancel` to protect against client disconnection race conditions.
    - Added request context propagation helpers (`WithRecord`, `RecordFromContext`) enabling downstream domain handlers to access the active reservation and participate in transactional completion (`postgres.TxFromContext(ctx)`).
  - **Remediation B (Stale Reservation Cleanup & Fencing)**:
    - Removed in-line deletion of in-progress records from `AcquireOrGet`. An old timestamp alone never authorizes another request to acquire the key or delete the reservation.
    - Added optimistic fencing to `store.Complete`: `WHERE id = $3 AND tenant_id = $4 AND response_status IS NULL`. Verifies `RowsAffected() == 1`; if zero rows are updated, returns `ErrReservationLost`.
  - **Remediation C (Deterministic JSON Fingerprinting via RFC 8785)**:
    - Integrated RFC 8785 (JSON Canonicalization Scheme - JCS) using reference package `github.com/gowebpki/jcs`.
    - Normalizes JSON object keys (UTF-16 code unit ordering, nested objects), numbers (`1`, `1.0`, `1e0` serialize identically), unicode escapes, and whitespace.
    - Preserves array element order.
    - Rejects invalid JSON with `ErrMalformedJSON` (returns HTTP 400 `BAD_REQUEST`).
    - Explicit non-JSON content types use SHA-256 over trimmed raw bytes (`HashRawPayload`).
- **Persistence & Transaction Boundaries**:
  - Table: `platform.idempotency_key` backed by `CONSTRAINT uq_idempotency_tenant_key_op UNIQUE (tenant_id, key, operation)`.
  - `PgxStore` enforces PostgreSQL RLS via `postgres.WithTenantID` and `tm.WithinTx` (`SELECT set_config('app.tenant_id', $1, true)` per transaction).
- **Verification Evidence**:
  - 10 hasher unit tests covering whitespace, key reordering, nested objects, array ordering, numeric equivalences, unicode, invalid JSON, and content types.
  - 8 middleware and integration tests (including fail-closed on 500/panic, 4xx replay, 20 concurrent goroutines racing on the same key, multi-tenant RLS isolation, backdated stale timestamp protection, fencing lost reservation rejection, and process cold-restart replay).
  - All quality gates passed with exit code 0 (`make fmt`, `make lint`, `make test`, `make build`, `go test -race`, Redocly OpenAPI lint, and baseline checksum verification).

## Workstream 6 Details (Transactional Outbox Engine — Remediated & Hardened)

- **Status**: Remediated, Hardened & Verified (Remediation Complete)
- **Remediations Implemented (WS6-R1 through WS6-R7)**:
  - **WS6-R1 — Configurable Retry Backoff (`RESOLVED`)**:
    - Replaced hardcoded `INTERVAL '1 second'` in claim query with parameterized `$3::float8 * power(2, LEAST(e.retry_count, 10)) * interval '1 second'`.
    - Binds `cfg.BaseBackoff.Seconds()` into the claim SQL query.
    - Updated `CalculateBackoff` with safe bounding ($1\text{s} \le \text{BaseBackoff} \le 10\text{m}$, retry cap $\le 10$).
    - Verified via unit test `TestCalculateBackoff` and live PostgreSQL integration test `TestOutbox_Integration_ConfigurableBaseBackoff`.
  - **WS6-R2 — Permanent Versus Transient Errors (`RESOLVED`)**:
    - Introduced `ErrNonRetryable` and `NewNonRetryableError` in `outbox/dispatcher.go`.
    - Handlers can wrap permanent errors via `NewNonRetryableError(err)`.
    - Poller detects `errors.Is(dispatchErr, ErrNonRetryable)` and immediately transitions the event to `DEAD_LETTER` with `response_code = 'PERMANENT_ERROR'` and single attempt logged without burning 5 retry backoff cycles.
    - Unclassified errors preserve normal transient retry and exponential backoff behavior.
    - Verified via unit test `TestNonRetryableError_Wrapping` and live PostgreSQL integration test `TestOutbox_Integration_NonRetryableErrorImmediateDeadLetter`.
  - **WS6-R3 — Event Payload Size Limit (`RESOLVED`)**:
    - Documented and enforced `MaxPayloadSize = 1024 * 1024` (1 MiB default) and `ErrPayloadTooLarge` in `outbox/event.go`.
    - `Validate` rejects payloads $> 1\text{ MiB}$ before database insertion without exposing raw payload bytes in error strings.
    - Batch write atomically rejects the entire batch if any event exceeds the limit.
    - Verified via unit test `TestEvent_ValidationAndEnvelope/payload_size_limit_enforcement` and integration test `TestOutbox_Integration_BatchWriteOversizedPayloadRejection`.
  - **WS6-R4 — Structural Sensitive-Key Validation (`RESOLVED`)**:
    - Replaced raw JSON substring scanning with recursive structural JSON inspection in `outbox/event.go`.
    - Normalizes JSON property keys (lowercase, strips punctuation/separators) and handles Unicode escapes automatically via `json.Unmarshal`.
    - Blocked property names: `password`, `clientsecret`, `accesstoken`, `refreshtoken`, `apikey`, `privatekey`, `authorization`, `cvv`, `creditcard`.
    - Checks property keys only, preventing false positives on ordinary text values containing sensitive words.
    - Verified via unit tests in `TestEvent_ValidationAndEnvelope` (nested objects, arrays, Unicode escapes `\u0070assword`, case variations, and harmless text values).
  - **WS6-R5 — Batch Size and Database Connection Pressure (`RESOLVED`)**:
    - Reduced default `BatchSize` in `DefaultPollerConfig()` from 50 to 5.
    - Bounded in `NewPoller` to $[1, 100]$.
    - Reduces maximum single-batch transaction hold duration from 250s to 25s ($5 \times 5\text{s}$).
    - Verified via unit test `TestPollerConfig_DefaultsAndBounds`.
  - **WS6-R6 — Shutdown and Cancellation Safety (`RESOLVED`)**:
    - Added pre-dispatch cancellation guard (`if txCtx.Err() != nil { break }`) inside the batch dispatch loop.
    - Prevents initiating new external deliveries when cancellation is signaled.
    - Uncommitted events roll back cleanly to `PENDING` upon cancellation and are recovered on clean restart.
    - Verified via live integration test `TestOutbox_Integration_CancellationSafetyAndRecovery`.
  - **WS6-R7 — Delivery Attempt Query Performance (`DEFERRED`)**:
    - The baseline table `delivery_attempt` has `UNIQUE (event_id, target_system, attempt_no)` where `event_id` is the leading column, allowing an index range scan followed by attempt sorting.
    - To preserve the locked baseline migration (`000001_baseline_hardened.sql`), 	- Performance optimization via composite index `CREATE INDEX ix_delivery_attempt_event_attempt ON integration.delivery_attempt(event_id, attempt_no DESC);` is deferred to a future governance-approved forward migration.

## Workstream 7 Details (Append-Only Audit Subsystem)

- **Status**: Accepted with Findings (Targeted Read-Only Review Completed)
- **Components Implemented**:
  - `backend/internal/platform/audit/catalog.go`: Approved action catalog registry, severity classifications, mandatory reason constraints, and action validation. Reconciles action discrepancies between `13-audit-action-catalog.md` and baseline migration seed rows (`PRICE_CHANGED`, `PAYMENT_SUCCESS`, `USER_ROLE_CHANGED`, `SALE_REFUNDED`, `PAYMENT_REFUNDED`, `STOCK_TRANSFERRED`).
  - `backend/internal/platform/audit/errors.go`: Typed audit errors (`ErrNoActiveTransaction`, `ErrInvalidAction`, `ErrReasonRequired`, `ErrMissingTenantContext`, `ErrTenantMismatch`, `ErrInvalidStatus`, `ErrInvalidSeverity`, `ErrInvalidMetadata`, `ErrMetadataTooLarge`, `ErrSensitiveDataDetected`, `ErrInvalidPagination`, `ErrFieldLengthExceeded`).
  - `backend/internal/platform/audit/sanitizer.go`: Recursive structural JSON metadata scrubber with 256 KiB size enforcement (`MaxMetadataSize = 256 * 1024`). Masks sensitive credentials (`password`, `secret`, `token`, `mfa_code`, `cvv`, `pan`, `card_number`, `auth_header`, `apikey`, `privatekey`, etc.) with `"[REDACTED]"`.
  - `backend/internal/platform/audit/model.go`: Domain models (`Log`, `DataChange`, `SecurityEvent`, `LoginHistory`, `Filter`) and field/constraint validation. Normalizes status (`SUCCESS`, `FAILED`, `DENIED`), maps `FAILURE` to `FAILED` for catalog compatibility, and enforces reason requirements.
  - `backend/internal/platform/audit/store.go`: Append-only `PgxStore` implementation. Enforces active transaction requirement (`postgres.TxFromContext(ctx)`), ambient tenant identity matching, RLS session context (`SELECT set_config('app.tenant_id', ...)`), standalone managed transactions (`RecordStandalone`), security events (`RecordSecurityEvent`), login history (`RecordLoginHistory`), bounded pagination (`FindAuditLogs`), and tenant-isolated data change queries (`GetDataChanges`). Strictly exposes no update or delete operations.
  - `backend/internal/platform/testfixture/fixture.go`: Test fixture initialization for `audit` schema, tables (`audit.audit_log`, `audit.data_change`, `audit.security_event`, `audit.login_history`, `audit.audit_action_catalog`), RLS policy `tenant_audit_isolation`, and database-level immutability triggers (`trg_audit_log_immutable`, `trg_audit_data_change_immutable`, `trg_security_event_immutable`).
- **Verification Evidence**:
  - 8 unit tests in `catalog_test.go`, `sanitizer_test.go`, `model_test.go` covering approved actions, aliases, invalid actions, recursive sensitive key scrubbing, nested arrays/objects, unicode escaping, harmless text preservation, empty JSON handling, payload size limits, and constraint validation.
  - 8 PostgreSQL live integration tests in `audit_integration_test.go`:
    - `TestAudit_Integration_AppendOnlyAndImmutability`: Insertion and database-level trigger immutability (`platform.prevent_mutation()` rejecting `UPDATE` and `DELETE` on `audit.audit_log` and `audit.data_change`).
    - `TestAudit_Integration_TenantIsolationAndRLS`: Multi-tenant RLS isolation with `nexora_app` (NOBYPASSRLS); Tenant B cannot read Tenant A records or data changes; cross-tenant write forging rejected by both application and PostgreSQL RLS.
    - `TestAudit_Integration_TransactionAtomicityAndRollback`: Business write and audit write atomic commit and rollback verification; failure to supply transaction context fails closed with `ErrNoActiveTransaction`.
    - `TestAudit_Integration_SensitiveMetadataScrubbing`: Verification that raw credentials are sanitized to `"[REDACTED]"` in PostgreSQL storage.
    - `TestAudit_Integration_ConcurrentMultiTenantWrites`: 20 concurrent goroutines with alternating tenant contexts under pool contention with zero cross-tenant contamination.
    - `TestAudit_Integration_StandaloneAndSecurityEvents`: Dedicated transaction execution (`RecordStandalone`), security telemetry (`RecordSecurityEvent`), and login history (`RecordLoginHistory`).
    - `TestAudit_Integration_FilteringAndPagination`: Filtering by action and module; deterministic ordering (`created_at DESC, id DESC`), bounded pagination (limit capped to 100).
    - `TestAudit_Integration_ValidationFailures`: Rejection of unknown actions, missing reason on mandatory actions, and invalid status values.

## Workstream 8 Details (Observability & Operational Readiness)

- **Status**: Completed & Verified (Accepted)
- **Components Implemented**:
  - `backend/internal/platform/logger/logger.go`: Production-grade structured `log/slog` logger with `RedactAttr` attribute replacement handler. Automatically scrubs sensitive keys (`authorization`, `token`, `password`, `secret`, `api_key`, `cvv`, `pan`, `cookie`, `jwt`, etc.) with `"[REDACTED]"`. Implements regex pattern masking for `Bearer <token>` and `Basic <token>` authorization headers, and masks database DSN credentials (`postgres://user:[REDACTED]@host...`).
  - `backend/internal/platform/transport/http/middleware/logging.go`: Enhanced HTTP request logging middleware. Logs parameterized route templates (`rctx.RoutePattern()`) rather than raw URLs to prevent high-entropy cardinality explosion and sensitive URL parameter leakage.
  - `backend/internal/platform/observability/metrics.go`: Thread-safe, bounded-cardinality in-memory metrics engine exposing standard Prometheus v0.0.4 text format (`text/plain; version=0.0.4; charset=utf-8`) at `GET /metrics`. Implements bounded label normalizers for HTTP methods (`NormalizeMethod`), route templates (`NormalizeRoute`), status classes (`NormalizeStatusClass`), and worker outcomes (`NormalizeWorkerStatus`). Rejects arbitrary IDs, tokens, or raw paths from metric labels.
  - `backend/internal/platform/observability/middleware.go`: `HTTPMetricsMiddleware` chi middleware tracking in-flight requests gauge (`http_requests_in_flight`), total request counters (`http_requests_total`), and duration histograms (`http_request_duration_seconds`) across 11 standard Prometheus latency buckets.
  - `backend/internal/platform/observability/tracing.go`: W3C TraceContext (`traceparent`) parsing, formatting, and context propagation helpers linking HTTP correlation IDs with distributed trace contexts.
  - `backend/internal/platform/transport/http/server.go`: Decoupled health probes:
    - `GET /healthz` (Liveness): Returns HTTP 200 OK without pinging downstream dependencies, preventing Kubernetes container restart cascades during transient dependency outages.
    - `GET /readyz` (Readiness): Performs bounded (3-second timeout) checks on PostgreSQL connection pool and Redis. On failure, returns HTTP 503 `SERVICE_UNAVAILABLE` adhering to the canonical `ErrorResponse` envelope without exposing internal connection strings, credentials, or stack traces.
    - `GET /metrics`: Serves Prometheus metrics exposition.
  - `backend/internal/platform/outbox/poller.go`: Integrated `MetricsRecorder` interface and `PollerStatus` diagnostic inspection. Tracks outbox poll cycles (`worker_poll_cycles_total`), claimed events (`worker_events_claimed_total`), worker active state (`worker_active`), and dispatch outcomes (`worker_dispatch_total{status}`).
  - `backend/api.go` & `backend/worker.go`: Standardized service naming (`nexora-api`, `nexora-worker`) and wired metrics tracking into worker options and HTTP server options.
  - `docs/architecture/18-operational-readiness-guide.md`: Comprehensive operational manual covering logging schemas, redaction behavior, metric catalogs, probe semantics, graceful shutdown sequencing, alerting thresholds, and proxy architecture requirements.
- **Verification Evidence**:
  - Unit tests for secret/DSN/header redaction in `logger_test.go` and `logging_test.go`.
  - Normalization, Prometheus exposition, handler, middleware, and 20-goroutine concurrency race tests in `metrics_test.go`.
  - Live dependency failure simulations in `server_test.go` (`TestReadyzEndpoint_DatabaseFailure`, `TestReadyzEndpoint_RedisFailure`, `TestHealthz_LivenessIndependence`, `TestMetricsEndpoint`).
  - Worker outbox metrics tracking tests in `poller_test.go`.
  - All tests passed with zero data races (`-race`).

## Comprehensive Reconciled Finding Register (WS1–WS8)

The following register reconciles every original finding across Workstreams WS1 through WS8, preserving stable finding IDs, original scope, implementation evidence, and final disposition:

| Finding ID | Workstream & Topic | Original Root Cause & Scope | Implementation Evidence | Test / Verification Evidence | Status | Disposition & Boundary Notes |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **B1-DISC-01** | Baseline Bootstrap | `platform.enforce_same_tenant_fk` trigger loop included child tables lacking `tenant_id` (`sales_order_item`, etc.) | Excluded non-tenant child tables in `000001_baseline_hardened.sql` | 100% clean bootstrap on fresh PostgreSQL 16 creating all 76 tables | **RESOLVED** | Baseline checksum locked at `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`. |
| **WS1-01** | Core Lifecycle | Single shutdown context shared across all hooks; hanging hook starved remaining cleanup | Per-resource context timeout derived from `CleanupItemTimeout` in `lifecycle/manager.go` | Unit test `TestLifecycleManager_CleanupItemTimeout` verifies isolation | **RESOLVED** | Teardown hooks execute with bounded isolation in reverse order. |
| **WS1-02** | Configuration | Hardcoded/unbounded readiness and cleanup durations | Configured `ReadinessTimeout` and `CleanupItemTimeout` with validation bounds in `config/config.go` | Tested configuration defaults and fallback limits in `config_test.go` | **RESOLVED** | Safe fallbacks (3s readiness, 5s cleanup) bounded to 100ms–30s. |
| **WS2-01** | PostgreSQL Transactions | Lack of savepoint rollback support; inner failure corrupted outer transaction | Implemented `WithinSavepoint` on `TxManager` and `pgxTxManager` in `transaction.go` | Tested in `TestTxManager_Savepoints`: partial rollback, panic recovery, `ErrNoActiveTransaction` | **RESOLVED** | Preserves ambient transaction and RLS context; recovers cleanly. |
| **WS3-01** | Ingress Security | Unverified `X-Forwarded-For` / `X-Real-IP` allowed client IP spoofing | Implemented `TrustedProxyManager` with CIDR right-to-left traversal in `middleware/ratelimit.go` | Tested in `TestTrustedProxy_Resolution` (single hop, multi-hop, malformed, untrusted client) | **RESOLVED** | Untrusted clients cannot forge client IP. Direct connections fallback safely. |
| **WS3-02** | Transport Concurrency | `timeoutWriter` concurrent access to `Header()` raced with timeout responses | Isolated handler header map (`tw.h`) and buffered response (`tw.buf`) in `timeoutWriter` | Tested under race detector across 50 stress iterations in `TestTimeout` | **RESOLVED** | Closed header race; late writes return `ErrHandlerTimeout`. |
| **WS3-03** | Distributed Rate Limiting | Node-local token bucket was per-process and not cluster-aware | Distributed `RedisRateLimiter` using atomic sliding-window Lua script with configurable fallback policies | Tested live Redis concurrency and fallback modes in `TestRedisRateLimiter` | **RESOLVED** | Atomic across cluster instances; bounded key cardinality and TTL expiration. |
| **WS4-01** | Session Management | Unmounted `/auth/refresh` and missing persistent refresh token model | Forward migration `000002_refresh_tokens.sql`, opaque SHA-256 tokens, single-use rotation, replay detection | Legacy auth tests verify rotation and replay behavior; actual RLS isolation is independently covered by `WS4-RLS-01` migration-backed tests | **RESOLVED** | Cryptographically secure rotation with token family replay revocation. |
| **WS4-RLS-01** | Authentication RLS Bootstrap | Session and refresh-token queries ran without establishing tenant-local RLS context; original fixtures omitted authentication-table RLS | `000005_auth_rls_bootstrap.sql` adds narrow hash-to-tenant resolvers and tenant policies; auth queries resolve by bearer-token hash then execute under tenant-scoped transactions | `TestAuthenticationWithActualMigrationRLS`, `TestRefreshTokenConcurrentRotationWithActualMigrationRLS`, and session RLS integration test; uncached race run passed against migrations 000001–000005 using disposable DB and `nexora_app` | **RESOLVED** | Resolver role is NOLOGIN/NOSUPERUSER/NOBYPASSRLS, has only hash/tenant column reads, fixed search path; app role has execute only, no resolver membership. Client tenant header is comparison-only. |
| **CI-BASELINE-01** | CI Baseline Checksum | Workflow expected stale SHA-256 `7f92a76e...` while governance and actual locked baseline use the approved hash | Updated only `.github/workflows/ci.yml` expected value to `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`; baseline bytes untouched | `sha256sum db/migrations/000001_baseline_hardened.sql` matched both governance records and workflow expectation | **RESOLVED** | Preserves baseline migration and all prior forward migrations. |
| **CI-LINT-01** | Backend Lint Package Loading | Prescribed `make lint` could not load packages when Go attempted to access the host build cache outside the writable sandbox; with an accessible Go cache, lint exposed one test-helper signature issue | No package suppression or gate substitution; helper signature fixed; prescribed root module pattern `golangci-lint run ./...` retained | `GOCACHE=/private/tmp/nexora-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/nexora-golangci-cache make lint` passed: golangci-lint v2.14.0, 0 issues, frontend lint clean; Go `./...` scope (16 packages) | **RESOLVED** | Root cause was environment cache access plus one lint finding revealed after package loading, not module or package configuration. |
| **WS4-02** | Session Scalability | High-frequency `TouchSession` executed SQL update on every request | In-memory lock-free debounce cache + 30-second SQL threshold in `session_store.go` | Integration test `TestAuthenticate_TouchSessionThrottling` | **RESOLVED** | Eliminates row lock contention under concurrent load. |
| **WS4-03** | Authorization Primitives | Segregation of Duties (SoD) enforcement needed explicit foundation contract | Implemented `EnforceSegregationOfDuties` in `auth/authorizer.go` rejecting conflicting role pairs | Unit test `TestAuthorizer_SegregationOfDuties` using test-only fixtures | **RESOLVED (Primitive)** | Generic primitive verified. Domain command bindings scheduled for Phase 2. |
| **WS5-01** | Idempotency Index | Baseline migration erratum: column referenced as `idempotency_key` instead of `key` | Corrected column name to `key` in `000001_baseline_hardened.sql` | Verified unique constraint and index in PostgreSQL 16 | **RESOLVED** | Resolved in Remediation Batch 1. |
| **WS5-02** | Idempotency Atomicity | Separate transactions for business operation and idempotency completion | Implemented `ExecuteAtomic` in `store.go` coordinating operation and `Complete` within same transaction | Integration test in `store_integration_test.go` verifying atomic commit and rollback | **RESOLVED (Capability)** | Framework capability complete. Phase 2 handlers must bind via `RecordFromContext`. |
| **WS5-03** | Idempotency Retention | Unbounded retention of completed records without safe pruning | Implemented `CleanupExpired` using CTE with `FOR UPDATE SKIP LOCKED` preserving active reservations | Integration test `TestStore_CleanupExpired` verifying active records untouched | **RESOLVED** | Batch-limited pruning of expired completed records. |
| **WS5-04** | Stuck Reservations | In-progress reservations permanently blocked after unrecoverable process crash | Implemented `FindStuckReservations` and `MarkStuckAsFailed` with optimistic fencing in `store.go` | Integration test `TestStore_StuckReservationsRecovery` | **RESOLVED** | Background/administrative recovery unblocks stuck records safely. |
| **WS6-01** | Transactional Outbox | Outbox claims lacked persistent leases; claim tx held during external dispatch | Forward migration `000003_outbox_leases.sql` + decoupled `claimBatch` (`SKIP LOCKED`) and external dispatch | Integration test `TestOutbox_Integration_DecoupledPollerAndLeases` | **RESOLVED** | Short claim tx; dispatch executed outside DB transaction; lease recovery verified. |
| **WS6-02** | Outbox Error Handling | Permanent errors burned 5 retry cycles before dead-lettering | Implemented `ErrNonRetryable` immediately transitioning event to `DEAD_LETTER` | Integration test `TestOutbox_Integration_NonRetryableErrorImmediateDeadLetter` | **RESOLVED** | Permanent failures dead-letter immediately with single attempt. |
| **WS6-03** | Outbox Security & Size | Unbounded payload size and raw JSON credential substring scanning | Bounded `MaxPayloadSize = 1 MiB` and structural JSON credential key inspection in `event.go` | Unit tests in `event_test.go` verifying oversized rejection and sensitive key masking | **RESOLVED** | Structural key inspection prevents false positives on text values. |
| **WS6-04** | Outbox Query Index | Missing attempt sorting index on `integration.delivery_attempt` | Created composite index `ix_delivery_attempt_event_attempt` in `000003_outbox_leases.sql` | Verified in PostgreSQL 16 schema | **RESOLVED** | Optimizes delivery attempt queries by event and attempt sequence. |
| **WS6-05** | Outbox Deduplication | Duplicate delivery risk across process crashes and retries | Documented at-least-once delivery; test fixture `DeduplicatingConsumer` | Integration test `TestOutbox_Integration_AtLeastOnceDeduplicationWithConsumer` | **RESOLVED (Documented Boundary)** | **Boundary**: In-memory deduplication does not survive restarts; Phase 2 consumers must implement durable idempotency ledgers. |
| **WS7-01** | Audit Child Table RLS | Baseline enabled RLS only on parent `audit.audit_log`, leaving child tables exposed | Forward migration `000004_audit_hardening.sql` added and forced RLS on `data_change`, `security_event`, `login_history` | Integration test `TestAudit_Integration_ChildTableRLSAndImmutability` | **RESOLVED** | Multi-tenant RLS enforced at PostgreSQL engine level across all child tables. |
| **WS7-02** | Login History Immutability | Baseline trigger omitted `audit.login_history` | Added `trg_login_history_immutable` executing `prevent_mutation()` in `000004_audit_hardening.sql` | Tested `UPDATE` and `DELETE` rejection in `TestAudit_Integration_ChildTableRLSAndImmutability` | **RESOLVED** | Immutability trigger active and tested with application role `nexora_app`. |
| **WS7-03** | Audit Index Parity | Fixture had `ix_audit_tenant_user_time`, omitted from baseline migration | Added `ix_audit_tenant_user_time` in `000004_audit_hardening.sql` | Verified index creation in clean PostgreSQL 16 bootstrap | **RESOLVED** | Production migration matches test fixture index. |
| **WS7-04** | Audit Catalog Constraint | `audit.audit_log(action)` was unconstrained string | Added foreign key constraint `fk_audit_log_action` in `000004_audit_hardening.sql` | Verified foreign key constraint enforcement on clean PostgreSQL 16 | **RESOLVED** | All audit logs must reference valid catalog codes. |
| **WS7-05** | Canonical Action Codes | Baseline seed rows used legacy alias variants (`PRICE_CHANGED`, `PAYMENT_SUCCESS`, etc.) | Seeded all 54 canonical actions in `000004_audit_hardening.sql`; registry supports aliases | Verified catalog registry and database seed insertion in `catalog_test.go` | **RESOLVED** | Canonical codes and backward-compatible aliases fully reconciled. |
| **WS7-06** | Catalog Severity Check | Baseline check constraint omitted `NOTICE`, `LOW`, `MEDIUM`, causing bootstrap failure | Updated constraint in `000004_audit_hardening.sql` block `4b` to include all catalog severities | 100% clean migration execution on fresh PostgreSQL 16 | **RESOLVED** | Check constraint updated prior to canonical action seeding. |
| **WS7-07** | Superuser Audit Boundary | Database superuser/table owner can bypass triggers or disable RLS | Application role `nexora_app` is non-superuser (`NOBYPASSRLS`); triggers block non-superusers | Documented architectural boundary | **DEFERRED (Architectural Boundary)** | Enterprise cloud WORM storage or cryptographic chaining deferred to deployment phase. |
| **WS7-08** | Audit HTTP Endpoints | Public HTTP endpoints for querying audit logs unmounted | Repository provides tenant-isolated `FindAuditLogs` in platform layer | Tested in `audit_integration_test.go` | **DEFERRED (Contract Lock)** | Deferred to avoid unapproved OpenAPI v1.0 schema modifications. |
| **WS8-01** | Distributed Tracing Export | Tracing limited to in-memory/log context; no external collector export | Implemented lightweight standard OTLP/HTTP JSON trace exporter in `observability/exporter.go` | Unit tests in `exporter_test.go` verifying OTLP JSON structure, batching, and flush | **RESOLVED** | Asynchronous OTLP/HTTP export to standard collectors (port 4318). |
| **WS8-02** | Multi-Instance Scraping | Metrics were in-memory per pod without scraper guidance | Documented Prometheus pod-level scraping architecture in `18-operational-readiness-guide.md` | Verified `/metrics` endpoint exposition | **RESOLVED** | Multi-instance scraping contract documented and operational. |

---

## Migration Checksums & Consistency Register

All migrations have been executed and verified in strict sequential order against PostgreSQL 16:

| Migration File | SHA-256 Checksum | Purpose & Scope |
| :--- | :--- | :--- |
| `db/migrations/000001_baseline_hardened.sql` | `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f` | Hardened schema baseline (18 schemas, 76 tables, core RLS and triggers). |
| `db/migrations/000002_refresh_tokens.sql` | `47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733` | Refresh token table (`identity.refresh_token`) with RLS, FKs, and indexes. |
| `db/migrations/000003_outbox_leases.sql` | `15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279` | Outbox leases (`claimed_by`, `lease_expires_at`, `next_attempt_at`), status check, attempt index. |
| `db/migrations/000004_audit_hardening.sql` | `1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f` | Audit child table RLS, login history trigger, catalog FK, severity check, and canonical actions. |
| `db/migrations/000005_auth_rls_bootstrap.sql` | `8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0` | Restricted token-hash tenant resolution, forced auth-table RLS policies, and corrected refresh-token tenant-FK trigger arguments. |
| `db/migrations/000006_identity_permissions.sql` | `a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2` | Canonical permission catalog seed for Identity Increment 1 (Option 1 approved). |

---

## Architectural & Consumer Boundaries

1. **Transactional Outbox Consumer Deduplication Boundary:**
   - The engine provides **at-least-once delivery**.
   - Test fixture `DeduplicatingConsumer` is in-memory and ephemeral.
   - **Requirement for Phase 2**: Downstream business consumers must implement durable database-backed idempotency (e.g. `processed_events` table) within their transaction boundaries wherever duplicate side effects cannot be tolerated.
2. **OpenTelemetry OTLP/HTTP Protocol Boundary:**
   - The exporter communicates via standard **OTLP/HTTP JSON** (`POST /v1/traces`, `Content-Type: application/json`).
   - Protobuf binary wire format (`application/x-protobuf`) is omitted to avoid introducing third-party dependencies into `go.mod`.
   - String attribute encoding is used for span attributes. Compatible with OpenTelemetry Collector OTLP/HTTP receivers.
3. **Database Privilege Boundary:**
   - The platform application runs under role `nexora_app` (`NOBYPASSRLS`).
   - Row-Level Security and immutability triggers are strictly enforced against `nexora_app`.


## Remediation Batch 1 Details (Baseline Bootstrap Integrity)

- **Status**: Completed & Verified (Approved Baseline Correction)
- **Changes Applied**:
  - `db/migrations/000001_baseline_hardened.sql`: Corrected line 1438 from `ON platform.idempotency_key(tenant_id, operation, idempotency_key)` to `ON platform.idempotency_key(tenant_id, operation, key)`.
  - Verified index creation and uniqueness semantics in PostgreSQL 16.
  - Previous SHA-256: `7f92a76ec0fa5c700569621021fb41278730c2bf10d996aa5b0f58fc4f2dfa7c`.
  - Batch 1 Approved Baseline SHA-256: `28aa62fee1c9b22a5bc59fb6faceecb3aa5d0a6683a78b950889108f894183a4`.

## Remediation Batch 2 Details (Bootstrap Integrity, HTTP Concurrency & Refresh Token Architecture)

- **Status**: Completed & Verified
- **Findings Remediated**:
  1. `B1-DISC-01`: Baseline Bootstrap Trigger Loop Defect
     - *Root Cause*: `platform.enforce_same_tenant_fk` trigger loop targeted tables lacking `tenant_id` column (`sales.sales_order_item`, `promotion.promotion_condition`, `promotion.promotion_reward`), causing clean bootstrap to fail.
     - *Fix*: Excluded non-tenant child tables from the trigger loop in `db/migrations/000001_baseline_hardened.sql`.
     - *Approved Baseline SHA-256*: `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`.
     - *Evidence*: 100% clean execution on disposable PostgreSQL 16 instance creating all 76 tables.
  2. `WS3-02`: HTTP `timeoutWriter` Concurrency & Header Data Race
     - *Root Cause*: `timeoutWriter` embedded `http.ResponseWriter` without intercepting `Header()`, allowing concurrent handler calls and timeout error responses to race on the shared header map.
     - *Fix*: Re-architected `timeoutWriter` with isolated handler headers (`tw.h`) and response buffer (`tw.buf`). Handlers cannot mutate the underlying `ResponseWriter` once timed out; late writes return `http.ErrHandlerTimeout`.
     - *Evidence*: Zero data races under `go test -race` across 50 stress iterations.
  3. `WS4-01`: Refresh Token Architecture & Persistent Store
     - *Root Cause*: `/auth/refresh` was present in OpenAPI specification but unmounted in transport, and session schema lacked persistent refresh token support.
     - *Fix*:
       - Forward migration `db/migrations/000002_refresh_tokens.sql` (SHA-256 `47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733`) creating `identity.refresh_token` table with RLS, foreign keys, and indexes.
       - Implemented opaque refresh token generation (`nex_rt_...`), SHA-256 hashing, atomic single-use rotation, token family replay/reuse detection (revoking family and session on reuse), tenant scoping, and error handling.
       - Mounted `POST /auth/refresh` and `POST /api/v1/auth/refresh` in `transporthttp.Server`.
     - *Evidence*: Full suite of unit tests, live DB integration tests, and 10-way concurrency race tests passing.

## Remediation Batch 3 Details (Reliability, Tenant Security & Transaction Integrity)

- **Status**: Completed, Hardened & Verified
- **Scope**: Workstreams WS1 through WS8 remediation addressing foundational reliability, tenant boundaries, and transaction integrity.
- **Remediations Implemented**:
  1. **WS1 — Core Lifecycle & Configuration Boundaries**:
     - *WS1-01 (Per-Item Cleanup Timeout)*: Implemented in `backend/internal/platform/lifecycle/manager.go` via per-resource context deadlines derived from `CleanupItemTimeout`. A hanging cleanup hook cannot starve other registered teardown hooks during graceful shutdown.
     - *WS1-02 (Bounded Configuration)*: Added `ReadinessTimeout` and `CleanupItemTimeout` to `backend/internal/platform/config/config.go` with safe fallbacks and bounds validation.
  2. **WS2 — Savepoints & Partial Rollback**:
     - *WithinSavepoint*: Implemented on `postgres.TxManager` and `pgxTxManager` in `backend/internal/platform/postgres/transaction.go`. Generates unique SQL-safe savepoint identifiers, issues `SAVEPOINT`, handles partial rollback via `ROLLBACK TO SAVEPOINT` on errors, releases savepoint on success via `RELEASE SAVEPOINT`, recovers gracefully from panics, and returns `ErrNoActiveTransaction` if called outside an ambient transaction.
  3. **WS3 — Ingress Network Boundary & Distributed Rate Limiting**:
     - *WS3-01 (Trusted Proxy Traversal)*: Implemented `TrustedProxyManager` in `backend/internal/platform/transport/http/middleware/ratelimit.go`. Parses CIDR blocks from `TrustedProxies` config and resolves authentic client IP traversing `X-Forwarded-For` strictly right-to-left. Prevents client IP spoofing while correctly resolving multi-hop trusted proxies.
     - *WS3-03 (Distributed Redis Rate Limiter)*: Implemented `RedisRateLimiter` utilizing an atomic sliding-window Lua script with sub-second accuracy and TTL automatic expiration. Supports configurable failure modes (`FailClosed` vs fail-open) and falls back safely to in-memory rate limiting when Redis is unavailable.
  4. **WS4 — Session Scalability & Segregation of Duties Primitive**:
     - *WS4-02 (Debounced Session Activity)*: Implemented in-memory lock-free debounce cache combined with a 30-second PostgreSQL update threshold in `backend/internal/platform/auth/session_store.go`, eliminating high-concurrency database row lock contention on session records.
     - *SoD Primitive*: Implemented `EnforceSegregationOfDuties` in `backend/internal/platform/auth/authorizer.go` providing foundational conflict detection between sensitive role pairs (e.g. cashier vs supervisor, requester vs approver).
  5. **WS5 — Idempotency Atomicity & Recovery Fencing**:
     - *ExecuteAtomic*: Implemented atomic orchestration combining reservation, domain execution, and response recording within `backend/internal/platform/idempotency/store.go`.
     - *CleanupExpired*: Implemented batch cleanup of expired idempotency keys using a single efficient CTE query.
     - *Stuck Reservation Recovery*: Implemented `FindStuckReservations` and `MarkStuckAsFailed` with optimistic concurrency fencing, allowing background recovery workers to unblock stuck reservations after unrecoverable crashes.
  6. **WS6 — Decoupled Transactional Outbox Engine**:
     - *Forward Migration (`000003_outbox_leases.sql`)*: Added `claimed_by`, `lease_expires_at`, and `next_attempt_at` columns, and expanded check constraint to include `PROCESSING` status.
     - *Decoupled Poller Architecture*: Completely decoupled database claim transactions (`claimBatch` using short transaction with `FOR UPDATE SKIP LOCKED`) from external dispatch (`dispatcher.Dispatch` executed entirely outside DB transactions) and completion (`finalizeEvent` short completion transaction with exponential backoff on retry).
     - *DeduplicatingConsumer*: Implemented in-memory and interface-level event deduplication verifying at-least-once external delivery semantics without message loss.
     - *Test DDL Serialization*: Added transaction-level advisory locking (`SELECT pg_advisory_xact_lock(...)`) in `testfixture.fixture.go` ensuring parallel package test runners cleanly serialize DDL without PostgreSQL catalog collision (`XX000`).
  7. **WS7 — Append-Only Audit Subsystem Hardening**:
     - *Forward Migration (`000004_audit_hardening.sql`)*:
       - Enabled and forced Row-Level Security on child and telemetry tables: `audit.data_change` (isolated via join to `audit.audit_log`), `audit.security_event`, and `audit.login_history`.
       - Installed database immutability trigger `trg_login_history_immutable` executing `platform.prevent_mutation()` on `audit.login_history`.
       - Added foreign key constraint `fk_audit_log_action` enforcing that every audit log references a valid catalog action in `audit.audit_action_catalog`.
       - Seeded missing canonical action codes and updated severity checks to match catalog specifications.
     - *Store Hardening*: Updated `backend/internal/platform/audit/store.go` so `RecordSecurityEvent` and `RecordLoginHistory` establish local RLS transaction context when invoked without an ambient transaction.
  8. **WS8 — Observability & Tracing Export**:
     - *OTLP Trace Exporter*: Implemented lightweight standard OTLP/HTTP JSON trace exporter in `backend/internal/platform/observability/exporter.go` featuring a non-blocking bounded queue, asynchronous batch delivery, and graceful flush on shutdown without third-party dependencies.
     - *Configuration & Guidance*: Added `OTLPEndpoint` and `ServiceName` to `config/config.go` and documented the per-instance Prometheus scraping architecture in `docs/architecture/18-operational-readiness-guide.md`.
- **Quality Gates & Checksums**:
  - `000001_baseline_hardened.sql` SHA-256 remains strictly locked at `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`.
  - Zero third-party dependencies added (`go.mod` untouched).
  - All tests pass with `-race` across entire workspace (`make test-backend`).
  - Zero linter findings (`golangci-lint run ./...`).
  - Compiles cleanly (`make build`).

## Phase 1 Blocker Remediation Batch 4 — Result

- **Status**: Complete for the three scoped blockers; this does not constitute Phase 1 acceptance or begin Phase 2.
- **Scope**: `WS4-RLS-01`, `CI-BASELINE-01`, and `CI-LINT-01` only. No Phase 2 work.
- **Preflight evidence**:
  - The approved baseline checksum is `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`, recorded in this status register and `17-foundation-implementation-plan.md`; the current baseline file had the same SHA-256. Reproduced the CI mismatch: its old expected value `7f92a76ec0fa5c700569621021fb41278730c2bf10d996aa5b0f58fc4f2dfa7c` differed from the actual approved checksum `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f` (comparison exit 1).
  - Reproduced lint before remediation: `make lint` exited 2; the backend target invoked `golangci-lint run ./...`, which exited 5 with `context loading failed: no go files to analyze`. `go list ./...` identified 16 root-module packages. The detailed loader error was denied access to the default host Go build cache (`open /Users/trmmac/Library/Caches/go-build/...: operation not permitted`). Pointing `GOCACHE` at `/private/tmp/nexora-go-build-cache` allowed package loading and exposed a separate helper signature lint finding; after fixing that helper, the prescribed command passed. No package exclusions or gate substitutions were made.
  - Existing identity integration fixtures create identity tables without tenant RLS. Their passing refresh tests therefore do not establish compatibility with the baseline RLS policies.
- **WS4-RLS-01 design and security boundary**:
  - A forward migration will install explicit tenant RLS policies on the authentication tables and two narrowly scoped token-hash-to-tenant resolver functions.
  - The functions will be `SECURITY DEFINER`, use a fixed `pg_catalog` search path and fully qualified table names, accept only the SHA-256 token hash, and return only the associated tenant UUID. Their owner will be a dedicated `NOLOGIN`, `NOSUPERUSER`, `NOBYPASSRLS` role with column-level `SELECT` on token hash and tenant columns only, plus a role-targeted lookup policy on the two token tables. The application role receives `EXECUTE` on the resolver functions but no membership in the resolver role and no cross-tenant table policy.
  - Threat model: token hashes are derived from high-entropy opaque bearer credentials; a caller cannot choose an independent tenant selector. Possession of the credential is required to obtain its tenant UUID. The resolver reveals no token hash, user ID, session ID, or other tenant's records. All subsequent reads and writes re-read the row inside an application transaction with transaction-local RLS context set from the resolver result. A supplied `X-Tenant-ID` is only compared with that result and never establishes the database context.
  - This is an implementation-level RLS bootstrap mechanism; it does not change the locked architecture or grant `BYPASSRLS` to the application role.
- **Acceptance criteria satisfied**:
  - Applied the new forward migration in disposable integration-test databases and verified the application role is non-superuser and `NOBYPASSRLS`, forced RLS is enabled on relevant authentication tables, and the app role has no resolver membership.
  - Tested access-session lookup and refresh rotation against tenant RLS, including wrong-tenant denial, unknown/expired/revoked/replayed tokens, concurrent rotation, and transaction-local tenant propagation.
  - Verified the baseline checksum remains byte-for-byte consistent with the governance checksum and updated CI's stale expectation.
  - Ran repository lint with accessible Go and golangci caches; recorded exact package coverage. Uncached race tests, `go vet`, and API/worker builds passed.

- **Final evidence and finding status**:
  - `WS4-RLS-01 — RESOLVED`: Both focused `GOCACHE=/private/tmp/nexora-go-build-cache go test -race -count=1 ./backend/internal/platform/auth` and full `GOCACHE=/private/tmp/nexora-go-build-cache go test -race -count=1 ./backend/... ./apps/api/... ./apps/worker/...` passed; the focused auth race test was rerun after the final non-owner assertion was added. Migration-backed tests apply actual migrations 000001–000005 to a uniquely named disposable PostgreSQL database, run app requests as `nexora_app` (`NOBYPASSRLS`), and verify the app role is not table owner, tenant isolation, invalid/unknown and wrong-tenant token rejection, expired/revoked/replayed token behavior, refresh rotation, and concurrent rotation. The test confirms forced RLS on relevant identity tables and resolver role restrictions. The disposable database is dropped by test cleanup.
  - `CI-BASELINE-01 — RESOLVED`: approved/current baseline SHA-256 is `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`; CI now expects this exact value. Baseline migration was not edited.
  - `CI-LINT-01 — RESOLVED`: `GOCACHE=/private/tmp/nexora-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/nexora-golangci-cache make lint` passed with golangci-lint v2.14.0, `0 issues`, and no frontend ESLint warnings/errors. Backend scope is `golangci-lint run ./...` from the root Go module, covering all 16 packages returned by `go list ./...`. Initial failure was reproduced when default host cache access was denied; no lint gate or package scope was weakened.
  - `GOCACHE=/private/tmp/nexora-go-build-cache go vet ./backend/... ./apps/api/... ./apps/worker/...` passed.
  - `GOCACHE=/private/tmp/nexora-go-build-cache go build ./apps/api/cmd/api` and `GOCACHE=/private/tmp/nexora-go-build-cache go build ./apps/worker/cmd/worker` both passed.
  - The uncached full Go race command above passed all backend, API, and worker packages. No test result is inferred from cache (`-count=1`).
  - CI was updated to provide PostgreSQL 16 at the test helper's configured local port, so migration-backed integration tests can run in the CI backend job.
  - No production/persistent database migration, deployment, or clean deployment/bootstrap claim was made. No Phase 2 work was started.

---

## Phase 1 — Final Acceptance Decision (Gate 1)

- **Date:** 2026-10-11
- **Verdict:** `ACCEPTED WITH DOCUMENTED LIMITATIONS`
- **Scope Verified:** Workstreams 1–8, Remediation Batches 1–4 (`WS4-RLS-01`, `CI-BASELINE-01`, `CI-LINT-01`).
- **Remediated Findings Status:**
  - `WS4-RLS-01`: **RESOLVED**. Forward migration `000005_auth_rls_bootstrap.sql` enforces forced tenant RLS on `identity` tables and provides token-hash-to-tenant resolution via `platform.resolve_session_tenant` / `platform.resolve_refresh_token_tenant` (`SECURITY DEFINER`, owned by `nexora_auth_resolver`). Application role `nexora_app` (`NOBYPASSRLS`, non-owner, non-member of resolver role) verified in live integration tests with multi-tenant isolation, rotation, replay detection, and concurrency checks.
  - `CI-BASELINE-01`: **RESOLVED**. `.github/workflows/ci.yml` expects canonical SHA-256 `e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`, matching the immutable `000001_baseline_hardened.sql`.
  - `CI-LINT-01`: **RESOLVED**. `make lint` passes cleanly (0 golangci-lint issues across all 16 packages; 0 frontend ESLint issues).
- **Test & Race Detector Evidence:**
  - Focused auth race test (`go test -race -count=1 ./backend/internal/platform/auth`) passed (verified re-run after final table non-ownership assertions).
  - Full uncached race suite (`go test -race -count=1 ./backend/... ./apps/api/... ./apps/worker/...`) passed across all 14 backend packages.
  - Workspace test pass: 0 failures, 0 data races.
- **Documented Non-Blocking Limitations & Operational Boundaries:**
  1. *Host Cache Permissions*: macOS host build cache permission constraints require explicit `GOCACHE` / `GOLANGCI_LINT_CACHE` directory redirection when running local commands outside standard CI container runners.
  2. *Outbox Consumer Deduplication Boundary*: The engine poller guarantees at-least-once delivery; the test consumer `DeduplicatingConsumer` is ephemeral/in-memory and does not survive restarts. Phase 2 domain consumers must implement durable database-backed idempotency (e.g. `processed_events` table).
  3. *OTLP Protocol Wire Format*: OTLP exporter uses standard OTLP/HTTP JSON (`POST /v1/traces`, port 4318); binary protobuf wire format (`application/x-protobuf`) is omitted to avoid external dependencies.
  4. *Superuser Immutability Boundary (`WS7-07`)*: Immutability triggers protect tables against application role `nexora_app` (`NOBYPASSRLS`); enterprise WORM export deferred to deployment phase.
  5. *Public Audit Inspection Endpoint (`WS7-08`)*: Platform method `FindAuditLogs` is implemented and verified; public HTTP endpoint mounting is deferred to Phase 2 to prevent unapproved OpenAPI v1.0 drift.
- **Phase 2 Authorization:** Authorized strictly for **Batch 1 / Increment 1 (Identity Core & Credentials)**. All subsequent increments and domains remain unauthorized until separate explicit authorization.

---

## Phase 2 Batch 1 — Increment 1: Identity Core & Credentials

- **Status:** COMPLETED & VERIFIED
- **Approved Architectural Decisions:**
  - `DEC-B1-001`: Argon2id password hashing parameters ($m=64\text{ MiB}, t=3, p=4$, salt 16B, key 32B).
  - `DEC-B1-002`: Migration allocation: `000006_identity_permissions.sql` for permission catalog seeding; `000007_crm_audit_actions.sql` reserved for CRM and deferred.
  - `DEC-B1-003`: Permission catalog metadata seeded in `identity.permission`; handlers/services for future domains strictly excluded.
  - `DEC-B1-004`: Server-issued POS terminal tokens architectural model preserved.
  - `DEC-B1-005`: 5-consecutive-failure, 15-minute account lockout policy.
- **Migration Details:**
  - File: `db/migrations/000006_identity_permissions.sql`
  - SHA-256: `a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2`
  - Advisory lock: `hashtext('nexora.identity.permissions.v1')`
  - Behavior: Seeds canonical permissions into `identity.permission` using `ON CONFLICT (code) DO NOTHING`.
  - Migration `000007_crm_audit_actions.sql` was NOT created or executed.
  - Approved baseline `000001_baseline_hardened.sql` (`e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`) and migrations 000002–000005 remain completely unmodified.
- **Components Implemented (`backend/internal/identity`):**
  - `hasher.go`: Argon2id password hashing and constant-time verification adhering to DEC-B1-001.
  - `model.go`: Domain entities (`UserAccount`, `Role`, `Permission`, `UserScope`, `LoginAttempt`, `UserWithDetails`), constants, and lockout predicate (`IsLocked`).
  - `errors.go`: Sentinel domain errors and `MapToAppError` mapping to canonical HTTP error envelopes.
  - `store.go`: PostgreSQL persistence layer (`PgxStore`) using `postgres.DBTX` for tenant-scoped user CRUD, optimistic versioning, role assignments, scope bindings, and atomic failure/lockout counters.
  - `service.go`: Business logic (`DomainService`) enforcing password length policy, Segregation of Duties (`ErrSelfRoleAssignmentDenied`), 5-failure 15-minute lockout policy, atomic append-only audit logging (`USER_CREATED`, `USER_UPDATED`, `USER_LOCKED`, `USER_ROLE_ASSIGNED`, `USER_ROLE_REVOKED`, `USER_SCOPE_CHANGED`, `LOGIN_SUCCESS`, `LOGIN_FAILURE`, `PASSWORD_RESET`), and transactional outbox event emission (`identity.user.created.v1`, `identity.user.locked.v1`, `identity.role.assigned.v1`, `identity.role.revoked.v1`, `identity.scope.assigned.v1`, `identity.scope.revoked.v1`).
  - `handler.go`: RESTful endpoints mounted on Chi router with canonical JSON error envelopes (`apperr.ErrorResponse`), authentication checks, dual permission validation (`canonical.dot` and `legacy:colon`), and tenant extraction from `X-Tenant-ID` or authenticated principal.
- **Verification Evidence:**
  - Uncached full backend race suite passed: `go test -count=1 -race -p 1 ./backend/...` (0 data races, 0 failures).
  - All identity unit and integration tests passed:
    - User account lifecycle and Argon2id hash format verification.
    - Forced RLS multi-tenant isolation under `nexora_app` non-superuser (`NOBYPASSRLS`).
    - 5-failure 15-minute account lockout: attempts 1–4 increment failure counter; attempt 5 locks account, emits outbox event and audit log; attempt 6 while locked fails closed; attempt after simulated expiry succeeds and resets counter.
    - Concurrent failed login attempts (10 concurrent goroutines) execute safely without deadlocks or lost updates.
    - Segregation of Duties: self-assignment of administrative roles rejected with 403 `ErrSelfRoleAssignmentDenied`.
    - Migration 000006 catalog seed verified.
  - Static analysis: `golangci-lint run ./...` passed with 0 issues; `go vet ./...` passed with 0 issues; `go build ./backend/...` succeeded.
- **Excluded Domains Confirmed:**
  - Master data, product catalog, variants, barcodes remain unimplemented.
  - CRM customers, groups, addresses remain unimplemented.
  - POS sessions, cash management, sales orders, payment, loyalty, purchasing, inventory, reporting remain unimplemented.
  - Migration `000007_crm_audit_actions.sql` has NOT been created.

---

## Phase 2 Batch 1 — Increment 2: Organization Hierarchy & Terminal Security

- **Status:** COMPLETED & VERIFIED
- **Approved Architectural Decisions:**
  - `DEC-B1-006` (Option A): Forward migration `000008_organization_hardening.sql` enables RLS on `organization.region` with tenant-isolation policy and expands `organization.terminal` status constraint to include `DISABLED` and `RETIRED`.
  - `DEC-B1-007` (Option A): Server-issued terminal device tokens store only their SHA-256 hash in `organization.terminal.device_identifier`. Plaintext token (`dev_tok_` + 32-byte hex entropy) is returned strictly once upon registration/rotation.
- **Migration Details:**
  - File: `db/migrations/000008_organization_hardening.sql`
  - SHA-256: `f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb`
  - Advisory lock: `hashtext('nexora.organization.hardening.v1')`
  - Behavior:
    - Enables RLS on `organization.region` with policy `tenant_region_isolation` (`tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`).
    - Expands `organization.terminal.terminal_status_check` constraint to `CHECK (status IN ('ACTIVE', 'INACTIVE', 'MAINTENANCE', 'DISABLED', 'RETIRED'))`.
  - Migration `000007_crm_audit_actions.sql` remains absent / strictly reserved for CRM.
  - Approved baseline `000001_baseline_hardened.sql` (`e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f`) and migrations 000002–000006 remain completely unmodified.
- **Components Implemented (`backend/internal/organization`):**
  - `token.go`: Cryptographically secure device token generator (`dev_tok_` prefix + 32 random bytes from `crypto/rand`), SHA-256 hex hashing, and constant-time token verification via `subtle.ConstantTimeCompare`.
  - `model.go`: Domain entities (`Company`, `Region`, `Store`, `Warehouse`, `Terminal`), status constants, request/response DTOs, and event/audit definitions.
  - `errors.go`: Sentinel domain errors and `MapToAppError` mapping to canonical HTTP error envelopes (`apperr.AppError`).
  - `store.go`: PostgreSQL persistence layer (`PgxStore`) using `postgres.DBTX` for tenant-scoped CRUD, optimistic locking on `version`, unique constraint handling, parent-child referential validation, device token hash lookup and updates, and cross-domain scope entity validation (`ValidateScopeEntity`).
  - `service.go`: Business domain service (`DomainService`) orchestrating entity lifecycle, device token issuance/rotation/revocation, parent-child referential integrity checks (store $\rightarrow$ region $\rightarrow$ company; warehouse $\rightarrow$ store/company; terminal $\rightarrow$ store/company), atomic append-only audit logging (`COMPANY_CREATED`, `COMPANY_UPDATED`, `REGION_CREATED`, `REGION_UPDATED`, `STORE_CREATED`, `STORE_UPDATED`, `WAREHOUSE_CREATED`, `WAREHOUSE_UPDATED`, `TERMINAL_REGISTERED`, `TERMINAL_UPDATED`, `TERMINAL_REVOKED`), and transactional outbox event emission (`organization.company.created.v1`, `organization.region.created.v1`, `organization.store.created.v1`, `organization.warehouse.created.v1`, `organization.terminal.registered.v1`, `organization.terminal.updated.v1`, `organization.terminal.revoked.v1`).
  - `handler.go`: RESTful endpoints mounted on Chi router under `/api/v1/organization/` with canonical JSON error envelopes (`apperr.ErrorResponse`), authentication checks (`auth.Principal`), dual permission checks (`company.create`, `region.create`, `store.create`, `warehouse.create`, `terminal.register`, etc.), tenant context injection, and single-exposure token return.
  - Route mounting: Wired into `backend/api.go` via `/api/v1/organization`.
- **Verification Evidence:**
  - Uncached full backend race suite passed: `go test -count=1 -race -p 1 ./backend/...` across all 15 packages (0 data races, 0 failures).
  - All organization unit and integration tests passed:
    - Token generation entropy, SHA-256 hashing, and constant-time comparison tests.
    - Full entity lifecycle CRUD for Company, Region, Store, Warehouse, and Terminal.
    - Tenant RLS isolation verified on `organization.region` and all organization entities under `nexora_app` non-superuser role (`NOBYPASSRLS`).
    - Device token security: plaintext returned only once; only SHA-256 hash stored in PostgreSQL; presented token verified via constant-time comparison.
    - Terminal rotation & revocation: old token superseded/revoked immediately; status updated to `DISABLED` on revocation.
    - Optimistic concurrency conflicts: conflicting concurrent updates rejected with 409 `ErrOptimisticLockConflict`.
    - Parent-child referential validation: store creation with mismatched company/region rejected with 400 `ErrParentNotFound`.
    - User scope assignment integration: `ValidateScopeEntity` verifies entity active status for Identity scope bindings.
    - Atomic audit log and transactional outbox event emission verified in PostgreSQL.
  - Static analysis: `golangci-lint run ./...` passed with 0 issues; `go vet ./...` passed with 0 issues; `go build ./backend/...` succeeded.
- **Excluded Domains Confirmed at Increment 2 Scope:**
  - Master data, product catalog, variants, barcodes remained unimplemented at Increment 2.
  - CRM customers, groups, addresses remained unimplemented at Increment 2.
  - POS sessions, cash management, sales orders, payment, loyalty, purchasing, inventory, reporting remained unimplemented.
  - Migration `000007_crm_audit_actions.sql` remained uncreated at Increment 2.

---

## Phase 2 Batch 1 — Closure Summary

- **Status:** CLOSED WITH DOCUMENTED FOLLOW-UPS
- **Closure Review Artifact:** `docs/architecture/32-phase2-batch1-closure-review.md`
- **Increments Summary & Acceptance Verdicts:**
  - **Increment 1 (Identity Core & Credentials):** `ACCEPTED WITH DOCUMENTED LIMITATIONS` (MFA foundation deferred).
  - **Increment 2 (Organization Hierarchy & Terminal Security):** `ACCEPTED WITH DOCUMENTED LIMITATIONS` (POS sessions and cash operations deferred to POS domain; `docs/architecture/23-organization-increment-acceptance-review.md`).
  - **Increment 3 (Master Data & Catalog):** `ACCEPTED WITH DOCUMENTED LIMITATIONS` (Authoritative pricing calculations deferred to Pricing domain, physical serial inventory movements deferred to Inventory domain; `docs/architecture/27-master-data-increment-acceptance-review.md`).
  - **Increment 4 (Customer Relationship Management):** `ACCEPTED WITH DOCUMENTED LIMITATIONS` (Loyalty points deferred to Loyalty domain, sales checkout customer linking deferred to Sales domain, customer group & lifecycle routes canonical-only; `docs/architecture/31-crm-increment-acceptance-review.md`).
- **Follow-Up Status Synchronization:**
  - The Batch 1 closure review identified a status synchronization requirement for `docs/architecture/IMPLEMENTATION_STATUS.md` to reflect that all four increments have completed independent acceptance reviews and are formally accepted with documented limitations.
  - Completed with this update. Batch 2 (Store Operations, Rules & Inventory) is unblocked and awaits pre-implementation review authorization.

---

## Phase 2 Batch 2 — Increment 6: Pricing Engine Summary

- **Status:** ACCEPTED (Completed & Verified)
- **Approved Architectural Decisions:**
  - `DEC-B2-003`: Canonical Pricing administration endpoints `/api/v1/pricing/price-lists` and `/api/v1/pricing/rules` established with OpenAPI v1.0.0 parity.
  - `DEC-B2-004`: Seed canonical audit actions (`PRICE_CREATED`, `PRICE_UPDATED`, `PRICE_SUBMITTED`, `PRICE_APPROVED`, `PRICE_REJECTED`, `PRICE_ACTIVATED`, `PRICE_DEACTIVATED`, `PRICE_EXPIRED`) and permissions (`price.read`, `price_list.create`, `price_list.update`, `price_rule.read`, `price_rule.create`, `price_rule.update`, `price_rule.submit`, `price_rule.approve`, `price_rule.activate`, `price_rule.deactivate`).
  - `DEC-B2-009`: Sequential increment plan (Increment 5 POS $\rightarrow$ Increment 6 Pricing $\rightarrow$ Increment 7 Promotion $\rightarrow$ Increment 8 Inventory).
  - Specificity hierarchy (Tiers 1–8), exact rational arithmetic (`*big.Rat`), half-up 4-decimal rounding, zero precision loss.
  - Strict channel matching (exact `channel = $3`, no silent fallback to `ALL`; missing default returns `PRICE_LIST_NOT_FOUND` 422).
  - `MissingPriceError` all-or-nothing rejection with `details.unpriced_items` envelope.
- **Migration Details:**
  - File: `db/migrations/000011_pricing_engine.up.sql`
  - SHA-256: `bb29308c0162d21355470d81a8860f23c8dfc4aef0278515a8e99fb436ab6bc7`
- **Components Implemented (`backend/internal/pricing`):**
  - `model.go`: Domain models (`PriceList`, `PriceRule`, `CartItem`, `PriceQuoteRequest`, `PriceQuote`, `PriceQuoteItem`, DTOs), validation rules, exact rational setters/getters.
  - `calculator.go`: Pure deterministic price resolution, 8-tier specificity hierarchy, quantity tier selection, exact rational multiplication (`MultiplyExactRat`), line item rounding, subtotal rational accumulation.
  - `store.go`: PostgreSQL persistence layer (`PgxStore`) using `postgres.DBTX` for tenant-scoped CRUD, optimistic locking on `version`, unique constraint handling, direct numeric string parsing to `*big.Rat`.
  - `service.go`: Business domain service (`DomainService`) orchestrating quote calculation, price list CRUD, price rule draft CRUD, lifecycle state transitions (`Submit`, `Approve`, `Reject`, `Activate`, `Deactivate`), atomic audit logging, and transactional outbox event emission (`pricing.price_rule.<action>.v1`).
  - `handler.go`: RESTful endpoints mounted on Chi router under `/api/v1/pricing/` and alias `/pricing/` with canonical error handling (`apperr.AppError`), authentication/permission checks, and JSON response formatting.
- **Stages Executed & Verified:**
  - **Stage 3:** Design Review & Decisions Locked.
  - **Stage 4:** Pricing Backend Implementation (`backend/internal/pricing/`, route registration in `backend/api.go`).
  - **Stage 4.1:** Targeted Consistency Audit & Correction (Strict default list channel matching, approve/reject permission alignment, outbox event constants `pricing.price_rule.<action>.v1`, state transition restriction `APPROVED -> ACTIVE`).
  - **Stage 4.2:** Decimal Arithmetic Verification & Targeted Correction (Direct numeric text to `*big.Rat`, rational multiplication `MultiplyExactRat`, rational subtotal accumulation, 4-decimal rounding).
  - **Stage 5:** OpenAPI Parity (`contracts/openapi/openapi-v1.0.0.yaml` synchronized with quote, price lists, rules, and lifecycle transitions; Redocly lint passed with 0 errors).
  - **Final Acceptance:** `ACCEPTED` (All migration integrity, database decimal round-trip, pricing service, POS regression, and full backend suites passed).
- **Next Milestone:** Increment 6 officially closed.

---

## Phase 2 Batch 2 — Increment 7: Promotion Engine Summary

- **Status:** ACCEPTED (Completed, Verified & Officially Closed)
- **Approved Architectural Decisions:**
  - `DEC-B2-005`: Canonical Promotion administration endpoints `/api/v1/promotions` established with OpenAPI v1.0.0 parity.
  - `DEC-B2-009`: Sequential increment plan (Increment 5 POS $\rightarrow$ Increment 6 Pricing $\rightarrow$ Increment 7 Promotion $\rightarrow$ Increment 8 Inventory).
  - Canonical lifecycle statuses: `DRAFT`, `PENDING_APPROVAL`, `APPROVED`, `SCHEDULED`, `ACTIVE`, `PAUSED`, `EXPIRED`, `REJECTED`, `CANCELLED`.
  - Immutability of approved promotions: in-place edits allowed only in `DRAFT` status; approved/active promotions require new draft version creation (`/promotions/{id}/draft-version`).
  - Approval Segregation of Duties (SoD): promotion creator cannot approve their own promotion (`ErrSoDViolation`).
  - Pure read-only cart evaluation: `/promotions/evaluate` executes pure evaluation without mutating or reserving `usage_count`.
  - Exact currency arithmetic via `math/big.Rat`: zero precision loss, non-negative net prices, and discount caps preventing total discount from exceeding eligible subtotal.
  - Deterministic tie-breaking (`Priority DESC` $\rightarrow$ `EffectiveFrom ASC` $\rightarrow$ `CreatedAt ASC` $\rightarrow$ `ID ASC`) and stacking policies (`EXCLUSIVE`, `BEST_ONLY`, `STACKABLE`).
  - `BUY_X_GET_Y` reward support: floor division calculation, `max_applications` cap, and detailed `BonusItem` outputs (`product_id`, `variant_id`, `item_name`, `free_quantity`, `discount_amount`, `promotion_id`, `promotion_code`).
  - Canonical fields with documented backward compatibility: `effective_from`/`effective_to` canonical (`valid_from`/`valid_to` aliases); `reason` canonical (`comment` alias); `/pause` canonical (`/deactivate` alias).
- **Migration Details:**
  - File: `db/migrations/000012_promotion_hardening.sql`
  - SHA-256: `e4cf0d75d9524291958a4bc95422fd7aa3b063980ffcaf7347b5900c8945d122`
  - Advisory lock: `hashtext('nexora.promotion.hardening.v1')`
  - Behavior:
    - Status check constraint with canonical statuses.
    - Stacking check constraint (`STACKABLE`, `EXCLUSIVE`, `BEST_ONLY`).
    - Partial evaluation index: `idx_promotions_active_eval` on `(tenant_id, status, channel, priority DESC, effective_from, id) WHERE status = 'ACTIVE'`.
    - Cross-tenant trigger: `trg_prevent_cross_tenant_promotion_version`.
    - Seeded canonical audit actions (`PROMOTION_CREATED`, `PROMOTION_UPDATED`, `PROMOTION_SUBMITTED`, `PROMOTION_APPROVED`, `PROMOTION_REJECTED`, `PROMOTION_ACTIVATED`, `PROMOTION_PAUSED`, `PROMOTION_VERSION_CREATED`).
    - Seeded canonical permissions (`promotion.create`, `promotion.read`, `promotion.update`, `promotion.submit`, `promotion.approve`, `promotion.reject`, `promotion.activate`, `promotion.pause`, `promotion.evaluate`).
- **Components Implemented (`backend/internal/promotion`):**
  - `model.go`: Domain models (`Promotion`, `PromotionCondition`, `PromotionReward`, `PromotionVersion`, DTOs), validation rules, versioning definitions.
  - `evaluator.go`: Deterministic pure promotion evaluator, 4 condition evaluators (`MIN_ORDER_AMOUNT`, `MIN_ITEM_QTY`, `PRODUCT_IN_LIST`, `CATEGORY_IN_LIST`), 3 reward evaluators (`PERCENTAGE_DISCOUNT`, `FIXED_AMOUNT_DISCOUNT`, `BUY_X_GET_Y`), exact rational arithmetic via `math/big.Rat`, discount caps, non-negative net totals, tie-breaking, stacking resolution, and `BonusItem` output generation.
  - `pgx_store.go`: PostgreSQL persistence layer (`PgxStore`) using `postgres.DBTX` for tenant-scoped queries, RLS context injection, optimistic locking on `version`, unique code constraint handling, draft version branching, and active promotion query filtering.
  - `service.go`: Business domain service (`DomainService`) orchestrating promotion lifecycle (`Create`, `UpdateDraft`, `CreateDraftVersion`, `Submit`, `Approve` with SoD, `Reject`, `Activate`, `Pause`), pure cart evaluation, atomic append-only audit logging (`PROMOTION_*`), and transactional outbox event emission (`promotion.<action>.v1`).
  - `handler.go`: RESTful endpoints mounted on Chi router under `/api/v1/promotions` and alias `/promotions` with canonical error handling (`apperr.AppError`), authentication/permission checks, idempotency middleware on mutating commands, and JSON response formatting.
  - `promotion_contract_test.go`: Automated contract tests verifying OpenAPI specification parity against implemented routes, methods, schemas, reward types, and error envelopes.
- **Stages Executed & Verified:**
  - **Stage 1:** Promotion Migration (`000012_promotion_hardening.sql`, constraints, indexes, seeds, triggers).
  - **Stage 2:** Promotion Domain Engine (`evaluator.go`, exact rational arithmetic, condition/reward evaluators, `BUY_X_GET_Y`, stacking).
  - **Stage 3:** Repository & Application Service (`pgx_store.go`, `service.go`, SoD approval guard, immutability, optimistic locking, atomic audit/outbox).
  - **Stage 4:** HTTP Handlers & Transport Integration (`handler.go`, route mounting, idempotency, canonical error mapping, compatibility routing).
  - **Stage 5:** OpenAPI Parity & Contract Hardening (`contracts/openapi/openapi-v1.0.0.yaml` synchronized with all 12 endpoints and schemas; Redocly lint passed with 0 errors; contract tests passed).
  - **Final Acceptance & Closure Review:** `ACCEPTED` (All migration integrity, evaluator test suite, store integration, service lifecycle, handler transport, contract parity, regression suites, and static analysis passed with zero blockers).
- **Non-Blocking Notes:**
  - Minor formatting/indentation in test files recorded per documentation convention with zero functional impact.
- **Next Milestone:** Awaiting owner authorization to initiate Increment 8 (Inventory Domain).




