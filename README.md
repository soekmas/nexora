# NEXORA

Enterprise Retail / NEXORA Retail Business Platform — modular monolith implementation repository.

## Current phase

**Coding Phase — Repository Bootstrap / Go Backend Foundation**

Architecture is locked through the approved architecture artifacts. Business-domain implementation starts only after the Foundation Gate passes.

## AI / Engineering Control

- `AGENTS.md` — mandatory Antigravity engineering rules.
- `IMPLEMENTATION_GUIDE.md` — implementation standards.
- `TASKS.md` — execution backlog and gates.
- `docs/architecture/09-architecture-lock.md` — highest implementation authority.

## Repository map

- `apps/` — deployable applications.
- `backend/` — Go modular-monolith backend.
- `contracts/` — machine-readable API/event/schema contracts.
- `db/` — migrations, seeds, fixtures.
- `docs/` — architecture and operational documentation.
- `tests/` — cross-module and end-to-end tests.
- `deploy/` — deployment assets.
- `.github/` — CI/CD workflows.

## Important rule

Do not start with Sales, Inventory, Payment, Promotion, or Offline Sync. Complete **Phase 0 + Phase 1 Foundation Gate** first.

## Architecture authority

If source code conflicts with an architecture lock, stop and resolve the architecture decision. Do not silently change the architecture.
