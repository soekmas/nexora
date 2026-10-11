-- Migration 000011: Pricing Engine Hardening, Audit Seeds, and Permissions
-- Implements DEC-P2-016 through DEC-P2-022 and Q-P2-001 through Q-P2-003.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.pricing.hardening.v1'));

-- 1. Add is_default and version to pricing.price_list
ALTER TABLE pricing.price_list
  ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

-- 2. Enforce Tenant-Wide Scope for Default Price Lists (company_id IS NULL)
ALTER TABLE pricing.price_list
  DROP CONSTRAINT IF EXISTS ck_price_list_default_tenant_wide;

ALTER TABLE pricing.price_list
  ADD CONSTRAINT ck_price_list_default_tenant_wide
  CHECK (NOT is_default OR company_id IS NULL);

-- 3. Named Status Check Constraint on pricing.price_list
ALTER TABLE pricing.price_list
  DROP CONSTRAINT IF EXISTS price_list_status_check;

ALTER TABLE pricing.price_list
  ADD CONSTRAINT price_list_status_check
  CHECK (status IN ('ACTIVE','INACTIVE','ARCHIVED'));

-- 4. Partial Unique Index: Exactly One Active Default per Tenant/Currency/Channel
CREATE UNIQUE INDEX IF NOT EXISTS ux_price_list_active_default
  ON pricing.price_list (tenant_id, currency, channel)
  WHERE (is_default = true AND status = 'ACTIVE');

-- 5. Add variant_id and version to pricing.price_rule
ALTER TABLE pricing.price_rule
  ADD COLUMN IF NOT EXISTS variant_id uuid REFERENCES master.product_variant(id) ON DELETE RESTRICT,
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

-- 6. Named Status Check Constraint on pricing.price_rule
ALTER TABLE pricing.price_rule
  DROP CONSTRAINT IF EXISTS price_rule_status_check;

ALTER TABLE pricing.price_rule
  ADD CONSTRAINT price_rule_status_check
  CHECK (status IN ('DRAFT','APPROVED','ACTIVE','INACTIVE','EXPIRED'));

-- 7. Trigger Function: Cross-Tenant and Product-Variant Consistency Check
CREATE OR REPLACE FUNCTION pricing.enforce_price_rule_variant_product_match()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_product_id uuid;
BEGIN
  IF NEW.variant_id IS NOT NULL THEN
    SELECT product_id INTO v_product_id
    FROM master.product_variant
    WHERE id = NEW.variant_id AND tenant_id = NEW.tenant_id;
    
    IF v_product_id IS NULL THEN
      RAISE EXCEPTION 'Variant % does not exist or does not belong to tenant %', NEW.variant_id, NEW.tenant_id;
    END IF;
    IF v_product_id <> NEW.product_id THEN
      RAISE EXCEPTION 'Variant % does not belong to product %', NEW.variant_id, NEW.product_id;
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_price_rule_variant_product_match ON pricing.price_rule;
CREATE TRIGGER trg_price_rule_variant_product_match
  BEFORE INSERT OR UPDATE OF tenant_id, product_id, variant_id ON pricing.price_rule
  FOR EACH ROW EXECUTE FUNCTION pricing.enforce_price_rule_variant_product_match();

-- 8. Same-Tenant FK Guard for variant_id
DROP TRIGGER IF EXISTS trg_same_tenant_pricerule_variant ON pricing.price_rule;
CREATE TRIGGER trg_same_tenant_pricerule_variant
  BEFORE INSERT OR UPDATE OF tenant_id, variant_id ON pricing.price_rule
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('variant_id', 'master.product_variant');

-- 9. Compound Lookup Indexes for Pricing Engine
CREATE INDEX IF NOT EXISTS ix_price_rule_variant_lookup
  ON pricing.price_rule (tenant_id, variant_id, price_list_id, status, effective_from, effective_to);

CREATE INDEX IF NOT EXISTS ix_price_rule_list_status
  ON pricing.price_rule (tenant_id, price_list_id, status);

-- 10. Seed Missing Canonical Audit Actions in audit.audit_action_catalog
INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
  ('PRICE_SUBMITTED',   'PRICING', 'Price rule submitted for approval', 'NOTICE', false, true),
  ('PRICE_APPROVED',    'PRICING', 'Price rule approved',               'HIGH',   false, true),
  ('PRICE_REJECTED',    'PRICING', 'Price rule rejected',               'NOTICE', false, true),
  ('PRICE_ACTIVATED',   'PRICING', 'Price rule activated',              'HIGH',   false, true),
  ('PRICE_DEACTIVATED', 'PRICING', 'Price rule deactivated',            'HIGH',   false, true),
  ('PRICE_EXPIRED',     'PRICING', 'Price rule expired',                'INFO',   false, true)
ON CONFLICT (code) DO NOTHING;

-- 11. Seed Canonical Pricing Permissions in identity.permission
INSERT INTO identity.permission (code, name, module, action) VALUES
  ('price.read',            'Read Price Lists and Quotes', 'PRICING', 'PRICE_READ'),
  ('price_list.create',     'Create Price List',           'PRICING', 'PRICE_LIST_CREATE'),
  ('price_list.update',     'Update Price List',           'PRICING', 'PRICE_LIST_UPDATE'),
  ('price_rule.read',       'Read Price Rules',            'PRICING', 'PRICE_RULE_READ'),
  ('price_rule.create',     'Create Price Rule',           'PRICING', 'PRICE_RULE_CREATE'),
  ('price_rule.update',     'Update Price Rule',           'PRICING', 'PRICE_RULE_UPDATE'),
  ('price_rule.submit',     'Submit Price Rule',           'PRICING', 'PRICE_RULE_SUBMIT'),
  ('price_rule.approve',    'Approve Price Rule',          'PRICING', 'PRICE_RULE_APPROVE'),
  ('price_rule.activate',   'Activate Price Rule',         'PRICING', 'PRICE_RULE_ACTIVATE'),
  ('price_rule.deactivate', 'Deactivate Price Rule',       'PRICING', 'PRICE_RULE_DEACTIVATE')
ON CONFLICT (code) DO NOTHING;

COMMIT;
