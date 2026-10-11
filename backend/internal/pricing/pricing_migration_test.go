package pricing_test

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

	dbName := fmt.Sprintf("nexora_pricing_migration_test_%d", time.Now().UnixNano())
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

// 1. Verify Baseline Checksums 000001-000010 remain byte-for-byte unchanged
func TestPricing_Migration000011_BaselineChecksumIntegrity(t *testing.T) {
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

	// Verify 000011 exists and compute its hash
	content011, err := os.ReadFile(filepath.Join(migrationDir, "000011_pricing_hardening.sql"))
	if err != nil {
		t.Fatalf("failed to read 000011_pricing_hardening.sql: %v", err)
	}
	hash011 := sha256.Sum256(content011)
	t.Logf("Migration 000011 computed SHA-256: %s", hex.EncodeToString(hash011[:]))
}

// 2. Verify Schema Structure, Indexes, and Constraints after 000011
func TestPricing_Migration000011_SchemaStructure(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	// 2.1 Verify pricing.price_list columns: is_default, version
	var isDefaultType, isDefaultNullable, isDefaultDefault string
	err := migrationPool.QueryRow(ctx, `
		SELECT data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'pricing' AND table_name = 'price_list' AND column_name = 'is_default'
	`).Scan(&isDefaultType, &isDefaultNullable, &isDefaultDefault)
	if err != nil {
		t.Fatalf("query column is_default: %v", err)
	}
	if isDefaultType != "boolean" || isDefaultNullable != "NO" {
		t.Errorf("unexpected is_default column def: type=%s, nullable=%s", isDefaultType, isDefaultNullable)
	}

	var verType, verNullable string
	err = migrationPool.QueryRow(ctx, `
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'pricing' AND table_name = 'price_list' AND column_name = 'version'
	`).Scan(&verType, &verNullable)
	if err != nil {
		t.Fatalf("query price_list column version: %v", err)
	}
	if verType != "bigint" || verNullable != "NO" {
		t.Errorf("unexpected price_list version column def: type=%s, nullable=%s", verType, verNullable)
	}

	// 2.2 Verify pricing.price_rule columns: variant_id, version
	var variantType, variantNullable string
	err = migrationPool.QueryRow(ctx, `
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'pricing' AND table_name = 'price_rule' AND column_name = 'variant_id'
	`).Scan(&variantType, &variantNullable)
	if err != nil {
		t.Fatalf("query price_rule column variant_id: %v", err)
	}
	if variantType != "uuid" || variantNullable != "YES" {
		t.Errorf("unexpected variant_id column def: type=%s, nullable=%s", variantType, variantNullable)
	}

	// 2.3 Verify Check Constraints
	constraints := []string{
		"ck_price_list_default_tenant_wide",
		"price_list_status_check",
		"price_rule_status_check",
	}
	for _, cName := range constraints {
		var count int
		err = migrationPool.QueryRow(ctx, `
			SELECT count(*) FROM pg_constraint WHERE conname = $1
		`, cName).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("constraint %s not found (count=%d, err=%v)", cName, count, err)
		}
	}

	// 2.4 Verify Indexes
	indexes := []string{
		"ux_price_list_active_default",
		"ix_price_rule_variant_lookup",
		"ix_price_rule_list_status",
	}
	for _, idx := range indexes {
		var count int
		err = migrationPool.QueryRow(ctx, `
			SELECT count(*) FROM pg_indexes WHERE indexname = $1
		`, idx).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("index %s not found (count=%d, err=%v)", idx, count, err)
		}
	}

	// 2.5 Confirm ix_price_rule_quote_eval does NOT exist
	var evalIdxCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes WHERE indexname = 'ix_price_rule_quote_eval'
	`).Scan(&evalIdxCount)
	if err != nil || evalIdxCount != 0 {
		t.Errorf("ix_price_rule_quote_eval should NOT exist, count=%d", evalIdxCount)
	}
}

// 3. Verify Existing-Data Compatibility (Pre-000011 data preserves values and defaults)
func TestPricing_Migration000011_ExistingDataCompatibility(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 10)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	// Seed baseline master data before 000011 using migrationPool
	tenantID := uuid.New()
	uomID := uuid.New()
	brandID := uuid.New()
	catID := uuid.New()
	productID := uuid.New()
	companyID := uuid.New()
	priceListID := uuid.New()
	priceRuleID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organization.company (id, tenant_id, code, name, base_currency) VALUES ($1, $2, 'CMP-COMPAT', 'Compat Co', 'IDR')`, []any{companyID, tenantID}},
		{`INSERT INTO master.brand (id, tenant_id, code, name) VALUES ($1, $2, 'BR-COMPAT', 'Compat Brand')`, []any{brandID, tenantID}},
		{`INSERT INTO master.category (id, tenant_id, code, name) VALUES ($1, $2, 'CAT-COMPAT', 'Compat Cat')`, []any{catID, tenantID}},
		{`INSERT INTO master.uom (id, tenant_id, code, name) VALUES ($1, $2, 'PCS', 'Pieces')`, []any{uomID, tenantID}},
		{`INSERT INTO master.product (id, tenant_id, company_id, sku, name, brand_id, category_id, base_uom_id) VALUES ($1, $2, $3, 'SKU-COMPAT', 'Compat Product', $4, $5, $6)`, []any{productID, tenantID, companyID, brandID, catID, uomID}},
		{`INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, status) VALUES ($1, $2, $3, 'PL-COMPAT', 'Pre-existing List', 'IDR', 'ALL', 'ACTIVE')`, []any{priceListID, tenantID, companyID}},
		{`INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, min_qty, unit_price, effective_from, status) VALUES ($1, $2, $3, $4, 1.0, 50000.0, NOW() - INTERVAL '1 day', 'ACTIVE')`, []any{priceRuleID, tenantID, priceListID, productID}},
	} {
		if _, err := migrationPool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed pre-000011 data: %v", err)
		}
	}

	// Apply migration 000011
	migrationDir := getMigrationDir()
	mig011, err := os.ReadFile(filepath.Join(migrationDir, "000011_pricing_hardening.sql"))
	if err != nil {
		t.Fatalf("read migration 000011: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, string(mig011)); err != nil {
		t.Fatalf("apply migration 000011 over existing data: %v", err)
	}

	// Verify existing price_list received is_default = false and version = 1
	var isDefault bool
	var version int64
	var code, name string
	err = migrationPool.QueryRow(ctx, `
		SELECT is_default, version, code, name FROM pricing.price_list WHERE id = $1
	`, priceListID).Scan(&isDefault, &version, &code, &name)
	if err != nil {
		t.Fatalf("query migrated price_list: %v", err)
	}
	if isDefault != false {
		t.Errorf("expected is_default=false, got %v", isDefault)
	}
	if version != 1 {
		t.Errorf("expected version=1, got %d", version)
	}
	if code != "PL-COMPAT" || name != "Pre-existing List" {
		t.Errorf("pre-existing business columns corrupted: code=%s, name=%s", code, name)
	}

	// Verify existing price_rule received variant_id = NULL and version = 1
	var ruleVariantID *uuid.UUID
	var ruleVersion int64
	var unitPrice float64
	err = migrationPool.QueryRow(ctx, `
		SELECT variant_id, version, unit_price FROM pricing.price_rule WHERE id = $1
	`, priceRuleID).Scan(&ruleVariantID, &ruleVersion, &unitPrice)
	if err != nil {
		t.Fatalf("query migrated price_rule: %v", err)
	}
	if ruleVariantID != nil {
		t.Errorf("expected variant_id=nil, got %v", ruleVariantID)
	}
	if ruleVersion != 1 {
		t.Errorf("expected version=1, got %d", ruleVersion)
	}
	if unitPrice != 50000.0 {
		t.Errorf("expected unit_price=50000, got %f", unitPrice)
	}
}

// 4. Verify Default Price List Invariants (uniqueness, tenant-wide scope, lifecycle)
func TestPricing_Migration000011_DefaultPriceListInvariants(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()
	companyA := uuid.New()

	// Seed company
	_, err := migrationPool.Exec(ctx, `
		INSERT INTO organization.company (id, tenant_id, code, name, base_currency)
		VALUES ($1, $2, 'CMP-A', 'Company A', 'IDR');
	`, companyA, tenantA)
	if err != nil {
		t.Fatalf("seed company: %v", err)
	}

	// 4.1 Insert valid tenant-wide active default price list (company_id IS NULL)
	listA1 := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, NULL, 'PL-DEF-IDR-POS', 'Default POS IDR', 'IDR', 'POS', true, 'ACTIVE');
	`, listA1, tenantA)
	if err != nil {
		t.Fatalf("insert tenant-wide default list: %v", err)
	}

	// 4.2 Attempt second active default with same (tenant, currency, channel) -> MUST FAIL (ux_price_list_active_default)
	listA2 := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, NULL, 'PL-DEF-IDR-POS-2', 'Duplicate Default', 'IDR', 'POS', true, 'ACTIVE');
	`, listA2, tenantA)
	if err == nil {
		t.Fatalf("expected duplicate active default to be rejected by unique index, but succeeded")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code != "23505" {
		t.Errorf("expected unique_violation 23505, got %s", pgErr.Code)
	}

	// 4.3 Attempt company-specific default (company_id NOT NULL AND is_default = true) -> MUST FAIL (ck_price_list_default_tenant_wide)
	listCoDef := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, $3, 'PL-CO-DEF', 'Company Default', 'IDR', 'POS', true, 'ACTIVE');
	`, listCoDef, tenantA, companyA)
	if err == nil {
		t.Fatalf("expected company-specific default to be rejected by ck_price_list_default_tenant_wide, but succeeded")
	}

	// 4.4 Company-specific non-default (company_id NOT NULL AND is_default = false) -> MUST SUCCEED
	listCoNonDef := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, $3, 'PL-CO-NONDEF', 'Company Non-Default', 'IDR', 'POS', false, 'ACTIVE');
	`, listCoNonDef, tenantA, companyA)
	if err != nil {
		t.Fatalf("insert company-specific non-default: %v", err)
	}

	// 4.5 Active default in different channel ('ALL') -> MUST SUCCEED
	listDefAll := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, NULL, 'PL-DEF-IDR-ALL', 'Default ALL IDR', 'IDR', 'ALL', true, 'ACTIVE');
	`, listDefAll, tenantA)
	if err != nil {
		t.Fatalf("insert default with different channel: %v", err)
	}

	// 4.6 Active default in different currency ('USD') -> MUST SUCCEED
	listDefUSD := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, NULL, 'PL-DEF-USD-POS', 'Default POS USD', 'USD', 'POS', true, 'ACTIVE');
	`, listDefUSD, tenantA)
	if err != nil {
		t.Fatalf("insert default with different currency: %v", err)
	}

	// 4.7 Active default in different tenant (Tenant B) -> MUST SUCCEED
	listDefTenantB := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, NULL, 'PL-DEF-TB-POS', 'Default Tenant B', 'IDR', 'POS', true, 'ACTIVE');
	`, listDefTenantB, tenantB)
	if err != nil {
		t.Fatalf("insert default in different tenant: %v", err)
	}

	// 4.8 Deactivate listA1 (status -> 'INACTIVE') frees the active-default slot
	_, err = migrationPool.Exec(ctx, `
		UPDATE pricing.price_list SET status = 'INACTIVE' WHERE id = $1;
	`, listA1)
	if err != nil {
		t.Fatalf("deactivate default list: %v", err)
	}

	// Now listA2 can become the new active default for (Tenant A, IDR, POS)
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, company_id, code, name, currency, channel, is_default, status)
		VALUES ($1, $2, NULL, 'PL-DEF-IDR-POS-REPLACE', 'Replacement Default', 'IDR', 'POS', true, 'ACTIVE');
	`, listA2, tenantA)
	if err != nil {
		t.Fatalf("insert replacement active default after deactivation: %v", err)
	}
}

// 5. Verify Variant Integrity & Consistency Triggers (including RLS non-superuser verification)
func TestPricing_Migration000011_VariantIntegrityAndTriggers(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t, 11)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	tenantA := uuid.New()
	tenantB := uuid.New()

	uomID := uuid.New()
	uomB := uuid.New()
	brandA := uuid.New()
	catA := uuid.New()
	prodA1 := uuid.New()
	prodA2 := uuid.New()
	variantA1 := uuid.New()
	variantA2 := uuid.New()

	brandB := uuid.New()
	catB := uuid.New()
	prodB := uuid.New()
	variantB := uuid.New()

	priceListA := uuid.New()

	// Seed Master Data for Tenant A and Tenant B using migrationPool
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO master.uom (id, tenant_id, code, name) VALUES ($1, $2, 'PCS', 'Pieces')`, []any{uomID, tenantA}},
		{`INSERT INTO master.uom (id, tenant_id, code, name) VALUES ($1, $2, 'PCS', 'Pieces')`, []any{uomB, tenantB}},
		{`INSERT INTO master.brand (id, tenant_id, code, name) VALUES ($1, $2, 'BR-A', 'Brand A')`, []any{brandA, tenantA}},
		{`INSERT INTO master.category (id, tenant_id, code, name) VALUES ($1, $2, 'CAT-A', 'Cat A')`, []any{catA, tenantA}},
		{`INSERT INTO master.product (id, tenant_id, sku, name, brand_id, category_id, base_uom_id) VALUES ($1, $2, 'SKU-A1', 'Product A1', $3, $4, $5)`, []any{prodA1, tenantA, brandA, catA, uomID}},
		{`INSERT INTO master.product (id, tenant_id, sku, name, brand_id, category_id, base_uom_id) VALUES ($1, $2, 'SKU-A2', 'Product A2', $3, $4, $5)`, []any{prodA2, tenantA, brandA, catA, uomID}},
		{`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name) VALUES ($1, $2, $3, 'VAR-A1', 'Variant A1-1')`, []any{variantA1, tenantA, prodA1}},
		{`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name) VALUES ($1, $2, $3, 'VAR-A2', 'Variant A2-1')`, []any{variantA2, tenantA, prodA2}},
		{`INSERT INTO master.brand (id, tenant_id, code, name) VALUES ($1, $2, 'BR-B', 'Brand B')`, []any{brandB, tenantB}},
		{`INSERT INTO master.category (id, tenant_id, code, name) VALUES ($1, $2, 'CAT-B', 'Cat B')`, []any{catB, tenantB}},
		{`INSERT INTO master.product (id, tenant_id, sku, name, brand_id, category_id, base_uom_id) VALUES ($1, $2, 'SKU-B', 'Product B', $3, $4, $5)`, []any{prodB, tenantB, brandB, catB, uomB}},
		{`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name) VALUES ($1, $2, $3, 'VAR-B', 'Variant B')`, []any{variantB, tenantB, prodB}},
		{`INSERT INTO pricing.price_list (id, tenant_id, code, name, currency, channel, status) VALUES ($1, $2, 'PL-A', 'Price List A', 'IDR', 'ALL', 'ACTIVE')`, []any{priceListA, tenantA}},
	} {
		if _, err := migrationPool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed master test fixtures: %v", err)
		}
	}

	// 5.1 Valid variant belonging to prodA1 and tenantA under nexora_app role with tenant context -> MUST SUCCEED
	rule1 := uuid.New()
	txA, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txA: %v", err)
	}
	defer txA.Rollback(ctx)

	if _, err := txA.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantA.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}
	_, err = txA.Exec(ctx, `
		INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, variant_id, min_qty, unit_price, effective_from, status)
		VALUES ($1, $2, $3, $4, $5, 1.0, 10000.0, NOW(), 'ACTIVE');
	`, rule1, tenantA, priceListA, prodA1, variantA1)
	if err != nil {
		t.Fatalf("insert valid variant price rule as app role: %v", err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit txA: %v", err)
	}

	// 5.2 Product-level rule (variant_id IS NULL) -> MUST SUCCEED
	ruleNullVar := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, variant_id, min_qty, unit_price, effective_from, status)
		VALUES ($1, $2, $3, $4, NULL, 1.0, 12000.0, NOW(), 'ACTIVE');
	`, ruleNullVar, tenantA, priceListA, prodA1)
	if err != nil {
		t.Fatalf("insert valid product-level rule: %v", err)
	}

	// 5.3 Non-existent variant -> MUST FAIL (FK violation)
	ruleNonExistVar := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, variant_id, min_qty, unit_price, effective_from, status)
		VALUES ($1, $2, $3, $4, $5, 1.0, 10000.0, NOW(), 'ACTIVE');
	`, ruleNonExistVar, tenantA, priceListA, prodA1, uuid.New())
	if err == nil {
		t.Fatalf("expected non-existent variant to fail FK, but succeeded")
	}

	// 5.4 Cross-tenant variant (variantB belongs to tenantB, rule in tenantA) -> MUST FAIL (trg_same_tenant_pricerule_variant / consistency trigger)
	ruleCrossTenant := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, variant_id, min_qty, unit_price, effective_from, status)
		VALUES ($1, $2, $3, $4, $5, 1.0, 10000.0, NOW(), 'ACTIVE');
	`, ruleCrossTenant, tenantA, priceListA, prodA1, variantB)
	if err == nil {
		t.Fatalf("expected cross-tenant variant to fail trigger, but succeeded")
	}

	// 5.5 Mismatched product (variantA2 belongs to prodA2, but rule targets prodA1) -> MUST FAIL (trg_price_rule_variant_product_match)
	ruleMismatchProd := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, variant_id, min_qty, unit_price, effective_from, status)
		VALUES ($1, $2, $3, $4, $5, 1.0, 10000.0, NOW(), 'ACTIVE');
	`, ruleMismatchProd, tenantA, priceListA, prodA1, variantA2)
	if err == nil {
		t.Fatalf("expected mismatched variant-product to fail trigger, but succeeded")
	}

	// 5.6 ON DELETE RESTRICT on master.product_variant while referenced by price_rule -> MUST FAIL
	_, err = migrationPool.Exec(ctx, `
		DELETE FROM master.product_variant WHERE id = $1;
	`, variantA1)
	if err == nil {
		t.Fatalf("expected variant deletion to be restricted while referenced by price_rule, but succeeded")
	}

	// 5.7 ON DELETE RESTRICT holds even if price_rule is INACTIVE or EXPIRED
	_, err = migrationPool.Exec(ctx, `
		UPDATE pricing.price_rule SET status = 'EXPIRED' WHERE id = $1;
	`, rule1)
	if err != nil {
		t.Fatalf("expire price rule: %v", err)
	}
	_, err = migrationPool.Exec(ctx, `
		DELETE FROM master.product_variant WHERE id = $1;
	`, variantA1)
	if err == nil {
		t.Fatalf("expected variant deletion to remain restricted even when price_rule is expired, but succeeded")
	}
}

// 6. Verify Status Constraints for Price List and Price Rule
func TestPricing_Migration000011_StatusConstraints(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	tenantID := uuid.New()
	uomID := uuid.New()
	brandID := uuid.New()
	catID := uuid.New()
	productID := uuid.New()
	priceListID := uuid.New()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO master.uom (id, tenant_id, code, name) VALUES ($1, $2, 'PCS', 'Pieces')`, []any{uomID, tenantID}},
		{`INSERT INTO master.brand (id, tenant_id, code, name) VALUES ($1, $2, 'BR-STAT', 'Brand Stat')`, []any{brandID, tenantID}},
		{`INSERT INTO master.category (id, tenant_id, code, name) VALUES ($1, $2, 'CAT-STAT', 'Cat Stat')`, []any{catID, tenantID}},
		{`INSERT INTO master.product (id, tenant_id, sku, name, brand_id, category_id, base_uom_id) VALUES ($1, $2, 'SKU-STAT', 'Product Stat', $3, $4, $5)`, []any{productID, tenantID, brandID, catID, uomID}},
		{`INSERT INTO pricing.price_list (id, tenant_id, code, name, currency, channel, status) VALUES ($1, $2, 'PL-STAT-BASE', 'Base List', 'IDR', 'ALL', 'ACTIVE')`, []any{priceListID, tenantID}},
	} {
		if _, err := migrationPool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed fixtures: %v", err)
		}
	}

	// 6.1 Price List Status: Allowed = ACTIVE, INACTIVE, ARCHIVED
	validListStatuses := []string{"ACTIVE", "INACTIVE", "ARCHIVED"}
	for _, st := range validListStatuses {
		id := uuid.New()
		_, err := migrationPool.Exec(ctx, `
			INSERT INTO pricing.price_list (id, tenant_id, code, name, currency, channel, status)
			VALUES ($1, $2, $3, $4, 'IDR', 'ALL', $5);
		`, id, tenantID, fmt.Sprintf("PL-ST-%s", st), "List "+st, st)
		if err != nil {
			t.Errorf("expected price_list status %s to be accepted, got err: %v", st, err)
		}
	}

	// Invalid price_list status -> MUST FAIL
	_, err := migrationPool.Exec(ctx, `
		INSERT INTO pricing.price_list (id, tenant_id, code, name, currency, channel, status)
		VALUES (gen_random_uuid(), $1, 'PL-INVALID', 'Invalid List', 'IDR', 'ALL', 'DELETED');
	`, tenantID)
	if err == nil {
		t.Errorf("expected invalid price_list status 'DELETED' to fail check constraint")
	}

	// 6.2 Price Rule Status: Allowed = DRAFT, APPROVED, ACTIVE, INACTIVE, EXPIRED
	validRuleStatuses := []string{"DRAFT", "APPROVED", "ACTIVE", "INACTIVE", "EXPIRED"}
	for _, st := range validRuleStatuses {
		id := uuid.New()
		_, err := migrationPool.Exec(ctx, `
			INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, min_qty, unit_price, effective_from, status)
			VALUES ($1, $2, $3, $4, 1.0, 1000.0, NOW(), $5);
		`, id, tenantID, priceListID, productID, st)
		if err != nil {
			t.Errorf("expected price_rule status %s to be accepted, got err: %v", st, err)
		}
	}

	// Invalid price_rule statuses (SUBMITTED, PENDING_APPROVAL, REJECTED) -> MUST FAIL
	for _, inv := range []string{"SUBMITTED", "PENDING_APPROVAL", "REJECTED", "DELETED"} {
		_, err := migrationPool.Exec(ctx, `
			INSERT INTO pricing.price_rule (id, tenant_id, price_list_id, product_id, min_qty, unit_price, effective_from, status)
			VALUES (gen_random_uuid(), $1, $2, $3, 1.0, 1000.0, NOW(), $4);
		`, tenantID, priceListID, productID, inv)
		if err == nil {
			t.Errorf("expected invalid price_rule status '%s' to fail check constraint", inv)
		}
	}
}

// 7. Verify Audit Action Seeds in audit.audit_action_catalog
func TestPricing_Migration000011_AuditActionSeeds(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	expectedActions := []struct {
		code           string
		severity       string
		requiresReason bool
		active         bool
	}{
		// Existing baseline seeds
		{"PRICE_CREATED", "INFO", false, true},
		{"PRICE_UPDATED", "HIGH", true, true},
		{"PRICE_CHANGED", "HIGH", true, true},
		// New seeds in 000011
		{"PRICE_SUBMITTED", "NOTICE", false, true},
		{"PRICE_APPROVED", "HIGH", false, true},
		{"PRICE_REJECTED", "NOTICE", false, true},
		{"PRICE_ACTIVATED", "HIGH", false, true},
		{"PRICE_DEACTIVATED", "HIGH", false, true},
		{"PRICE_EXPIRED", "INFO", false, true},
	}

	for _, a := range expectedActions {
		var sev string
		var reqReason, active bool
		err := migrationPool.QueryRow(ctx, `
			SELECT severity, requires_reason, active
			FROM audit.audit_action_catalog
			WHERE code = $1
		`, a.code).Scan(&sev, &reqReason, &active)
		if err != nil {
			t.Errorf("audit action %s not found: %v", a.code, err)
			continue
		}
		if sev != a.severity || reqReason != a.requiresReason || active != a.active {
			t.Errorf("audit action %s mismatch: got (sev=%s, reason=%v, active=%v), expected (sev=%s, reason=%v, active=%v)",
				a.code, sev, reqReason, active, a.severity, a.requiresReason, a.active)
		}
	}
}

// 8. Verify Permission Seeds in identity.permission
func TestPricing_Migration000011_PermissionSeeds(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	expectedPerms := []struct {
		code   string
		module string
		action string
	}{
		{"price.read", "PRICING", "PRICE_READ"},
		{"price_list.create", "PRICING", "PRICE_LIST_CREATE"},
		{"price_list.update", "PRICING", "PRICE_LIST_UPDATE"},
		{"price_rule.read", "PRICING", "PRICE_RULE_READ"},
		{"price_rule.create", "PRICING", "PRICE_RULE_CREATE"},
		{"price_rule.update", "PRICING", "PRICE_RULE_UPDATE"},
		{"price_rule.submit", "PRICING", "PRICE_RULE_SUBMIT"},
		{"price_rule.approve", "PRICING", "PRICE_RULE_APPROVE"},
		{"price_rule.activate", "PRICING", "PRICE_RULE_ACTIVATE"},
		{"price_rule.deactivate", "PRICING", "PRICE_RULE_DEACTIVATE"},
	}

	for _, p := range expectedPerms {
		var module, action string
		err := migrationPool.QueryRow(ctx, `
			SELECT module, action
			FROM identity.permission
			WHERE code = $1
		`, p.code).Scan(&module, &action)
		if err != nil {
			t.Errorf("permission %s not found: %v", p.code, err)
			continue
		}
		if module != p.module || action != p.action {
			t.Errorf("permission %s mismatch: got (module=%s, action=%s), expected (module=%s, action=%s)",
				p.code, module, action, p.module, p.action)
		}
	}

	// Verify no role assignments were created by the migration
	var rolePermCount int
	err := migrationPool.QueryRow(ctx, `
		SELECT count(*)
		FROM identity.role_permission rp
		JOIN identity.permission p ON rp.permission_id = p.id
		WHERE p.module = 'PRICING'
	`).Scan(&rolePermCount)
	if err != nil {
		t.Fatalf("query role_permission count: %v", err)
	}
	if rolePermCount != 0 {
		t.Errorf("expected 0 role assignments created by migration for PRICING, got %d", rolePermCount)
	}
}

// 9. Verify Idempotency of Migration 000011 (Applying twice on same database)
func TestPricing_Migration000011_IdempotentReapplication(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 11)
	if migrationPool == nil {
		return
	}
	ctx := context.Background()

	// Re-apply migration 000011
	migrationDir := getMigrationDir()
	mig011, err := os.ReadFile(filepath.Join(migrationDir, "000011_pricing_hardening.sql"))
	if err != nil {
		t.Fatalf("read migration 000011: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, string(mig011)); err != nil {
		t.Fatalf("re-apply migration 000011 failed (violates idempotency): %v", err)
	}

	// Verify schema is still healthy after re-application
	var count int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'pricing' AND table_name = 'price_list' AND column_name = 'is_default'
	`).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("is_default verification failed after re-application: %v", err)
	}
}
