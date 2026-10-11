# Product Identity

This repository implements **NEXORA — Retail Business Platform**. See `docs/architecture/NEXORA_Product_Identity_Naming_v1.md` for naming rules.

---
author:
- Coding Phase Baseline
date: 2026-10-06
title: POS Platform --- Implementation Guide v1
---

# 1. Purpose

This document is the implementation bridge between the approved POS
Platform architecture and production coding.

It defines how the engineering team and AI coding agents must translate
the architecture into source code without introducing architecture
drift.

This document does not replace the architecture artifacts. It
operationalizes them.

# 2. Authority and Precedence

When documents appear to conflict, use this precedence:

1.  Explicit business decision approved after architecture lock.
2.  `POS_ADR_Architecture_Lock_v1.md`
3.  Security and domain review.
4.  State machine catalog.
5.  Permission matrix.
6.  Event catalog.
7.  Audit action catalog.
8.  Offline sync and offline database design.
9.  OpenAPI contract.
10. Database design.
11. Go backend architecture.
12. Repository / CI/CD architecture.
13. This implementation guide.
14. Existing source code.

Source code must be changed to conform to the architecture; architecture
must not silently be changed to justify existing code.

# 3. Architecture Baseline

The platform is:

-   A modular monolith.
-   PostgreSQL-first and PostgreSQL is the system of record.
-   API-first.
-   Domain-oriented.
-   Multi-tenant.
-   Scope-aware.
-   RLS protected.
-   Idempotent for critical commands.
-   Audit-first.
-   Transactional-outbox based.
-   Event-ready.
-   Payment-provider agnostic.
-   ERP-provider agnostic.
-   Offline-capable with explicit policy controls.
-   Observable.
-   Designed for future service extraction without premature
    microservices.

Do not introduce microservices, message brokers, CQRS infrastructure,
Kubernetes, or distributed transactions merely because they are
fashionable or theoretically scalable.

# 4. Technology Baseline

Backend:

-   Go.
-   `net/http` with a lightweight router such as `chi`, unless
    explicitly changed by ADR.
-   PostgreSQL.
-   `pgx` / pgxpool.
-   Redis only where justified for cache, rate limiting, or
    non-authoritative coordination.
-   OpenTelemetry for traces/metrics/log correlation.
-   Structured JSON logging.
-   Migration tooling selected and locked during foundation
    implementation.

Frontend:

-   Next.js / React / TypeScript.
-   IndexedDB for POS offline persistence.
-   Generated TypeScript API client from the canonical OpenAPI contract.

Deployment:

-   Docker.
-   Separate API and worker processes.
-   PostgreSQL and Redis as infrastructure dependencies.
-   Immutable production artifacts.

# 5. Repository Rules

The intended repository shape is:

    nexora/
    ├── apps/
    │   ├── api/
    │   ├── worker/
    │   └── pos-web/
    ├── backend/
    │   └── internal/
    │       ├── platform/
    │       ├── identity/
    │       ├── organization/
    │       ├── master/
    │       ├── crm/
    │       ├── pricing/
    │       ├── promotion/
    │       ├── sales/
    │       ├── pos/
    │       ├── payment/
    │       ├── inventory/
    │       ├── warehouse/
    │       ├── purchasing/
    │       ├── finance/
    │       ├── loyalty/
    │       ├── workflow/
    │       ├── integration/
    │       ├── audit/
    │       └── reporting/
    ├── contracts/
    │   ├── openapi/
    │   ├── events/
    │   └── json-schema/
    ├── db/
    │   ├── migrations/
    │   ├── seeds/
    │   └── fixtures/
    ├── docs/
    ├── tests/
    ├── deploy/
    └── .github/

Prefer one Go module for the backend.

# 6. Backend Layering

Each domain follows:

    domain/
    application/
    ports/
    infrastructure/
    transport/

Dependency direction:

    transport → application → domain → ports
                              ↑
                       infrastructure implements ports

Rules:

-   Domain must not depend on infrastructure.
-   Domain must not import HTTP, PostgreSQL, Redis, SDKs, or framework
    packages.
-   Transport must not contain business rules.
-   Application owns use-case orchestration and transaction boundaries.
-   Ports express required capabilities.
-   Infrastructure implements ports.
-   Cross-domain communication goes through application/domain
    contracts, not direct table mutation.
-   A repository must not become a generic dumping ground for arbitrary
    SQL.

# 7. Platform Foundation Standards

The foundation must provide:

-   Configuration.
-   Application lifecycle.
-   HTTP server.
-   Graceful shutdown.
-   Request ID.
-   Authentication context.
-   Authorization context.
-   Tenant/company/store/warehouse/terminal scope.
-   PostgreSQL connection pool.
-   Transaction manager.
-   RLS context setup.
-   Domain error model.
-   Idempotency.
-   Audit writing.
-   Transactional outbox.
-   Structured logging.
-   Observability.
-   Health/readiness endpoints.
-   Security middleware.
-   Worker runtime.
-   Configuration validation.

A business domain must not reimplement these independently.

# 8. Transaction Boundary

For a critical command:

    authenticate
      ↓
    authorize
      ↓
    resolve tenant/scope
      ↓
    begin DB transaction
      ↓
    SET LOCAL RLS context
      ↓
    check idempotency
      ↓
    load aggregate/state
      ↓
    validate domain preconditions
      ↓
    execute domain command
      ↓
    persist domain changes
      ↓
    write audit evidence
      ↓
    write outbox event
      ↓
    persist idempotency result
      ↓
    commit

External provider calls must not be held inside long-running database
transactions.

When an external call is required, use a state machine plus
reconciliation/outbox/worker pattern appropriate to the operation.

# 9. PostgreSQL and RLS

PostgreSQL is authoritative.

Every authenticated request that touches tenant data must establish
transaction-local context such as:

-   `app.tenant_id`
-   `app.user_id`
-   `app.company_id`
-   `app.store_id`
-   `app.warehouse_id`
-   `app.terminal_id`

Use `SET LOCAL` inside the transaction.

Never rely only on a connection-level setting with pooled connections.

RLS is defense-in-depth. Application authorization remains mandatory.

Tenant-aware relationships must prevent cross-tenant FK leakage.

Do not use the application DB superuser for normal runtime access.

# 10. Authorization

Authorization is:

    identity
      +
    permission
      +
    tenant
      +
    organizational scope
      +
    resource ownership/scope
      +
    business policy
      +
    current state

A permission alone is insufficient.

Examples:

-   `sales.void` does not permit voiding a sale outside the user's store
    scope.
-   `inventory.adjust.approve` does not permit approving an adjustment
    created by the same actor where SoD forbids it.
-   Offline authorization must use a server-issued policy and quota;
    client-local permissions are not authoritative.

# 11. State Machine Rules

Critical status fields are not arbitrary editable attributes.

Never implement:

    PATCH /orders/{id}
    { "status": "COMPLETED" }

Instead use explicit commands:

    ConfirmOrder
    CancelOrder
    CompleteOrder
    VoidOrder
    RequestRefund
    ApproveRefund

Every critical transition must validate:

-   Current state.
-   Command.
-   Actor.
-   Permission.
-   Scope.
-   Business preconditions.
-   Concurrency.
-   Offline policy.
-   Side effects.
-   Audit action.
-   Event.

Invalid transitions return machine-readable conflict errors.

# 12. Idempotency

Canonical header:

    Idempotency-Key

Compatibility alias may be supported only if explicitly required.

Logical key:

    tenant_id + operation + idempotency_key

Persist a request hash.

Rules:

-   Same key + same request = return the original business result.
-   Same key + different request = `409 IDEMPOTENCY_KEY_REUSED`.
-   Critical commands must be safe to retry.
-   Idempotency must be enforced server-side.
-   Idempotency state must survive process restart.
-   Do not use in-memory maps as the authoritative idempotency store.

# 13. Inventory

Inventory truth consists of:

-   Stock balance.
-   Immutable stock ledger.
-   Reservation state.
-   Relevant document/state history.

Rules:

-   Never trust client inventory totals.
-   Never use Redis as inventory truth.
-   Use database concurrency controls.
-   Lock or version the relevant stock rows.
-   Reservation and deduction must be atomic where the business
    operation requires it.
-   Ledger entries are append-only.
-   Corrections use reversal/adjustment flows, not mutation of
    historical ledger rows.
-   Stock conflict must be explicit.

# 14. Payment

Payment must be provider-agnostic.

Sales must not import Xendit/Nicepay/Midtrans SDKs directly.

Use:

    Sales / Payment domain
             ↓
       Payment port
             ↓
       Provider adapter
             ↓
       Xendit / Nicepay / etc.

Payment state must follow the state machine catalog.

When a provider response is ambiguous:

    PROCESSING → UNKNOWN → RECONCILIATION_REQUIRED

Never blindly charge again because an HTTP request timed out.

Never store CVV, raw card credentials, tokens/secrets, or sensitive
provider credentials in application logs.

# 15. Promotion

Promotion calculation must be deterministic and reproducible.

A completed sale must preserve enough snapshot information to explain
why a discount/reward was applied.

Do not calculate historical transaction totals using today's promotion
rules.

Promotion activation/approval follows workflow and permission rules.

# 16. Audit

Audit is a platform capability.

Critical actions must answer:

    WHO
    DID WHAT
    TO WHICH OBJECT
    WHEN
    UNDER WHICH SCOPE
    WITH WHICH PERMISSION
    WHY
    BEFORE
    AFTER
    WHAT EVENT FOLLOWED

Never log:

-   Passwords.
-   Password hashes where avoidable.
-   CVV.
-   Full card numbers.
-   JWTs.
-   Refresh tokens.
-   API secrets.
-   MFA secrets.
-   Encryption keys.

Audit records are immutable.

# 17. Outbox and Events

Domain events are written transactionally with the business change.

Minimum envelope:

    event_type
    event_version
    aggregate_type
    aggregate_id
    occurred_at
    payload
    metadata

Delivery is at-least-once.

Consumers must be idempotent.

Event contracts are versioned.

Do not use events as a substitute for the synchronous command result.

# 18. Offline POS

Offline is supported as a controlled platform capability, not
unrestricted offline-first behavior.

Authoritative server:

-   Inventory.
-   Final sale state.
-   Payment.
-   Loyalty.
-   Voucher redemption.
-   Permissions.
-   Promotion activation.
-   Finance.
-   ERP.
-   Audit.

Client-local operational state:

-   Cart.
-   Queue.
-   Cached master data.
-   Device state.
-   Local receipt snapshot.

Offline commands must have:

-   Client transaction ID.
-   Idempotency key.
-   Payload version.
-   Device identity.
-   Terminal identity.
-   Client sequence.
-   Dependency where required.

At-least-once network delivery + server idempotency provides
exactly-once business effect.

Payment ambiguity must enter reconciliation.

Never silently resolve conflicts.

# 19. API Standards

API base:

    /api/v1

Use command-oriented endpoints for critical transitions.

Examples:

    POST /sales/orders/{id}/confirm
    POST /sales/orders/{id}/cancel
    POST /sales/orders/{id}/void
    POST /payments/{id}/capture
    POST /payments/{id}/refund

Do not expose database tables as APIs.

Do not allow arbitrary status updates.

Do not trust tenant IDs supplied by the client for authorization.

Standard error shape:

    {
      "error": {
        "code": "STOCK_CONFLICT",
        "message": "Requested quantity is no longer available",
        "field": null,
        "details": {}
      },
      "request_id": "..."
    }

# 20. Database Standards

Rules:

-   UUID public identifiers are preferred.
-   Business document numbers are generated server-side.
-   Never use `MAX(number)+1`.
-   Money uses decimal/numeric, not floating point.
-   Currency is explicit.
-   Timestamps are UTC internally.
-   Store timezone is configured per organizational context.
-   Transactional records are not hard-deleted.
-   Master records are normally deactivated rather than deleted.
-   Immutable ledgers are append-only.
-   Migrations are immutable after shared deployment.
-   Risky schema changes use expand/contract.
-   Indexes must support actual query patterns.
-   Avoid generic JSON fields for core relational business facts unless
    justified.

# 21. Error Handling

Use typed/domain error codes.

Errors should distinguish:

-   Validation.
-   Authentication.
-   Authorization.
-   Not found.
-   Conflict.
-   State conflict.
-   Idempotency conflict.
-   Concurrency conflict.
-   Provider rejection.
-   Provider ambiguity.
-   Retryable infrastructure failure.
-   Permanent business failure.

Do not expose SQL errors, stack traces, secrets, or internal
implementation details to clients.

Every API response should have a request ID.

# 22. Observability

Every request and worker execution should support correlation through:

-   request ID.
-   trace ID.
-   structured logs.
-   metrics.
-   traces where applicable.

Critical metrics include:

-   API latency.
-   error rate.
-   DB pool saturation.
-   idempotency conflicts.
-   payment UNKNOWN count.
-   outbox backlog.
-   worker retry count.
-   sync conflict count.
-   inventory conflict count.
-   failed authentication.
-   authorization denials.

Business identifiers may be logged only when safe and appropriately
masked.

# 23. Testing Standards

Minimum test layers:

1.  Unit tests.
2.  Application/use-case tests.
3.  Repository/integration tests.
4.  PostgreSQL/RLS tests.
5.  Authorization tests.
6.  Idempotency tests.
7.  Concurrency tests.
8.  State machine tests.
9.  API contract tests.
10. Event contract tests.
11. Offline sync tests.
12. End-to-end tests.
13. Performance tests for critical paths.
14. Security regression tests.

A feature is not complete because its happy path works.

Critical tests must include:

-   Duplicate request.
-   Same idempotency key with different payload.
-   Concurrent update.
-   Unauthorized user.
-   Wrong store.
-   Wrong tenant.
-   Invalid state.
-   Retry after process failure.
-   Outbox retry.
-   Provider timeout/ambiguity.

# 24. Worker Standards

Workers must be safe to run with multiple replicas.

Rules:

-   Claim work safely.
-   Use retry limits.
-   Use exponential backoff where appropriate.
-   Classify retryable vs permanent failures.
-   Avoid duplicate business effects.
-   Record failure context.
-   Support dead-letter/reconciliation workflows where required.
-   Never assume a single worker instance.

# 25. Security Standards

Minimum:

-   Least privilege.
-   MFA for privileged operations/accounts.
-   Secure password handling.
-   Session/token revocation.
-   Rate limiting.
-   Input validation.
-   Output encoding where relevant.
-   Secret management.
-   Dependency scanning.
-   Container scanning.
-   Audit security events.
-   No secrets in Git.
-   No secrets in logs.
-   No client-side authorization as final authority.

# 26. Integration Standards

External integrations include payment providers, SAP, shipping,
messaging, and other third parties.

Rules:

-   Adapter behind a port.
-   Provider DTOs remain inside adapter/integration boundary.
-   Do not leak provider-specific fields into core domain models.
-   External calls must have timeouts.
-   Retry only safe operations.
-   Ambiguous operations require reconciliation.
-   Persist external references.
-   Use outbox for asynchronous integration.
-   Record integration delivery attempts.
-   ERP failure must not silently corrupt POS business state.

# 27. Code Review Rules

Every implementation should be reviewed for:

-   Architecture boundary.
-   Security.
-   Tenant isolation.
-   Scope isolation.
-   State transition correctness.
-   Idempotency.
-   Transaction boundary.
-   Audit.
-   Outbox/event.
-   Concurrency.
-   Error mapping.
-   Test coverage.
-   Observability.
-   Performance.
-   Offline implications.

If any critical item is missing, implementation is not done.

# 28. Definition of Done

A task is DONE only when:

-   Code compiles.
-   Formatter passes.
-   Static analysis passes.
-   Unit tests pass.
-   Relevant integration tests pass.
-   Relevant security tests pass.
-   API contract is updated if needed.
-   Event contract is updated if needed.
-   Audit action is defined if needed.
-   Permission is defined if needed.
-   State transition is defined if needed.
-   Migration is included if needed.
-   Documentation is updated.
-   No architecture rule is violated.
-   No secrets are committed.
-   Logs do not expose sensitive data.
-   CI gates pass.

# 29. Forbidden Patterns

Never introduce:

-   Microservices without ADR.
-   Generic god service.
-   Generic repository hiding business semantics.
-   Direct SQL from handlers.
-   Direct cross-domain table writes.
-   Arbitrary status PATCH.
-   `MAX(number)+1`.
-   In-memory source of truth for payment/inventory/idempotency.
-   Provider SDK calls from core Sales domain.
-   External HTTP calls inside long DB transactions.
-   Client-controlled tenant authorization.
-   Client-controlled final inventory.
-   Client-controlled final payment state.
-   Mutable financial/stock ledgers.
-   Hard delete of completed transactions.
-   Secrets in source code.
-   Secrets in logs.
-   Silent conflict resolution.
-   Blind payment retry after ambiguity.
-   Bypassing audit.
-   Bypassing idempotency.
-   Bypassing RLS.
-   Disabling tests to make CI green.

# 30. Implementation Order

Phase 0 --- Foundation

Phase 1 --- Identity and Organization

Phase 2 --- Master Data

Phase 3 --- POS Session

Phase 4 --- Sales

Phase 5 --- Inventory

Phase 6 --- Payment

Phase 7 --- Pricing and Promotion

Phase 8 --- Customer and Loyalty

Phase 9 --- Workflow and Approval

Phase 10 --- Purchasing / Warehouse

Phase 11 --- Finance

Phase 12 --- Integration / ERP

Phase 13 --- Offline Sync

Phase 14 --- Reporting

Do not skip ahead merely because a later feature looks easier.

# 31. AI Coding Agent Protocol

Before coding:

1.  Read `AGENTS.md`.
2.  Read the relevant architecture artifact.
3.  Identify the domain and ownership boundary.
4.  Identify required state machine transitions.
5.  Identify permissions.
6.  Identify audit actions.
7.  Identify events.
8.  Identify API contract changes.
9.  Identify DB migration changes.
10. Identify tests required.

During coding:

-   Make the smallest coherent change.
-   Preserve existing architecture.
-   Do not invent missing business rules.
-   Flag ambiguity instead of silently deciding.
-   Keep changes reviewable.
-   Add tests with implementation.

After coding:

1.  Format.
2.  Lint/static analysis.
3.  Unit tests.
4.  Relevant integration tests.
5.  Security/RLS tests.
6.  Contract validation.
7.  Architecture checks.
8.  Summarize changed files and risks.

# 32. Architecture Change Protocol

If implementation appears to require changing an architecture lock:

STOP implementation of that part.

Report:

-   Existing rule.
-   Why it blocks implementation.
-   Proposed alternative.
-   Security impact.
-   Data impact.
-   API impact.
-   Migration impact.
-   Offline impact.
-   Operational impact.

Wait for an explicit architecture decision.

Do not silently rewrite the architecture.

# 33. First Coding Target

The first production coding target is GO BACKEND FOUNDATION.

It must establish:

-   Go module.
-   API process.
-   Worker process.
-   Config.
-   Logging.
-   HTTP server.
-   Request ID.
-   Error model.
-   PostgreSQL pool.
-   Transaction manager.
-   RLS context.
-   Authentication/authorization context.
-   Idempotency foundation.
-   Audit foundation.
-   Outbox foundation.
-   Health/readiness.
-   Graceful shutdown.
-   Observability.
-   Test harness.

The foundation must compile and pass tests before domain implementation
begins.
