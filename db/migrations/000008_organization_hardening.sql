-- Migration 000008: Organization Schema Hardening
-- 1. Enable Row Level Security and establish tenant isolation policy on organization.region
-- 2. Expand organization.terminal status constraint to include DISABLED and RETIRED per state machine catalog

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.organization.hardening.v1'));

-- 1. Region RLS Enablement & Policy
ALTER TABLE organization.region ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_region_isolation ON organization.region;
CREATE POLICY tenant_region_isolation ON organization.region
  USING (tenant_id = platform.current_tenant_id())
  WITH CHECK (tenant_id = platform.current_tenant_id());

-- 2. Terminal Status Constraint Expansion
ALTER TABLE organization.terminal DROP CONSTRAINT IF EXISTS terminal_status_check;
ALTER TABLE organization.terminal ADD CONSTRAINT terminal_status_check
  CHECK (status IN ('ACTIVE', 'INACTIVE', 'MAINTENANCE', 'DISABLED', 'RETIRED'));

COMMIT;
