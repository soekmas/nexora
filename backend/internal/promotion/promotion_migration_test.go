package promotion_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

func getMigrationDir() string {
	_, sourceFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations")
}

func setupDisposableDatabase(t *testing.T, applyUpToMigration int) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()

	adminCfg := getAdminTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := postgres.NewPool(ctx, &adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("PostgreSQL unavailable at %s:%d (%v); skipping integration test", adminCfg.DBHost, adminCfg.DBPort, err)
		return nil, nil
	}

	dbName := fmt.Sprintf("nexora_promo_mig_test_%d", time.Now().UnixNano())
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

	migrationFiles := []string{
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
		"000011_pricing_hardening.sql",
		"000012_promotion_hardening.sql",
	}

	if applyUpToMigration > len(migrationFiles) {
		applyUpToMigration = len(migrationFiles)
	}

	migrationDir := getMigrationDir()
	for i := 0; i < applyUpToMigration; i++ {
		name := migrationFiles[i]
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
		GRANT USAGE ON SCHEMA identity, organization, master, crm, pricing, promotion, sales, pos, platform, audit, integration TO nexora_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, organization, master, crm, pricing, promotion, sales, pos, platform, audit, integration TO nexora_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity, organization, master, crm, pricing, promotion, sales, pos, platform, audit, integration TO nexora_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA platform, pricing TO nexora_app;
	`
	if _, err := migrationPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant permissions to nexora_app: %v", err)
	}

	return migrationPool, appPool
}

// 1. Verify Baseline Checksums 000001-000011 remain byte-for-byte unchanged
func TestPromotion_Migration000012_BaselineChecksumIntegrity(t *testing.T) {
	migrationDir := getMigrationDir()

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
		"000010_pos_hardening.sql":          "e6ef51060f5174d4540ef98c648a334f394c5cdb2d322acff68d4f8b180b3ef7",
		"000011_pricing_hardening.sql":      "bb29308c0162d21355470d81a8860f23c8dfc4aef0278515a8e99fb436ab6bc7",
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

	// Verify 000012 exists and compute its hash
	content012, err := os.ReadFile(filepath.Join(migrationDir, "000012_promotion_hardening.sql"))
	if err != nil {
		t.Fatalf("failed to read 000012_promotion_hardening.sql: %v", err)
	}
	hash012 := sha256.Sum256(content012)
	t.Logf("Migration 000012 computed SHA-256: %s", hex.EncodeToString(hash012[:]))
}

// 2. Verify Schema Structure, Indexes, and Constraints after 000012
func TestPromotion_Migration000012_SchemaStructure(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	// 2.1 Verify promotion.promotion columns: approved_by, approved_at, usage_limit, usage_count
	type colSpec struct {
		name     string
		dataType string
		nullable string
	}
	expectedCols := []colSpec{
		{"approved_by", "uuid", "YES"},
		{"approved_at", "timestamp with time zone", "YES"},
		{"usage_limit", "integer", "YES"},
		{"usage_count", "integer", "NO"},
	}

	for _, spec := range expectedCols {
		var dataType, nullable string
		err := migrationPool.QueryRow(ctx, `
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = 'promotion' AND table_name = 'promotion' AND column_name = $1
		`, spec.name).Scan(&dataType, &nullable)
		if err != nil {
			t.Fatalf("query column %s: %v", spec.name, err)
		}
		if dataType != spec.dataType || nullable != spec.nullable {
			t.Errorf("unexpected column %s: type=%s (want %s), nullable=%s (want %s)",
				spec.name, dataType, spec.dataType, nullable, spec.nullable)
		}
	}

	// 2.2 Verify Constraints on promotion.promotion
	expectedConstraints := []string{
		"ck_promotion_status",
		"ck_promotion_stacking_mode",
		"ck_promotion_usage_limit",
		"ck_promotion_usage_count",
	}
	for _, cName := range expectedConstraints {
		var count int
		err := migrationPool.QueryRow(ctx, `
			SELECT count(*) FROM pg_constraint WHERE conname = $1
		`, cName).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("constraint %s not found (count=%d, err=%v)", cName, count, err)
		}
	}

	// 2.3 Verify Indexes
	expectedIndexes := []string{
		"ix_promotion_evaluation_priority",
		"ix_promotion_active_eval_lookup",
		"ix_promotion_condition_promo",
		"ix_promotion_reward_promo",
		"ix_promotion_app_tenant_order",
		"ix_promotion_app_tenant_promo",
	}
	for _, idx := range expectedIndexes {
		var count int
		err := migrationPool.QueryRow(ctx, `
			SELECT count(*) FROM pg_indexes WHERE indexname = $1
		`, idx).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("index %s not found (count=%d, err=%v)", idx, count, err)
		}
	}
}

// 3. Verify Status Check Constraint Enforces Canonical Lifecycle States
func TestPromotion_Migration000012_StatusConstraints(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()
	tenantID := uuid.New()

	canonicalStatuses := []string{
		"DRAFT",
		"PENDING_APPROVAL",
		"APPROVED",
		"SCHEDULED",
		"ACTIVE",
		"PAUSED",
		"EXPIRED",
		"REJECTED",
		"CANCELLED",
	}

	now := time.Now().UTC()
	for i, st := range canonicalStatuses {
		code := fmt.Sprintf("PROMO-STAT-%d", i)
		_, err := migrationPool.Exec(ctx, `
			INSERT INTO promotion.promotion (
				tenant_id, code, name, channel, stacking_mode, priority,
				effective_from, effective_to, status
			) VALUES ($1, $2, $3, 'ALL', 'STACKABLE', 100, $4, $5, $6)
		`, tenantID, code, "Promo "+st, now, now.Add(24*time.Hour), st)
		if err != nil {
			t.Errorf("expected status %s to be valid, but got error: %v", st, err)
		}
	}

	// Invalid statuses must be rejected
	invalidStatuses := []string{"INACTIVE", "ARCHIVED", "UNKNOWN", "DELETED"}
	for _, inv := range invalidStatuses {
		code := fmt.Sprintf("PROMO-INV-%s", inv)
		_, err := migrationPool.Exec(ctx, `
			INSERT INTO promotion.promotion (
				tenant_id, code, name, channel, stacking_mode, priority,
				effective_from, effective_to, status
			) VALUES ($1, $2, 'Invalid Promo', 'ALL', 'STACKABLE', 100, $3, $4, $5)
		`, tenantID, code, now, now.Add(24*time.Hour), inv)
		if err == nil {
			t.Errorf("expected status %s to be rejected by constraint, but insert succeeded", inv)
		} else {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code != "23514" {
				t.Errorf("expected check_violation (23514), got %s", pgErr.Code)
			}
		}
	}
}

// 4. Verify Stacking Mode Check Constraint
func TestPromotion_Migration000012_StackingModeConstraints(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()
	tenantID := uuid.New()
	now := time.Now().UTC()

	validModes := []string{"STACKABLE", "EXCLUSIVE", "BEST_ONLY"}
	for i, mode := range validModes {
		code := fmt.Sprintf("PROMO-STACK-%d", i)
		_, err := migrationPool.Exec(ctx, `
			INSERT INTO promotion.promotion (
				tenant_id, code, name, channel, stacking_mode, priority,
				effective_from, effective_to, status
			) VALUES ($1, $2, 'Stack Promo', 'ALL', $3, 100, $4, $5, 'DRAFT')
		`, tenantID, code, mode, now, now.Add(24*time.Hour))
		if err != nil {
			t.Errorf("expected stacking mode %s to be valid, but got error: %v", mode, err)
		}
	}

	// Invalid stacking mode must fail
	_, err := migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, effective_to, status
		) VALUES ($1, 'PROMO-INVALID-STACK', 'Invalid Stack', 'ALL', 'COMBINED', 100, $2, $3, 'DRAFT')
	`, tenantID, now, now.Add(24*time.Hour))
	if err == nil {
		t.Error("expected invalid stacking mode to be rejected, but insert succeeded")
	}
}

// 5. Verify Usage Limit and Usage Count Constraints
func TestPromotion_Migration000012_UsageLimitAndCountConstraints(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()
	tenantID := uuid.New()
	now := time.Now().UTC()

	// 5.1 Valid: usage_limit = NULL, usage_count = 0
	_, err := migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, status, usage_limit, usage_count
		) VALUES ($1, 'PROMO-USAGE-1', 'Unlimited', 'ALL', 'STACKABLE', 100, $2, 'DRAFT', NULL, 0)
	`, tenantID, now)
	if err != nil {
		t.Fatalf("insert with NULL usage_limit: %v", err)
	}

	// 5.2 Valid: usage_limit = 500, usage_count = 10
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, status, usage_limit, usage_count
		) VALUES ($1, 'PROMO-USAGE-2', 'Limited', 'ALL', 'STACKABLE', 100, $2, 'DRAFT', 500, 10)
	`, tenantID, now)
	if err != nil {
		t.Fatalf("insert with valid usage_limit: %v", err)
	}

	// 5.3 Invalid: usage_limit <= 0
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, status, usage_limit, usage_count
		) VALUES ($1, 'PROMO-USAGE-INV-1', 'Invalid Limit', 'ALL', 'STACKABLE', 100, $2, 'DRAFT', 0, 0)
	`, tenantID, now)
	if err == nil {
		t.Error("expected usage_limit=0 to fail check constraint, but succeeded")
	}

	// 5.4 Invalid: usage_count < 0
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, status, usage_limit, usage_count
		) VALUES ($1, 'PROMO-USAGE-INV-2', 'Invalid Count', 'ALL', 'STACKABLE', 100, $2, 'DRAFT', 100, -1)
	`, tenantID, now)
	if err == nil {
		t.Error("expected usage_count=-1 to fail check constraint, but succeeded")
	}
}

// 6. Verify Existing-Data Compatibility (Pre-000012 'INACTIVE' data is migrated to 'PAUSED')
func TestPromotion_Migration000012_ExistingDataCompatibility(t *testing.T) {
	// Setup DB up to 000011 (pre-000012)
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()
	tenantID := uuid.New()
	now := time.Now().UTC()

	// Seed pre-000012 data with status 'INACTIVE'
	_, err := migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, status
		) VALUES ($1, 'PRE-MIG-INACTIVE', 'Legacy Inactive Promo', 'ALL', 'STACKABLE', 100, $2, 'INACTIVE')
	`, tenantID, now)
	if err != nil {
		t.Fatalf("seed pre-000012 INACTIVE promotion: %v", err)
	}

	// Apply migration 000012
	migrationDir := getMigrationDir()
	mig012, err := os.ReadFile(filepath.Join(migrationDir, "000012_promotion_hardening.sql"))
	if err != nil {
		t.Fatalf("read migration 000012: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, string(mig012)); err != nil {
		t.Fatalf("apply migration 000012 over existing data: %v", err)
	}

	// Verify legacy INACTIVE was migrated to PAUSED
	var status string
	var usageCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT status, usage_count
		FROM promotion.promotion
		WHERE tenant_id = $1 AND code = 'PRE-MIG-INACTIVE'
	`, tenantID).Scan(&status, &usageCount)
	if err != nil {
		t.Fatalf("query migrated promo: %v", err)
	}
	if status != "PAUSED" {
		t.Errorf("expected legacy INACTIVE to become PAUSED, got %s", status)
	}
	if usageCount != 0 {
		t.Errorf("expected default usage_count = 0, got %d", usageCount)
	}
}

// 7. Verify Cross-Tenant Isolation Trigger on promotion.promotion_version
func TestPromotion_Migration000012_CrossTenantTriggerOnVersion(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	now := time.Now().UTC()

	// Create promotion in Tenant A
	var promoA uuid.UUID
	err := migrationPool.QueryRow(ctx, `
		INSERT INTO promotion.promotion (
			tenant_id, code, name, channel, stacking_mode, priority,
			effective_from, status
		) VALUES ($1, 'PROMO-TENANT-A', 'Promo Tenant A', 'ALL', 'STACKABLE', 100, $2, 'DRAFT')
		RETURNING id
	`, tenantA, now).Scan(&promoA)
	if err != nil {
		t.Fatalf("insert promotion tenant A: %v", err)
	}

	// Attempt to create promotion_version in Tenant B pointing to Tenant A's promotion
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO promotion.promotion_version (
			tenant_id, promotion_id, version_no, rule_snapshot, status
		) VALUES ($1, $2, 1, '{}'::jsonb, 'DRAFT')
	`, tenantB, promoA)
	if err == nil {
		t.Fatal("expected cross-tenant promotion_version insertion to fail, but succeeded")
	}
	t.Logf("Cross-tenant insert successfully blocked: %v", err)
}

// 8. Verify Canonical Promotion Audit Action Seeds
func TestPromotion_Migration000012_AuditActionSeeds(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	expectedAuditActions := []struct {
		code     string
		module   string
		severity string
	}{
		{"PROMOTION_CREATED", "PROMOTION", "NOTICE"},
		{"PROMOTION_UPDATED", "PROMOTION", "HIGH"},
		{"PROMOTION_SUBMITTED", "PROMOTION", "NOTICE"},
		{"PROMOTION_APPROVED", "PROMOTION", "HIGH"},
		{"PROMOTION_REJECTED", "PROMOTION", "NOTICE"},
		{"PROMOTION_ACTIVATED", "PROMOTION", "HIGH"},
		{"PROMOTION_PAUSED", "PROMOTION", "HIGH"},
		{"PROMOTION_EXPIRED", "PROMOTION", "INFO"},
		{"PROMOTION_VERSION_CREATED", "PROMOTION", "NOTICE"},
		{"PROMOTION_RULE_CHANGED", "PROMOTION", "HIGH"},
		{"PROMOTION_APPLIED", "PROMOTION", "INFO"},
		{"PROMOTION_DEACTIVATED", "PROMOTION", "HIGH"},
	}

	for _, act := range expectedAuditActions {
		var active bool
		var severity, module string
		err := migrationPool.QueryRow(ctx, `
			SELECT active, severity, module
			FROM audit.audit_action_catalog
			WHERE code = $1
		`, act.code).Scan(&active, &severity, &module)
		if err != nil {
			t.Errorf("audit action %s not found: %v", act.code, err)
			continue
		}
		if !active {
			t.Errorf("audit action %s is inactive", act.code)
		}
		if module != act.module {
			t.Errorf("audit action %s module mismatch: got %s, want %s", act.code, module, act.module)
		}
		if severity != act.severity {
			t.Errorf("audit action %s severity mismatch: got %s, want %s", act.code, severity, act.severity)
		}
	}
}

// 9. Verify Canonical Promotion Permission Seeds
func TestPromotion_Migration000012_PermissionSeeds(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	expectedPermissions := []struct {
		code   string
		module string
		action string
	}{
		{"promotion.read", "PROMOTION", "PROMOTION_READ"},
		{"promotion.create", "PROMOTION", "PROMOTION_CREATE"},
		{"promotion.update", "PROMOTION", "PROMOTION_UPDATE"},
		{"promotion.submit", "PROMOTION", "PROMOTION_SUBMIT"},
		{"promotion.approve", "PROMOTION", "PROMOTION_APPROVE"},
		{"promotion.activate", "PROMOTION", "PROMOTION_ACTIVATE"},
		{"promotion.pause", "PROMOTION", "PROMOTION_PAUSE"},
		{"promotion.expire", "PROMOTION", "PROMOTION_EXPIRE"},
		{"promotion.evaluate", "PROMOTION", "PROMOTION_EVALUATE"},
		{"promotion.schedule", "PROMOTION", "PROMOTION_SCHEDULE"},
	}

	for _, perm := range expectedPermissions {
		var name, module, action string
		err := migrationPool.QueryRow(ctx, `
			SELECT name, module, action
			FROM identity.permission
			WHERE code = $1
		`, perm.code).Scan(&name, &module, &action)
		if err != nil {
			t.Errorf("permission %s not found: %v", perm.code, err)
			continue
		}
		if module != perm.module {
			t.Errorf("permission %s module mismatch: got %s, want %s", perm.code, module, perm.module)
		}
		if action != perm.action {
			t.Errorf("permission %s action mismatch: got %s, want %s", perm.code, action, perm.action)
		}
	}
}

// 10. Verify Idempotency of Migration 000012 (Applying twice on same database)
func TestPromotion_Migration000012_IdempotentReapplication(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	// Re-apply migration 000012
	migrationDir := getMigrationDir()
	mig012, err := os.ReadFile(filepath.Join(migrationDir, "000012_promotion_hardening.sql"))
	if err != nil {
		t.Fatalf("read migration 000012: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, string(mig012)); err != nil {
		t.Fatalf("re-apply migration 000012 failed (violates idempotency): %v", err)
	}

	// Verify status constraint remains functional
	var count int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint WHERE conname = 'ck_promotion_status'
	`).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("ck_promotion_status constraint missing after reapplication: count=%d, err=%v", count, err)
	}
}
