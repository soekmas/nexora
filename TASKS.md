# Product Identity

This repository implements **NEXORA — Retail Business Platform**. See `docs/architecture/NEXORA_Product_Identity_Naming_v1.md` for naming rules.

# POS Platform --- Implementation Tasks v1

## Status Legend

-   `[ ]` Not started
-   `[~]` In progress
-   `[x]` Completed
-   `[!]` Blocked / architecture decision required

## Execution Rule

Work sequentially unless a dependency is explicitly independent.

Do not begin a later phase while a blocking item in the current phase
remains unresolved.

Every completed task must pass the relevant tests and architecture
gates.

------------------------------------------------------------------------

# PHASE 0 --- Repository Bootstrap

## 0.1 Repository

-   [x] Create monorepo root.
-   [x] Add `AGENTS.md`.
-   [x] Add `IMPLEMENTATION_GUIDE.md`.
-   [x] Add `TASKS.md`.
-   [x] Add architecture documentation under `docs/architecture/`.
-   [x] Add canonical OpenAPI under `contracts/openapi/`.
-   [x] Add event contract directory.
-   [x] Add JSON schema directory.
-   [x] Add DB migration directory.
-   [x] Add test directories.
-   [x] Add deployment directories.
-   [x] Add root README.

## 0.2 Tooling

-   [x] Initialize Go module.
-   [x] Initialize frontend workspace.
-   [x] Configure formatting.
-   [x] Configure linting.
-   [x] Configure static analysis.
-   [x] Configure dependency management.
-   [x] Configure Makefile/Taskfile.
-   [x] Configure local developer commands.

## 0.3 Docker Development Environment

-   [x] PostgreSQL container.
-   [x] Redis container.
-   [~] API container (deferred to deployment phase).
-   [~] Worker container (deferred to deployment phase).
-   [x] POS web container or development workflow.
-   [x] Health checks.
-   [x] Persistent local database volume.
-   [x] Environment variable templates.
-   [x] No secrets committed.

### Gate 0

-   [x] Repository structure matches blueprint.
-   [x] Go project compiles.
-   [x] Frontend project starts.
-   [x] PostgreSQL is reachable.
-   [x] Redis is reachable.
-   [x] Basic CI can execute.

------------------------------------------------------------------------

# PHASE 1 --- GO BACKEND FOUNDATION

## 1.1 Application Lifecycle

-   [x] `cmd/api/main.go`.
-   [x] `cmd/worker/main.go`.
-   [x] Configuration loader.
-   [x] Configuration validation.
-   [x] Graceful shutdown.
-   [x] Signal handling.
-   [x] Dependency initialization.
-   [x] Dependency cleanup.

## 1.2 HTTP Foundation

-   [x] HTTP server.
-   [x] Router.
-   [x] Request ID middleware.
-   [x] Recovery middleware.
-   [x] Structured logging middleware.
-   [x] Authentication context.
-   [x] Authorization context.
-   [x] Tenant/scope context.
-   [x] Request timeout.
-   [x] Security headers where applicable.
-   [x] Health endpoint.
-   [x] Readiness endpoint.

## 1.3 Error System

-   [x] Domain error type.
-   [x] API error mapping.
-   [x] Standard error envelope.
-   [x] Request ID in error responses.
-   [x] No internal stack traces exposed.
-   [x] Conflict/error code catalog baseline.

## 1.4 PostgreSQL

-   [x] pgx pool.
-   [x] Pool configuration.
-   [x] Connection health check.
-   [x] Transaction manager.
-   [x] Transaction context.
-   [x] RLS context setter.
-   [x] Safe transaction cleanup.
-   [x] DB role assumptions documented.

## 1.5 RLS

-   [x] Establish transaction-local tenant context.
-   [x] Establish user context.
-   [x] Establish company context.
-   [x] Establish store context.
-   [x] Establish warehouse context where relevant.
-   [x] Establish terminal context where relevant.
-   [x] RLS integration test.
-   [x] Cross-tenant isolation test.

## 1.6 Authentication / Authorization Foundation

-   [x] Authentication interface.
-   [x] Authenticated principal model.
-   [x] Permission checker.
-   [x] Scope checker.
-   [x] SoD policy hook.
-   [x] Authorization middleware/application component.
-   [x] Permission-denied error.
-   [x] Authorization tests.

## 1.7 Idempotency

-   [x] Idempotency port.
-   [x] PostgreSQL implementation.
-   [x] Request hash.
-   [x] Same-key/same-request replay.
-   [x] Same-key/different-request conflict.
-   [x] Result persistence.
-   [x] Concurrency test.
-   [x] Process-restart durability test.

## 1.8 Audit

-   [x] Audit port.
-   [x] Audit writer.
-   [x] Immutable audit persistence.
-   [x] Actor/scope/request metadata.
-   [x] Before/after support.
-   [x] Audit integration test.
-   [x] Audit immutability test.

## 1.9 Outbox

-   [x] Outbox port.
-   [x] Transactional outbox persistence.
-   [x] Event envelope.
-   [x] Delivery status.
-   [x] Retry metadata.
-   [x] Worker polling/claiming foundation.
-   [x] Duplicate delivery safety.
-   [x] Outbox integration test.

## 1.10 Observability

-   [x] Structured logger.
-   [x] Trace context.
-   [x] Metrics baseline.
-   [x] DB metrics.
-   [x] HTTP metrics.
-   [x] Worker metrics.
-   [x] Correlation IDs.

### Gate 1 --- FOUNDATION LOCK

-   [x] `go test ./...` passes.
-   [x] Static analysis passes.
-   [x] PostgreSQL integration tests pass.
-   [x] RLS isolation tests pass.
-   [x] Authorization tests pass.
-   [x] Idempotency concurrency tests pass.
-   [x] Audit immutability tests pass.
-   [x] Outbox atomicity tests pass.
-   [x] Health/readiness works.
-   [x] Graceful shutdown works.
-   [x] No architecture violations.
-   [x] Foundation documented.

Do not begin business domains until Gate 1 passes.

------------------------------------------------------------------------

# PHASE 2 --- IDENTITY

-   [ ] User account model.
-   [ ] Role model.
-   [ ] Permission model.
-   [ ] User-role assignment.
-   [ ] Role-permission assignment.
-   [ ] User scope.
-   [ ] Session model.
-   [ ] Login attempts.
-   [ ] MFA foundation.
-   [ ] Password policy.
-   [ ] Authentication flow.
-   [ ] Authorization integration.
-   [ ] Audit/security events.
-   [ ] Tests.

### Gate 2

-   [ ] Tenant isolation.
-   [ ] Scope isolation.
-   [ ] Permission tests.
-   [ ] SoD tests.
-   [ ] MFA privileged path.
-   [ ] Session revocation.

------------------------------------------------------------------------

# PHASE 3 --- ORGANIZATION

-   [ ] Company.
-   [ ] Region.
-   [ ] Store.
-   [ ] Warehouse.
-   [ ] Terminal.
-   [ ] User scope assignment.
-   [ ] Terminal/store relationship.
-   [ ] Organization APIs.
-   [ ] Authorization integration.
-   [ ] Audit.
-   [ ] Tests.

------------------------------------------------------------------------

# PHASE 4 --- MASTER DATA

-   [ ] Brand.
-   [ ] Category.
-   [ ] UOM.
-   [ ] Product.
-   [ ] Product variant.
-   [ ] Barcode.
-   [ ] Serial number.
-   [ ] Customer group.
-   [ ] Customer.
-   [ ] Customer address.
-   [ ] Master data APIs.
-   [ ] Search/index strategy.
-   [ ] Audit.
-   [ ] Tests.

------------------------------------------------------------------------

# PHASE 5 --- POS SESSION

-   [ ] Device identity foundation.
-   [ ] Terminal registration.
-   [ ] Terminal lifecycle.
-   [ ] POS session opening.
-   [ ] Active-session uniqueness.
-   [ ] Cash movement.
-   [ ] Session closing.
-   [ ] Variance workflow hook.
-   [ ] Session audit.
-   [ ] Session events.
-   [ ] Tests.
-   [ ] Concurrency test for terminal session.

------------------------------------------------------------------------

# PHASE 6 --- SALES

-   [ ] Sales order aggregate.
-   [ ] Sales order item.
-   [ ] Order creation.
-   [ ] Order confirmation.
-   [ ] Order cancellation.
-   [ ] Order completion.
-   [ ] Order void.
-   [ ] Document numbering.
-   [ ] Sales status history.
-   [ ] Idempotency.
-   [ ] Audit.
-   [ ] Events.
-   [ ] API contract.
-   [ ] Concurrency tests.
-   [ ] Scope tests.

------------------------------------------------------------------------

# PHASE 7 --- INVENTORY

-   [ ] Stock balance.
-   [ ] Stock ledger.
-   [ ] Reservation.
-   [ ] Reservation lifecycle.
-   [ ] Stock deduction.
-   [ ] Stock release.
-   [ ] Stock conflict handling.
-   [ ] Stock adjustment workflow.
-   [ ] Stock transfer.
-   [ ] Warehouse/location integration.
-   [ ] Inventory audit.
-   [ ] Inventory events.
-   [ ] Concurrency tests.
-   [ ] Negative-stock policy enforcement.

------------------------------------------------------------------------

# PHASE 8 --- PAYMENT

-   [ ] Payment aggregate.
-   [ ] Payment attempt.
-   [ ] Payment state machine.
-   [ ] Payment port.
-   [ ] Provider adapter interface.
-   [ ] First provider adapter when selected.
-   [ ] Webhook verification.
-   [ ] Webhook dedupe.
-   [ ] Payment UNKNOWN state.
-   [ ] Reconciliation.
-   [ ] Refund.
-   [ ] Refund approval.
-   [ ] Payment audit.
-   [ ] Payment events.
-   [ ] Provider contract tests.
-   [ ] Timeout/ambiguity tests.

------------------------------------------------------------------------

# PHASE 9 --- PRICING AND PROMOTION

-   [ ] Price list.
-   [ ] Price rule.
-   [ ] Price validity.
-   [ ] Promotion master.
-   [ ] Promotion condition.
-   [ ] Promotion reward.
-   [ ] Stackability.
-   [ ] Net-after-promo calculation.
-   [ ] Promotion approval.
-   [ ] Promotion activation.
-   [ ] Promotion versioning.
-   [ ] Deterministic calculation tests.
-   [ ] Historical reproducibility tests.
-   [ ] Audit/events.

------------------------------------------------------------------------

# PHASE 10 --- CRM AND LOYALTY

-   [ ] Customer profile.
-   [ ] Customer group.
-   [ ] Loyalty account.
-   [ ] Loyalty earning.
-   [ ] Loyalty redemption.
-   [ ] Loyalty ledger.
-   [ ] Voucher.
-   [ ] Voucher redemption.
-   [ ] Redemption concurrency.
-   [ ] Offline restrictions.
-   [ ] Audit/events.
-   [ ] Tests.

------------------------------------------------------------------------

# PHASE 11 --- WORKFLOW AND APPROVAL

-   [ ] Workflow definition.
-   [ ] State definition.
-   [ ] Transition definition.
-   [ ] Workflow instance.
-   [ ] Approval.
-   [ ] SoD enforcement.
-   [ ] Approval history.
-   [ ] Approval notifications hook.
-   [ ] Audit.
-   [ ] Tests.

------------------------------------------------------------------------

# PHASE 12 --- PURCHASING AND WAREHOUSE

-   [ ] Supplier.
-   [ ] Purchase order.
-   [ ] Purchase approval.
-   [ ] Goods receipt.
-   [ ] Partial receipt.
-   [ ] Warehouse operations.
-   [ ] Stock transfer integration.
-   [ ] Audit/events.
-   [ ] Tests.

------------------------------------------------------------------------

# PHASE 13 --- FINANCE

-   [ ] Tax.
-   [ ] Accounting mapping.
-   [ ] Settlement.
-   [ ] Financial references.
-   [ ] Document linkage.
-   [ ] Finance audit.
-   [ ] Tests.

Do not embed SAP-specific structures directly into core finance domain.

------------------------------------------------------------------------

# PHASE 14 --- ERP / INTEGRATION

-   [ ] Integration port.
-   [ ] External reference model.
-   [ ] Delivery attempt model.
-   [ ] Retry engine.
-   [ ] Dead-letter/reconciliation.
-   [ ] SAP adapter.
-   [ ] SAP payload mapping.
-   [ ] ERP status tracking.
-   [ ] ERP reconciliation.
-   [ ] Integration audit.
-   [ ] Integration contract tests.

External ERP failure must not corrupt already-committed POS
transactions.

------------------------------------------------------------------------

# PHASE 15 --- OFFLINE POS

-   [ ] Device registration API.
-   [ ] Offline policy API.
-   [ ] Offline quota API.
-   [ ] Master-data sync.
-   [ ] IndexedDB schema.
-   [ ] Offline command queue.
-   [ ] Client sequence.
-   [ ] Dependency handling.
-   [ ] Sync endpoint.
-   [ ] Retry classification.
-   [ ] Conflict classification.
-   [ ] Reconciliation endpoint.
-   [ ] Clock drift policy.
-   [ ] Payment ambiguity handling.
-   [ ] Device revocation.
-   [ ] Terminal heartbeat.
-   [ ] Browser concurrency control.
-   [ ] Offline receipt behavior.
-   [ ] Sync observability.
-   [ ] Full offline regression tests.

------------------------------------------------------------------------

# PHASE 16 --- REPORTING

-   [ ] Reporting read models.
-   [ ] Sales reporting.
-   [ ] Inventory reporting.
-   [ ] Payment reporting.
-   [ ] Audit reporting.
-   [ ] Operational dashboards.
-   [ ] Performance validation.

Reporting must not compromise transactional database integrity.

------------------------------------------------------------------------

# PHASE 17 --- HARDENING

-   [ ] Full RLS test suite.
-   [ ] Full authorization test suite.
-   [ ] Security regression.
-   [ ] Dependency scan.
-   [ ] Secret scan.
-   [ ] Container scan.
-   [ ] SBOM.
-   [ ] Image signing/provenance.
-   [ ] API breaking-change check.
-   [ ] Event contract check.
-   [ ] Migration validation.
-   [ ] Backup validation.
-   [ ] Restore test.
-   [ ] Load test.
-   [ ] Concurrency test.
-   [ ] Offline stress test.
-   [ ] Payment reconciliation test.
-   [ ] Inventory reconciliation test.

------------------------------------------------------------------------

# PHASE 18 --- PRODUCTION READINESS

-   [ ] Production environment configuration.
-   [ ] Secret management.
-   [ ] Database backup.
-   [ ] PITR/WAL strategy.
-   [ ] Restore runbook.
-   [ ] Monitoring.
-   [ ] Alerting.
-   [ ] Log retention.
-   [ ] Audit retention.
-   [ ] Incident runbook.
-   [ ] Payment incident runbook.
-   [ ] Inventory incident runbook.
-   [ ] Offline sync recovery runbook.
-   [ ] ERP reconciliation runbook.
-   [ ] Deployment approval process.
-   [ ] Rollback/forward-fix strategy.
-   [ ] Production smoke tests.

------------------------------------------------------------------------

# Architecture Gates

## Gate A --- Architecture

-   [ ] No unauthorized architecture changes.
-   [ ] Domain boundaries respected.
-   [ ] ADR implications reviewed.

## Gate B --- Security

-   [ ] Tenant isolation.
-   [ ] Scope authorization.
-   [ ] RLS.
-   [ ] MFA where required.
-   [ ] Secrets safe.
-   [ ] Audit safe.

## Gate C --- Data Integrity

-   [ ] Transactions.
-   [ ] Idempotency.
-   [ ] Concurrency.
-   [ ] Immutable ledgers.
-   [ ] Correct numbering.

## Gate D --- Contract

-   [ ] OpenAPI valid.
-   [ ] Event contracts valid.
-   [ ] Backward compatibility checked.

## Gate E --- Operational

-   [ ] Logs.
-   [ ] Metrics.
-   [ ] Traces.
-   [ ] Health.
-   [ ] Retry/recovery.

## Gate F --- Release

-   [ ] CI green.
-   [ ] Artifact immutable.
-   [ ] Security scans green.
-   [ ] Migration tested.
-   [ ] Deployment audited.

------------------------------------------------------------------------

# Task Execution Template

For every implementation task, record:

## Task

`<task ID and title>`

## Scope

What is being implemented.

## Architecture References

-   Relevant ADR.
-   State machine.
-   Permission.
-   Event.
-   Audit.
-   API.
-   DB.

## Implementation

Files and design changes.

## Tests

Tests added and executed.

## Validation

-   Format.
-   Lint.
-   Unit.
-   Integration.
-   Security.
-   Contract.
-   Architecture.

## Risks

Known limitations or follow-up work.

## Result

`DONE` / `BLOCKED` / `NEEDS ARCHITECTURE DECISION`

------------------------------------------------------------------------

# First Execution Batch

The first Antigravity session should execute only:

1.  Repository bootstrap.
2.  Go module initialization.
3.  API and worker skeleton.
4.  Configuration.
5.  PostgreSQL connection.
6.  HTTP server.
7.  Request ID.
8.  Error model.
9.  Health/readiness.
10. Graceful shutdown.
11. Basic tests.

Do not implement Sales, Inventory, Payment, Promotion, or Offline Sync
in the first foundation batch.

The first milestone is:

**FOUNDATION COMPILES + TESTS PASS + ARCHITECTURE GATE PASSES.**
