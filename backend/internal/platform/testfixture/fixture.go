// Package testfixture provides synthetic platform test entities and operations for infrastructure
// testing without polluting or depending on business domain packages.
package testfixture

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"example.com/nexora/backend/internal/platform/postgres"
)

const fixtureAdvisoryLockID = int64(749281749)

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

func executeWithAdvisoryLock(ctx context.Context, dbtx postgres.DBTX, queries []string) error {
	if b, ok := dbtx.(beginner); ok {
		tx, err := b.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for fixture setup: %w", postgres.MapError(err))
		}
		defer func() {
			_ = tx.Rollback(ctx)
		}()

		if _, err := tx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", fixtureAdvisoryLockID)); err != nil {
			return fmt.Errorf("acquire fixture advisory lock: %w", postgres.MapError(err))
		}

		for _, q := range queries {
			if _, err := tx.Exec(ctx, q); err != nil {
				return fmt.Errorf("exec fixture query: %w", postgres.MapError(err))
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit fixture setup tx: %w", postgres.MapError(err))
		}
		return nil
	}

	if _, err := dbtx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", fixtureAdvisoryLockID)); err != nil {
		return fmt.Errorf("acquire fixture advisory lock: %w", postgres.MapError(err))
	}

	for _, q := range queries {
		if _, err := dbtx.Exec(ctx, q); err != nil {
			return fmt.Errorf("exec fixture query: %w", postgres.MapError(err))
		}
	}
	return nil
}

// TestItem represents a synthetic tenant-isolated test fixture entity.
type TestItem struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Name     string
}

// SetupTestTable creates the platform_test schema, table, and RLS policy matching the baseline architecture.
func SetupTestTable(ctx context.Context, dbtx postgres.DBTX) error {
	queries := []string{
		`DO $$
		BEGIN
			IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'nexora_app') THEN
				CREATE ROLE nexora_app WITH LOGIN PASSWORD 'app_secret_pass' NOSUPERUSER NOBYPASSRLS;
			END IF;
		END
		$$`,
		`CREATE SCHEMA IF NOT EXISTS platform_test`,
		`GRANT ALL ON SCHEMA platform_test TO PUBLIC`,
		`CREATE TABLE IF NOT EXISTS platform_test.test_items (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			name varchar(100) NOT NULL
		)`,
		`GRANT ALL ON ALL TABLES IN SCHEMA platform_test TO PUBLIC`,
		`ALTER TABLE platform_test.test_items ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE platform_test.test_items FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation_test ON platform_test.test_items`,
		`CREATE POLICY tenant_isolation_test ON platform_test.test_items
			USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
			WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`,
	}

	return executeWithAdvisoryLock(ctx, dbtx, queries)
}

// SetupIdentityTables creates organization and identity schemas and tables matching the baseline migration.
func SetupIdentityTables(ctx context.Context, dbtx postgres.DBTX) error {
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS citext`,
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE SCHEMA IF NOT EXISTS organization`,
		`CREATE SCHEMA IF NOT EXISTS identity`,
		`CREATE SCHEMA IF NOT EXISTS platform`,
		`GRANT ALL ON SCHEMA organization, identity, platform TO PUBLIC`,
		`CREATE OR REPLACE FUNCTION platform.current_tenant_id()
		RETURNS uuid LANGUAGE sql STABLE AS $$
			SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
		$$`,
		`CREATE TABLE IF NOT EXISTS organization.company (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			code varchar(50) NOT NULL,
			name varchar(150) NOT NULL,
			currency varchar(3) NOT NULL DEFAULT 'IDR',
			is_active boolean NOT NULL DEFAULT true,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS organization.store (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			company_id uuid NOT NULL REFERENCES organization.company(id),
			code varchar(50) NOT NULL,
			name varchar(150) NOT NULL,
			is_active boolean NOT NULL DEFAULT true,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS organization.warehouse (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			company_id uuid NOT NULL REFERENCES organization.company(id),
			code varchar(50) NOT NULL,
			name varchar(150) NOT NULL,
			warehouse_type varchar(30) NOT NULL DEFAULT 'STORE_BACKROOM',
			is_active boolean NOT NULL DEFAULT true,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS organization.terminal (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			store_id uuid NOT NULL REFERENCES organization.store(id),
			device_identifier varchar(100),
			terminal_code varchar(50) NOT NULL,
			name varchar(100) NOT NULL,
			status varchar(20) NOT NULL DEFAULT 'ACTIVE',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS identity.user_account (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			username citext NOT NULL,
			email citext,
			password_hash text,
			display_name varchar(150) NOT NULL,
			status varchar(20) NOT NULL DEFAULT 'ACTIVE'
				CHECK (status IN ('ACTIVE','LOCKED','DISABLED','PENDING')),
			failed_login_count integer NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
			locked_until timestamptz,
			last_login_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			created_by uuid,
			updated_by uuid,
			version bigint NOT NULL DEFAULT 1,
			UNIQUE (tenant_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS identity.role (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			code varchar(50) NOT NULL,
			name varchar(100) NOT NULL,
			description text,
			is_system boolean NOT NULL DEFAULT false,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			created_by uuid,
			updated_by uuid,
			version bigint NOT NULL DEFAULT 1,
			UNIQUE (tenant_id, code)
		)`,
		`CREATE TABLE IF NOT EXISTS identity.permission (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			code varchar(100) NOT NULL UNIQUE,
			name varchar(150) NOT NULL,
			module varchar(50) NOT NULL,
			action varchar(50) NOT NULL,
			UNIQUE (module, action)
		)`,
		`CREATE TABLE IF NOT EXISTS identity.user_role (
			user_id uuid NOT NULL REFERENCES identity.user_account(id),
			role_id uuid NOT NULL REFERENCES identity.role(id),
			assigned_at timestamptz NOT NULL DEFAULT now(),
			assigned_by uuid,
			PRIMARY KEY (user_id, role_id)
		)`,
		`CREATE TABLE IF NOT EXISTS identity.role_permission (
			role_id uuid NOT NULL REFERENCES identity.role(id),
			permission_id uuid NOT NULL REFERENCES identity.permission(id),
			PRIMARY KEY (role_id, permission_id)
		)`,
		`CREATE TABLE IF NOT EXISTS identity.user_scope (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			user_id uuid NOT NULL REFERENCES identity.user_account(id),
			company_id uuid REFERENCES organization.company(id),
			region_id uuid,
			store_id uuid REFERENCES organization.store(id),
			warehouse_id uuid REFERENCES organization.warehouse(id),
			terminal_id uuid REFERENCES organization.terminal(id),
			scope_type varchar(20) NOT NULL
				CHECK (scope_type IN ('COMPANY','REGION','STORE','WAREHOUSE','TERMINAL')),
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS identity.session (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			user_id uuid NOT NULL REFERENCES identity.user_account(id),
			token_hash text NOT NULL UNIQUE,
			ip_address inet,
			user_agent text,
			issued_at timestamptz NOT NULL DEFAULT now(),
			expires_at timestamptz NOT NULL,
			revoked_at timestamptz,
			last_seen_at timestamptz,
			CHECK (expires_at > issued_at)
		)`,
		`CREATE TABLE IF NOT EXISTS identity.refresh_token (
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
		)`,
		`CREATE OR REPLACE FUNCTION platform.resolve_session_tenant(p_token_hash text)
		RETURNS uuid LANGUAGE sql STABLE AS $$
			SELECT tenant_id FROM identity.session WHERE token_hash = p_token_hash LIMIT 1
		$$`,
		`CREATE OR REPLACE FUNCTION platform.resolve_refresh_token_tenant(p_token_hash text)
		RETURNS uuid LANGUAGE sql STABLE AS $$
			SELECT tenant_id FROM identity.refresh_token WHERE token_hash = p_token_hash LIMIT 1
		$$`,
		`GRANT ALL ON ALL TABLES IN SCHEMA organization, identity TO PUBLIC`,
	}

	return executeWithAdvisoryLock(ctx, dbtx, queries)
}

// InsertTestItem inserts a synthetic test item.
func InsertTestItem(ctx context.Context, dbtx postgres.DBTX, item TestItem) error {
	query := `INSERT INTO platform_test.test_items (id, tenant_id, name) VALUES ($1, $2, $3)`
	_, err := dbtx.Exec(ctx, query, item.ID, item.TenantID, item.Name)
	if err != nil {
		return postgres.MapError(err)
	}
	return nil
}

// CountTestItems counts visible test items under the current RLS context.
func CountTestItems(ctx context.Context, dbtx postgres.DBTX) (int, error) {
	var count int
	row := dbtx.QueryRow(ctx, `SELECT count(*) FROM platform_test.test_items`)
	if err := row.Scan(&count); err != nil {
		return 0, postgres.MapError(err)
	}
	return count, nil
}

// SetupIdempotencyTable creates the platform.idempotency_key table and RLS policies matching baseline migration.
func SetupIdempotencyTable(ctx context.Context, dbtx postgres.DBTX) error {
	queries := []string{
		`CREATE SCHEMA IF NOT EXISTS platform`,
		`GRANT ALL ON SCHEMA platform TO PUBLIC`,
		`CREATE TABLE IF NOT EXISTS platform.idempotency_key (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			key varchar(200) NOT NULL,
			operation varchar(150) NOT NULL,
			request_hash varchar(128) NOT NULL,
			response_status integer,
			response_body jsonb,
			created_at timestamptz NOT NULL DEFAULT now(),
			expires_at timestamptz,
			UNIQUE (tenant_id, key, operation)
		)`,
		`GRANT ALL ON platform.idempotency_key TO PUBLIC`,
		`ALTER TABLE platform.idempotency_key ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE platform.idempotency_key FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_idempotency_isolation ON platform.idempotency_key`,
		`CREATE POLICY tenant_idempotency_isolation ON platform.idempotency_key
			USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
			WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`,
	}

	return executeWithAdvisoryLock(ctx, dbtx, queries)
}

// SetupOutboxTables creates integration.outbox_event and integration.delivery_attempt tables
// matching the approved schema, leases, and RLS policies.
func SetupOutboxTables(ctx context.Context, dbtx postgres.DBTX) error {
	queries := []string{
		`CREATE SCHEMA IF NOT EXISTS integration`,
		`GRANT ALL ON SCHEMA integration TO PUBLIC`,
		`CREATE TABLE IF NOT EXISTS integration.outbox_event (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id uuid NOT NULL,
			aggregate_type varchar(100) NOT NULL,
			aggregate_id uuid NOT NULL,
			event_type varchar(150) NOT NULL,
			event_version integer NOT NULL DEFAULT 1,
			payload jsonb NOT NULL,
			status varchar(20) NOT NULL DEFAULT 'PENDING'
				CHECK (status IN ('PENDING','PROCESSING','PUBLISHED','FAILED','DEAD_LETTER')),
			occurred_at timestamptz NOT NULL DEFAULT now(),
			published_at timestamptz,
			retry_count integer NOT NULL DEFAULT 0,
			last_error text,
			claimed_by text,
			lease_expires_at timestamptz,
			next_attempt_at timestamptz DEFAULT now(),
			UNIQUE (id)
		)`,
		`ALTER TABLE integration.outbox_event ADD COLUMN IF NOT EXISTS claimed_by text`,
		`ALTER TABLE integration.outbox_event ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz`,
		`ALTER TABLE integration.outbox_event ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz DEFAULT now()`,
		`DO $$
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
				  AND pg_get_constraintdef(c.oid) NOT LIKE '%PROCESSING%'
			) LOOP
				EXECUTE 'ALTER TABLE integration.outbox_event DROP CONSTRAINT ' || quote_ident(r.conname);
				EXECUTE 'ALTER TABLE integration.outbox_event ADD CONSTRAINT outbox_event_status_check CHECK (status IN (''PENDING'',''PROCESSING'',''PUBLISHED'',''FAILED'',''DEAD_LETTER''))';
			END LOOP;
		END $$`,
		`CREATE TABLE IF NOT EXISTS integration.delivery_attempt (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			event_id uuid NOT NULL REFERENCES integration.outbox_event(id),
			target_system varchar(100) NOT NULL,
			attempt_no integer NOT NULL,
			status varchar(20) NOT NULL,
			response_code varchar(50),
			response_body text,
			attempted_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (event_id, target_system, attempt_no)
		)`,
		`GRANT ALL ON ALL TABLES IN SCHEMA integration TO PUBLIC`,
		`ALTER TABLE integration.outbox_event ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE integration.outbox_event FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_outbox_isolation ON integration.outbox_event`,
		`CREATE POLICY tenant_outbox_isolation ON integration.outbox_event
			USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
			WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`,
		`CREATE INDEX IF NOT EXISTS ix_outbox_pending ON integration.outbox_event(status, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS ix_outbox_aggregate ON integration.outbox_event(aggregate_type, aggregate_id)`,
		`CREATE INDEX IF NOT EXISTS ix_outbox_tenant_status_time ON integration.outbox_event(tenant_id, status, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS ix_outbox_claimable ON integration.outbox_event(status, next_attempt_at, lease_expires_at)`,
		`CREATE INDEX IF NOT EXISTS ix_delivery_attempt_event_attempt ON integration.delivery_attempt(event_id, attempt_no DESC)`,
	}

	return executeWithAdvisoryLock(ctx, dbtx, queries)
}

// SetupAuditTables creates audit schema, tables, triggers, and RLS policies matching baseline and hardening migrations.
func SetupAuditTables(ctx context.Context, dbtx postgres.DBTX) error {
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS citext`,
		`CREATE SCHEMA IF NOT EXISTS platform`,
		`CREATE SCHEMA IF NOT EXISTS audit`,
		`GRANT ALL ON SCHEMA platform, audit TO PUBLIC`,
		`CREATE OR REPLACE FUNCTION platform.prevent_mutation()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'Immutable record: %.%', TG_TABLE_SCHEMA, TG_TABLE_NAME;
		END $$;`,
		`CREATE OR REPLACE FUNCTION platform.current_tenant_id()
		RETURNS uuid LANGUAGE sql STABLE AS $$
			SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
		$$;`,
		`CREATE TABLE IF NOT EXISTS audit.audit_action_catalog (
			code varchar(100) PRIMARY KEY,
			module varchar(50) NOT NULL,
			description varchar(255) NOT NULL,
			severity varchar(20) NOT NULL DEFAULT 'INFO'
				CHECK (severity IN ('INFO','NOTICE','LOW','MEDIUM','WARNING','HIGH','CRITICAL')),
			requires_reason boolean NOT NULL DEFAULT false,
			active boolean NOT NULL DEFAULT true
		)`,
		`DO $$
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
		END $$`,
		`INSERT INTO audit.audit_action_catalog(code,module,description,severity,requires_reason) VALUES
			('SALE_CREATED','SALES','Sales transaction created','INFO',false),
			('SALE_COMPLETED','SALES','Sales transaction completed','INFO',false),
			('SALE_VOIDED','SALES','Sales transaction voided','HIGH',true),
			('SALE_CANCELLED','SALES','Sales transaction cancelled','WARNING',true),
			('SALE_REFUNDED','SALES','Sales transaction refunded','HIGH',true),
			('PAYMENT_CREATED','PAYMENT','Payment created','INFO',false),
			('PAYMENT_SUCCESS','PAYMENT','Payment completed','INFO',false),
			('PAYMENT_FAILED','PAYMENT','Payment failed','WARNING',false),
			('PAYMENT_REFUNDED','PAYMENT','Payment refunded','HIGH',true),
			('STOCK_ADJUSTED','INVENTORY','Stock adjusted','HIGH',true),
			('STOCK_TRANSFERRED','INVENTORY','Stock transferred','INFO',false),
			('STOCK_OPNAME_POSTED','INVENTORY','Stock opname posted','HIGH',true),
			('PRICE_CREATED','PRICING','Price created','INFO',false),
			('PRICE_CHANGED','PRICING','Price changed','HIGH',true),
			('PROMOTION_CREATED','PROMOTION','Promotion created','INFO',false),
			('PROMOTION_APPROVED','PROMOTION','Promotion approved','HIGH',true),
			('PROMOTION_ACTIVATED','PROMOTION','Promotion activated','HIGH',false),
			('USER_CREATED','IDENTITY','User created','INFO',false),
			('USER_DISABLED','IDENTITY','User disabled','HIGH',true),
			('USER_ROLE_CHANGED','IDENTITY','User role changed','HIGH',true),
			('LOGIN_SUCCESS','SECURITY','Successful login','INFO',false),
			('LOGIN_FAILURE','SECURITY','Failed login','WARNING',false),
			('MFA_FAILURE','SECURITY','MFA failure','HIGH',false),
			('PERMISSION_DENIED','SECURITY','Permission denied','HIGH',false)
		ON CONFLICT (code) DO NOTHING`,
		`INSERT INTO audit.audit_action_catalog (code, module, description, severity, requires_reason, active) VALUES
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
			('CUSTOMER_CREATED', 'CRM', 'Customer created', 'NOTICE', false, true),
			('CUSTOMER_UPDATED', 'CRM', 'Customer updated', 'NOTICE', false, true),
			('CUSTOMER_DISABLED', 'CRM', 'Customer disabled', 'HIGH', false, true),
			('PRICE_UPDATED', 'PRICING', 'Price updated', 'HIGH', true, true),
			('PROMOTION_DEACTIVATED', 'PROMOTION', 'Promotion deactivated', 'HIGH', false, true),
			('SALE_HELD', 'SALES', 'Sale placed on hold', 'INFO', false, true),
			('SALE_RESUMED', 'SALES', 'Sale resumed from hold', 'INFO', false, true),
			('PAYMENT_COMPLETED', 'PAYMENT', 'Payment completed', 'INFO', false, true),
			('PAYMENT_VOIDED', 'PAYMENT', 'Payment voided', 'HIGH', true, true),
			('REFUND_COMPLETED', 'PAYMENT', 'Refund completed', 'HIGH', true, true),
			('STOCK_TRANSFER_RECEIVED', 'INVENTORY', 'Stock transfer received', 'INFO', false, true),
			('STOCK_RESERVED', 'INVENTORY', 'Stock reserved', 'INFO', false, true),
			('STOCK_RELEASED', 'INVENTORY', 'Stock released', 'INFO', false, true),
			('RATE_LIMIT_TRIGGERED', 'SECURITY', 'Rate limit triggered', 'WARNING', false, true),
			('SUSPICIOUS_AUTH_ACTIVITY', 'SECURITY', 'Suspicious auth activity', 'HIGH', false, true)
		ON CONFLICT (code) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS audit.audit_log (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			tenant_id uuid NOT NULL,
			company_id uuid,
			store_id uuid,
			terminal_id uuid,
			user_id uuid,
			module varchar(50) NOT NULL,
			action varchar(100) NOT NULL,
			resource_type varchar(100),
			resource_id uuid,
			business_number varchar(100),
			status varchar(20) NOT NULL DEFAULT 'SUCCESS'
				CHECK (status IN ('SUCCESS','FAILED','DENIED')),
			request_id uuid,
			trace_id varchar(100),
			ip_address inet,
			user_agent text,
			reason text,
			metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'fk_audit_log_action'
			) THEN
				ALTER TABLE audit.audit_log
					ADD CONSTRAINT fk_audit_log_action
					FOREIGN KEY (action) REFERENCES audit.audit_action_catalog(code);
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS audit.data_change (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			audit_log_id bigint NOT NULL REFERENCES audit.audit_log(id),
			table_schema varchar(100) NOT NULL,
			table_name varchar(100) NOT NULL,
			row_id uuid,
			field_name varchar(150) NOT NULL,
			old_value jsonb,
			new_value jsonb,
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS audit.security_event (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			tenant_id uuid,
			user_id uuid,
			event_type varchar(80) NOT NULL,
			severity varchar(20) NOT NULL DEFAULT 'INFO'
				CHECK (severity IN ('INFO','LOW','MEDIUM','HIGH','CRITICAL')),
			ip_address inet,
			user_agent text,
			request_id uuid,
			trace_id varchar(100),
			metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
			occurred_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS audit.login_history (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			tenant_id uuid,
			user_id uuid,
			username citext,
			success boolean NOT NULL,
			failure_reason varchar(100),
			ip_address inet,
			user_agent text,
			occurred_at timestamptz NOT NULL DEFAULT now()
		)`,
		`GRANT ALL ON ALL TABLES IN SCHEMA audit TO PUBLIC`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA audit TO PUBLIC`,
		`ALTER TABLE audit.audit_log ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE audit.audit_log FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_audit_isolation ON audit.audit_log`,
		`CREATE POLICY tenant_audit_isolation ON audit.audit_log
			USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
			WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`,
		`ALTER TABLE audit.data_change ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE audit.data_change FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_data_change_isolation ON audit.data_change`,
		`CREATE POLICY tenant_data_change_isolation ON audit.data_change
			USING (EXISTS (
				SELECT 1 FROM audit.audit_log al
				WHERE al.id = audit.data_change.audit_log_id
				  AND al.tenant_id = platform.current_tenant_id()
			))
			WITH CHECK (EXISTS (
				SELECT 1 FROM audit.audit_log al
				WHERE al.id = audit.data_change.audit_log_id
				  AND al.tenant_id = platform.current_tenant_id()
			))`,
		`ALTER TABLE audit.security_event ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE audit.security_event FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_security_event_isolation ON audit.security_event`,
		`CREATE POLICY tenant_security_event_isolation ON audit.security_event
			USING (tenant_id IS NULL OR tenant_id = platform.current_tenant_id())
			WITH CHECK (tenant_id IS NULL OR tenant_id = platform.current_tenant_id())`,
		`ALTER TABLE audit.login_history ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE audit.login_history FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_login_history_isolation ON audit.login_history`,
		`CREATE POLICY tenant_login_history_isolation ON audit.login_history
			USING (tenant_id IS NULL OR tenant_id = platform.current_tenant_id())
			WITH CHECK (tenant_id IS NULL OR tenant_id = platform.current_tenant_id())`,
		`DROP TRIGGER IF EXISTS trg_audit_log_immutable ON audit.audit_log`,
		`CREATE TRIGGER trg_audit_log_immutable
			BEFORE UPDATE OR DELETE ON audit.audit_log
			FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation()`,
		`DROP TRIGGER IF EXISTS trg_audit_data_change_immutable ON audit.data_change`,
		`CREATE TRIGGER trg_audit_data_change_immutable
			BEFORE UPDATE OR DELETE ON audit.data_change
			FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation()`,
		`DROP TRIGGER IF EXISTS trg_security_event_immutable ON audit.security_event`,
		`CREATE TRIGGER trg_security_event_immutable
			BEFORE UPDATE OR DELETE ON audit.security_event
			FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation()`,
		`DROP TRIGGER IF EXISTS trg_login_history_immutable ON audit.login_history`,
		`CREATE TRIGGER trg_login_history_immutable
			BEFORE UPDATE OR DELETE ON audit.login_history
			FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation()`,
		`CREATE INDEX IF NOT EXISTS ix_audit_time ON audit.audit_log(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_audit_user_time ON audit.audit_log(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_audit_resource ON audit.audit_log(resource_type, resource_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_audit_module_action ON audit.audit_log(module, action, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_audit_trace ON audit.audit_log(trace_id)`,
		`CREATE INDEX IF NOT EXISTS ix_audit_tenant_user_time ON audit.audit_log(tenant_id, user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS ix_data_change_audit ON audit.data_change(audit_log_id)`,
		`CREATE INDEX IF NOT EXISTS ix_security_event_time ON audit.security_event(occurred_at DESC)`,
	}

	return executeWithAdvisoryLock(ctx, dbtx, queries)
}
