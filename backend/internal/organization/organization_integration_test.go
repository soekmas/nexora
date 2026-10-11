package organization_test

import (
	"context"
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
		Env:                 "test",
		LogLevel:            "debug",
		DBHost:              "127.0.0.1",
		DBPort:              15432,
		DBName:              "postgres",
		DBUser:              "nexora_test",
		DBPassword:          "test_secret_pass",
		DBSSLMode:           "disable",
		DBMaxConns:          10,
		DBMinConns:          2,
		DBMaxConnLifetime:   15 * time.Minute,
		DBConnectTimeout:    3 * time.Second,
		ShutdownTimeout:     5 * time.Second,
		HTTPPort:            8080,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		RateLimitRequests:   100,
		RateLimitWindow:     time.Minute,
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

	dbName := fmt.Sprintf("nexora_org_test_%d", time.Now().UnixNano())
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

	// Run migrations 000001 through 000006 and 000008 (skipping 000007 reserved for CRM)
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
		GRANT USAGE ON SCHEMA identity, organization, platform, audit, integration TO nexora_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, organization, platform, audit, integration TO nexora_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity, organization, platform, audit, integration TO nexora_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA platform TO nexora_app;
	`
	if _, err := migrationPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant permissions to nexora_app: %v", err)
	}

	return migrationPool, appPool
}

func TestOrganization_Integration_HierarchyLifecycle(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := organization.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := organization.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	actorID := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	// 1. Create Company
	comp, err := svc.CreateCompany(ctx, &actorID, organization.CreateCompanyRequest{
		Code: "CORP-01",
		Name: "Retail Holdings Corp",
	})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	// 2. Create Region
	reg, err := svc.CreateRegion(ctx, &actorID, organization.CreateRegionRequest{
		CompanyID: comp.ID,
		Code:      "REG-WEST",
		Name:      "West Territory",
	})
	if err != nil {
		t.Fatalf("create region: %v", err)
	}

	// 3. Create Store
	addr := "100 Retail Boulevard"
	storeEnt, err := svc.CreateStore(ctx, &actorID, organization.CreateStoreRequest{
		CompanyID: comp.ID,
		RegionID:  &reg.ID,
		Code:      "STORE-01",
		Name:      "Flagship Downtown",
		Address:   &addr,
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	// 4. Create Warehouse
	wh, err := svc.CreateWarehouse(ctx, &actorID, organization.CreateWarehouseRequest{
		CompanyID:     comp.ID,
		StoreID:       &storeEnt.ID,
		Code:          "WH-01",
		Name:          "Downtown Store Warehouse",
		WarehouseType: organization.WarehouseTypeStore,
	})
	if err != nil {
		t.Fatalf("create warehouse: %v", err)
	}

	// 5. Register Terminal
	termRes, err := svc.RegisterTerminal(ctx, &actorID, organization.RegisterTerminalRequest{
		CompanyID: comp.ID,
		StoreID:   storeEnt.ID,
		Code:      "POS-01",
		Name:      "Cash Register 1",
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if termRes.DeviceToken == "" {
		t.Fatalf("expected plaintext token returned")
	}

	// 6. Test Terminal Status Transitions (DISABLED and RETIRED allowed by migration 000008)
	disabledStatus := organization.TerminalStatusDisabled
	updatedTerm, err := svc.UpdateTerminal(ctx, &actorID, termRes.Terminal.ID, organization.UpdateTerminalRequest{
		Status: &disabledStatus,
	})
	if err != nil {
		t.Fatalf("update terminal status to DISABLED failed: %v", err)
	}
	if updatedTerm.Status != organization.TerminalStatusDisabled {
		t.Fatalf("expected status DISABLED, got %s", updatedTerm.Status)
	}

	retiredStatus := organization.TerminalStatusRetired
	updatedTerm2, err := svc.UpdateTerminal(ctx, &actorID, termRes.Terminal.ID, organization.UpdateTerminalRequest{
		Status: &retiredStatus,
	})
	if err != nil {
		t.Fatalf("update terminal status to RETIRED failed: %v", err)
	}
	if updatedTerm2.Status != organization.TerminalStatusRetired {
		t.Fatalf("expected status RETIRED, got %s", updatedTerm2.Status)
	}

	// Verify all entities can be retrieved
	if _, err := svc.GetCompany(ctx, comp.ID); err != nil {
		t.Fatalf("get company: %v", err)
	}
	if _, err := svc.GetRegion(ctx, reg.ID); err != nil {
		t.Fatalf("get region: %v", err)
	}
	if _, err := svc.GetStore(ctx, storeEnt.ID); err != nil {
		t.Fatalf("get store: %v", err)
	}
	if _, err := svc.GetWarehouse(ctx, wh.ID); err != nil {
		t.Fatalf("get warehouse: %v", err)
	}
	if _, err := svc.GetTerminal(ctx, termRes.Terminal.ID); err != nil {
		t.Fatalf("get terminal: %v", err)
	}
}

func TestOrganization_Integration_RLS_TenantIsolation(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := organization.NewPgxStore(appPool)
	svc := organization.NewService(store, tm, nil, nil, logger)

	tenantA := uuid.New()
	tenantB := uuid.New()
	actor := uuid.New()

	ctxA := postgres.WithTenantID(ctx, tenantA)
	ctxB := postgres.WithTenantID(ctx, tenantB)

	// Create hierarchy in Tenant A
	compA, err := svc.CreateCompany(ctxA, &actor, organization.CreateCompanyRequest{
		Code: "CORP-A",
		Name: "Tenant A Corp",
	})
	if err != nil {
		t.Fatalf("create company in tenant A: %v", err)
	}

	regA, err := svc.CreateRegion(ctxA, &actor, organization.CreateRegionRequest{
		CompanyID: compA.ID,
		Code:      "REG-A",
		Name:      "Tenant A Region",
	})
	if err != nil {
		t.Fatalf("create region in tenant A: %v", err)
	}

	storeA, err := svc.CreateStore(ctxA, &actor, organization.CreateStoreRequest{
		CompanyID: compA.ID,
		RegionID:  &regA.ID,
		Code:      "STORE-A",
		Name:      "Tenant A Store",
	})
	if err != nil {
		t.Fatalf("create store in tenant A: %v", err)
	}

	termResA, err := svc.RegisterTerminal(ctxA, &actor, organization.RegisterTerminalRequest{
		CompanyID: compA.ID,
		StoreID:   storeA.ID,
		Code:      "TERM-A",
		Name:      "Tenant A Terminal",
	})
	if err != nil {
		t.Fatalf("register terminal in tenant A: %v", err)
	}

	// Now connect as Tenant B: Under forced RLS, none of Tenant A's records should be visible
	if _, err := svc.GetCompany(ctxB, compA.ID); err == nil {
		t.Fatalf("RLS breach: Tenant B could read Tenant A Company")
	}

	// CRITICAL CHECK: Verify organization.region RLS policy added in migration 000008!
	if _, err := svc.GetRegion(ctxB, regA.ID); err == nil {
		t.Fatalf("RLS breach: Tenant B could read Tenant A Region! 000008 policy failed")
	}

	if _, err := svc.GetStore(ctxB, storeA.ID); err == nil {
		t.Fatalf("RLS breach: Tenant B could read Tenant A Store")
	}

	if _, err := svc.GetTerminal(ctxB, termResA.Terminal.ID); err == nil {
		t.Fatalf("RLS breach: Tenant B could read Tenant A Terminal")
	}

	// Tenant B listing operations must return 0 items
	companies, totalComp, err := svc.ListCompanies(ctxB, 10, 0)
	if err != nil || totalComp != 0 || len(companies) != 0 {
		t.Fatalf("RLS breach: Tenant B list companies returned items: total=%d", totalComp)
	}

	regions, totalReg, err := svc.ListRegions(ctxB, nil, 10, 0)
	if err != nil || totalReg != 0 || len(regions) != 0 {
		t.Fatalf("RLS breach: Tenant B list regions returned items: total=%d", totalReg)
	}
}

func TestOrganization_Integration_DeviceTokenSecurity(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := organization.NewPgxStore(appPool)
	svc := organization.NewService(store, tm, nil, nil, logger)

	tenantID := uuid.New()
	actor := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	comp, _ := svc.CreateCompany(ctx, &actor, organization.CreateCompanyRequest{Code: "C", Name: "C"})
	st, _ := svc.CreateStore(ctx, &actor, organization.CreateStoreRequest{CompanyID: comp.ID, Code: "S", Name: "S"})

	// 1. Register terminal and receive token
	res, err := svc.RegisterTerminal(ctx, &actor, organization.RegisterTerminalRequest{
		CompanyID: comp.ID,
		StoreID:   st.ID,
		Code:      "TERM-SEC",
		Name:      "Secure Terminal",
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	plainToken := res.DeviceToken
	if plainToken == "" {
		t.Fatalf("plaintext token was empty")
	}

	// 2. Direct DB verification: Check that only SHA-256 hash was persisted (DEC-B1-007)
	var storedDeviceID *string
	err = tm.WithinTx(ctx, func(txCtx context.Context) error {
		tRec, err := store.GetTerminalByID(txCtx, res.Terminal.ID)
		if err != nil {
			return err
		}
		storedDeviceID = tRec.DeviceIdentifier
		return nil
	})
	if err != nil {
		t.Fatalf("failed to retrieve terminal from db: %v", err)
	}

	if storedDeviceID == nil {
		t.Fatalf("stored device identifier was nil")
	}
	if *storedDeviceID == plainToken {
		t.Fatalf("SECURITY VIOLATION: Plaintext device token was stored in database!")
	}
	expectedHash := organization.HashDeviceToken(plainToken)
	if *storedDeviceID != expectedHash {
		t.Fatalf("mismatched hash in database: expected %s, got %s", expectedHash, *storedDeviceID)
	}

	// 3. Verify valid token
	term, err := svc.VerifyDeviceToken(ctx, res.Terminal.ID, plainToken)
	if err != nil {
		t.Fatalf("verify valid token failed: %v", err)
	}
	if term.ID != res.Terminal.ID {
		t.Fatalf("mismatched terminal ID")
	}

	// 4. Verify invalid token fails
	if _, err := svc.VerifyDeviceToken(ctx, res.Terminal.ID, "dev_tok_tamperedtoken123"); err == nil {
		t.Fatalf("expected invalid token to fail verification")
	}

	// 5. Rotate token
	rotRes, err := svc.RotateDeviceToken(ctx, &actor, res.Terminal.ID)
	if err != nil {
		t.Fatalf("rotate device token failed: %v", err)
	}

	// Old token must fail
	if _, err := svc.VerifyDeviceToken(ctx, res.Terminal.ID, plainToken); err == nil {
		t.Fatalf("old token succeeded after rotation!")
	}

	// New token must succeed
	if _, err := svc.VerifyDeviceToken(ctx, res.Terminal.ID, rotRes.DeviceToken); err != nil {
		t.Fatalf("new token verification failed: %v", err)
	}

	// 6. Revoke token
	if err := svc.RevokeDeviceToken(ctx, &actor, res.Terminal.ID); err != nil {
		t.Fatalf("revoke device token failed: %v", err)
	}

	// Verification must fail after revocation
	if _, err := svc.VerifyDeviceToken(ctx, res.Terminal.ID, rotRes.DeviceToken); err == nil {
		t.Fatalf("expected verification to fail after token revocation!")
	}
}

func TestOrganization_Integration_OptimisticConcurrency(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := organization.NewPgxStore(appPool)
	svc := organization.NewService(store, tm, nil, nil, logger)

	tenantID := uuid.New()
	actor := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	comp, _ := svc.CreateCompany(ctx, &actor, organization.CreateCompanyRequest{Code: "C-CONC", Name: "C"})
	st, _ := svc.CreateStore(ctx, &actor, organization.CreateStoreRequest{CompanyID: comp.ID, Code: "S-CONC", Name: "Original Name"})

	// Concurrent read by two actors
	newName1 := "Name updated by Actor 1"
	newName2 := "Name updated by Actor 2"

	// Actor 1 updates store successfully (version becomes 2)
	_, err := svc.UpdateStore(ctx, &actor, st.ID, organization.UpdateStoreRequest{Name: &newName1})
	if err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	// Actor 2 tries to update with stale store object (version 1) directly via store
	st.Name = newName2
	err = tm.WithinTx(ctx, func(txCtx context.Context) error {
		return store.UpdateStore(txCtx, st) // st.Version is still 1
	})
	if err == nil {
		t.Fatalf("expected optimistic concurrency conflict, got nil")
	}
}

func TestOrganization_Integration_AuditAndOutboxAtomicity(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := organization.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := organization.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	actor := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	// Create Company
	comp, err := svc.CreateCompany(ctx, &actor, organization.CreateCompanyRequest{
		Code: "AUDIT-CO",
		Name: "Audit Test Company",
	})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	// Verify audit log record exists in audit.audit_log
	var auditCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE tenant_id = $1 AND action = 'COMPANY_CREATED' AND resource_id = $2
	`, tenantID, comp.ID).Scan(&auditCount)
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit log record, found %d", auditCount)
	}

	// Verify outbox event exists in integration.outbox_event
	var outboxCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM integration.outbox_event
		WHERE tenant_id = $1 AND aggregate_id = $2 AND event_type = 'org.company.created.v1'
	`, tenantID, comp.ID).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event record, found %d", outboxCount)
	}
}

func TestOrganization_Integration_ScopeValidation(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := organization.NewPgxStore(appPool)
	svc := organization.NewService(store, tm, nil, nil, logger)

	tenantID := uuid.New()
	actor := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	comp, _ := svc.CreateCompany(ctx, &actor, organization.CreateCompanyRequest{Code: "SC", Name: "SC"})
	st, _ := svc.CreateStore(ctx, &actor, organization.CreateStoreRequest{CompanyID: comp.ID, Code: "SS", Name: "SS"})

	// Validate active entities
	var validComp, validStore bool
	_ = tm.WithinTx(ctx, func(txCtx context.Context) error {
		validComp, _ = svc.ValidateScopeEntity(txCtx, "COMPANY", comp.ID)
		validStore, _ = svc.ValidateScopeEntity(txCtx, "STORE", st.ID)
		return nil
	})
	if !validComp || !validStore {
		t.Fatalf("expected active entities to validate successfully: comp=%v, store=%v", validComp, validStore)
	}

	// Validate nonexistent entity
	var validFake bool
	_ = tm.WithinTx(ctx, func(txCtx context.Context) error {
		validFake, _ = svc.ValidateScopeEntity(txCtx, "STORE", uuid.New())
		return nil
	})
	if validFake {
		t.Fatalf("expected nonexistent entity to fail validation")
	}
}
