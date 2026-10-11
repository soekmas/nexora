-- Migration 000004: Audit Subsystem Hardening
-- Findings: WS7 - Database RLS on Child Tables, Login History Immutability, Catalog Foreign Key, and Canonical Actions

BEGIN;

-- 1. Database-Level RLS on audit.data_change
ALTER TABLE audit.data_change ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.data_change FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_data_change_isolation ON audit.data_change;
CREATE POLICY tenant_data_change_isolation ON audit.data_change
  USING (EXISTS (
    SELECT 1 FROM audit.audit_log al
    WHERE al.id = audit.data_change.audit_log_id
      AND al.tenant_id = platform.current_tenant_id()
  ))
  WITH CHECK (EXISTS (
    SELECT 1 FROM audit.audit_log al
    WHERE al.id = audit.data_change.audit_log_id
      AND al.tenant_id = platform.current_tenant_id()
  ));

-- 2. Database-Level RLS on audit.security_event
ALTER TABLE audit.security_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.security_event FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_security_event_isolation ON audit.security_event;
CREATE POLICY tenant_security_event_isolation ON audit.security_event
  USING (tenant_id IS NULL OR tenant_id = platform.current_tenant_id())
  WITH CHECK (tenant_id IS NULL OR tenant_id = platform.current_tenant_id());

-- 3. Database-Level RLS on audit.login_history
ALTER TABLE audit.login_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.login_history FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_login_history_isolation ON audit.login_history;
CREATE POLICY tenant_login_history_isolation ON audit.login_history
  USING (tenant_id IS NULL OR tenant_id = platform.current_tenant_id())
  WITH CHECK (tenant_id IS NULL OR tenant_id = platform.current_tenant_id());

-- 4. Database-Level Immutability Trigger on audit.login_history
DROP TRIGGER IF EXISTS trg_login_history_immutable ON audit.login_history;
CREATE TRIGGER trg_login_history_immutable
  BEFORE UPDATE OR DELETE ON audit.login_history
  FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation();

-- 4b. Update severity check constraint on audit.audit_action_catalog
DO $$
DECLARE
  r RECORD;
BEGIN
  FOR r IN (
    SELECT conname
    FROM pg_constraint c
    JOIN pg_class t ON c.conrelid = t.oid
    JOIN pg_namespace n ON t.relnamespace = n.oid
    WHERE n.nspname = 'audit'
      AND t.relname = 'audit_action_catalog'
      AND c.contype = 'c'
      AND pg_get_constraintdef(c.oid) LIKE '%severity%'
      AND pg_get_constraintdef(c.oid) NOT LIKE '%NOTICE%'
  ) LOOP
    EXECUTE 'ALTER TABLE audit.audit_action_catalog DROP CONSTRAINT ' || quote_ident(r.conname);
    EXECUTE 'ALTER TABLE audit.audit_action_catalog ADD CONSTRAINT audit_action_catalog_severity_check CHECK (severity IN (''INFO'',''NOTICE'',''LOW'',''MEDIUM'',''WARNING'',''HIGH'',''CRITICAL''))';
  END LOOP;
END $$;

-- 5. Standardize and Seed All Canonical Action Codes in audit.audit_action_catalog
INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
  -- Identity
  ('USER_UPDATED', 'IDENTITY', 'User updated', 'NOTICE', false, true),
  ('USER_ENABLED', 'IDENTITY', 'User enabled', 'NOTICE', true, true),
  ('USER_DELETED_REQUESTED', 'IDENTITY', 'User deletion requested', 'HIGH', true, true),
  ('USER_ROLE_ASSIGNED', 'IDENTITY', 'User role assigned', 'HIGH', true, true),
  ('USER_ROLE_REVOKED', 'IDENTITY', 'User role revoked', 'HIGH', true, true),
  ('USER_SCOPE_CHANGED', 'IDENTITY', 'User scope changed', 'HIGH', true, true),
  ('MFA_SUCCESS', 'SECURITY', 'MFA successful', 'INFO', false, true),
  ('SESSION_CREATED', 'SECURITY', 'Session created', 'INFO', false, true),
  ('SESSION_REVOKED', 'SECURITY', 'Session revoked', 'HIGH', true, true),
  ('API_KEY_CREATED', 'SECURITY', 'API key created', 'HIGH', true, true),
  ('API_KEY_REVOKED', 'SECURITY', 'API key revoked', 'HIGH', true, true),
  ('BREAK_GLASS_GRANTED', 'SECURITY', 'Break glass access granted', 'CRITICAL', true, true),
  ('BREAK_GLASS_EXPIRED', 'SECURITY', 'Break glass access expired', 'HIGH', false, true),
  -- Organization
  ('COMPANY_CREATED', 'ORGANIZATION', 'Company created', 'NOTICE', false, true),
  ('COMPANY_UPDATED', 'ORGANIZATION', 'Company updated', 'NOTICE', false, true),
  ('REGION_CREATED', 'ORGANIZATION', 'Region created', 'NOTICE', false, true),
  ('REGION_UPDATED', 'ORGANIZATION', 'Region updated', 'NOTICE', false, true),
  ('STORE_CREATED', 'ORGANIZATION', 'Store created', 'NOTICE', false, true),
  ('STORE_UPDATED', 'ORGANIZATION', 'Store updated', 'NOTICE', false, true),
  ('STORE_ENABLED', 'ORGANIZATION', 'Store enabled', 'NOTICE', false, true),
  ('STORE_DISABLED', 'ORGANIZATION', 'Store disabled', 'HIGH', false, true),
  ('WAREHOUSE_CREATED', 'ORGANIZATION', 'Warehouse created', 'NOTICE', false, true),
  ('WAREHOUSE_UPDATED', 'ORGANIZATION', 'Warehouse updated', 'NOTICE', false, true),
  ('TERMINAL_REGISTERED', 'ORGANIZATION', 'Terminal registered', 'NOTICE', false, true),
  ('TERMINAL_ENABLED', 'ORGANIZATION', 'Terminal enabled', 'NOTICE', false, true),
  ('TERMINAL_DISABLED', 'ORGANIZATION', 'Terminal disabled', 'HIGH', false, true),
  ('TERMINAL_RETIRED', 'ORGANIZATION', 'Terminal retired', 'HIGH', false, true),
  ('DEVICE_REGISTERED', 'ORGANIZATION', 'Device registered', 'NOTICE', false, true),
  ('DEVICE_SUSPENDED', 'ORGANIZATION', 'Device suspended', 'HIGH', false, true),
  ('DEVICE_REVOKED', 'ORGANIZATION', 'Device revoked', 'CRITICAL', false, true),
  -- Master Data
  ('PRODUCT_CREATED', 'MASTER', 'Product created', 'NOTICE', false, true),
  ('PRODUCT_UPDATED', 'MASTER', 'Product updated', 'NOTICE', false, true),
  ('PRODUCT_ACTIVATED', 'MASTER', 'Product activated', 'NOTICE', false, true),
  ('PRODUCT_DEACTIVATED', 'MASTER', 'Product deactivated', 'HIGH', false, true),
  ('PRODUCT_VARIANT_CREATED', 'MASTER', 'Product variant created', 'NOTICE', false, true),
  ('PRODUCT_VARIANT_UPDATED', 'MASTER', 'Product variant updated', 'NOTICE', false, true),
  ('BARCODE_CREATED', 'MASTER', 'Barcode created', 'NOTICE', false, true),
  ('BARCODE_CHANGED', 'MASTER', 'Barcode changed', 'HIGH', false, true),
  ('SERIAL_REGISTERED', 'MASTER', 'Serial registered', 'NOTICE', false, true),
  ('SERIAL_STATUS_CHANGED', 'MASTER', 'Serial status changed', 'HIGH', false, true),
  ('CATEGORY_CREATED', 'MASTER', 'Category created', 'NOTICE', false, true),
  ('CATEGORY_UPDATED', 'MASTER', 'Category updated', 'NOTICE', false, true),
  ('UOM_CREATED', 'MASTER', 'Unit of measure created', 'NOTICE', false, true),
  ('UOM_UPDATED', 'MASTER', 'Unit of measure updated', 'NOTICE', false, true),
  -- Pricing & Promotions
  ('PRICE_UPDATED', 'PRICING', 'Price updated', 'HIGH', true, true),
  ('PROMOTION_DEACTIVATED', 'PROMOTION', 'Promotion deactivated', 'HIGH', false, true),
  -- Sales & Payments
  ('SALE_HELD', 'SALES', 'Sale placed on hold', 'INFO', false, true),
  ('SALE_RESUMED', 'SALES', 'Sale resumed from hold', 'INFO', false, true),
  ('PAYMENT_COMPLETED', 'PAYMENT', 'Payment completed', 'INFO', false, true),
  ('PAYMENT_VOIDED', 'PAYMENT', 'Payment voided', 'HIGH', true, true),
  ('REFUND_COMPLETED', 'PAYMENT', 'Refund completed', 'HIGH', true, true),
  -- Inventory
  ('STOCK_TRANSFER_RECEIVED', 'INVENTORY', 'Stock transfer received', 'INFO', false, true),
  ('STOCK_RESERVED', 'INVENTORY', 'Stock reserved', 'INFO', false, true),
  ('STOCK_RELEASED', 'INVENTORY', 'Stock released', 'INFO', false, true)
ON CONFLICT (code) DO NOTHING;

-- 6. Foreign Key Constraint on audit.audit_log(action)
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_audit_log_action'
  ) THEN
    ALTER TABLE audit.audit_log
      ADD CONSTRAINT fk_audit_log_action
      FOREIGN KEY (action) REFERENCES audit.audit_action_catalog(code);
  END IF;
END $$;

-- 7. Composite Performance Index on audit.audit_log
CREATE INDEX IF NOT EXISTS ix_audit_tenant_user_time
  ON audit.audit_log(tenant_id, user_id, created_at DESC);

COMMIT;
