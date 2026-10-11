package master_test

import (
	"context"
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
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/master"
	"example.com/nexora/backend/internal/organization"
	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/outbox"
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

	dbName := fmt.Sprintf("nexora_master_test_%d", time.Now().UnixNano())
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

	// Run migrations 000001-000006, 000008, 000009 (skipping 000007 reserved for CRM)
	_, sourceFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations")
	for _, name := range []string{
		"000001_baseline_hardened.sql",
		"000002_refresh_tokens.sql",
		"000003_outbox_leases.sql",
		"000004_audit_hardening.sql",
		"000005_auth_rls_bootstrap.sql",
		"000006_identity_permissions.sql",
		"000008_organization_hardening.sql",
		"000009_master_hardening.sql",
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
		GRANT USAGE ON SCHEMA identity, organization, master, platform, audit, integration TO nexora_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, organization, master, platform, audit, integration TO nexora_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity, organization, master, platform, audit, integration TO nexora_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA platform TO nexora_app;
	`
	if _, err := migrationPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant permissions to nexora_app: %v", err)
	}

	return migrationPool, appPool
}

func TestMaster_Integration_Migration000009_IdempotencyAndSeeds(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}

	ctx := context.Background()

	// 1. Verify audit action catalog seeds (DEC-B1-008)
	var countAudit int
	err := appPool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_action_catalog WHERE code IN ('BRAND_CREATED', 'BRAND_UPDATED')`).Scan(&countAudit)
	if err != nil {
		t.Fatalf("query audit actions: %v", err)
	}
	if countAudit != 2 {
		t.Fatalf("expected 2 brand audit actions, got: %d", countAudit)
	}

	// 2. Verify reference permissions seeds (DEC-B1-009)
	var countPerms int
	err = appPool.QueryRow(ctx, `SELECT count(*) FROM identity.permission WHERE code IN ('brand.create', 'brand.update', 'category.create', 'category.update', 'uom.create', 'uom.update')`).Scan(&countPerms)
	if err != nil {
		t.Fatalf("query permissions: %v", err)
	}
	if countPerms != 6 {
		t.Fatalf("expected 6 reference data permissions, got: %d", countPerms)
	}

	// 3. Verify Idempotency of Migration 000009
	_, sourceFile, _, _ := runtime.Caller(0)
	migration009Path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations", "000009_master_hardening.sql")
	content, err := os.ReadFile(migration009Path)
	if err != nil {
		t.Fatalf("read migration 000009: %v", err)
	}

	if _, err := migrationPool.Exec(ctx, string(content)); err != nil {
		t.Fatalf("re-applying migration 000009 failed (not idempotent): %v", err)
	}

	// Verify counts unchanged
	err = appPool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_action_catalog WHERE code IN ('BRAND_CREATED', 'BRAND_UPDATED')`).Scan(&countAudit)
	if err != nil || countAudit != 2 {
		t.Fatalf("expected count 2 after re-apply, got %d (err: %v)", countAudit, err)
	}
}

func TestMaster_Integration_RLSTenantIsolation(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}

	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := master.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := master.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantA := uuid.New()
	tenantB := uuid.New()
	actorA := uuid.New()

	ctxA := postgres.WithTenantID(context.Background(), tenantA)
	ctxB := postgres.WithTenantID(context.Background(), tenantB)

	// Create Brand, Category, UOM, Product in Tenant A
	brandA, err := svc.CreateBrand(ctxA, &actorA, master.CreateBrandRequest{
		Code: "BRAND-A",
		Name: "Brand Tenant A",
	})
	if err != nil {
		t.Fatalf("create brand A: %v", err)
	}

	catA, err := svc.CreateCategory(ctxA, &actorA, master.CreateCategoryRequest{
		Code: "CAT-A",
		Name: "Category Tenant A",
	})
	if err != nil {
		t.Fatalf("create cat A: %v", err)
	}

	scale0 := int16(0)
	uomA, err := svc.CreateUOM(ctxA, &actorA, master.CreateUOMRequest{
		Code:           "UOM-A",
		Name:           "UOM Tenant A",
		PrecisionScale: &scale0,
	})
	if err != nil {
		t.Fatalf("create uom A: %v", err)
	}

	prodA, err := svc.CreateProduct(ctxA, &actorA, master.CreateProductRequest{
		SKU:        "PROD-A-001",
		Name:       "Product Tenant A",
		BrandID:    &brandA.ID,
		CategoryID: &catA.ID,
		BaseUOMID:  uomA.ID,
	})
	if err != nil {
		t.Fatalf("create product A: %v", err)
	}

	bcA, err := svc.CreateBarcode(ctxA, &actorA, prodA.ID, master.CreateBarcodeRequest{
		Barcode:   "777777777777",
		IsPrimary: true,
	})
	if err != nil {
		t.Fatalf("create barcode A: %v", err)
	}

	// 1. Verify Tenant B cannot read Brand A
	_, err = svc.GetBrand(ctxB, brandA.ID)
	if !errors.Is(err, master.ErrBrandNotFound) {
		t.Fatalf("tenant B should not see tenant A brand; got: %v", err)
	}

	// 2. Verify Tenant B cannot read Category A
	_, err = svc.GetCategory(ctxB, catA.ID)
	if !errors.Is(err, master.ErrCategoryNotFound) {
		t.Fatalf("tenant B should not see tenant A category; got: %v", err)
	}

	// 3. Verify Tenant B cannot read UOM A
	_, err = svc.GetUOM(ctxB, uomA.ID)
	if !errors.Is(err, master.ErrUOMNotFound) {
		t.Fatalf("tenant B should not see tenant A uom; got: %v", err)
	}

	// 4. Verify Tenant B cannot read Product A
	_, err = svc.GetProduct(ctxB, prodA.ID)
	if !errors.Is(err, master.ErrProductNotFound) {
		t.Fatalf("tenant B should not see tenant A product; got: %v", err)
	}

	// 5. Verify Tenant B cannot scan Barcode A
	_, err = svc.LookupBarcode(ctxB, bcA.Barcode)
	if !errors.Is(err, master.ErrBarcodeNotFound) {
		t.Fatalf("tenant B should not scan tenant A barcode; got: %v", err)
	}
}

func TestMaster_Integration_HybridCompanyOwnership(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}

	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	masterStore := master.NewPgxStore(appPool)
	orgStore := organization.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	masterSvc := master.NewService(masterStore, tm, auditStore, outboxWriter, logger)
	orgSvc := organization.NewService(orgStore, tm, auditStore, outboxWriter, logger)

	tenantA := uuid.New()
	tenantB := uuid.New()
	actorA := uuid.New()
	actorB := uuid.New()

	ctxA := postgres.WithTenantID(context.Background(), tenantA)
	ctxB := postgres.WithTenantID(context.Background(), tenantB)

	// Create Company under Tenant A
	compA, err := orgSvc.CreateCompany(ctxA, &actorA, organization.CreateCompanyRequest{
		Code:         "COMP-A",
		Name:         "Company Alpha",
		BaseCurrency: "USD",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create company A: %v", err)
	}

	// Create Company under Tenant B
	compB, err := orgSvc.CreateCompany(ctxB, &actorB, organization.CreateCompanyRequest{
		Code:         "COMP-B",
		Name:         "Company Beta",
		BaseCurrency: "EUR",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create company B: %v", err)
	}

	// Create Base UOM under Tenant A
	uomA, err := masterSvc.CreateUOM(ctxA, &actorA, master.CreateUOMRequest{
		Code: "EA",
		Name: "Each",
	})
	if err != nil {
		t.Fatalf("create uom A: %v", err)
	}

	// 1. Tenant-wide product under Tenant A (company_id is nil) - Valid
	pTenantWide, err := masterSvc.CreateProduct(ctxA, &actorA, master.CreateProductRequest{
		SKU:       "SKU-TW-01",
		Name:      "Tenant Wide Product",
		BaseUOMID: uomA.ID,
	})
	if err != nil {
		t.Fatalf("create tenant wide product: %v", err)
	}
	if pTenantWide.CompanyID != nil {
		t.Fatalf("expected nil company_id")
	}

	// 2. Company-scoped product under Tenant A referencing compA - Valid
	pCompA, err := masterSvc.CreateProduct(ctxA, &actorA, master.CreateProductRequest{
		CompanyID: &compA.ID,
		SKU:       "SKU-CA-01",
		Name:      "Company Scoped Product",
		BaseUOMID: uomA.ID,
	})
	if err != nil {
		t.Fatalf("create company scoped product: %v", err)
	}
	if pCompA.CompanyID == nil || *pCompA.CompanyID != compA.ID {
		t.Fatalf("expected company_id %v, got %v", compA.ID, pCompA.CompanyID)
	}

	// 3. Attempting to create product under Tenant A referencing Tenant B's company - MUST FAIL CLOSED
	_, err = masterSvc.CreateProduct(ctxA, &actorA, master.CreateProductRequest{
		CompanyID: &compB.ID,
		SKU:       "SKU-FAIL-01",
		Name:      "Cross Tenant Fail Product",
		BaseUOMID: uomA.ID,
	})
	if !errors.Is(err, master.ErrCompanyCrossTenant) {
		t.Fatalf("expected ErrCompanyCrossTenant when referencing Tenant B company, got: %v", err)
	}
}

func TestMaster_Integration_OptimisticLockingAndLifecycle(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}

	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	masterStore := master.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := master.NewService(masterStore, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	actorID := uuid.New()
	ctx := postgres.WithTenantID(context.Background(), tenantID)

	uom, _ := svc.CreateUOM(ctx, &actorID, master.CreateUOMRequest{Code: "PCS", Name: "Pieces"})

	p, err := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:       "SKU-OPT-01",
		Name:      "Optimistic Product",
		BaseUOMID: uom.ID,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if p.Version != 1 {
		t.Fatalf("expected initial version 1, got %d", p.Version)
	}

	// Update 1
	name1 := "Optimistic Product Updated 1"
	p1, err := svc.UpdateProduct(ctx, &actorID, p.ID, master.UpdateProductRequest{Name: &name1})
	if err != nil {
		t.Fatalf("update 1: %v", err)
	}
	if p1.Version != 2 {
		t.Fatalf("expected version 2, got %d", p1.Version)
	}

	// Deactivate
	deactivated, err := svc.DeactivateProduct(ctx, &actorID, p.ID, "Seasonal pause")
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if deactivated.Status != master.StatusInactive {
		t.Fatalf("expected INACTIVE, got %s", deactivated.Status)
	}

	// Reactivate
	activated, err := svc.ActivateProduct(ctx, &actorID, p.ID)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if activated.Status != master.StatusActive {
		t.Fatalf("expected ACTIVE, got %s", activated.Status)
	}
}

func TestMaster_Integration_AuditAndOutboxAtomicity(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}

	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	masterStore := master.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := master.NewService(masterStore, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	actorID := uuid.New()
	ctx := postgres.WithTenantID(context.Background(), tenantID)

	uom, _ := svc.CreateUOM(ctx, &actorID, master.CreateUOMRequest{Code: "BOX", Name: "Box"})

	p, err := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:       "SKU-TX-ATOM",
		Name:      "Atomic Transaction Product",
		BaseUOMID: uom.ID,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	// Verify Audit Record created in DB
	var auditCount int
	err = migrationPool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_log WHERE tenant_id = $1 AND action = $2`, tenantID, master.AuditActionProductCreated).Scan(&auditCount)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit log entry, got %d", auditCount)
	}

	// Verify Outbox Event created in DB
	var outboxCount int
	err = migrationPool.QueryRow(ctx, `SELECT count(*) FROM integration.outbox_event WHERE tenant_id = $1 AND aggregate_id = $2 AND event_type = $3`, tenantID, p.ID, master.EventProductCreated).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", outboxCount)
	}
}
