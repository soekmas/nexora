# NEXORA — Increment 5 POS Session & Cash Management
## Design Finalization

- **Document ID:** `docs/architecture/34-pos-increment-design-finalization.md`
- **Domain Area:** POS Session & Cash Management (`backend/internal/pos`)
- **Phase / Batch / Increment:** Phase 2 / Batch 2 / Increment 5
- **Status:** APPROVED DESIGN / READY FOR IMPLEMENTATION AUTHORIZATION
- **Verdict:** `READY FOR IMPLEMENTATION`

---

## 1. Scope and Exclusions

### 1.1 In-Scope Capabilities [Confirmed]
1. **POS Session Lifecycle:**
   - Formal transition lifecycle: `OPENING → OPEN → CLOSING → CLOSED` and `CLOSING → VARIANCE_REVIEW → CLOSED`.
   - Strict database-enforced session exclusivity: exactly one non-closed session per physical terminal.
2. **Terminal & Store Binding:**
   - Hardware terminal binding through `organization.terminal` and `organization.store`.
   - Verification of store and terminal operational status (`ACTIVE`).
   - Automatic inheritance of `company_id` and `store_id` from the registered terminal.
3. **Cash Float Declaration:**
   - Cash float declaration upon session opening with non-negative validation (`opening_cash >= 0`).
   - Default currency validation (e.g. `IDR`).
4. **Mid-Shift Cash Movements:**
   - Explicit cash drops/pickups (`CASH_OUT`) and mid-shift additions/floats (`CASH_IN`).
   - Compulsory reason tracking and operational reference tracking (`pos.cash_movement`).
   - Validation that `CASH_OUT` does not drain cash drawer beyond calculated drawer balance without override.
5. **Blind Cash Count Closing:**
   - Confidentiality of expected cash: cashiers submit physical count (`actual_cash` / `closing_cash`) without seeing expected cash totals in the close form.
6. **Cash Variance Calculation & Supervisor Review:**
   - Deterministic calculation of expected drawer cash: `expected_cash = opening_cash + total_cash_in - total_cash_out`.
   - Deterministic variance calculation: `variance = actual_cash - expected_cash`.
   - Immediate close if `abs(variance) <= threshold`; automatic diversion to `VARIANCE_REVIEW` if `abs(variance) > threshold`.
   - Supervisor variance resolution requiring explicit reason and Segregation of Duties (SoD).
7. **Audit & Event Integration:**
   - Atomic writing of canonical audit logs (`audit.audit_log`) and transactional outbox events (`platform.outbox`).

### 1.2 Explicit Exclusions [Deferred to Batch 3 / Phase 3]
1. **Sales & Cart Management [Deferred to Batch 3]:** Item scanning, cart calculation, promotional discounting, line voids, and sales order creation.
2. **Payment Processing [Deferred to Batch 3]:** Payment provider integration, QRIS / credit card terminal webhooks, multi-tender split payments, cash drawer triggers via hardware drivers.
3. **Inventory Decrements [Deferred to Batch 3]:** Stock reservations or stock ledger deductions.
4. **Hardware Peripherals [Deferred to Phase 3 Client]:** Physical ESC/POS printer control, serial cash drawer kickers, physical display pole drivers.
5. **Offline POS Session Opening [Confirmed Excluded]:** In accordance with `docs/architecture/08-offline-sync-contract.md`, POS session opening requires real-time server authorization and online validation.

---

## 2. Decisions Inherited from Batch 2 Pre-Review

The following decisions were approved during the Phase 2 / Batch 2 Pre-Implementation Review ([33-phase2-batch2-pre-implementation-review.md](file:///Users/trmmac/Library/CloudStorage/GoogleDrive-dev.mikamiko@gmail.com/My%20Drive/nexora/docs/architecture/33-phase2-batch2-pre-implementation-review.md)):

| Decision ID | Status | Content & Architectural Impact |
|---|---|---|
| **DEC-B2-001** | **Approved** | Update `pos.session.status` check constraint in forward migration `000010` to include `'VARIANCE_REVIEW'`. Update `ux_pos_session_terminal_open` so terminals with sessions in `VARIANCE_REVIEW` cannot open new shifts until variance is settled. |
| **DEC-B2-002** | **Approved** | Seed all nine canonical POS audit actions into `audit.audit_action_catalog` via migration `000010` to eliminate foreign key failure against `fk_audit_log_action`. |
| **DEC-B2-003** | **Approved** | Establish canonical Pricing administration endpoints for Increment 6. |
| **DEC-B2-004** | **Approved** | Seed Pricing audit actions and permissions via migration `000011`. |
| **DEC-B2-005** | **Approved** | Define typed Go condition and reward schemas for Promotion Engine in Increment 7. |
| **DEC-B2-006** | **Approved** | Seed Promotion audit actions and permissions via migration `000012`. |
| **DEC-B2-007** | **Approved** | Create child relational line item tables for stock adjustments and transfers in migration `000013` with forced RLS. |
| **DEC-B2-008** | **Approved** | Inventory Domain (Increment 8) owns serial-number physical state transitions. |
| **DEC-B2-009** | **Approved** | Adopt the sequential increment plan: Increment 5 (POS) → Increment 6 (Pricing) → Increment 7 (Promotion) → Increment 8 (Inventory). |

---

## 3. Existing-State Inspection Findings

### 3.1 Baseline Database Schema (`000001_baseline_hardened.sql`) [Confirmed]
1. **Table `pos.session`:**
   - Columns: `id`, `tenant_id`, `company_id`, `store_id`, `terminal_id`, `cashier_user_id`, `session_number`, `opened_at`, `closed_at`, `opening_cash`, `expected_cash`, `actual_cash`, `variance`, `status`, `created_at`, `updated_at`, `version`.
   - Existing Status Constraint: `CHECK (status IN ('OPEN','ACTIVE','CLOSING','CLOSED'))` (L607). Notice `'VARIANCE_REVIEW'` is absent.
   - Existing Unique Index: `ux_pos_session_terminal_open ON pos.session(terminal_id) WHERE status IN ('OPEN','ACTIVE','CLOSING')` (L615).
   - Tenant isolation: Secured by `tenant_isolation_v11` policy with forced RLS.
2. **Table `pos.cash_movement`:**
   - Columns: `id`, `tenant_id`, `session_id`, `movement_type`, `amount`, `reason`, `reference_type`, `reference_id`, `created_at`, `created_by`.
   - Movement Type Constraint: `CHECK (movement_type IN ('CASH_IN','CASH_OUT','SALE','REFUND','OPENING','CLOSING','ADJUSTMENT'))` (L624).
   - Amount Constraint: `CHECK (amount > 0)`.
   - Tenant isolation: Secured by `tenant_isolation_v11` policy with forced RLS.

### 3.2 Audit Action Database Gap (`000004_audit_hardening.sql`) [Confirmed]
- Table `audit.audit_log` enforces foreign key `fk_audit_log_action (action) REFERENCES audit.audit_action_catalog(code)`.
- In `000004_audit_hardening.sql`, **zero** POS audit actions were seeded into `audit.audit_action_catalog`.
- In `backend/internal/platform/audit/catalog.go`, nine POS audit actions are defined in Go memory.
- Forward migration `000010_pos_hardening.sql` is strictly required to insert these nine actions into PostgreSQL.

### 3.3 Permissions State (`000006_identity_permissions.sql`) [Confirmed]
- `000006_identity_permissions.sql` seeded the following POS permissions under module `POS`:
  - `session.read`, `session.open`, `session.close`, `session.variance.review`, `session.variance.approve`, `cash_movement.create`, `cash_movement.approve`.
- No new permission rows are strictly required, but the permission assignment matrix in authorization checks must enforce these exact codes.

### 3.4 OpenAPI Parity (`contracts/openapi/openapi-v1.0.0.yaml`) [Confirmed]
- Paths currently specified in OpenAPI:
  - `POST /pos/sessions/open`
  - `GET /pos/sessions/{id}`
  - `POST /pos/sessions/{id}/cash-in`
  - `POST /pos/sessions/{id}/cash-out`
  - `POST /pos/sessions/{id}/close`
  - `GET /pos/sessions/{id}/summary`
- Omission: Path `POST /pos/sessions/{id}/approve-variance` is not yet present in OpenAPI v1.0.0.

---

## 4. Final POS Session State Machine

### 4.1 State Machine Diagram [Approved]
```text
                  ┌───────────────┐
                  │    OPENING    │
                  └───────┬───────┘
                          │ OpenSession (command)
                          ▼
                  ┌───────────────┐
                  │  OPEN/ACTIVE  │◄────────────────┐
                  └───────┬───────┘                 │
                          │                         │ Cash Movements
                          │                         │ (CASH_IN, CASH_OUT)
                          │ CloseSession (blind)    │
                          ▼                         │
                  ┌───────────────┐                 │
                  │    CLOSING    │─────────────────┘
                  └───────┬───────┘
                          │
         ┌────────────────┴────────────────┐
         │ variance <= threshold           │ variance > threshold
         ▼                                 ▼
  ┌──────────────┐                 ┌─────────────────┐
  │    CLOSED    │                 │ VARIANCE_REVIEW │
  └──────────────┘                 └────────┬────────┘
                                            │ ApproveVariance (supervisor)
                                            ▼
                                   ┌─────────────────┐
                                   │     CLOSED      │
                                   └─────────────────┘
```

### 4.2 State Transition Matrix [Approved]

| Transition | Source Status | Target Status | Authorized Actor | Permission Required | Preconditions | DB Transaction Actions | Audit Action | Domain Event | Idempotency Key |
|---|---|---|---|---|---|---|---|---|---|
| **T-1: OpenSession** | *(None)* | `OPEN` | Cashier, Supervisor, Store Mgr | `session.open` | 1. Terminal exists and `status == 'ACTIVE'`.<br>2. Store exists and `status == 'ACTIVE'`.<br>3. No existing session in `('OPENING','OPEN','ACTIVE','CLOSING','VARIANCE_REVIEW')` for this terminal.<br>4. Cashier account `status == 'ACTIVE'`.<br>5. `opening_cash >= 0`. | 1. Insert `pos.session` (`status = 'OPEN'`).<br>2. Insert `pos.cash_movement` (`movement_type = 'OPENING'`).<br>3. Insert `audit.audit_log`.<br>4. Insert `platform.outbox`. | `POS_SESSION_OPENED` | `pos.session.opened.v1` | Required |
| **T-2: CashIn** | `OPEN` | `OPEN` | Cashier, Supervisor, Store Mgr | `cash_movement.create` | 1. Session is `OPEN`.<br>2. Amount `> 0`.<br>3. Non-empty `reason`. | 1. Insert `pos.cash_movement` (`movement_type = 'CASH_IN'`).<br>2. Update `pos.session.expected_cash += amount`.<br>3. Insert `audit.audit_log`.<br>4. Insert `platform.outbox`. | `POS_CASH_IN` | `pos.cash.movement_created.v1` | Required |
| **T-3: CashOut** | `OPEN` | `OPEN` | Cashier, Supervisor, Store Mgr | `cash_movement.create` | 1. Session is `OPEN`.<br>2. Amount `> 0`.<br>3. Non-empty `reason`.<br>4. Amount `<= expected_cash` (unless supervisor overrides). | 1. Insert `pos.cash_movement` (`movement_type = 'CASH_OUT'`).<br>2. Update `pos.session.expected_cash -= amount`.<br>3. Insert `audit.audit_log`.<br>4. Insert `platform.outbox`. | `POS_CASH_OUT` | `pos.cash.movement_created.v1` | Required |
| **T-4: CloseSession (Balanced)** | `OPEN` | `CLOSED` | Cashier, Supervisor, Store Mgr | `session.close` | 1. Session is `OPEN`.<br>2. `actual_cash >= 0`.<br>3. Calculated `abs(variance) <= threshold`. | 1. Transition `pos.session` status to `CLOSED`.<br>2. Set `closed_at = now()`, `actual_cash`, `variance`.<br>3. Insert `pos.cash_movement` (`movement_type = 'CLOSING'`).<br>4. Insert `audit.audit_log`.<br>5. Insert `platform.outbox`. | `POS_SESSION_CLOSED` | `pos.session.closed.v1` | Required |
| **T-5: CloseSession (Variance Flagged)** | `OPEN` | `VARIANCE_REVIEW` | Cashier, Supervisor, Store Mgr | `session.close` | 1. Session is `OPEN`.<br>2. `actual_cash >= 0`.<br>3. Calculated `abs(variance) > threshold`. | 1. Transition `pos.session` status to `VARIANCE_REVIEW`.<br>2. Set `actual_cash`, `variance`.<br>3. Insert `pos.cash_movement` (`movement_type = 'CLOSING'`).<br>4. Insert `audit.audit_log`.<br>5. Insert `platform.outbox`. | `POS_SESSION_VARIANCE_DETECTED` | `pos.session.variance_flagged.v1` | Required |
| **T-6: ApproveVariance** | `VARIANCE_REVIEW` | `CLOSED` | Supervisor, Store Mgr | `session.variance.approve` | 1. Session is `VARIANCE_REVIEW`.<br>2. Approver `!= cashier_user_id` (SoD).<br>3. Non-empty approval `reason`. | 1. Transition `pos.session` status to `CLOSED`.<br>2. Set `closed_at = now()`, `variance_approved_by`, `variance_approved_at`, `variance_reason`.<br>3. Insert `audit.audit_log`.<br>4. Insert `platform.outbox`. | `POS_SESSION_VARIANCE_APPROVED` | `pos.session.closed.v1` | Required |

---

## 5. Cash Management Business Rules

### 5.1 Opening Float Rules [Confirmed]
- **Minimum Amount:** `opening_cash` must be `>= 0.0000`. Negative float values are strictly rejected (`422 ValidationError`).
- **Currency:** Must match store/company operational currency (default: `IDR`).
- **Initial Drawer State:** Setting `opening_cash` automatically initializes `expected_cash = opening_cash` and generates an initial cash movement record with `movement_type = 'OPENING'`.

### 5.2 Mid-Shift Cash Drops & Payouts [Confirmed]
- **Validation:** Every cash movement requires a strictly positive amount (`amount > 0.0000`) and a non-empty `reason` (minimum 3 characters).
- **Float Depletion Invariant:** A standard `CASH_OUT` transaction must not exceed `expected_cash`. Attempting to withdraw more cash than the calculated drawer balance returns `422 ValidationError (INSUFFICIENT_DRAWER_CASH)`.
- **Movement Immutability:** Cash movement records (`pos.cash_movement`) are append-only. They are never updated or deleted. Corrections must be made via compensating movements.

### 5.3 Blind Count Invariant [Confirmed]
- **Confidentiality:** The cashier submitting the close request must not be informed of `expected_cash` before or during the counting process.
- **API Response Filtering:** When `GET /pos/sessions/{id}` is queried while the session is `OPEN`, if the authenticated user is the cashier (or lacks supervisor role), `expected_cash` and `variance` are returned as `null` / omitted in the JSON response.
- **Closing Submission:** The cashier submits `closing_cash` (representing the physical bill/coin count) and an optional comment.

### 5.4 Expected Cash and Variance Calculation [Confirmed]
- **Expected Cash Formula:**
  $$\text{expected\_cash} = \text{opening\_cash} + \sum \text{CASH\_IN} - \sum \text{CASH\_OUT}$$
  *(In Batch 3, cash sales and cash refunds will be added: $+ \sum \text{CASH\_SALES} - \sum \text{CASH\_REFUNDS}$)*.
- **Variance Formula:**
  $$\text{variance} = \text{actual\_cash} - \text{expected\_cash}$$
  - $\text{variance} = 0$: Balanced.
  - $\text{variance} > 0$: Cash surplus (overage).
  - $\text{variance} < 0$: Cash shortage (deficit).

### 5.5 Variance Threshold & Review Policy [Approved]
- **Default Tolerance Threshold:** `0.0000` (zero tolerance). Any non-zero discrepancy triggers `VARIANCE_REVIEW`.
- **Configurable Store Tolerance [Proposed]:** Future policy may allow store-specific tolerance thresholds. For Increment 5, exact balance ($| \text{variance} | = 0$) auto-closes; any difference triggers `VARIANCE_REVIEW`.
- **Segregation of Duties (SoD):** A cashier cannot approve their own shift variance. The approver identity (`user_id`) must differ from the session's `cashier_user_id`. An attempt by the cashier to approve their own variance returns `403 Forbidden (SOD_VIOLATION)`.

### 5.6 Reopening Policy [Confirmed]
- **Strict Prohibition:** A `CLOSED` POS session can **never** be reopened under any circumstances. If operations need to resume on the terminal, a brand new POS session must be opened.

---

## 6. Proposed Database and Migration Specification

### 6.1 Migration File Identity [Approved]
- **Sequence Number:** `000010`
- **Filename:** `000010_pos_hardening.sql`
- **Advisory Lock Key:** `hashtext('nexora.pos.hardening.v1')`

### 6.2 DDL Specifications

```sql
-- Migration 000010: POS Hardening, Audit Seeds, and Variance Review
BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.pos.hardening.v1'));

-- 1. Add Variance Review Metadata Columns to pos.session
ALTER TABLE pos.session
  ADD COLUMN IF NOT EXISTS variance_approved_by uuid REFERENCES identity.user_account(id),
  ADD COLUMN IF NOT EXISTS variance_approved_at timestamptz,
  ADD COLUMN IF NOT EXISTS variance_reason text;

-- 2. Expand Status Check Constraint on pos.session
ALTER TABLE pos.session
  DROP CONSTRAINT IF EXISTS session_status_check;

ALTER TABLE pos.session
  ADD CONSTRAINT session_status_check
  CHECK (status IN ('OPENING','OPEN','ACTIVE','CLOSING','CLOSED','VARIANCE_REVIEW'));

-- 3. Realign Partial Unique Index on pos.session(terminal_id)
DROP INDEX IF EXISTS pos.ux_pos_session_terminal_open;

CREATE UNIQUE INDEX ux_pos_session_terminal_open
  ON pos.session (terminal_id)
  WHERE (status IN ('OPENING','OPEN','ACTIVE','CLOSING','VARIANCE_REVIEW'));

-- 4. Seed Canonical POS Audit Actions in audit.audit_action_catalog
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

### 6.3 Concurrency, Locking & RLS Behavior [Confirmed]
1. **Row Level Security:**
   - Both `pos.session` and `pos.cash_movement` already have RLS enabled and forced via policy `tenant_isolation_v11`.
   - Every transaction begins with `SET LOCAL app.current_tenant_id = '<uuid>'`.
2. **Optimistic Locking:**
   - Mutations on `pos.session` increment `version = version + 1` and check `WHERE id = ? AND version = ?`. Concurrency conflicts return `409 Conflict`.
3. **Pessimistic Terminal Exclusivity:**
   - The PostgreSQL partial unique index `ux_pos_session_terminal_open` acts as the physical concurrency fence. Concurrent shift openings on the same terminal immediately fail with PostgreSQL unique constraint violation (`23505`), translated by platform error handling to `409 Conflict (TERMINAL_SESSION_ACTIVE)`.

---

## 7. Proposed API Contract

### 7.1 Router Organization [Approved]
- **Canonical Prefix:** `/api/v1/pos/sessions`
- **Compatibility Alias:** `/api/v1/pos/sessions` (OpenAPI v1.0.0 paths `/pos/sessions/...` mounted identically).

### 7.2 Detailed Endpoints Specification

#### 1. Open POS Session
- **Path:** `POST /api/v1/pos/sessions/open` (Alias: `POST /pos/sessions/open`)
- **Permission:** `session.open`
- **Idempotency:** Mandatory (`Idempotency-Key`)
- **Request Body:**
  ```json
  {
    "terminal_id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "opening_cash": 100000.00,
    "currency": "IDR"
  }
  ```
- **Responses:**
  - `201 Created`: Returns `PosSession` schema.
  - `400 Bad Request`: Invalid payload syntax.
  - `409 Conflict`: Terminal already has an active session (`TERMINAL_SESSION_ACTIVE`) or idempotency key reused.
  - `422 Unprocessable Entity`: Terminal inactive, store inactive, or negative float.

#### 2. Get POS Session
- **Path:** `GET /api/v1/pos/sessions/{id}` (Alias: `GET /pos/sessions/{id}`)
- **Permission:** `session.read`
- **Responses:**
  - `200 OK`: Returns `PosSession` schema. (Note: `expected_cash` hidden if session is still `OPEN` and caller lacks supervisor permission).
  - `404 Not Found`: Session not found or belongs to another tenant.

#### 3. Record Cash In
- **Path:** `POST /api/v1/pos/sessions/{id}/cash-in` (Alias: `POST /pos/sessions/{id}/cash-in`)
- **Permission:** `cash_movement.create`
- **Idempotency:** Mandatory (`Idempotency-Key`)
- **Request Body:**
  ```json
  {
    "amount": 50000.00,
    "reason": "Mid-day petty cash addition",
    "reference": "REF-CI-001"
  }
  ```
- **Responses:**
  - `200 OK`: Returns `CashMovement` schema.
  - `409 Conflict`: Session is not in `OPEN` status.
  - `422 Unprocessable Entity`: Amount `<= 0` or missing reason.

#### 4. Record Cash Out
- **Path:** `POST /api/v1/pos/sessions/{id}/cash-out` (Alias: `POST /pos/sessions/{id}/cash-out`)
- **Permission:** `cash_movement.create`
- **Idempotency:** Mandatory (`Idempotency-Key`)
- **Request Body:**
  ```json
  {
    "amount": 20000.00,
    "reason": "Cash drop to safe",
    "reference": "SAFE-DROP-01"
  }
  ```
- **Responses:**
  - `200 OK`: Returns `CashMovement` schema.
  - `409 Conflict`: Session is not in `OPEN` status.
  - `422 Unprocessable Entity`: Amount exceeds calculated drawer cash (`INSUFFICIENT_DRAWER_CASH`).

#### 5. Close POS Session
- **Path:** `POST /api/v1/pos/sessions/{id}/close` (Alias: `POST /pos/sessions/{id}/close`)
- **Permission:** `session.close`
- **Idempotency:** Mandatory (`Idempotency-Key`)
- **Request Body:**
  ```json
  {
    "closing_cash": 130000.00,
    "comment": "End of morning shift"
  }
  ```
- **Responses:**
  - `200 OK`: Returns `PosSession` schema.
    - If variance == 0: `status = "CLOSED"`.
    - If variance != 0: `status = "VARIANCE_REVIEW"`.
  - `409 Conflict`: Session already closed or concurrent close in progress.
  - `422 Unprocessable Entity`: Negative closing cash.

#### 6. Approve Session Variance [Proposed for OpenAPI update]
- **Path:** `POST /api/v1/pos/sessions/{id}/approve-variance`
- **Permission:** `session.variance.approve`
- **Idempotency:** Mandatory (`Idempotency-Key`)
- **Request Body:**
  ```json
  {
    "reason": "Approved cash shortage due to unrecorded change discrepancy"
  }
  ```
- **Responses:**
  - `200 OK`: Returns `PosSession` with `status = "CLOSED"`.
  - `403 Forbidden`: Actor is the cashier of the shift (SoD violation).
  - `409 Conflict`: Session is not in `VARIANCE_REVIEW` status.
  - `422 Unprocessable Entity`: Missing approval reason.

#### 7. Get POS Session Summary
- **Path:** `GET /api/v1/pos/sessions/{id}/summary` (Alias: `GET /pos/sessions/{id}/summary`)
- **Permission:** `session.read`
- **Responses:**
  - `200 OK`: Returns `PosSessionSummary` schema containing session metrics, cash movements totals, and variance.

---

## 8. Permission, Audit, and Event Traceability

| Operation | Canonical Permission | Audit Action | Severity | Outbox Event | Segregation of Duties |
|---|---|---|---|---|---|
| Open Session | `session.open` | `POS_SESSION_OPENED` | NOTICE | `pos.session.opened.v1` | Cashier/Supervisor |
| Cash In | `cash_movement.create` | `POS_CASH_IN` | NOTICE | `pos.cash.movement_created.v1` | Cashier/Supervisor |
| Cash Out | `cash_movement.create` | `POS_CASH_OUT` | NOTICE | `pos.cash.movement_created.v1` | Cashier/Supervisor |
| Close (Balanced) | `session.close` | `POS_SESSION_CLOSED` | HIGH | `pos.session.closed.v1` | Shift Cashier |
| Close (Variance) | `session.close` | `POS_SESSION_VARIANCE_DETECTED` | HIGH | `pos.session.variance_flagged.v1` | Shift Cashier |
| Approve Variance | `session.variance.approve` | `POS_SESSION_VARIANCE_APPROVED` | HIGH | `pos.session.closed.v1` | **Supervisor ONLY** (`approver != cashier`) |
| Read Session | `session.read` | *(None - Read)* | - | *(None)* | Cashier/Supervisor |

---

## 9. Acceptance Criteria

### 9.1 Session Invariants & Lifecycle
- [ ] Attempting to open a session on a terminal with an active session returns `409 Conflict (TERMINAL_SESSION_ACTIVE)`.
- [ ] Attempting to open a session on a disabled terminal (`status != 'ACTIVE'`) returns `422 Unprocessable Entity`.
- [ ] Opening float `< 0` returns `422 Unprocessable Entity`.
- [ ] Session opening automatically seeds a cash movement record with `movement_type = 'OPENING'`.
- [ ] A session in `CLOSED` status rejects any subsequent cash movement or close request with `409 Conflict`.

### 9.2 Cash Drawer Calculations
- [ ] Cash in increases `expected_cash`; cash out decreases `expected_cash`.
- [ ] Cash out exceeding `expected_cash` is rejected with `422 Unprocessable Entity`.
- [ ] Cash movements without reason are rejected with `422 Unprocessable Entity`.

### 9.3 Blind Count & Variance Review
- [ ] Cashier reading an active session does not receive `expected_cash`.
- [ ] Submitting exact expected cash in close request transitions session directly to `CLOSED`.
- [ ] Submitting non-matching cash transitions session to `VARIANCE_REVIEW` and records `variance = actual_cash - expected_cash`.
- [ ] A terminal whose session is in `VARIANCE_REVIEW` cannot open a new session.
- [ ] Cashier cannot approve their own variance (`403 Forbidden`).
- [ ] Supervisor approving variance transitions session to `CLOSED`, records `variance_approved_by`, `variance_approved_at`, and `variance_reason`.

### 9.4 Security, Audit & Outbox
- [ ] Cross-tenant request is rejected by PostgreSQL RLS with empty/error result.
- [ ] Every session and cash mutation writes to `audit.audit_log` and `platform.outbox` within the same transaction.
- [ ] Replaying an identical request with the same `Idempotency-Key` returns the cached response.
- [ ] Replaying a modified payload with the same `Idempotency-Key` returns `409 Conflict (IDEMPOTENCY_KEY_REUSED)`.

---

## 10. Test Plan

### 10.1 Unit Tests (`backend/internal/pos/...`)
- State machine unit tests verifying valid transitions and rejecting illegal transitions (e.g. `CLOSED → OPEN`).
- Expected cash calculation unit tests covering multiple cash-in and cash-out interleavings.
- Blind count calculation and variance determination tests.
- Segregation of duties validation logic tests.

### 10.2 Integration Tests with Disposable PostgreSQL (`backend/internal/pos/pos_integration_test.go`)
- **Harness:** Ephemeral Dockerized PostgreSQL via `testfixture.New(t)` applying migrations `000001` through `000010`.
- **Role:** Running as unprivileged application user `nexora_app` under `NOBYPASSRLS`.
- **Test Scenarios:**
  1. *Full Happy Path:* Open shift → Cash in → Cash out → Balanced close → Session `CLOSED`.
  2. *Variance Review Path:* Open shift → Cash in → Under-count close → Session `VARIANCE_REVIEW` → Supervisor approve → Session `CLOSED`.
  3. *SoD Enforcement:* Cashier attempts `POST /approve-variance` → fails with `403 Forbidden`.
  4. *Terminal Exclusivity:* Concurrent goroutines attempt `OpenSession` on same terminal → exactly one succeeds, others get `409 Conflict`.
  5. *Optimistic Lock Conflict:* Concurrent cash movements verify version increment and conflict resolution.
  6. *Idempotency Replay:* Validates duplicate request handling and payload mutation conflict.
  7. *Audit & Outbox Verification:* Verifies row insertions in `audit.audit_log` and `platform.outbox`.
  8. *Cross-Tenant Isolation:* Direct query under Tenant B returns zero records for Tenant A's session.

---

## 11. Risks, Limitations, and Unresolved Decisions

### 11.1 Risks & Mitigations
1. **Terminal Stalling in Variance Review:** If a supervisor is unavailable, the physical terminal cannot be opened for the next cashier.
   - *Mitigation:* The design allows supervisors to review variances from any administrative terminal/portal.
2. **Offline Drawer Tracking:** In offline mode, shifts cannot be opened or closed on the server.
   - *Mitigation:* Confirmed by architecture lock: offline POS operations must occur within an already opened session.

### 11.2 Unresolved Decisions [None Blocking]
- All critical decisions (DEC-B2-001 through DEC-B2-009) have been approved and reconciled.
- The design is fully closed and ready for implementation authorization.

---

## 12. Implementation Readiness Verdict

### **VERDICT:** `READY FOR IMPLEMENTATION`

#### Rationale:
- The state machine, business rules, database schema migration `000010`, API routes, permissions, audit actions, and integration test plans are 100% defined and reconciled against existing repository evidence.
- No open decisions remain for Increment 5.
- Implementation may proceed immediately upon user authorization.
