package crm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/crm"
	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
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

	dbName := fmt.Sprintf("nexora_crm_test_%d", time.Now().UnixNano())
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

	// Run all migrations sequentially: 000001 through 000009
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
		GRANT USAGE ON SCHEMA identity, organization, master, crm, platform, audit, integration TO nexora_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, organization, master, crm, platform, audit, integration TO nexora_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity, organization, master, crm, platform, audit, integration TO nexora_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA platform TO nexora_app;
	`
	if _, err := migrationPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant permissions to nexora_app: %v", err)
	}

	return migrationPool, appPool
}

func createTenantContext(tenantID uuid.UUID) context.Context {
	return postgres.WithTenantID(context.Background(), tenantID)
}

func strPtr(s string) *string {
	return &s
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestCRM_Integration_CustomerGroupLifecycle(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	// 1. Create Group
	group, err := svc.CreateCustomerGroup(ctx, &actorID, crm.CreateCustomerGroupRequest{
		Code: "VIP_GOLD",
		Name: "Gold VIP Segment",
	})
	if err != nil {
		t.Fatalf("create customer group: %v", err)
	}
	if group.Status != crm.StatusActive {
		t.Fatalf("expected status ACTIVE, got %s", group.Status)
	}

	// 2. Duplicate Code Conflict
	_, err = svc.CreateCustomerGroup(ctx, &actorID, crm.CreateCustomerGroupRequest{
		Code: "VIP_GOLD",
		Name: "Duplicate VIP",
	})
	if !errors.Is(err, crm.ErrCustomerGroupCodeConflict) {
		t.Fatalf("expected ErrCustomerGroupCodeConflict, got %v", err)
	}

	// 3. Deactivate Group (DEC-B1-014)
	inactiveStatus := crm.StatusInactive
	updatedGroup, err := svc.UpdateCustomerGroup(ctx, &actorID, group.ID, crm.UpdateCustomerGroupRequest{
		Status: &inactiveStatus,
	})
	if err != nil {
		t.Fatalf("update customer group status: %v", err)
	}
	if updatedGroup.Status != crm.StatusInactive {
		t.Fatalf("expected status INACTIVE, got %s", updatedGroup.Status)
	}

	// 4. Inactive Group cannot be assigned to new customer
	_, err = svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name:    "Alice Doe",
		GroupID: &group.ID,
	})
	if !errors.Is(err, crm.ErrCustomerGroupInactive) {
		t.Fatalf("expected ErrCustomerGroupInactive, got %v", err)
	}

	// 5. Reactivate Group
	activeStatus := crm.StatusActive
	_, err = svc.UpdateCustomerGroup(ctx, &actorID, group.ID, crm.UpdateCustomerGroupRequest{
		Status: &activeStatus,
	})
	if err != nil {
		t.Fatalf("reactivate group: %v", err)
	}

	// 6. Now assignable
	cust, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name:    "Alice Doe",
		GroupID: &group.ID,
	})
	if err != nil {
		t.Fatalf("create customer with active group: %v", err)
	}
	if cust.CustomerGroupID == nil || *cust.CustomerGroupID != group.ID {
		t.Fatalf("expected group ID %s, got %v", group.ID, cust.CustomerGroupID)
	}
}

func TestCRM_Integration_CustomerCreationAndAutoNumber(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	// 1. Auto-generated customer_no (DEC-B1-016)
	c1, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name:  "Bob Builder",
		Email: strPtr("Bob.Builder@Example.COM"),
		Phone: strPtr("+62 812-3456-7890"),
	})
	if err != nil {
		t.Fatalf("create customer auto-no: %v", err)
	}
	if c1.CustomerNo == "" || len(c1.CustomerNo) < 15 {
		t.Fatalf("expected format CUST-YYYYMMDD-XXXXXX, got %q", c1.CustomerNo)
	}
	// Normalization checks
	if *c1.Email != "bob.builder@example.com" {
		t.Fatalf("expected lowercased email, got %q", *c1.Email)
	}
	if *c1.Phone != "+6281234567890" {
		t.Fatalf("expected normalized phone, got %q", *c1.Phone)
	}

	// 2. Custom customer_no
	c2, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		CustomerNo: "CUST-CUSTOM-999",
		Name:       "Charlie Custom",
	})
	if err != nil {
		t.Fatalf("create customer custom-no: %v", err)
	}
	if c2.CustomerNo != "CUST-CUSTOM-999" {
		t.Fatalf("expected CUST-CUSTOM-999, got %q", c2.CustomerNo)
	}

	// 3. Duplicate custom customer_no
	_, err = svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		CustomerNo: "CUST-CUSTOM-999",
		Name:       "Charlie Duplicate",
	})
	if !errors.Is(err, crm.ErrCustomerNumberConflict) {
		t.Fatalf("expected ErrCustomerNumberConflict, got %v", err)
	}
}

func TestCRM_Integration_OptimisticLocking(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	c, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name: "David Concurrent",
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if c.Version != 1 {
		t.Fatalf("expected version 1, got %d", c.Version)
	}

	// Update 1 with version 1 -> succeeds, version becomes 2
	expectedVer1 := int64(1)
	upd1, err := svc.UpdateCustomer(ctx, &actorID, c.ID, crm.UpdateCustomerRequest{
		Name:            strPtr("David Modified 1"),
		ExpectedVersion: &expectedVer1,
	})
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if upd1.Version != 2 {
		t.Fatalf("expected version 2, got %d", upd1.Version)
	}

	// Update 2 with stale version 1 -> fails with ErrCustomerVersionConflict
	_, err = svc.UpdateCustomer(ctx, &actorID, c.ID, crm.UpdateCustomerRequest{
		Name:            strPtr("David Modified Stale"),
		ExpectedVersion: &expectedVer1,
	})
	if !errors.Is(err, crm.ErrCustomerVersionConflict) {
		t.Fatalf("expected ErrCustomerVersionConflict, got %v", err)
	}
}

func TestCRM_Integration_CustomerAddressDualLayerDefault(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	c, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name: "Eva Address",
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// 1. Add Address 1 as default
	addr1, err := svc.CreateAddress(ctx, &actorID, c.ID, crm.CreateAddressRequest{
		AddressType: crm.AddressTypeShipping,
		AddressLine: "Jalan Sudirman No 1",
		City:        strPtr("Jakarta"),
		PostalCode:  strPtr("10110"),
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("create address 1: %v", err)
	}
	if !addr1.IsDefault {
		t.Fatalf("expected addr1 IsDefault = true")
	}

	// 2. Add Address 2 as default -> atomically unsets Address 1
	addr2, err := svc.CreateAddress(ctx, &actorID, c.ID, crm.CreateAddressRequest{
		AddressType: crm.AddressTypeBilling,
		AddressLine: "Jalan Thamrin No 2",
		City:        strPtr("Jakarta"),
		PostalCode:  strPtr("10120"),
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("create address 2: %v", err)
	}
	if !addr2.IsDefault {
		t.Fatalf("expected addr2 IsDefault = true")
	}

	// 3. Verify Address 1 is no longer default
	list, err := svc.ListAddresses(ctx, c.ID)
	if err != nil {
		t.Fatalf("list addresses: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(list))
	}
	for _, a := range list {
		if a.ID == addr1.ID && a.IsDefault {
			t.Fatalf("expected addr1 to be reset to false")
		}
		if a.ID == addr2.ID && !a.IsDefault {
			t.Fatalf("expected addr2 to remain true")
		}
	}

	// 4. Verify PostgreSQL Partial Unique Index ux_customer_default_address (DEC-B1-017)
	// Bypassing service layer to directly insert two default addresses should fail at DB constraint!
	_, err = migrationPool.Exec(context.Background(), `
		INSERT INTO crm.customer_address (id, tenant_id, customer_id, address_type, address_line, is_default)
		VALUES (gen_random_uuid(), $1, $2, 'OTHER', 'Direct SQL duplicate default', true)
	`, tenantID, c.ID)
	if err == nil {
		t.Fatalf("expected PostgreSQL unique index ux_customer_default_address violation, got nil")
	}
}

func TestCRM_Integration_CustomerDeactivationAndAddressGuard(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	c, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name: "Frank Inactive",
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// 1. Disable customer
	disabled, err := svc.DisableCustomer(ctx, &actorID, c.ID, nil)
	if err != nil {
		t.Fatalf("disable customer: %v", err)
	}
	if disabled.Status != crm.StatusInactive {
		t.Fatalf("expected status INACTIVE, got %s", disabled.Status)
	}

	// 2. Adding address to inactive customer must fail
	_, err = svc.CreateAddress(ctx, &actorID, c.ID, crm.CreateAddressRequest{
		AddressLine: "Forbidden Street",
	})
	if !errors.Is(err, crm.ErrCustomerInactive) {
		t.Fatalf("expected ErrCustomerInactive, got %v", err)
	}

	// 3. Re-enable customer
	enabled, err := svc.EnableCustomer(ctx, &actorID, c.ID, nil)
	if err != nil {
		t.Fatalf("enable customer: %v", err)
	}
	if enabled.Status != crm.StatusActive {
		t.Fatalf("expected status ACTIVE, got %s", enabled.Status)
	}

	// 4. Now adding address succeeds
	_, err = svc.CreateAddress(ctx, &actorID, c.ID, crm.CreateAddressRequest{
		AddressLine: "Allowed Street",
	})
	if err != nil {
		t.Fatalf("create address after enable: %v", err)
	}
}

func TestCRM_Integration_MultiTenantRLSIsolation(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantA := uuid.New()
	tenantB := uuid.New()
	ctxA := createTenantContext(tenantA)
	ctxB := createTenantContext(tenantB)
	actorID := uuid.New()

	// Tenant A creates customer
	cA, err := svc.CreateCustomer(ctxA, &actorID, crm.CreateCustomerRequest{
		Name: "Customer Tenant A",
	})
	if err != nil {
		t.Fatalf("create customer A: %v", err)
	}

	// Tenant B cannot read Tenant A's customer
	_, err = svc.GetCustomer(ctxB, cA.ID)
	if !errors.Is(err, crm.ErrCustomerNotFound) {
		t.Fatalf("expected ErrCustomerNotFound for cross-tenant access, got %v", err)
	}

	// Tenant B cannot update Tenant A's customer
	_, err = svc.UpdateCustomer(ctxB, &actorID, cA.ID, crm.UpdateCustomerRequest{
		Name: strPtr("Hacked by Tenant B"),
	})
	if !errors.Is(err, crm.ErrCustomerNotFound) {
		t.Fatalf("expected ErrCustomerNotFound for cross-tenant update, got %v", err)
	}

	// Tenant B cannot add address to Tenant A's customer
	_, err = svc.CreateAddress(ctxB, &actorID, cA.ID, crm.CreateAddressRequest{
		AddressLine: "Cross Tenant Attack",
	})
	if !errors.Is(err, crm.ErrCustomerNotFound) {
		t.Fatalf("expected ErrCustomerNotFound for cross-tenant address addition, got %v", err)
	}
}

func TestCRM_Integration_AuditAndOutboxAtomicity(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	c, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
		Name:  "Grace Audit",
		Email: strPtr("grace.audit@example.com"),
		Phone: strPtr("+6281298765432"),
		TaxID: strPtr("01.234.567.8-999.000"),
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// 1. Verify Outbox Event created
	var outboxCount int
	err = migrationPool.QueryRow(context.Background(), `
		SELECT count(*) FROM integration.outbox_event
		WHERE tenant_id = $1 AND aggregate_id = $2 AND event_type = $3
	`, tenantID, c.ID, crm.EventCustomerCreated).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", outboxCount)
	}

	// 2. Verify Audit Log entry created with PII masked
	var metaJSON []byte
	err = migrationPool.QueryRow(context.Background(), `
		SELECT metadata FROM audit.audit_log
		WHERE tenant_id = $1 AND resource_id = $2 AND action = $3
	`, tenantID, c.ID, crm.AuditActionCustomerCreated).Scan(&metaJSON)
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}

	var meta map[string]any
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}

	// Verify PII masking in audit snapshot
	emailVal, _ := meta["email"].(string)
	if emailVal != "g***@example.com" {
		t.Fatalf("expected masked email g***@example.com, got %q", emailVal)
	}
	phoneVal, _ := meta["phone"].(string)
	if phoneVal != "+62****432" {
		t.Fatalf("expected masked phone +62****432, got %q", phoneVal)
	}
	taxVal, _ := meta["tax_id"].(string)
	if taxVal != "****************.000" {
		t.Fatalf("expected masked tax_id, got %q", taxVal)
	}
}

func TestCRM_Integration_ConcurrentCustomerNumberGeneration(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx := createTenantContext(tenantID)
	actorID := uuid.New()

	const concurrency = 10
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)
	numbers := make([]string, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cust, err := svc.CreateCustomer(ctx, &actorID, crm.CreateCustomerRequest{
				Name: fmt.Sprintf("Concurrent Customer %d", idx),
			})
			if err != nil {
				errCh <- fmt.Errorf("worker %d failed: %w", idx, err)
				return
			}
			numbers[idx] = cust.CustomerNo
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency error: %v", err)
	}

	seen := make(map[string]bool)
	for i, no := range numbers {
		if seen[no] {
			t.Fatalf("duplicate customer number generated under concurrency: %s", no)
		}
		seen[no] = true
		if no == "" {
			t.Fatalf("worker %d produced empty customer number", i)
		}
	}
}

func TestCRM_Integration_HTTPHandlersAndRouteParity(t *testing.T) {
	_, appPool := setupDisposableDatabase(t)
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := crm.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := crm.NewService(store, tm, auditStore, outboxWriter, logger)
	handler := crm.NewHandler(svc, logger)

	tenantID := uuid.New()
	userID := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		UserID:   userID,
		TenantID: tenantID,
		Permissions: []string{
			crm.PermCustomerCreate,
			crm.PermCustomerRead,
			crm.PermCustomerUpdate,
			crm.PermCustomerAddressManage,
			crm.PermCustomerGroupCreate,
			crm.PermCustomerGroupRead,
			crm.PermCustomerGroupUpdate,
		},
	}

	// Router setup mounting both canonical and alias routers
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithPrincipal(req.Context(), principal)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Mount("/api/v1/crm", handler.Routes())
	r.Mount("/api/v1/customers", handler.CustomerAliasRoutes())

	// 1. Create customer via canonical route /api/v1/crm/customers
	bodyJSON, _ := json.Marshal(crm.CreateCustomerRequest{
		Name: "Henry Canonical",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crm/customers", bytes.NewReader(bodyJSON))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/crm/customers failed with %d: %s", rec.Code, rec.Body.String())
	}
	var canonicalResp crm.CustomerResponse
	if err := json.NewDecoder(rec.Body).Decode(&canonicalResp); err != nil {
		t.Fatalf("decode canonical response: %v", err)
	}

	// 2. Create customer via compatibility alias /api/v1/customers (DEC-B1-018)
	bodyAliasJSON, _ := json.Marshal(crm.CreateCustomerRequest{
		Name: "Ivy Alias",
	})
	reqAlias := httptest.NewRequest(http.MethodPost, "/api/v1/customers", bytes.NewReader(bodyAliasJSON))
	recAlias := httptest.NewRecorder()
	r.ServeHTTP(recAlias, reqAlias)
	if recAlias.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/customers failed with %d: %s", recAlias.Code, recAlias.Body.String())
	}
	var aliasResp crm.CustomerResponse
	if err := json.NewDecoder(recAlias.Body).Decode(&aliasResp); err != nil {
		t.Fatalf("decode alias response: %v", err)
	}

	// 3. Get customer via alias route
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+canonicalResp.ID.String(), nil)
	recGet := httptest.NewRecorder()
	r.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/customers/{id} failed with %d: %s", recGet.Code, recGet.Body.String())
	}

	// 4. Parity check on responses
	if !canonicalResp.Active || !aliasResp.Active {
		t.Fatalf("expected Active = true on both responses")
	}
}
