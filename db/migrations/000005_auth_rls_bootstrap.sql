-- Migration 000005: Auth token resolution under tenant RLS.
-- The application cannot establish RLS from a client-supplied tenant ID.
-- These two resolvers accept only a stored token hash and return only its tenant ID.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.auth.rls.bootstrap.v1'));

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexora_auth_resolver') THEN
    CREATE ROLE nexora_auth_resolver
      NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
  END IF;
END
$$;

ALTER ROLE nexora_auth_resolver
  NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexora_app') THEN
    RAISE EXCEPTION 'nexora_app must exist before applying auth RLS bootstrap migration';
  END IF;
  IF pg_has_role('nexora_app', 'nexora_auth_resolver', 'MEMBER') THEN
    RAISE EXCEPTION 'nexora_app must not be a member of nexora_auth_resolver';
  END IF;
END
$$;

GRANT USAGE ON SCHEMA identity, platform TO nexora_auth_resolver;
GRANT SELECT (tenant_id, token_hash) ON identity.session TO nexora_auth_resolver;
GRANT SELECT (tenant_id, token_hash) ON identity.refresh_token TO nexora_auth_resolver;

-- Reassert the baseline tenant policies on auth-related tenant tables so this
-- forward migration is independently testable against the auth schema fixture.
DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'user_account', 'role', 'user_scope', 'session', 'refresh_token'
  ] LOOP
    EXECUTE format('ALTER TABLE identity.%I ENABLE ROW LEVEL SECURITY', table_name);
    EXECUTE format('ALTER TABLE identity.%I FORCE ROW LEVEL SECURITY', table_name);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation_v11 ON identity.%I', table_name);
    EXECUTE format(
      'CREATE POLICY tenant_isolation_v11 ON identity.%I USING (tenant_id = platform.current_tenant_id()) WITH CHECK (tenant_id = platform.current_tenant_id())',
      table_name
    );
  END LOOP;
END
$$;

DROP POLICY IF EXISTS auth_resolver_lookup ON identity.session;
CREATE POLICY auth_resolver_lookup ON identity.session
  FOR SELECT TO nexora_auth_resolver USING (true);

DROP POLICY IF EXISTS auth_resolver_lookup ON identity.refresh_token;
CREATE POLICY auth_resolver_lookup ON identity.refresh_token
  FOR SELECT TO nexora_auth_resolver USING (true);

-- Correct the v1.0 refresh-token trigger arguments. The trigger function expects
-- (referencing_column, referenced_table), matching the baseline call convention.
DROP TRIGGER IF EXISTS trg_refresh_token_session_fk ON identity.refresh_token;
CREATE TRIGGER trg_refresh_token_session_fk
  BEFORE INSERT OR UPDATE OF tenant_id, session_id ON identity.refresh_token
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('session_id', 'identity.session');

DROP TRIGGER IF EXISTS trg_refresh_token_user_fk ON identity.refresh_token;
CREATE TRIGGER trg_refresh_token_user_fk
  BEFORE INSERT OR UPDATE OF tenant_id, user_id ON identity.refresh_token
  FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk('user_id', 'identity.user_account');

CREATE OR REPLACE FUNCTION platform.resolve_session_tenant(p_token_hash text)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
  SELECT s.tenant_id
  FROM identity.session AS s
  WHERE s.token_hash = p_token_hash
  LIMIT 1
$$;

ALTER FUNCTION platform.resolve_session_tenant(text) OWNER TO nexora_auth_resolver;
REVOKE ALL ON FUNCTION platform.resolve_session_tenant(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_session_tenant(text) TO nexora_app;

CREATE OR REPLACE FUNCTION platform.resolve_refresh_token_tenant(p_token_hash text)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
  SELECT rt.tenant_id
  FROM identity.refresh_token AS rt
  WHERE rt.token_hash = p_token_hash
  LIMIT 1
$$;

ALTER FUNCTION platform.resolve_refresh_token_tenant(text) OWNER TO nexora_auth_resolver;
REVOKE ALL ON FUNCTION platform.resolve_refresh_token_tenant(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_refresh_token_tenant(text) TO nexora_app;

COMMIT;
