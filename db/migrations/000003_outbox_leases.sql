-- Migration 000003: Outbox Persistent Leases & Delivery Decoupling
-- Finding: WS6 - Persistent Claims, Lease Expiry, and At-Least-Once Reliability

BEGIN;

-- 1. Replace status check constraint to include 'PROCESSING'
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN (
        SELECT conname
        FROM pg_constraint c
        JOIN pg_class t ON c.conrelid = t.oid
        JOIN pg_namespace n ON t.relnamespace = n.oid
        WHERE n.nspname = 'integration'
          AND t.relname = 'outbox_event'
          AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) LIKE '%status%'
    ) LOOP
        EXECUTE 'ALTER TABLE integration.outbox_event DROP CONSTRAINT ' || quote_ident(r.conname);
    END LOOP;
END $$;

ALTER TABLE integration.outbox_event
  ADD CONSTRAINT outbox_event_status_check
  CHECK (status IN ('PENDING', 'PROCESSING', 'PUBLISHED', 'FAILED', 'DEAD_LETTER'));

-- 2. Add columns for persistent claim ownership, lease expiry, and next attempt scheduling
ALTER TABLE integration.outbox_event
  ADD COLUMN IF NOT EXISTS claimed_by text,
  ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz DEFAULT now();

-- 3. Composite index for claimable events with SKIP LOCKED
CREATE INDEX IF NOT EXISTS ix_outbox_claimable
  ON integration.outbox_event(status, next_attempt_at, lease_expires_at);

-- 4. Composite index for delivery attempts (ordered attempt lookup)
CREATE INDEX IF NOT EXISTS ix_delivery_attempt_event_attempt
  ON integration.delivery_attempt(event_id, attempt_no DESC);

COMMIT;
