-- Migration 000002: Add Refresh Token Support
-- Finding: WS4-01 - Persistent Refresh Token Model & Rotation

BEGIN;

CREATE TABLE IF NOT EXISTS identity.refresh_token (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  user_id uuid NOT NULL REFERENCES identity.user_account(id) ON DELETE CASCADE,
  session_id uuid NOT NULL REFERENCES identity.session(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  family_id uuid NOT NULL,
  issued_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  replaced_by_token_id uuid REFERENCES identity.refresh_token(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > issued_at)
);

CREATE INDEX IF NOT EXISTS ix_refresh_token_hash ON identity.refresh_token(token_hash);
CREATE INDEX IF NOT EXISTS ix_refresh_token_session ON identity.refresh_token(session_id);
CREATE INDEX IF NOT EXISTS ix_refresh_token_family ON identity.refresh_token(family_id);
CREATE INDEX IF NOT EXISTS ix_refresh_token_tenant ON identity.refresh_token(tenant_id);

ALTER TABLE identity.refresh_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.refresh_token FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation_v11 ON identity.refresh_token;
CREATE POLICY tenant_isolation_v11 ON identity.refresh_token
  USING (tenant_id = platform.current_tenant_id())
  WITH CHECK (tenant_id = platform.current_tenant_id());

DROP TRIGGER IF EXISTS trg_refresh_token_session_fk ON identity.refresh_token;
CREATE TRIGGER trg_refresh_token_session_fk
  BEFORE INSERT OR UPDATE OF tenant_id, session_id ON identity.refresh_token
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('identity.session', 'session_id');

DROP TRIGGER IF EXISTS trg_refresh_token_user_fk ON identity.refresh_token;
CREATE TRIGGER trg_refresh_token_user_fk
  BEFORE INSERT OR UPDATE OF tenant_id, user_id ON identity.refresh_token
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('identity.user_account', 'user_id');

COMMIT;
