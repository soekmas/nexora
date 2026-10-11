-- Migration 000012: Promotion Engine Hardening, Audit Seeds, and Permissions
-- Implements DEC-B2-005, DEC-B2-006, DEC-B2-009, and Increment 7 Stage 1 database requirements.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.promotion.hardening.v1'));

-- 1. Align Status Check Constraint with Canonical Lifecycle
-- Canonical states: DRAFT, PENDING_APPROVAL, APPROVED, SCHEDULED, ACTIVE, PAUSED, EXPIRED, REJECTED, CANCELLED
ALTER TABLE promotion.promotion
  DROP CONSTRAINT IF EXISTS promotion_status_check;

ALTER TABLE promotion.promotion
  DROP CONSTRAINT IF EXISTS ck_promotion_status;

-- Migrate legacy INACTIVE status to canonical PAUSED
UPDATE promotion.promotion
  SET status = 'PAUSED'
  WHERE status = 'INACTIVE';

ALTER TABLE promotion.promotion
  ADD CONSTRAINT ck_promotion_status
  CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','SCHEDULED','ACTIVE','PAUSED','EXPIRED','REJECTED','CANCELLED'));

-- 2. Align Stacking Mode Check Constraint with Canonical Options
ALTER TABLE promotion.promotion
  DROP CONSTRAINT IF EXISTS promotion_stacking_mode_check;

ALTER TABLE promotion.promotion
  DROP CONSTRAINT IF EXISTS ck_promotion_stacking_mode;

ALTER TABLE promotion.promotion
  ADD CONSTRAINT ck_promotion_stacking_mode
  CHECK (stacking_mode IN ('STACKABLE','EXCLUSIVE','BEST_ONLY'));

-- 3. Add Missing Governance and Quota Columns to promotion.promotion
ALTER TABLE promotion.promotion
  ADD COLUMN IF NOT EXISTS approved_by uuid,
  ADD COLUMN IF NOT EXISTS approved_at timestamptz,
  ADD COLUMN IF NOT EXISTS usage_limit integer,
  ADD COLUMN IF NOT EXISTS usage_count integer NOT NULL DEFAULT 0;

ALTER TABLE promotion.promotion
  DROP CONSTRAINT IF EXISTS ck_promotion_usage_limit;

ALTER TABLE promotion.promotion
  ADD CONSTRAINT ck_promotion_usage_limit
  CHECK (usage_limit IS NULL OR usage_limit > 0);

ALTER TABLE promotion.promotion
  DROP CONSTRAINT IF EXISTS ck_promotion_usage_count;

ALTER TABLE promotion.promotion
  ADD CONSTRAINT ck_promotion_usage_count
  CHECK (usage_count >= 0);

-- 4. Cross-Tenant FK Trigger Guard for promotion.promotion_version
DROP TRIGGER IF EXISTS trg_same_tenant_promotion_version ON promotion.promotion_version;
CREATE TRIGGER trg_same_tenant_promotion_version
  BEFORE INSERT OR UPDATE OF tenant_id, promotion_id ON promotion.promotion_version
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('promotion_id', 'promotion.promotion');

-- 5. Foreign Key and Sequence Lookup Indexes
CREATE INDEX IF NOT EXISTS ix_promotion_condition_promo
  ON promotion.promotion_condition (promotion_id, sequence_no);

CREATE INDEX IF NOT EXISTS ix_promotion_reward_promo
  ON promotion.promotion_reward (promotion_id, sequence_no);

CREATE INDEX IF NOT EXISTS ix_promotion_app_tenant_order
  ON promotion.promotion_application (tenant_id, sales_order_id);

CREATE INDEX IF NOT EXISTS ix_promotion_app_tenant_promo
  ON promotion.promotion_application (tenant_id, promotion_id);

-- 6. Canonical Evaluation Indexes (Compound and Partial)
-- Full compound index covering tenant, status, priority (descending), and effective date window
CREATE INDEX IF NOT EXISTS ix_promotion_evaluation_priority
  ON promotion.promotion (tenant_id, status, priority DESC, effective_from, effective_to);

-- Targeted partial index optimized for active promotion resolution at evaluation/checkout time
CREATE INDEX IF NOT EXISTS ix_promotion_active_eval_lookup
  ON promotion.promotion (tenant_id, priority DESC, effective_from, effective_to)
  WHERE (status = 'ACTIVE');

-- 7. Seed Canonical Promotion Audit Actions in audit.audit_action_catalog
INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
  ('PROMOTION_CREATED',         'PROMOTION', 'Promotion created',                  'NOTICE', false, true),
  ('PROMOTION_UPDATED',         'PROMOTION', 'Promotion updated',                  'HIGH',   false, true),
  ('PROMOTION_SUBMITTED',       'PROMOTION', 'Promotion submitted for approval',   'NOTICE', false, true),
  ('PROMOTION_APPROVED',        'PROMOTION', 'Promotion approved',                 'HIGH',   false, true),
  ('PROMOTION_REJECTED',        'PROMOTION', 'Promotion rejected',                 'NOTICE', false, true),
  ('PROMOTION_ACTIVATED',       'PROMOTION', 'Promotion activated',                'HIGH',   false, true),
  ('PROMOTION_PAUSED',          'PROMOTION', 'Promotion paused',                   'HIGH',   false, true),
  ('PROMOTION_EXPIRED',         'PROMOTION', 'Promotion expired',                  'INFO',   false, true),
  ('PROMOTION_VERSION_CREATED', 'PROMOTION', 'Promotion version created',          'NOTICE', false, true),
  ('PROMOTION_RULE_CHANGED',    'PROMOTION', 'Promotion rule changed',             'HIGH',   false, true),
  ('PROMOTION_APPLIED',         'PROMOTION', 'Promotion applied to order',         'INFO',   false, true)
ON CONFLICT (code) DO UPDATE SET
  module = EXCLUDED.module,
  description = EXCLUDED.description,
  severity = EXCLUDED.severity,
  requires_reason = EXCLUDED.requires_reason,
  active = EXCLUDED.active;

-- 8. Seed Canonical Promotion Permissions in identity.permission
INSERT INTO identity.permission (code, name, module, action) VALUES
  ('promotion.read',     'Read Promotions',        'PROMOTION', 'PROMOTION_READ'),
  ('promotion.create',   'Create Promotion',       'PROMOTION', 'PROMOTION_CREATE'),
  ('promotion.update',   'Update Promotion',       'PROMOTION', 'PROMOTION_UPDATE'),
  ('promotion.submit',   'Submit Promotion',       'PROMOTION', 'PROMOTION_SUBMIT'),
  ('promotion.approve',  'Approve Promotion',      'PROMOTION', 'PROMOTION_APPROVE'),
  ('promotion.activate', 'Activate Promotion',     'PROMOTION', 'PROMOTION_ACTIVATE'),
  ('promotion.pause',    'Pause Promotion',        'PROMOTION', 'PROMOTION_PAUSE'),
  ('promotion.expire',   'Expire Promotion',       'PROMOTION', 'PROMOTION_EXPIRE'),
  ('promotion.evaluate', 'Evaluate Promotions',    'PROMOTION', 'PROMOTION_EVALUATE'),
  ('promotion.schedule', 'Schedule Promotion',     'PROMOTION', 'PROMOTION_SCHEDULE')
ON CONFLICT (code) DO NOTHING;

COMMIT;
