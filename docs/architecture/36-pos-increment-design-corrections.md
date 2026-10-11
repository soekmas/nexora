# NEXORA Increment 5 — Approved Design Corrections
## POS Session & Cash Management

- **Document ID:** `docs/architecture/36-pos-increment-design-corrections.md`
- **Reference Gate Review:** [`docs/architecture/35-pos-increment-pre-implementation-gate-review.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/35-pos-increment-pre-implementation-gate-review.md)
- **Reference Design Finalization:** [`docs/architecture/34-pos-increment-design-finalization.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/34-pos-increment-design-finalization.md)
- **Phase / Batch / Increment:** Phase 2 / Batch 2 / Increment 5
- **Author:** Antigravity Engineering Agent
- **Date:** 2026-10-11
- **Status:** APPROVED CORRECTIONS DOCUMENTED
- **Verdict:** `CORRECTIONS DOCUMENTED — READY FOR FINAL GATE REVIEW`

---

## 1. Executive Summary & Purpose

During the Pre-Implementation Gate Review ([35-pos-increment-pre-implementation-gate-review.md](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/35-pos-increment-pre-implementation-gate-review.md)), three specific gaps were identified in the design documentation for POS Increment 5:
1. **CORR-POS-001:** Potential blind cash count information leak across API response paths (specifically the summary endpoint, error responses, and closing responses).
2. **CORR-POS-002:** Absence of a database-level check constraint preventing negative `expected_cash` on table `pos.session`.
3. **CORR-POS-003:** Omission of `POST /pos/sessions/{id}/approve-variance` in the OpenAPI v1.0.0 contract.

This document formally records the approved design corrections and their precise specifications. It serves as an addendum to [`docs/architecture/34-pos-increment-design-finalization.md`](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/34-pos-increment-design-finalization.md), establishing full architectural readiness prior to implementation authorization.

---

## 2. CORR-POS-001 — Blind Cash Count Confidentiality

### 2.1 Threat Model & Disclosure Vectors
The blind count invariant requires that a cashier physically counting the cash drawer at shift end has zero knowledge of the expected cash amount or running variance. If a cashier can deduce the expected cash total, they can manipulate the declared count or hide shortages.

Inspection revealed four potential disclosure vectors:
1. **Session Retrieval (`GET /pos/sessions/{id}`):** Direct exposure of `expected_cash` and `variance`.
2. **Session Summary (`GET /pos/sessions/{id}/summary`):** Indirect mathematical exposure via `opening_cash + cash_in - cash_out`.
3. **Close Response (`POST /pos/sessions/{id}/close`):** Premature exposure of variance magnitude when session routes to `VARIANCE_REVIEW`.
4. **Error Responses (`POST /pos/sessions/{id}/cash-out`):** Leakage of available drawer cash in validation error details (e.g. `"drawer cash is 50000"`).

### 2.2 Comprehensive Field-Level Confidentiality Rules

| API Endpoint & Method | Caller Identity & Permissions | Session Status | Sensitive Fields | Enforcement Behavior |
|---|---|---|---|---|
| `GET /api/v1/pos/sessions/{id}` | Shift Cashier (`session.read` only) | `OPEN` or `CLOSING` | `expected_cash`, `actual_cash`, `variance` | Masked in JSON response: returned as `null` / omitted. |
| `GET /api/v1/pos/sessions/{id}` | Supervisor (`session.variance.review` + Store Scope) | Any status | `expected_cash`, `actual_cash`, `variance` | Fully exposed with exact amounts. |
| `GET /api/v1/pos/sessions/{id}` | Shift Cashier (`session.read` only) | `CLOSED` | `expected_cash`, `actual_cash`, `variance` | Exposed after shift is officially finalized and locked. |
| `GET /api/v1/pos/sessions/{id}/summary` | Shift Cashier (`session.read` only) | `OPEN` or `CLOSING` | `cash_in`, `cash_out`, `expected_cash`, `variance`, `session.opening_cash` | **Blocked:** Returns `403 Forbidden` (`code: "BLIND_COUNT_RESTRICTED"`, message: `"Session summary is restricted to supervisors during active shift"`). |
| `GET /api/v1/pos/sessions/{id}/summary` | Supervisor (`session.variance.review` + Store Scope) | Any status | All summary fields | Fully exposed with exact breakdown. |
| `GET /api/v1/pos/sessions/{id}/summary` | Shift Cashier (`session.read` only) | `CLOSED` | All summary fields | Exposed for historical shift review after shift is closed. |
| `POST /api/v1/pos/sessions/{id}/close` | Submitting Cashier | Becomes `VARIANCE_REVIEW` | `expected_cash`, `variance` | Masked in close response: `expected_cash: null`, `variance: null`. Returns `status: "VARIANCE_REVIEW"` with informational message. |
| `POST /api/v1/pos/sessions/{id}/close` | Submitting Cashier | Becomes `CLOSED` | `expected_cash`, `variance` | Fully exposed (exact balance verified). |
| `POST /api/v1/pos/sessions/{id}/cash-out` | Shift Cashier | Any status | Drawer balance metadata | Error envelope returns generic message: `"Cash out exceeds available drawer cash"`. Details array MUST NOT contain balance numbers. |

### 2.3 Application & Logging Safeguards
1. **Logging Sanitization:** Application loggers and HTTP access loggers must never output serialized `expected_cash` or `variance` at `INFO` or `DEBUG` levels on requests initiated by cashiers.
2. **Audit Access Isolation:** Audit action records (`POS_CASH_IN`, `POS_CASH_OUT`, `POS_SESSION_VARIANCE_DETECTED`) remain queryable only by administrators and supervisors with `audit.read` permission.
3. **Negative Test Suite Requirements:**
   - Test `TestBlindCount_GetSession_CashierMasked`: Cashier receives `null` for `expected_cash` during `OPEN`.
   - Test `TestBlindCount_GetSummary_CashierForbidden`: Cashier receives `403 BLIND_COUNT_RESTRICTED` during `OPEN`.
   - Test `TestBlindCount_CashOutError_NoBalanceLeak`: Cashier triggering over-withdrawal receives error with zero numeric drawer balance disclosures.
   - Test `TestBlindCount_CloseVariance_CashierMasked`: Cashier triggering variance review receives masked `expected_cash` in `CloseSession` response.
   - Test `TestBlindCount_SupervisorUnrestricted`: Supervisor with `session.variance.review` receives full unmasked figures on all routes.

---

## 2. CORR-POS-002 — Database-Level Non-Negative Expected Cash Constraint

### 2.1 Forward Migration Specification (`000010_pos_hardening.sql`)
To guarantee that the physical cash drawer never enters an impossible negative state under concurrency races or logic faults, a database check constraint is added to `pos.session`.

```sql
-- Hardening Constraint: Prevent Negative Expected Cash
ALTER TABLE pos.session
  ADD CONSTRAINT pos_session_expected_cash_check
  CHECK (expected_cash >= 0);
```

### 2.2 Triple-Layer Defense-in-Depth Architecture

```text
┌────────────────────────────────────────────────────────────────────────┐
│ Layer 1: Application Domain Validation                                 │
│ Checks request.Amount <= session.ExpectedCash                          │
│ Fails fast with 422 Unprocessable Entity (INSUFFICIENT_DRAWER_CASH)    │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│ Layer 2: Concurrency & Transaction Row Locking                         │
│ Executes SELECT expected_cash, version FROM pos.session FOR UPDATE     │
│ Serializes parallel cash movements on same drawer; increments version  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│ Layer 3: PostgreSQL Database Check Constraint                          │
│ pos_session_expected_cash_check: CHECK (expected_cash >= 0)            │
│ Aborts with 23514 check_violation if race conditions bypass Layer 1/2  │
└────────────────────────────────────────────────────────────────────────┘
```

1. **Layer 1 (Application Precondition):**
   The application service calculates proposed new balance: `newExpected = currentExpected - amount`. If `newExpected < 0`, the transaction is aborted early and returns `422 Unprocessable Entity (INSUFFICIENT_DRAWER_CASH)`.
2. **Layer 2 (Concurrency Control & Serialization):**
   In `pos.cash_movement` insertion transactions, the service issues `SELECT expected_cash, version FROM pos.session WHERE id = ? FOR UPDATE`. This guarantees pessimistic lock serialization on that specific drawer. Simultaneously, optimistic locking verifies `WHERE id = ? AND version = ?` upon update.
3. **Layer 3 (Database Constraint Defense-in-Depth):**
   PostgreSQL enforces `pos_session_expected_cash_check`. If an unforeseen race condition or rogue update attempts to set `expected_cash < 0`, PostgreSQL raises error code `23514 (check_violation)` and rolls back the transaction.

---

## 3. CORR-POS-003 — OpenAPI Contract Parity for Variance Approval

### 3.1 Endpoint Specification for OpenAPI v1.0.0
When implementation is authorized, `contracts/openapi/openapi-v1.0.0.yaml` will be updated to include the variance approval endpoint.

```yaml
  /pos/sessions/{id}/approve-variance:
    post:
      tags:
      - POS
      summary: Approve POS session cash variance
      operationId: postPosSessionsIdApproveVariance
      responses:
        '200':
          description: Variance approved and session closed successfully
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/PosSession'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '403':
          $ref: '#/components/responses/Forbidden'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          $ref: '#/components/responses/Conflict'
        '422':
          $ref: '#/components/responses/ValidationError'
        '500':
          $ref: '#/components/responses/ServerError'
      parameters:
      - $ref: '#/components/parameters/TenantHeader'
      - $ref: '#/components/parameters/RequestIdHeader'
      - $ref: '#/components/parameters/IdempotencyHeader'
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ApproveVarianceRequest'
```

### 3.2 Request Schema Definition

```yaml
    ApproveVarianceRequest:
      type: object
      additionalProperties: false
      properties:
        reason:
          type: string
          minLength: 5
          maxLength: 500
          description: Mandatory documented justification for approving the cash variance
      required:
      - reason
```

### 3.3 Business Logic, Permissions & Concurrency
- **Canonical Route:** `/api/v1/pos/sessions/{id}/approve-variance`
- **Compatibility Route:** `/pos/sessions/{id}/approve-variance`
- **Permission Required:** `session.variance.approve`
- **Authorization Scope:** Caller must have `STORE` or `COMPANY` scope encompassing the session's `store_id`.
- **Segregation of Duties (SoD):** The authenticated approver's user ID MUST NOT match the session's `cashier_user_id`. Violation returns `403 Forbidden` (`code: "SOD_VIOLATION"`, message: `"Cashiers cannot approve variance for their own shift"`).
- **Precondition Status Check:** The session must be in status `VARIANCE_REVIEW`. If in any other status (`OPEN`, `CLOSED`), returns `409 Conflict` (`code: "INVALID_SESSION_STATE"`).
- **State Transition:** Status becomes `CLOSED`. Fields populated: `closed_at = now()`, `variance_approved_by = principal.UserID`, `variance_approved_at = now()`, `variance_reason = request.Reason`.
- **Audit & Outbox:** Atomically emits audit log `POS_SESSION_VARIANCE_APPROVED` (Severity: `HIGH`, requires reason) and outbox event `pos.session.closed.v1`.

---

## 4. Complete Updated DDL for Migration `000010_pos_hardening.sql`

With the incorporation of **CORR-POS-002**, the complete specification for migration `000010_pos_hardening.sql` is:

```sql
-- Migration 000010: POS Hardening, Audit Seeds, and Variance Review
-- Incorporates Approved Decisions DEC-B2-001, DEC-B2-002, and CORR-POS-002.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.pos.hardening.v1'));

-- 1. Add Variance Review Metadata Columns to pos.session
ALTER TABLE pos.session
  ADD COLUMN IF NOT EXISTS variance_approved_by uuid REFERENCES identity.user_account(id),
  ADD COLUMN IF NOT EXISTS variance_approved_at timestamptz,
  ADD COLUMN IF NOT EXISTS variance_reason text;

-- 2. Expand Status Check Constraint on pos.session (DEC-B2-001)
ALTER TABLE pos.session
  DROP CONSTRAINT IF EXISTS session_status_check;

ALTER TABLE pos.session
  ADD CONSTRAINT session_status_check
  CHECK (status IN ('OPENING','OPEN','ACTIVE','CLOSING','CLOSED','VARIANCE_REVIEW'));

-- 3. Non-Negative Expected Cash Constraint (CORR-POS-002)
ALTER TABLE pos.session
  DROP CONSTRAINT IF EXISTS pos_session_expected_cash_check;

ALTER TABLE pos.session
  ADD CONSTRAINT pos_session_expected_cash_check
  CHECK (expected_cash >= 0);

-- 4. Realign Partial Unique Index on pos.session(terminal_id) (DEC-B2-001)
DROP INDEX IF EXISTS pos.ux_pos_session_terminal_open;

CREATE UNIQUE INDEX ux_pos_session_terminal_open
  ON pos.session (terminal_id)
  WHERE (status IN ('OPENING','OPEN','ACTIVE','CLOSING','VARIANCE_REVIEW'));

-- 5. Seed Canonical POS Audit Actions in audit.audit_action_catalog (DEC-B2-002)
INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
  ('POS_SESSION_OPENED', 'POS', 'POS session opened', 'NOTICE', false, true),
  ('POS_CASH_MOVEMENT_CREATED', 'POS', 'Cash movement created', 'NOTICE', false, true),
  ('POS_CASH_IN', 'POS', 'Cash in recorded', 'NOTICE', false, true),
  ('POS_CASH_OUT', 'POS', 'Cash out recorded', 'NOTICE', false, true),
  ('POS_SESSION_CLOSING', 'POS', 'POS session closing', 'NOTICE', false, true),
  ('POS_SESSION_VARIANCE_DETECTED', 'POS', 'Cash variance detected', 'HIGH', false, true),
  ('POS_SESSION_VARIANCE_APPROVED', 'POS', 'Cash variance approved', 'HIGH', true, true),
  ('POS_SESSION_CLOSED', 'POS', 'POS session closed', 'HIGH', false, true),
  ('POS_SESSION_FORCE_CLOSED', 'POS', 'POS session force closed', 'CRITICAL', true, true)
ON CONFLICT (code) DO NOTHING;

COMMIT;
```

---

## 5. Unresolved Issues & Boundary Confirmations

- **Unresolved Issues:** **None.** All identified gaps have been completely resolved with concrete, testable specifications.
- **Execution Boundaries Preserved:**
  - No application code was written.
  - No SQL migration was created or executed.
  - OpenAPI files were not edited.
  - `IMPLEMENTATION_STATUS.md` and `TASKS.md` were not updated.
  - No database or Git operations were performed.

---

## 6. Verdict

### **VERDICT:** `CORRECTIONS DOCUMENTED — READY FOR FINAL GATE REVIEW`
