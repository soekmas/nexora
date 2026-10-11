# Product Identity

This repository implements **NEXORA — Retail Business Platform**. See `docs/architecture/NEXORA_Product_Identity_Naming_v1.md` for naming rules.

# POS Platform --- Antigravity Agent Instructions

## Mission

You are an engineering agent working on an enterprise-grade POS
platform.

Your job is to implement the approved architecture faithfully.

Do not redesign the platform while coding unless explicitly asked.

## Source of Truth

Before making architectural decisions, consult:

1.  `docs/architecture/09-architecture-lock.md`
2.  `docs/architecture/04-domain-security-review.md`
3.  `docs/architecture/10-state-machine-catalog.md`
4.  `docs/architecture/11-permission-matrix.md`
5.  `docs/architecture/12-event-catalog.md`
6.  `docs/architecture/13-audit-action-catalog.md`
7.  `docs/architecture/08-offline-sync-contract.md`
8.  `docs/architecture/14-offline-database-design.md`
9.  `contracts/openapi/openapi.yaml`
10. `docs/architecture/03-database-design.md`
11. `docs/architecture/06-go-backend-architecture.md`
12. `IMPLEMENTATION_GUIDE.md`
13. `TASKS.md`

If repository filenames differ, locate the equivalent approved artifact
before proceeding.

## Non-Negotiable Architecture

-   Modular monolith.
-   PostgreSQL is the system of record.
-   Domain boundaries are mandatory.
-   API-first.
-   RLS plus application authorization.
-   Tenant isolation is mandatory.
-   Scope authorization is mandatory.
-   Critical commands are idempotent.
-   Audit is mandatory.
-   Transactional outbox is mandatory for domain events.
-   Inventory is concurrency-safe.
-   Payment is provider-agnostic.
-   ERP is adapter-based.
-   Offline POS is policy-controlled.
-   Redis is never the business source of truth.

## Never Do These

Never:

-   Introduce microservices without an approved ADR.
-   Create a god service.
-   Create a generic repository that hides business semantics.
-   Put business logic in HTTP handlers.
-   Directly mutate another domain's tables.
-   Allow arbitrary status updates.
-   Use `MAX(number)+1`.
-   Use floating point for money.
-   Trust client tenant/store/terminal IDs for authorization.
-   Use Redis as authoritative inventory/payment state.
-   Call payment-provider SDKs directly from Sales domain code.
-   Call external providers inside long database transactions.
-   Blindly retry ambiguous payments.
-   Mutate immutable ledgers.
-   Hard-delete completed transactions.
-   Bypass audit.
-   Bypass idempotency.
-   Bypass RLS.
-   Store secrets in source code.
-   Log credentials, tokens, CVV, or sensitive payment data.
-   Silently resolve synchronization conflicts.
-   Disable tests just to make a build pass.

## Domain Dependency Rules

Allowed direction:

`transport → application → domain → ports`

Infrastructure implements ports.

Domain code must not depend on:

-   PostgreSQL.
-   Redis.
-   HTTP.
-   Provider SDKs.
-   ERP SDKs.
-   Framework-specific transport code.

Cross-domain access must use explicit application/domain contracts.

## Critical Command Rules

Use explicit commands for critical transitions.

Examples:

-   ConfirmOrder
-   CancelOrder
-   CompleteOrder
-   VoidOrder
-   CapturePayment
-   RefundPayment
-   OpenPOSSession
-   ClosePOSSession
-   ReserveStock
-   AdjustStock
-   ApprovePromotion

Do not expose arbitrary `UpdateStatus`.

Every critical command must consider:

-   Authorization.
-   Scope.
-   Current state.
-   Preconditions.
-   Concurrency.
-   Idempotency.
-   Audit.
-   Event.
-   Offline policy.

## Transaction Rules

Critical command flow:

`authorize → transaction → RLS context → idempotency → load state → domain command → persist → audit → outbox → idempotency result → commit`

Do not perform slow external HTTP calls inside the core DB transaction.

## RLS Rules

For every tenant-scoped transaction establish transaction-local context.

Never assume pooled PostgreSQL connections retain safe context.

The application authorization layer remains mandatory even with RLS.

## Idempotency Rules

Canonical header:

`Idempotency-Key`

Rules:

-   Same key + same request = same result.
-   Same key + different request = `409 IDEMPOTENCY_KEY_REUSED`.
-   Idempotency must survive process restart.
-   Do not implement authoritative idempotency in memory.

## State Machine Rules

Never mutate critical state arbitrarily.

Read the state machine catalog before implementing state transitions.

Invalid transitions must return a structured conflict error.

Historical state changes must remain auditable.

## Audit Rules

For critical business operations capture:

-   actor.
-   action.
-   aggregate/object.
-   scope.
-   permission.
-   request ID.
-   trace ID where available.
-   reason where required.
-   before state.
-   after state.
-   relevant metadata.

Never log secrets or sensitive payment credentials.

## Event Rules

Events are versioned contracts.

Write the outbox entry in the same transaction as the business change.

Consumers must tolerate at-least-once delivery.

Do not make event publication the only way to return a synchronous
command result.

## Payment Rules

Keep provider-specific code behind adapters.

Provider ambiguity:

`PROCESSING → UNKNOWN → RECONCILIATION_REQUIRED`

Never automatically charge again after an ambiguous timeout without an
explicit safe reconciliation strategy.

## Inventory Rules

Stock truth lives in PostgreSQL.

Use row locking/version checks where required.

Stock ledger is immutable.

Reservation, deduction, and release must follow the approved state
machine.

## Offline Rules

Offline is controlled by server-issued policy.

Server is authoritative for:

-   inventory.
-   final sales.
-   payment.
-   loyalty.
-   voucher redemption.
-   permissions.
-   finance.
-   ERP.
-   audit.

Offline delivery is at-least-once.

Business effect must be exactly-once through server idempotency.

Never silently merge conflicts.

Payment ambiguity always requires reconciliation.

## API Rules

Canonical API base:

`/api/v1`

Use explicit command endpoints for critical actions.

Keep OpenAPI as the API source of truth.

When API changes:

1.  update OpenAPI.
2.  validate it.
3.  update generated client if applicable.
4.  add/update contract tests.
5.  check backward compatibility.

## Database Rules

Use migrations.

Never edit an already-shared migration to change production history.

Use expand/contract for risky changes.

Transaction data is not hard-deleted.

Use explicit numeric/decimal types for money.

Document numbering is server-generated.

## Testing Rules

Every implementation must include appropriate tests.

At minimum for critical logic test:

-   happy path.
-   invalid state.
-   unauthorized actor.
-   wrong scope.
-   tenant isolation.
-   duplicate request.
-   idempotency conflict.
-   concurrency.
-   retry/recovery.
-   audit.
-   event/outbox behavior.

Do not consider a task complete with only compilation.

## Coding Workflow

For each task:

1.  Read task.
2.  Read relevant architecture.
3.  Identify impacted domain.
4.  Identify state transition.
5.  Identify permission.
6.  Identify audit action.
7.  Identify event.
8.  Identify API/DB changes.
9.  Implement.
10. Add tests.
11. Run validation.
12. Report result.

## Ambiguity Protocol

If a business rule is missing or contradictory:

STOP before inventing a rule.

Report:

-   what is ambiguous.
-   which artifacts are affected.
-   safe options.
-   recommended option if requested.

Do not silently choose a business rule that changes financial,
inventory, payment, security, or authorization behavior.

## Architecture Drift Protocol

If implementation requires changing an architecture lock:

STOP.

Do not rewrite the architecture file automatically.

Explain:

-   current locked decision.
-   implementation conflict.
-   proposed change.
-   security/data/API/migration implications.

Wait for explicit approval.

## Scope Discipline

Implement only the requested task and its necessary dependencies.

Do not refactor unrelated modules.

Do not rename large parts of the repository without a task.

Do not add libraries without justification.

## Completion Report

At the end of a task report:

-   What changed.
-   Files changed.
-   Tests added.
-   Tests executed.
-   Validation result.
-   Migration/API/event changes.
-   Known risks.
-   Follow-up task if any.

Keep the report factual and concise.
