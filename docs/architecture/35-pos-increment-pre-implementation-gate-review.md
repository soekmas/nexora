# NEXORA Increment 5 — Pre-Implementation Gate Review
## POS Session & Cash Management

- **Document ID:** `docs/architecture/35-pos-increment-pre-implementation-gate-review.md`
- **Target Design Document:** [`docs/architecture/34-pos-increment-design-finalization.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/34-pos-increment-design-finalization.md)
- **Phase / Batch / Increment:** Phase 2 / Batch 2 / Increment 5
- **Author:** Antigravity Engineering Agent
- **Date:** 2026-10-11
- **Status:** COMPLETED GATE REVIEW
- **Verdict:** `READY AFTER DESIGN CORRECTIONS`

---

## 1. Executive Summary & Objective

This document conducts a read-only, evidence-based pre-implementation gate review of [`docs/architecture/34-pos-increment-design-finalization.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/34-pos-increment-design-finalization.md).

The design is evaluated against:
1. `AGENTS.md` and `docs/architecture/09-architecture-lock.md`
2. `docs/architecture/10-state-machine-catalog.md` (Sections 21–26)
3. `docs/architecture/11-permission-matrix.md` (Section 11)
4. `docs/architecture/12-event-catalog.md` (Section 14)
5. `docs/architecture/13-audit-action-catalog.md` (Section 18)
6. `contracts/openapi/openapi-v1.0.0.yaml` (Lines 1452–1689, 4749–4873)
7. Checksum-governed migrations `000001`–`000009` and proposed `000010_pos_hardening.sql`
8. `docs/architecture/33-phase2-batch2-pre-implementation-review.md`

All findings are classified as **PASS**, **GAP**, or **UNVERIFIED**. Where gaps are identified, concrete design corrections are proposed.

---

## 2. Detailed Verification by Item

---

### Item 1: Complete POS Session State Machine & Status Consistency
**Evaluation: PASS WITH DESIGN CLARIFICATION**
- **Evidence Inspected:**
  - `docs/architecture/10-state-machine-catalog.md` (Lines 732–900): Lists states `OPENING`, `OPEN`, `CLOSING`, `CLOSED`, `VARIANCE_REVIEW`.
  - `db/migrations/000001_baseline_hardened.sql` (Line 607): Defines `CHECK (status IN ('OPEN','ACTIVE','CLOSING','CLOSED'))` and index `ux_pos_session_terminal_open` `WHERE status IN ('OPEN','ACTIVE','CLOSING')`.
  - `docs/architecture/34-pos-increment-design-finalization.md` (Sections 4.1, 4.2, 6.2): Proposes migration `000010` constraint `CHECK (status IN ('OPENING','OPEN','ACTIVE','CLOSING','CLOSED','VARIANCE_REVIEW'))` and index covering `('OPENING','OPEN','ACTIVE','CLOSING','VARIANCE_REVIEW')`.
- **Finding:**
  - The design successfully reconciles the baseline database status set (`'OPEN'`, `'ACTIVE'`, `'CLOSING'`, `'CLOSED'`) with the architecture catalog by adding `'VARIANCE_REVIEW'` and `'OPENING'`.
  - **Design Clarification:** In Section 4.2 table of doc 34, `OpenSession` transitions immediately to `OPEN`, and `CloseSession` transitions synchronously to either `CLOSED` (if balanced) or `VARIANCE_REVIEW` (if discrepancy). `OPENING` and `CLOSING` are defined in the schema constraint and index, but operate as transient execution phases during synchronous single-transaction requests. This is consistent and safe, but should be explicitly documented as synchronous atomic transitions.

---

### Item 2: Blind Cash Count Confidentiality Across All Response Paths
**Evaluation: GAP**
- **Evidence Inspected:**
  - `docs/architecture/34-pos-increment-design-finalization.md` (Section 5.3): Masks `expected_cash` and `variance` on `GET /api/v1/pos/sessions/{id}` when invoked by a cashier while session is `OPEN`.
  - `contracts/openapi/openapi-v1.0.0.yaml` (Lines 1663–1689, 4848–4873): Defines `GET /pos/sessions/{id}/summary` returning `PosSessionSummary` containing `session`, `sales_total`, `payment_total`, `cash_in`, `cash_out`, `refund_total`.
  - `docs/architecture/11-permission-matrix.md` (Line 655): Cashier has `session.read`.
- **Gap Analysis:**
  1. **Information Leak via Summary Endpoint:** In OpenAPI and doc 34, `GET /pos/sessions/{id}/summary` requires only `session.read`. If an active cashier calls this endpoint during an `OPEN` shift, the response returns `session.opening_cash`, `cash_in`, and `cash_out`. The cashier can trivially compute $\text{expected\_cash} = \text{opening\_cash} + \text{cash\_in} - \text{cash\_out}$, directly defeating the blind count rule!
  2. **Information Leak via Error Details:** If a cashier attempts a `CASH_OUT` exceeding drawer cash, returning detailed error envelopes like `INSUFFICIENT_DRAWER_CASH: available drawer cash is 150000` leaks the exact drawer balance.
  3. **Information Leak via Close Response:** When `POST /pos/sessions/{id}/close` results in `VARIANCE_REVIEW`, returning `expected_cash` and `variance` immediately to the shift cashier leaks the discrepancy magnitude before supervisor intervention.
- **Proposed Design Correction (CORR-POS-001):**
  - Restrict `GET /pos/sessions/{id}/summary` while session is `OPEN` or `CLOSING`: require supervisor permission (`session.variance.review`), OR explicitly mask `cash_in`, `cash_out`, `expected_cash`, and `variance` if the authenticated user is the assigned shift cashier.
  - Standardize error `INSUFFICIENT_DRAWER_CASH`: Return a generic message without balance numbers (e.g., `"cash out amount exceeds available drawer balance"`).
  - In close responses transitioning to `VARIANCE_REVIEW`: if returned to cashier, mask `expected_cash` and `variance` until supervisor approval.

---

### Item 3: Atomic Cash-Balance Validation Under Concurrent Operations
**Evaluation: PASS WITH RECOMMENDATION**
- **Evidence Inspected:**
  - `docs/architecture/34-pos-increment-design-finalization.md` (Section 6.3): Establishes optimistic locking via `version` column on `pos.session`.
  - `db/migrations/000001_baseline_hardened.sql` (Line 610): `version bigint NOT NULL DEFAULT 1`.
- **Finding:**
  - Every cash movement (`CASH_IN`, `CASH_OUT`) and session close mutates `pos.session` in the same transaction as inserting `pos.cash_movement`.
  - Checking `WHERE id = ? AND version = ?` with `version = version + 1` prevents concurrent interleaving: if two transactions attempt simultaneous mutations, the second fails with optimistic lock conflict (`409 Conflict`), requiring retry against fresh balance.
  - **Recommendation:** Implement explicit row locking (`SELECT expected_cash, version FROM pos.session WHERE id = ? FOR UPDATE`) during cash-in, cash-out, and close transactions to serialize drawer mutations cleanly without unnecessary optimistic retry loops.

---

### Item 4: Payout Limit & Prevention of Concurrent Over-Withdrawal
**Evaluation: GAP**
- **Evidence Inspected:**
  - `docs/architecture/34-pos-increment-design-finalization.md` (Section 5.2): Rules that `CASH_OUT` amount must be `<= expected_cash`.
  - `db/migrations/000001_baseline_hardened.sql` (Lines 602–603):
    - `opening_cash numeric(19,4) NOT NULL DEFAULT 0 CHECK (opening_cash >= 0)`
    - `expected_cash numeric(19,4) NOT NULL DEFAULT 0` (NO check constraint!)
- **Gap Analysis:**
  - While application logic checks `amount <= expected_cash`, PostgreSQL table `pos.session` does **not** have a check constraint `CHECK (expected_cash >= 0)`.
  - Under race conditions or potential application bugs, `expected_cash` could become negative at the database level.
- **Proposed Design Correction (CORR-POS-002):**
  - Add `ALTER TABLE pos.session ADD CONSTRAINT chk_pos_session_expected_cash_non_negative CHECK (expected_cash >= 0);` to migration `000010_pos_hardening.sql`. This provides unbreakable defense-in-depth at the database level against drawer deficit over-withdrawals.

---

### Item 5: Supervisor Variance Approval, Segregation of Duties, Scope & Isolation
**Evaluation: PASS**
- **Evidence Inspected:**
  - `docs/architecture/34-pos-increment-design-finalization.md` (Sections 5.5, 6.2, 7.2.6):
    - Dedicated endpoint `POST /api/v1/pos/sessions/{id}/approve-variance`.
    - Mandatory permission: `session.variance.approve`.
    - Segregation of Duties (SoD) check: `approver_user_id != cashier_user_id`. Cashier self-approval rejected with `403 Forbidden (SOD_VIOLATION)`.
    - Store scope validation: Approver must possess store/company scope covering `pos.session.store_id`.
    - Tenant isolation: Forced RLS on `pos.session` via `tenant_isolation_v11`.
- **Finding:**
  - Complete alignment with `docs/architecture/11-permission-matrix.md` (Line 659: Store Mgr approval) and `AGENTS.md` SoD principles.

---

### Item 6: Audit Actions and Outbox Events Consistency
**Evaluation: PASS**
- **Evidence Inspected:**
  - `backend/internal/platform/audit/catalog.go` (Lines 143–151): Defines all 9 canonical POS audit actions.
  - `db/migrations/000004_audit_hardening.sql` (Line 136): `fk_audit_log_action` enforces FK constraint.
  - `docs/architecture/34-pos-increment-design-finalization.md` (Section 6.2): Seeds all 9 actions into `audit.audit_action_catalog` via migration `000010`.
  - `docs/architecture/12-event-catalog.md` (Lines 384–395): Lists `POSSessionOpened`, `POSSessionClosing`, `POSSessionVarianceReviewRequired`, `POSSessionClosed`.
  - `docs/architecture/34-pos-increment-design-finalization.md` (Section 8): Maps outbox events:
    - `pos.session.opened.v1`
    - `pos.session.closing.v1`
    - `pos.session.variance_flagged.v1`
    - `pos.session.closed.v1`
    - `pos.cash.movement_created.v1`
- **Finding:**
  - Outbox events and audit logs are committed within the same database transaction as the session/cash mutation.
  - The seed in migration `000010` completely resolves the foreign key block against `audit.audit_action_catalog`.

---

### Item 7: Idempotency, Optimistic Locking, and Terminal Exclusivity
**Evaluation: PASS**
- **Evidence Inspected:**
  - `docs/architecture/34-pos-increment-design-finalization.md` (Sections 6.2, 6.3):
    - Recreates `ux_pos_session_terminal_open` on `pos.session(terminal_id)` filtering `WHERE (status IN ('OPENING','OPEN','ACTIVE','CLOSING','VARIANCE_REVIEW'))`.
    - Uses canonical header `Idempotency-Key` on all mutating routes (`/open`, `/close`, `/cash-in`, `/cash-out`, `/approve-variance`).
    - Replay with identical payload returns cached response; replay with modified payload returns `409 Conflict (IDEMPOTENCY_KEY_REUSED)`.
- **Finding:**
  - Terminal exclusivity is physically guaranteed by PostgreSQL partial unique index; concurrent open requests cannot result in multiple active sessions.
  - Idempotency middleware integration adheres strictly to `backend/internal/platform/idempotency`.

---

### Item 8: Proposed `approve-variance` Endpoint and OpenAPI Parity
**Evaluation: GAP**
- **Evidence Inspected:**
  - `contracts/openapi/openapi-v1.0.0.yaml` (Lines 1452–1689): Only specifies `/pos/sessions/open`, `/{id}`, `/{id}/cash-in`, `/{id}/cash-out`, `/{id}/close`, `/{id}/summary`. Does NOT define `/pos/sessions/{id}/approve-variance`.
  - `docs/architecture/34-pos-increment-design-finalization.md` (Section 7.2.6): Documents the proposed `POST /api/v1/pos/sessions/{id}/approve-variance` endpoint.
- **Gap Analysis:**
  - The OpenAPI contract currently lacks this required lifecycle endpoint.
  - `AGENTS.md` mandates that OpenAPI remains the API source of truth, and when an API changes:
    1. Update OpenAPI.
    2. Validate it.
    3. Update contract tests.
- **Proposed Design Correction (CORR-POS-003):**
  - When implementation is authorized, update `contracts/openapi/openapi-v1.0.0.yaml` to specify `POST /pos/sessions/{id}/approve-variance` with request schema `ApproveVarianceRequest` (`reason: string`) and responses `200`, `400`, `401`, `403`, `404`, `409`, `422`, `500`.

---

### Item 9: Migration `000010` Compatibility & Immutability Governance
**Evaluation: PASS**
- **Evidence Inspected:**
  - Migration sequence: Next available sequence is `000010` (following `000009_master_hardening.sql`).
  - Advisory lock: `hashtext('nexora.pos.hardening.v1')`.
  - Immutability check: Migrations `000001` through `000009` remain completely unmodified.
  - Forward-only DDL: Adds columns (`variance_approved_by`, `variance_approved_at`, `variance_reason`), drops/adds status check constraint, recreates partial unique index, seeds audit actions with `ON CONFLICT DO NOTHING`.
- **Finding:**
  - 100% compliant with repository migration rules. Safe to run against existing schemas without breaking existing tables or RLS policies.

---

### Item 10: Objectively Testable Acceptance Criteria
**Evaluation: PASS**
- **Evidence Inspected:**
  - `docs/architecture/34-pos-increment-design-finalization.md` (Sections 9, 10):
    - Detailed acceptance criteria across 4 categories (Session lifecycle, Cash drawer calculations, Blind count/variance, Security/audit/outbox).
    - Separation into Unit tests, Concurrency tests, and PostgreSQL integration tests (`pos_integration_test.go`) running against disposable Docker database via `testfixture.New(t)` under `NOBYPASSRLS`.
- **Finding:**
  - Every criterion is objectively verifiable with deterministic assertions.

---

## 3. Summary of Gaps & Proposed Design Corrections

| Gap ID | Category | Description | Proposed Correction |
|---|---|---|---|
| **CORR-POS-001** | Security / Blind Count | `GET /pos/sessions/{id}/summary` returns `cash_in` and `cash_out` to cashier during active shift, enabling mathematical deduction of expected cash and defeating the blind count invariant. | Restrict `GET /pos/sessions/{id}/summary` during `OPEN` status to supervisors (`session.variance.review`), or mask drawer movement sums for the shift cashier. Sanitize error messages and close responses. |
| **CORR-POS-002** | Database Integrity | Table `pos.session` lacks a database-level `CHECK (expected_cash >= 0)` constraint, risking negative drawer balance under unexpected race conditions. | Add `CONSTRAINT chk_pos_session_expected_cash_non_negative CHECK (expected_cash >= 0)` in `000010_pos_hardening.sql`. |
| **CORR-POS-003** | API Contract Parity | `POST /pos/sessions/{id}/approve-variance` is missing from `contracts/openapi/openapi-v1.0.0.yaml`. | Stage OpenAPI update for `POST /pos/sessions/{id}/approve-variance` alongside implementation authorization. |

---

## 4. Verification Performed

- Direct inspection of `docs/architecture/34-pos-increment-design-finalization.md`.
- Cross-examination against `000001_baseline_hardened.sql`, `000004_audit_hardening.sql`, and `000006_identity_permissions.sql`.
- Verification of OpenAPI v1.0.0 schemas and path definitions in `contracts/openapi/openapi-v1.0.0.yaml`.
- State machine alignment check against `docs/architecture/10-state-machine-catalog.md`.
- Permission matrix validation against `docs/architecture/11-permission-matrix.md`.
- Concurrency and transaction boundary analysis under optimistic locking vs row locking.
- **Zero changes made to application code, tests, migrations, OpenAPI, or task ledgers.**

---

## 5. Final Gate Review Verdict

### **VERDICT:** `READY AFTER DESIGN CORRECTIONS`

#### Verdict Rationale:
The design in `34-pos-increment-design-finalization.md` is exceptionally solid and aligns with the platform's core architecture locks. However, **implementation should not start** until the three identified design corrections (CORR-POS-001: Blind count leak prevention in summary endpoint, CORR-POS-002: Database constraint `expected_cash >= 0`, and CORR-POS-003: OpenAPI staging for variance approval) are acknowledged and incorporated into the design.
