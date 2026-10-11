package pos_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/postgres"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func getAdminTestConfig() config.Config {
	return config.Config{
		Env:                "test",
		LogLevel:           "debug",
		DBHost:             "127.0.0.1",
		DBPort:             15432,
		DBName:             "postgres",
		DBUser:             "nexora_test",
		DBPassword:         "test_secret_pass",
		DBSSLMode:          "disable",
		DBMaxConns:         10,
		DBMinConns:         2,
		DBMaxConnLifetime:  15 * time.Minute,
		DBConnectTimeout:   3 * time.Second,
		ShutdownTimeout:    5 * time.Second,
		HTTPPort:           8080,
		ReadTimeout:        5 * time.Second,
		WriteTimeout:       5 * time.Second,
		RateLimitRequests:  100,
		RateLimitWindow:    time.Minute,
		CleanupItemTimeout: 2 * time.Second,
	}
}

func setupDisposableDatabase(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()

	adminCfg := getAdminTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := postgres.NewPool(ctx, &adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("PostgreSQL unavailable at %s:%d (%v); skipping integration test", adminCfg.DBHost, adminCfg.DBPort, err)
		return nil, nil
	}

	dbName := fmt.Sprintf("nexora_pos_migration_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		adminPool.Close()
		t.Fatalf("create disposable database: %v", err)
	}

	migrationCfg := adminCfg
	migrationCfg.DBName = dbName
	migrationPool, err := postgres.NewPool(ctx, &migrationCfg, newDiscardLogger())
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		adminPool.Close()
		t.Fatalf("connect migration pool: %v", err)
	}

	appCfg := adminCfg
	appCfg.DBName = dbName
	appCfg.DBUser = "nexora_app"
	appCfg.DBPassword = "app_secret_pass"

	appPool, err := postgres.NewPool(ctx, &appCfg, newDiscardLogger())
	if err != nil {
		migrationPool.Close()
		_, _ = adminPool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		adminPool.Close()
		t.Fatalf("connect app pool: %v", err)
	}

	t.Cleanup(func() {
		appPool.Close()
		migrationPool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = adminPool.Exec(cleanupCtx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		adminPool.Close()
	})

	// Run migrations 000001 through 000010 sequentially
	_, sourceFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations")
	for _, name := range []string{
		"000001_baseline_hardened.sql",
		"000002_refresh_tokens.sql",
		"000003_outbox_leases.sql",
		"000004_audit_hardening.sql",
		"000005_auth_rls_bootstrap.sql",
		"000006_identity_permissions.sql",
		"000007_crm_audit_actions.sql",
		"000008_organization_hardening.sql",
		"000009_master_hardening.sql",
		"000010_pos_hardening.sql",
	} {
		migration, err := os.ReadFile(filepath.Join(migrationDir, name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := migrationPool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	// Grant necessary table permissions to nexora_app non-superuser role
	grant := `
		GRANT USAGE ON SCHEMA identity, organization, master, crm, pos, platform, audit, integration TO nexora_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, organization, master, crm, pos, platform, audit, integration TO nexora_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity, organization, master, crm, pos, platform, audit, integration TO nexora_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA platform TO nexora_app;
	`
	if _, err := migrationPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant permissions to nexora_app: %v", err)
	}

	return migrationPool, appPool
}

// 1. Verify Checksums of 000001-000009 remain byte-for-byte unchanged
func TestPOS_Migration000010_ChecksumIntegrity(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations")

	canonicalChecksums := map[string]string{
		"000001_baseline_hardened.sql":       "e7cefc2dfcdcf2b90a8418dcc2da8a091289ee00a5806c08278bf1516d5fb09f",
		"000002_refresh_tokens.sql":          "47ef2a3a9c7d2e445df4d5e90c11504e4145a41749f54643e3d386ecea06a733",
		"000003_outbox_leases.sql":           "15b6da589a153f64984b08ab393792dffc8b52d9e52d5f034dc0feb8966be279",
		"000004_audit_hardening.sql":         "1e89aed0c072fd1c6ee6ce9321ed1448df9dd97fd321ff93b39364dd5072176f",
		"000005_auth_rls_bootstrap.sql":      "8a54f965d850209d3bde285de68feb27f660633e2360f5fac6bb2fa90e4ab5e0",
		"000006_identity_permissions.sql":   "a41d19cfcb8960d0267872614e6946985241d83de928ffb6386373de5c43f8d2",
		"000007_crm_audit_actions.sql":       "84eb4c3d7987c616ae2bc4397bfc6cbffc17058293ad8af47425b9a29107e7c3",
		"000008_organization_hardening.sql": "f454c5db4a5e7f383ae21ffac63f575ff3508c334bcb16008b60a5c2dfe83dcb",
		"000009_master_hardening.sql":       "d4455ee66dab49d7eff8e41d9c24e6effb8bcc60dad3d88875e416d29bdca89e",
	}

	for filename, expectedHash := range canonicalChecksums {
		content, err := os.ReadFile(filepath.Join(migrationDir, filename))
		if err != nil {
			t.Fatalf("failed to read migration file %s: %v", filename, err)
		}
		hash := sha256.Sum256(content)
		actualHash := hex.EncodeToString(hash[:])
		if actualHash != expectedHash {
			t.Errorf("checksum mismatch on %s: expected %s, got %s", filename, expectedHash, actualHash)
		}
	}
}

// 2. Verify Migration 000010 Idempotency & Seeds
func TestPOS_Migration000010_IdempotencyAndAuditSeeds(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	// Verify all 9 POS audit actions seeded
	expectedActions := []struct {
		code           string
		severity       string
		requiresReason bool
	}{
		{"POS_SESSION_OPENED", "NOTICE", false},
		{"POS_CASH_MOVEMENT_CREATED", "NOTICE", false},
		{"POS_CASH_IN", "NOTICE", false},
		{"POS_CASH_OUT", "NOTICE", false},
		{"POS_SESSION_CLOSING", "NOTICE", false},
		{"POS_SESSION_VARIANCE_DETECTED", "HIGH", false},
		{"POS_SESSION_VARIANCE_APPROVED", "HIGH", true},
		{"POS_SESSION_CLOSED", "HIGH", false},
		{"POS_SESSION_FORCE_CLOSED", "CRITICAL", true},
	}

	for _, a := range expectedActions {
		var sev string
		var reqReason, active bool
		err := appPool.QueryRow(ctx, `
			SELECT severity, requires_reason, active 
			FROM audit.audit_action_catalog 
			WHERE code = $1 AND module = 'POS'
		`, a.code).Scan(&sev, &reqReason, &active)
		if err != nil {
			t.Fatalf("query audit action %s: %v", a.code, err)
		}
		if sev != a.severity {
			t.Errorf("action %s severity: expected %s, got %s", a.code, a.severity, sev)
		}
		if reqReason != a.requiresReason {
			t.Errorf("action %s requires_reason: expected %v, got %v", a.code, a.requiresReason, reqReason)
		}
		if !active {
			t.Errorf("action %s active: expected true, got false", a.code)
		}
	}

	// Verify re-applying migration 000010 is idempotent
	_, sourceFile, _, _ := runtime.Caller(0)
	migration010Path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations", "000010_pos_hardening.sql")
	content, err := os.ReadFile(migration010Path)
	if err != nil {
		t.Fatalf("read migration 000010: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, string(content)); err != nil {
		t.Fatalf("re-applying migration 000010 failed (not idempotent): %v", err)
	}

	// Re-verify count remains exactly 9
	var posCount int
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_action_catalog WHERE module = 'POS'`).Scan(&posCount); err != nil {
		t.Fatalf("query POS audit actions count: %v", err)
	}
	if posCount != 9 {
		t.Fatalf("expected exactly 9 POS audit actions, got %d", posCount)
	}
}

// 3. Verify Metadata Columns on pos.session
func TestPOS_Migration000010_MetadataColumns(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	expectedColumns := map[string]string{
		"variance_approved_by": "uuid",
		"variance_approved_at": "timestamp with time zone",
		"variance_reason":      "text",
	}

	for colName, expType := range expectedColumns {
		var dataType string
		var isNullable string
		err := appPool.QueryRow(ctx, `
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = 'pos' AND table_name = 'session' AND column_name = $1
		`, colName).Scan(&dataType, &isNullable)
		if err != nil {
			t.Fatalf("column %s missing on pos.session: %v", colName, err)
		}
		if dataType != expType {
			t.Errorf("column %s type: expected %s, got %s", colName, expType, dataType)
		}
		if isNullable != "YES" {
			t.Errorf("column %s should be nullable, got %s", colName, isNullable)
		}
	}
}

// 4. Verify Status Constraint accepts VARIANCE_REVIEW and rejects invalid values
func TestPOS_Migration000010_StatusConstraint(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	// Seed prerequisite hierarchy
	tenantID := uuid.New()
	companyID := uuid.New()
	storeID := uuid.New()
	terminalID := uuid.New()
	userID := uuid.New()

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.company (id, tenant_id, code, name, base_currency)
		VALUES ($1, $2, 'CMP-POS-01', 'POS Test Co', 'IDR');
	`, companyID, tenantID); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.store (id, tenant_id, company_id, code, name)
		VALUES ($1, $2, $3, 'STR-POS-01', 'Store POS 1');
	`, storeID, tenantID, companyID); err != nil {
		t.Fatalf("insert store: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.terminal (id, tenant_id, company_id, store_id, code, name, status)
		VALUES ($1, $2, $3, $4, 'TRM-POS-01', 'Terminal POS 1', 'ACTIVE');
	`, terminalID, tenantID, companyID, storeID); err != nil {
		t.Fatalf("insert terminal: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, password_hash, display_name, status)
		VALUES ($1, $2, 'cashier_pos', 'cashier@pos.test', 'hash', 'Cashier User', 'ACTIVE');
	`, userID, tenantID); err != nil {
		t.Fatalf("insert user_account: %v", err)
	}

	// Insert session with VARIANCE_REVIEW status (must succeed)
	sessionID := uuid.New()

	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO pos.session (id, tenant_id, company_id, store_id, terminal_id, cashier_user_id, session_number, status, opening_cash, expected_cash)
		VALUES ($1, $2, $3, $4, $5, $6, 'SES-VR-01', 'VARIANCE_REVIEW', 100000, 100000)
	`, sessionID, tenantID, companyID, storeID, terminalID, userID)
	if err != nil {
		t.Fatalf("insert with VARIANCE_REVIEW status failed: %v", err)
	}

	// Insert session with INVALID status (must fail check constraint)
	invalidSessionID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO pos.session (id, tenant_id, company_id, store_id, terminal_id, cashier_user_id, session_number, status, opening_cash, expected_cash)
		VALUES ($1, $2, $3, $4, $5, $6, 'SES-INV-01', 'INVALID_STATUS', 100000, 100000)
	`, invalidSessionID, tenantID, companyID, storeID, terminalID, userID)
	if err == nil {
		t.Fatalf("expected check constraint violation on INVALID_STATUS, but insertion succeeded")
	}
	var pgErr *pgconn.PgError
	if !asPgError(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("expected pg error 23514 (check_violation), got: %v", err)
	}
}

// 5. Verify Non-Negative Expected Cash Constraint (CORR-POS-002)
func TestPOS_Migration000010_ExpectedCashCheck(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	tenantID := uuid.New()
	companyID := uuid.New()
	storeID := uuid.New()
	terminalID := uuid.New()
	userID := uuid.New()

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.company (id, tenant_id, code, name, base_currency)
		VALUES ($1, $2, 'CMP-POS-02', 'POS Test Co 2', 'IDR');
	`, companyID, tenantID); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.store (id, tenant_id, company_id, code, name)
		VALUES ($1, $2, $3, 'STR-POS-02', 'Store POS 2');
	`, storeID, tenantID, companyID); err != nil {
		t.Fatalf("insert store: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.terminal (id, tenant_id, company_id, store_id, code, name, status)
		VALUES ($1, $2, $3, $4, 'TRM-POS-02', 'Terminal POS 2', 'ACTIVE');
	`, terminalID, tenantID, companyID, storeID); err != nil {
		t.Fatalf("insert terminal: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, password_hash, display_name, status)
		VALUES ($1, $2, 'cashier_pos2', 'cashier2@pos.test', 'hash', 'Cashier User 2', 'ACTIVE');
	`, userID, tenantID); err != nil {
		t.Fatalf("insert user_account: %v", err)
	}

	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	// Negative expected_cash must fail check constraint
	sessionID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO pos.session (id, tenant_id, company_id, store_id, terminal_id, cashier_user_id, session_number, status, opening_cash, expected_cash)
		VALUES ($1, $2, $3, $4, $5, $6, 'SES-NEG-01', 'OPEN', 100000, -50000)
	`, sessionID, tenantID, companyID, storeID, terminalID, userID)
	if err == nil {
		t.Fatalf("expected check violation for negative expected_cash, but insert succeeded")
	}
	var pgErr *pgconn.PgError
	if !asPgError(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("expected pg error 23514, got: %v", err)
	}
}

// 6. Verify Partial Unique Index prevents duplicate open sessions per terminal
func TestPOS_Migration000010_TerminalExclusivityIndex(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	tenantID := uuid.New()
	companyID := uuid.New()
	storeID := uuid.New()
	terminalID := uuid.New()
	userID := uuid.New()

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.company (id, tenant_id, code, name, base_currency)
		VALUES ($1, $2, 'CMP-POS-03', 'POS Test Co 3', 'IDR');
	`, companyID, tenantID); err != nil {
		t.Fatalf("insert company: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.store (id, tenant_id, company_id, code, name)
		VALUES ($1, $2, $3, 'STR-POS-03', 'Store POS 3');
	`, storeID, tenantID, companyID); err != nil {
		t.Fatalf("insert store: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.terminal (id, tenant_id, company_id, store_id, code, name, status)
		VALUES ($1, $2, $3, $4, 'TRM-POS-03', 'Terminal POS 3', 'ACTIVE');
	`, terminalID, tenantID, companyID, storeID); err != nil {
		t.Fatalf("insert terminal: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, password_hash, display_name, status)
		VALUES ($1, $2, 'cashier_pos3', 'cashier3@pos.test', 'hash', 'Cashier User 3', 'ACTIVE');
	`, userID, tenantID); err != nil {
		t.Fatalf("insert user_account: %v", err)
	}

	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	// 1. First session OPEN -> succeeds
	s1 := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO pos.session (id, tenant_id, company_id, store_id, terminal_id, cashier_user_id, session_number, status, opening_cash, expected_cash)
		VALUES ($1, $2, $3, $4, $5, $6, 'SES-EXCL-01', 'OPEN', 100000, 100000)
	`, s1, tenantID, companyID, storeID, terminalID, userID)
	if err != nil {
		t.Fatalf("insert first OPEN session failed: %v", err)
	}

	// 2. Second session OPEN on same terminal -> must fail unique constraint
	s2 := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO pos.session (id, tenant_id, company_id, store_id, terminal_id, cashier_user_id, session_number, status, opening_cash, expected_cash)
		VALUES ($1, $2, $3, $4, $5, $6, 'SES-EXCL-02', 'OPEN', 50000, 50000)
	`, s2, tenantID, companyID, storeID, terminalID, userID)
	if err == nil {
		t.Fatalf("expected unique constraint violation on second open session, but it succeeded")
	}
	var pgErr *pgconn.PgError
	if !asPgError(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("expected pg error 23505 (unique_violation), got: %v", err)
	}
}

// 7. Verify Audit Foreign Key integrity with newly seeded action
func TestPOS_Migration000010_AuditForeignKeyIntegrity(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	tenantID := uuid.New()
	userID := uuid.New()

	_, err := migrationPool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, password_hash, display_name, status)
		VALUES ($1, $2, 'audit_tester', 'audit@test.com', 'hash', 'Audit Tester', 'ACTIVE');
	`, userID, tenantID)
	if err != nil {
		t.Fatalf("seed user account: %v", err)
	}

	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	// Insert audit record with POS_SESSION_VARIANCE_APPROVED action
	_, err = tx.Exec(ctx, `
		INSERT INTO audit.audit_log (
			tenant_id, user_id, module, action, resource_type, resource_id,
			status, reason, metadata
		) VALUES (
			$1, $2, 'POS', 'POS_SESSION_VARIANCE_APPROVED', 'POS_SESSION', gen_random_uuid(),
			'SUCCESS', 'Shortage acknowledged', '{"amount": 500}'::jsonb
		)
	`, tenantID, userID)
	if err != nil {
		t.Fatalf("insert audit log with POS_SESSION_VARIANCE_APPROVED failed: %v", err)
	}
}

// 8. Verify RLS Tenant Isolation under non-superuser nexora_app role
func TestPOS_Migration000010_RLSTenantIsolation(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()
	companyA := uuid.New()
	companyB := uuid.New()
	storeA := uuid.New()
	storeB := uuid.New()
	terminalA := uuid.New()
	terminalB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.company (id, tenant_id, code, name, base_currency) VALUES
			($1, $2, 'CMP-A', 'Company A', 'IDR'),
			($3, $4, 'CMP-B', 'Company B', 'IDR');
	`, companyA, tenantA, companyB, tenantB); err != nil {
		t.Fatalf("insert companies: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.store (id, tenant_id, company_id, code, name) VALUES
			($1, $2, $3, 'STR-A', 'Store A'),
			($4, $5, $6, 'STR-B', 'Store B');
	`, storeA, tenantA, companyA, storeB, tenantB, companyB); err != nil {
		t.Fatalf("insert stores: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.terminal (id, tenant_id, company_id, store_id, code, name, status) VALUES
			($1, $2, $3, $4, 'TRM-A', 'Terminal A', 'ACTIVE'),
			($5, $6, $7, $8, 'TRM-B', 'Terminal B', 'ACTIVE');
	`, terminalA, tenantA, companyA, storeA, terminalB, tenantB, companyB, storeB); err != nil {
		t.Fatalf("insert terminals: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, password_hash, display_name, status) VALUES
			($1, $2, 'cashier_a', 'ca@test.com', 'h', 'Cashier A', 'ACTIVE'),
			($3, $4, 'cashier_b', 'cb@test.com', 'h', 'Cashier B', 'ACTIVE');
	`, userA, tenantA, userB, tenantB); err != nil {
		t.Fatalf("insert user accounts: %v", err)
	}

	// Insert session for Tenant A
	sessionA := uuid.New()
	txA, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txA: %v", err)
	}
	if _, err := txA.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantA.String()); err != nil {
		t.Fatalf("set context A: %v", err)
	}
	_, err = txA.Exec(ctx, `
		INSERT INTO pos.session (id, tenant_id, company_id, store_id, terminal_id, cashier_user_id, session_number, status, opening_cash, expected_cash)
		VALUES ($1, $2, $3, $4, $5, $6, 'SES-A-01', 'OPEN', 100000, 100000)
	`, sessionA, tenantA, companyA, storeA, terminalA, userA)
	if err != nil {
		t.Fatalf("insert session A: %v", err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit txA: %v", err)
	}

	// Query under Tenant B context (must see 0 rows due to RLS)
	txB, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txB: %v", err)
	}
	defer txB.Rollback(ctx)
	if _, err := txB.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantB.String()); err != nil {
		t.Fatalf("set context B: %v", err)
	}

	var count int
	if err := txB.QueryRow(ctx, `SELECT count(*) FROM pos.session WHERE id = $1`, sessionA).Scan(&count); err != nil {
		t.Fatalf("query session under tenant B: %v", err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant leakage! Tenant B saw %d sessions belonging to Tenant A", count)
	}
}

func asPgError(err error, target **pgconn.PgError) bool {
	if pgErr, ok := err.(*pgconn.PgError); ok {
		*target = pgErr
		return true
	}
	return false
}
