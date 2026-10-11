# Antigravity Bootstrap Prompt — POS Platform

Open the `nexora` repository and work strictly according to the repository instructions.

Before changing code:

1. Read `AGENTS.md`.
2. Read `IMPLEMENTATION_GUIDE.md`.
3. Read `TASKS.md`.
4. Read `docs/architecture/09-architecture-lock.md`.
5. Read `docs/architecture/06-go-backend-architecture.md`.
6. Read `docs/architecture/16-monorepo-blueprint.md`.
7. Read `docs/architecture/17-cicd-architecture.md`.

We are currently executing **Phase 0 — Repository Bootstrap**, then **Phase 1 — Go Backend Foundation**.

Do NOT implement Sales, Inventory, Payment, Promotion, Offline Sync, or other business domains yet.

Your immediate goal is to establish a clean, compiling, testable backend foundation while preserving the architecture.

First inspect the repository and report:

- current structure;
- missing bootstrap components;
- any contradictions between repository files;
- any architecture decision that blocks implementation.

Then implement only the minimum required for Phase 0 and the beginning of Phase 1:

- Go module initialization;
- API application skeleton;
- worker application skeleton;
- configuration loading and validation;
- PostgreSQL connection/pool foundation;
- HTTP server;
- request ID;
- structured error model;
- health/readiness endpoints;
- graceful shutdown;
- basic logging foundation;
- initial test harness.

Follow the layering rules exactly:

`transport → application → domain → ports`

Infrastructure implements ports and must not leak into domain code.

Do not invent business rules. If a requirement is ambiguous, stop and report it.

After implementation:

1. Run formatting.
2. Run unit tests.
3. Run static analysis available in the repository.
4. Run build/compile validation.
5. Review for architecture violations.
6. Update `TASKS.md` only for tasks actually completed.
7. Report changed files, tests, validation results, and remaining work.

Do not mark the Foundation Gate complete until every Gate 1 criterion in `TASKS.md` has actually passed.
