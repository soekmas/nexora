-- Migration 000007: CRM Audit Action Seeds, Permissions, and Schema Hardening
-- Implements DEC-B1-014, DEC-B1-015, and DEC-B1-017.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.crm.migration.v1'));

-- 1. Schema Hardening on crm.customer_group (DEC-B1-014)
ALTER TABLE crm.customer_group
  ADD COLUMN IF NOT EXISTS status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

-- 2. Partial Unique Index on crm.customer_address (DEC-B1-017)
CREATE UNIQUE INDEX IF NOT EXISTS ux_customer_default_address
  ON crm.customer_address (tenant_id, customer_id)
  WHERE (is_default = true);

-- 3. Seed Canonical CRM Audit Actions
INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
  ('CUSTOMER_CREATED', 'CRM', 'Customer created', 'NOTICE', false, true),
  ('CUSTOMER_UPDATED', 'CRM', 'Customer updated', 'NOTICE', false, true),
  ('CUSTOMER_DISABLED', 'CRM', 'Customer disabled', 'HIGH', false, true),
  ('CUSTOMER_ADDRESS_CREATED', 'CRM', 'Customer address created', 'NOTICE', false, true),
  ('CUSTOMER_ADDRESS_UPDATED', 'CRM', 'Customer address updated', 'NOTICE', false, true),
  ('CUSTOMER_ADDRESS_DELETED', 'CRM', 'Customer address deleted', 'HIGH', false, true),
  ('CUSTOMER_GROUP_CHANGED', 'CRM', 'Customer group changed', 'NOTICE', false, true),
  ('CUSTOMER_GROUP_CREATED', 'CRM', 'Customer group created', 'NOTICE', false, true),
  ('CUSTOMER_GROUP_UPDATED', 'CRM', 'Customer group updated', 'NOTICE', false, true)
ON CONFLICT (code) DO NOTHING;

-- 4. Seed Dedicated Customer Group Permissions (DEC-B1-015)
INSERT INTO identity.permission (code, name, module, action) VALUES
  ('customer_group.read', 'Read Customer Group', 'CRM', 'CUSTOMER_GROUP_READ'),
  ('customer_group.create', 'Create Customer Group', 'CRM', 'CUSTOMER_GROUP_CREATE'),
  ('customer_group.update', 'Update Customer Group', 'CRM', 'CUSTOMER_GROUP_UPDATE')
ON CONFLICT (code) DO NOTHING;

COMMIT;
