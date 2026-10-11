-- Migration 000009: Master Data Hardening
-- Seeds canonical Brand audit actions and fine-grained reference data permissions.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.master.hardening.v1'));

-- 1. Brand Audit Action Catalog Seeds (DEC-B1-008)
INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
  ('BRAND_CREATED', 'MASTER', 'Brand created', 'NOTICE', false, true),
  ('BRAND_UPDATED', 'MASTER', 'Brand updated', 'NOTICE', false, true)
ON CONFLICT (code) DO NOTHING;

-- 2. Master Reference Data Permissions (DEC-B1-009)
INSERT INTO identity.permission (code, name, module, action) VALUES
  ('brand.create', 'Create Product Brand', 'MASTER', 'BRAND_CREATE'),
  ('brand.update', 'Update Product Brand', 'MASTER', 'BRAND_UPDATE'),
  ('category.create', 'Create Product Category', 'MASTER', 'CATEGORY_CREATE'),
  ('category.update', 'Update Product Category', 'MASTER', 'CATEGORY_UPDATE'),
  ('uom.create', 'Create Unit of Measure', 'MASTER', 'UOM_CREATE'),
  ('uom.update', 'Update Unit of Measure', 'MASTER', 'UOM_UPDATE')
ON CONFLICT (code) DO NOTHING;

COMMIT;
