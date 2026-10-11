-- Migration 000010: POS Hardening, Audit Seeds, and Variance Review
-- Implements DEC-B2-001, DEC-B2-002, and CORR-POS-002.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.pos.hardening.v1'));

-- 1. Add Variance Review Metadata Columns to pos.session
ALTER TABLE pos.session
  ADD COLUMN IF NOT EXISTS variance_approved_by uuid REFERENCES identity.user_account(id),
  ADD COLUMN IF NOT EXISTS variance_approved_at timestamptz,
  ADD COLUMN IF NOT EXISTS variance_reason text;

-- 2. Attach Same-Tenant Ownership Guard for variance_approved_by
DROP TRIGGER IF EXISTS trg_same_tenant_varianceapprovedby ON pos.session;
CREATE TRIGGER trg_same_tenant_varianceapprovedby
  BEFORE INSERT OR UPDATE OF tenant_id, variance_approved_by ON pos.session
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('variance_approved_by', 'identity.user_account');

-- 3. Expand Status Check Constraint on pos.session (DEC-B2-001)
DO $$
DECLARE
  r RECORD;
BEGIN
  FOR r IN (
    SELECT conname
    FROM pg_constraint
    WHERE conrelid = 'pos.session'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%status%IN%'
  ) LOOP
    EXECUTE 'ALTER TABLE pos.session DROP CONSTRAINT ' || quote_ident(r.conname);
  END LOOP;
END $$;

ALTER TABLE pos.session
  ADD CONSTRAINT session_status_check
  CHECK (status IN ('OPENING','OPEN','ACTIVE','CLOSING','CLOSED','VARIANCE_REVIEW'));

-- 4. Non-Negative Expected Cash Constraint (CORR-POS-002)
ALTER TABLE pos.session
  DROP CONSTRAINT IF EXISTS pos_session_expected_cash_check;

ALTER TABLE pos.session
  ADD CONSTRAINT pos_session_expected_cash_check
  CHECK (expected_cash >= 0);

-- 5. Realign Partial Unique Index on pos.session(terminal_id) (DEC-B2-001)
DROP INDEX IF EXISTS pos.ux_pos_session_terminal_open;

CREATE UNIQUE INDEX ux_pos_session_terminal_open
  ON pos.session (terminal_id)
  WHERE (status IN ('OPENING','OPEN','ACTIVE','CLOSING','VARIANCE_REVIEW'));

-- 6. Seed Canonical POS Audit Actions in audit.audit_action_catalog (DEC-B2-002)
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
