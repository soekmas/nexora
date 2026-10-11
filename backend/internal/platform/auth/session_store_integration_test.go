package auth_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/platform/testfixture"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func getAdminTestConfig() *config.Config {
	return &config.Config{
		Env:                 "test",
		LogLevel:            "debug",
		DBHost:              "127.0.0.1",
		DBPort:              15432,
		DBName:              "nexora_test",
		DBUser:              "nexora_test",
		DBPassword:          "test_secret_pass",
		DBSSLMode:           "disable",
		DBMaxConns:          5,
		DBMinConns:          1,
		DBMaxConnLifetime:   15 * time.Minute,
		DBConnectTimeout:    3 * time.Second,
		RedisHost:           "127.0.0.1",
		RedisPort:           6379,
		RedisConnectTimeout: 2 * time.Second,
		ShutdownTimeout:     5 * time.Second,
		HTTPPort:            8080,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
	}
}

func getAppTestConfig() *config.Config {
	cfg := getAdminTestConfig()
	cfg.DBUser = "nexora_app"
	cfg.DBPassword = "app_secret_pass"
	cfg.DBMaxConns = 10
	cfg.DBMinConns = 2
	return cfg
}

func connectLivePool(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	adminCfg := getAdminTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	adminPool, err := postgres.NewPool(ctx, adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("PostgreSQL test instance unavailable at %s:%d (%v); skipping live DB integration test", adminCfg.DBHost, adminCfg.DBPort, err)
		return nil, nil
	}

	adminTM := postgres.NewTxManager(adminPool, newDiscardLogger())
	if setupErr := adminTM.WithinUnscopedTx(ctx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		if err := testfixture.SetupTestTable(txCtx, tx); err != nil {
			return err
		}
		return testfixture.SetupIdentityTables(txCtx, tx)
	}); setupErr != nil {
		adminPool.Close()
		t.Fatalf("failed to setup identity test tables: %v", setupErr)
	}

	appCfg := getAppTestConfig()
	appPool, err := postgres.NewPool(ctx, appCfg, newDiscardLogger())
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect as application role: %v", err)
	}

	return adminPool, appPool
}

func TestSessionStore_Integration_LifecycleAndRevocation(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	store := auth.NewPgxSessionStore(appPool, newDiscardLogger())

	tenantID := uuid.New()
	userID := uuid.New()
	companyID := uuid.New()
	storeID := uuid.New()
	roleID := uuid.New()
	permID := uuid.New()

	// Seed test user, company, store, role, permission, user_role, user_scope
	seedErr := func() error {
		// Insert company
		_, err := adminPool.Exec(ctx, `
			INSERT INTO organization.company (id, tenant_id, code, name)
			VALUES ($1, $2, 'CMP-TEST', 'Test Company')
		`, companyID, tenantID)
		if err != nil {
			return err
		}
		// Insert store
		_, err = adminPool.Exec(ctx, `
			INSERT INTO organization.store (id, tenant_id, company_id, code, name)
			VALUES ($1, $2, $3, 'STR-01', 'Test Store 1')
		`, storeID, tenantID, companyID)
		if err != nil {
			return err
		}
		// Insert active user
		_, err = adminPool.Exec(ctx, `
			INSERT INTO identity.user_account (id, tenant_id, username, display_name, status)
			VALUES ($1, $2, $3, 'Active Cashier', 'ACTIVE')
		`, userID, tenantID, "user-"+uuid.New().String()[:8])
		if err != nil {
			return err
		}
		// Insert role
		_, err = adminPool.Exec(ctx, `
			INSERT INTO identity.role (id, tenant_id, code, name)
			VALUES ($1, $2, 'CASHIER', 'Store Cashier')
		`, roleID, tenantID)
		if err != nil {
			return err
		}
		// Insert permission
		err = adminPool.QueryRow(ctx, `
			INSERT INTO identity.permission (id, code, name, module, action)
			VALUES ($1, 'pos:sale:create', 'Create POS Sale', 'pos', 'create')
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, permID).Scan(&permID)
		if err != nil {
			return err
		}
		// Link user_role
		_, err = adminPool.Exec(ctx, `
			INSERT INTO identity.user_role (user_id, role_id)
			VALUES ($1, $2)
		`, userID, roleID)
		if err != nil {
			return err
		}
		// Link role_permission
		_, err = adminPool.Exec(ctx, `
			INSERT INTO identity.role_permission (role_id, permission_id)
			VALUES ($1, $2)
		`, roleID, permID)
		if err != nil {
			return err
		}
		// Link user_scope
		_, err = adminPool.Exec(ctx, `
			INSERT INTO identity.user_scope (tenant_id, user_id, company_id, store_id, scope_type)
			VALUES ($1, $2, $3, $4, 'STORE')
		`, tenantID, userID, companyID, storeID)
		return err
	}()
	if seedErr != nil {
		t.Fatalf("failed to seed test identity entities: %v", seedErr)
	}

	// 1. Generate token
	rawToken, tokenHash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// 2. Create session
	now := time.Now().UTC()
	sessionID := uuid.New()
	ip := "192.168.1.100"
	ua := "Nexora-POS/1.0"
	session := &auth.Session{
		ID:        sessionID,
		TenantID:  tenantID,
		UserID:    userID,
		TokenHash: tokenHash,
		IPAddress: &ip,
		UserAgent: &ua,
		IssuedAt:  now,
		ExpiresAt: now.Add(2 * time.Hour),
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Verify raw token is NOT in database
	var countWithRawToken int
	err = adminPool.QueryRow(ctx, `SELECT count(*) FROM identity.session WHERE token_hash = $1`, rawToken).Scan(&countWithRawToken)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if countWithRawToken != 0 {
		t.Fatalf("CRITICAL SECURITY DEFECT: raw bearer token found in database token_hash column")
	}

	// 3. Lookup session by token hash
	fetched, err := store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed: %v", err)
	}
	if fetched.ID != sessionID || fetched.TenantID != tenantID || fetched.UserID != userID {
		t.Fatalf("fetched session mismatch: %+v vs %+v", fetched, session)
	}
	if fetched.IsRevoked() || fetched.IsExpired(now) {
		t.Fatalf("expected active valid session, got revoked=%v expired=%v", fetched.IsRevoked(), fetched.IsExpired(now))
	}

	// 4. Resolve authenticated principal
	principal, err := store.GetUserPrincipal(ctx, tenantID, userID)
	if err != nil {
		t.Fatalf("GetUserPrincipal failed: %v", err)
	}
	if principal.TenantID != tenantID || principal.UserID != userID {
		t.Fatalf("principal mismatch: tenant=%v user=%v", principal.TenantID, principal.UserID)
	}
	if !principal.HasRole("CASHIER") {
		t.Fatalf("expected principal to have role CASHIER, roles: %v", principal.Roles)
	}
	if !principal.HasPermission("pos:sale:create") {
		t.Fatalf("expected principal to have permission pos:sale:create, permissions: %v", principal.Permissions)
	}
	if !principal.HasScope("STORE", storeID) {
		t.Fatalf("expected principal to have scope STORE with storeID %v", storeID)
	}

	// 5. Revoke session and verify immediate zero-lag effect
	if err := store.RevokeSession(postgres.WithTenantID(ctx, tenantID), sessionID); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	revokedSession, err := store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash failed after revocation: %v", err)
	}
	if !revokedSession.IsRevoked() {
		t.Fatalf("expected session to be immediately revoked, got revoked_at=nil")
	}

	// 6. Test Inactive user rejection
	_, err = adminPool.Exec(ctx, `UPDATE identity.user_account SET status = 'LOCKED' WHERE id = $1`, userID)
	if err != nil {
		t.Fatalf("failed to lock user account: %v", err)
	}

	_, err = store.GetUserPrincipal(ctx, tenantID, userID)
	if err == nil {
		t.Fatalf("expected error for locked user account, got nil")
	}
	if !errors.Is(err, auth.ErrUserInactive) {
		t.Fatalf("expected ErrUserInactive for locked user, got %v", err)
	}
}

func TestSessionStore_Integration_RLSContextBinding(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	appTM := postgres.NewTxManager(appPool, newDiscardLogger())

	tenantA := uuid.New()
	tenantB := uuid.New()
	userA := uuid.New()

	// Seed platform_test.test_items directly under admin pool
	itemA := testfixture.TestItem{
		ID:       uuid.New(),
		TenantID: tenantA,
		Name:     "Tenant A Exclusive Item",
	}
	itemB := testfixture.TestItem{
		ID:       uuid.New(),
		TenantID: tenantB,
		Name:     "Tenant B Exclusive Item",
	}

	if err := testfixture.InsertTestItem(ctx, adminPool, itemA); err != nil {
		t.Fatalf("failed to insert itemA: %v", err)
	}
	if err := testfixture.InsertTestItem(ctx, adminPool, itemB); err != nil {
		t.Fatalf("failed to insert itemB: %v", err)
	}

	principalA := &auth.AuthenticatedPrincipal{
		TenantID: tenantA,
		UserID:   userA,
	}

	// 1. Execute read query within transaction bound with principalA context
	ctxA := postgres.WithPrincipalContext(ctx, principalA.ToPrincipalContext())
	err := appTM.WithinTx(ctxA, func(txCtx context.Context) error {
		tx, ok := postgres.TxFromContext(txCtx)
		if !ok {
			t.Fatal("expected tx in context")
		}

		// Count visible items: must see exactly 1 (itemA), itemB must be filtered by RLS
		count, err := testfixture.CountTestItems(txCtx, tx)
		if err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("expected 1 item visible for Tenant A under RLS, got %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read transaction failed: %v", err)
	}

	// 2. Attempting to insert an item with Tenant B under Tenant A transaction context must fail with RLS check violation
	err = appTM.WithinTx(ctxA, func(txCtx context.Context) error {
		tx, ok := postgres.TxFromContext(txCtx)
		if !ok {
			t.Fatal("expected tx in context")
		}

		crossTenantItem := testfixture.TestItem{
			ID:       uuid.New(),
			TenantID: tenantB,
			Name:     "Illegal Cross-Tenant Item",
		}
		return testfixture.InsertTestItem(txCtx, tx, crossTenantItem)
	})
	if err == nil {
		t.Fatal("expected RLS violation when inserting cross-tenant item, got nil error")
	}
	t.Logf("successfully caught expected RLS violation on cross-tenant insert: %v", err)
}
