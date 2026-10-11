package identity_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/identity"
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

	dbName := fmt.Sprintf("nexora_id_test_%d", time.Now().UnixNano())
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

	// Run migrations 000001 through 000006
	_, sourceFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "db", "migrations")
	for _, name := range []string{
		"000001_baseline_hardened.sql",
		"000002_refresh_tokens.sql",
		"000003_outbox_leases.sql",
		"000004_audit_hardening.sql",
		"000005_auth_rls_bootstrap.sql",
		"000006_identity_permissions.sql",
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

func TestIdentity_Integration_UserLifecycle_And_Argon2id(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := identity.NewPgxStore(appPool)
	hasher := identity.NewArgon2idHasher()
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	service := identity.NewService(store, hasher, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	creatorID := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	// 1. Create user
	rawPassword := "SecurePass123!"
	created, err := service.CreateUser(ctx, identity.CreateUserRequest{
		Username:    "alice_retail",
		DisplayName: "Alice Smith",
		Password:    rawPassword,
		CreatedBy:   &creatorID,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("expected non-nil user id")
	}
	if created.Username != "alice_retail" {
		t.Fatalf("expected username alice_retail, got %s", created.Username)
	}
	if created.PasswordHash == nil || *created.PasswordHash == "" {
		t.Fatal("expected non-empty password hash")
	}

	// Verify Argon2id hash parameters in stored string
	hashStr := *created.PasswordHash
	if len(hashStr) < 20 || hashStr[:10] != "$argon2id$" {
		t.Fatalf("stored hash is not argon2id format: %s", hashStr)
	}

	// 2. Fetch user with details
	fetched, err := service.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if fetched.User.DisplayName != "Alice Smith" {
		t.Fatalf("expected display name Alice Smith, got %s", fetched.User.DisplayName)
	}

	// 3. Verify audit log entry was created
	var auditCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE tenant_id = $1 AND action = 'USER_CREATED' AND resource_id = $2
	`, tenantID, created.ID).Scan(&auditCount)
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit log entry for USER_CREATED, found %d", auditCount)
	}

	// 4. Verify outbox event was written
	var outboxCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM integration.outbox_event
		WHERE tenant_id = $1 AND aggregate_id = $2 AND event_type = 'identity.user.created.v1'
	`, tenantID, created.ID).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event for identity.user.created.v1, found %d", outboxCount)
	}

	// 5. Update user
	newDisplayName := "Alice S. (Manager)"
	updated, err := service.UpdateUser(ctx, created.ID, identity.UpdateUserRequest{
		DisplayName: &newDisplayName,
	})
	if err != nil {
		t.Fatalf("failed to update user: %v", err)
	}
	if updated.DisplayName != newDisplayName {
		t.Fatalf("expected display name %s, got %s", newDisplayName, updated.DisplayName)
	}
	if updated.Version <= created.Version {
		t.Fatalf("expected version increment from %d, got %d", created.Version, updated.Version)
	}
}

func TestIdentity_Integration_RLS_TenantIsolation(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := identity.NewPgxStore(appPool)
	hasher := identity.NewArgon2idHasher()
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	service := identity.NewService(store, hasher, tm, auditStore, outboxWriter, logger)

	tenantA := uuid.New()
	tenantB := uuid.New()

	// Tenant A creates a user
	ctxA := postgres.WithTenantID(ctx, tenantA)
	userA, err := service.CreateUser(ctxA, identity.CreateUserRequest{
		Username:    "tenant_a_user",
		DisplayName: "User A",
		Password:    "PasswordA123!",
	})
	if err != nil {
		t.Fatalf("tenant A user creation failed: %v", err)
	}

	// Tenant B attempts to read Tenant A's user under non-superuser nexora_app connection
	ctxB := postgres.WithTenantID(ctx, tenantB)
	_, err = service.GetUser(ctxB, userA.ID)
	if err != identity.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for cross-tenant access under RLS, got %v", err)
	}

	// Tenant B attempts to update Tenant A's user
	newDisplayName := "Malicious Update"
	_, err = service.UpdateUser(ctxB, userA.ID, identity.UpdateUserRequest{
		DisplayName: &newDisplayName,
	})
	if err != identity.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for cross-tenant update under RLS, got %v", err)
	}

	// Tenant B attempts to authenticate using Tenant A's username
	_, err = service.AuthenticateUser(ctxB, identity.LoginRequest{
		Username: "tenant_a_user",
		Password: "PasswordA123!",
	})
	if err != identity.ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials when cross-tenant login attempted, got %v", err)
	}
}

func TestIdentity_Integration_LockoutPolicy_5Failures_15Minutes(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := identity.NewPgxStore(appPool)
	hasher := identity.NewArgon2idHasher()
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	service := identity.NewService(store, hasher, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	correctPassword := "CorrectPassword123!"
	user, err := service.CreateUser(ctx, identity.CreateUserRequest{
		Username:    "lockout_test_user",
		DisplayName: "Lockout Test User",
		Password:    correctPassword,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// 1. Attempts 1 to 4 with WRONG password: must return ErrInvalidCredentials
	for i := 1; i <= 4; i++ {
		_, err := service.AuthenticateUser(ctx, identity.LoginRequest{
			Username: "lockout_test_user",
			Password: "WrongPassword999!",
		})
		if err != identity.ErrInvalidCredentials {
			t.Fatalf("attempt %d: expected ErrInvalidCredentials, got %v", i, err)
		}

		uDetails, err := service.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("attempt %d: get user: %v", i, err)
		}
		u := uDetails.User
		if u.FailedLoginCount != i {
			t.Fatalf("attempt %d: expected failed_login_count=%d, got %d", i, i, u.FailedLoginCount)
		}
		if u.Status != identity.StatusActive {
			t.Fatalf("attempt %d: expected status ACTIVE, got %s", i, u.Status)
		}
		if u.LockedUntil != nil {
			t.Fatalf("attempt %d: expected locked_until to be nil, got %v", i, u.LockedUntil)
		}
	}

	// 2. 5th attempt with WRONG password: triggers account lockout
	_, err = service.AuthenticateUser(ctx, identity.LoginRequest{
		Username: "lockout_test_user",
		Password: "WrongPassword999!",
	})
	if err != identity.ErrAccountLocked {
		t.Fatalf("attempt 5: expected ErrAccountLocked, got %v", err)
	}

	uDetails, err := service.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("attempt 5: get user: %v", err)
	}
	u := uDetails.User
	if u.FailedLoginCount != 5 {
		t.Fatalf("attempt 5: expected failed_login_count=5, got %d", u.FailedLoginCount)
	}
	if u.Status != identity.StatusLocked {
		t.Fatalf("attempt 5: expected status LOCKED, got %s", u.Status)
	}
	if u.LockedUntil == nil {
		t.Fatal("attempt 5: expected locked_until to be set")
	}
	expectedLockout := time.Now().Add(15 * time.Minute)
	if u.LockedUntil.Before(expectedLockout.Add(-1*time.Minute)) || u.LockedUntil.After(expectedLockout.Add(1*time.Minute)) {
		t.Fatalf("locked_until outside expected 15-minute window: %v", u.LockedUntil)
	}

	// Verify outbox event identity.user.locked.v1 was written
	var lockEventCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM integration.outbox_event
		WHERE tenant_id = $1 AND aggregate_id = $2 AND event_type = 'identity.user.locked.v1'
	`, tenantID, user.ID).Scan(&lockEventCount)
	if err != nil {
		t.Fatalf("query outbox lock event: %v", err)
	}
	if lockEventCount != 1 {
		t.Fatalf("expected 1 lock event in outbox, found %d", lockEventCount)
	}

	// 3. 6th attempt WITH CORRECT PASSWORD while lock is active: must STILL fail with ErrAccountLocked
	_, err = service.AuthenticateUser(ctx, identity.LoginRequest{
		Username: "lockout_test_user",
		Password: correctPassword,
	})
	if err != identity.ErrAccountLocked {
		t.Fatalf("attempt during active lock: expected ErrAccountLocked, got %v", err)
	}

	// 4. Simulate expiration of 15-minute lockout period
	pastLockout := time.Now().Add(-1 * time.Minute)
	_, err = migrationPool.Exec(ctx, `
		UPDATE identity.user_account
		SET locked_until = $1
		WHERE id = $2
	`, pastLockout, user.ID)
	if err != nil {
		t.Fatalf("simulate lockout expiry: %v", err)
	}

	// 5. Authenticate WITH CORRECT PASSWORD after lockout expiry: must SUCCEED and RESET counter
	authSuccess, err := service.AuthenticateUser(ctx, identity.LoginRequest{
		Username: "lockout_test_user",
		Password: correctPassword,
	})
	if err != nil {
		t.Fatalf("authentication after lockout expiry failed: %v", err)
	}
	if authSuccess.User.FailedLoginCount != 0 {
		t.Fatalf("expected failed_login_count to reset to 0, got %d", authSuccess.User.FailedLoginCount)
	}
	if authSuccess.User.LockedUntil != nil {
		t.Fatalf("expected locked_until to be cleared, got %v", authSuccess.User.LockedUntil)
	}
	if authSuccess.User.Status != identity.StatusActive {
		t.Fatalf("expected status to be ACTIVE, got %s", authSuccess.User.Status)
	}
	if authSuccess.User.LastLoginAt == nil {
		t.Fatal("expected last_login_at to be populated")
	}
}

func TestIdentity_Integration_ConcurrentFailedLogins(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := identity.NewPgxStore(appPool)
	hasher := identity.NewArgon2idHasher()
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	service := identity.NewService(store, hasher, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	user, err := service.CreateUser(ctx, identity.CreateUserRequest{
		Username:    "concurrency_user",
		DisplayName: "Concurrency User",
		Password:    "Password123!",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Launch 10 concurrent failed login attempts
	var wg sync.WaitGroup
	workers := 10
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, _ = service.AuthenticateUser(ctx, identity.LoginRequest{
				Username: "concurrency_user",
				Password: "WrongPassword!",
			})
		}()
	}

	wg.Wait()

	// Verify that user reached lockout threshold and is locked
	uDetails, err := service.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	u := uDetails.User
	if u.FailedLoginCount < 5 {
		t.Fatalf("expected failed_login_count >= 5, got %d", u.FailedLoginCount)
	}
	if u.Status != identity.StatusLocked {
		t.Fatalf("expected status LOCKED, got %s", u.Status)
	}
	if u.LockedUntil == nil {
		t.Fatal("expected locked_until to be set")
	}
}

func TestIdentity_Integration_SegregationOfDuties(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := identity.NewPgxStore(appPool)
	hasher := identity.NewArgon2idHasher()
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	service := identity.NewService(store, hasher, tm, auditStore, outboxWriter, logger)

	tenantID := uuid.New()
	ctx = postgres.WithTenantID(ctx, tenantID)

	// Create user Bob
	bob, err := service.CreateUser(ctx, identity.CreateUserRequest{
		Username:    "bob_actor",
		DisplayName: "Bob Actor",
		Password:    "PasswordBob123!",
	})
	if err != nil {
		t.Fatalf("failed to create Bob: %v", err)
	}

	// Create administrative role
	adminRoleID := uuid.New()
	_, err = migrationPool.Exec(ctx, `
		INSERT INTO identity.role (id, tenant_id, code, name, description)
		VALUES ($1, $2, 'ROLE_ADMIN', 'Administrator', 'Full system access')
	`, adminRoleID, tenantID)
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	// Bob attempts to assign ROLE_ADMIN to himself: SoD denial expected
	err = service.AssignRole(ctx, bob.ID, bob.ID, adminRoleID)
	if err != identity.ErrSelfRoleAssignmentDenied {
		t.Fatalf("expected ErrSelfRoleAssignmentDenied, got %v", err)
	}

	// Alice (another administrator) assigns ROLE_ADMIN to Bob: must succeed
	aliceID := uuid.New()
	err = service.AssignRole(ctx, aliceID, bob.ID, adminRoleID)
	if err != nil {
		t.Fatalf("independent assignment failed: %v", err)
	}

	// Verify Bob now has ROLE_ADMIN
	details, err := service.GetUser(ctx, bob.ID)
	if err != nil {
		t.Fatalf("get Bob details: %v", err)
	}
	if len(details.Roles) != 1 || details.Roles[0].Code != "ROLE_ADMIN" {
		t.Fatalf("expected Bob to have ROLE_ADMIN, found %v", details.Roles)
	}

	// Verify audit log
	var auditCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE tenant_id = $1 AND action = 'USER_ROLE_ASSIGNED' AND resource_id = $2
	`, tenantID, bob.ID).Scan(&auditCount)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit entry for USER_ROLE_ASSIGNED, got %d", auditCount)
	}
}

func TestIdentity_Integration_PermissionCatalogSeeded(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t)
	if appPool == nil {
		return
	}
	defer migrationPool.Close()

	ctx := context.Background()
	store := identity.NewPgxStore(appPool)

	perms, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("failed to list permissions: %v", err)
	}

	if len(perms) == 0 {
		t.Fatal("expected seeded permissions from migration 000006, found 0")
	}

	permMap := make(map[string]bool)
	for _, p := range perms {
		permMap[p.Code] = true
	}

	requiredCodes := []string{
		"user.read",
		"user.create",
		"user.update",
		"user.disable",
		"user.unlock",
		"user.role.read",
		"user.role.assign",
		"user.role.revoke",
		"user.scope.manage",
		"role.read",
		"role.create",
		"role.update",
		"role.permission.assign",
		"mfa.manage",
		"session.revoke",
	}

	for _, code := range requiredCodes {
		if !permMap[code] {
			t.Errorf("missing expected seeded permission code: %s", code)
		}
	}
}
