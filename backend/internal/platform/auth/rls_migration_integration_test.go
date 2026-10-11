package auth_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/platform/testfixture"
)

func newMigratedAuthPools(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	adminCfg := getAdminTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := postgres.NewPool(ctx, adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("PostgreSQL test instance unavailable at %s:%d (%v)", adminCfg.DBHost, adminCfg.DBPort, err)
	}
	t.Cleanup(adminPool.Close)
	if err := testfixture.SetupTestTable(ctx, adminPool); err != nil {
		t.Fatalf("ensure application test role: %v", err)
	}

	var resolverRoleExisted bool
	if err := adminPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexora_auth_resolver')`).Scan(&resolverRoleExisted); err != nil {
		t.Fatalf("check resolver role: %v", err)
	}
	dbName := "nexora_auth_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		t.Fatalf("create disposable auth database: %v", err)
	}

	dbCfg := *adminCfg
	dbCfg.DBName = dbName
	migrationPool, err := postgres.NewPool(ctx, &dbCfg, newDiscardLogger())
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		t.Fatalf("connect to disposable auth database: %v", err)
	}

	appCfg := *getAppTestConfig()
	appCfg.DBName = dbName
	appPool, err := postgres.NewPool(ctx, &appCfg, newDiscardLogger())
	if err != nil {
		migrationPool.Close()
		_, _ = adminPool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		t.Fatalf("connect application-equivalent role to disposable auth database: %v", err)
	}
	t.Cleanup(func() {
		appPool.Close()
		migrationPool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop disposable auth database: %v", err)
		}
		if !resolverRoleExisted {
			if _, err := adminPool.Exec(cleanupCtx, `DROP ROLE IF EXISTS nexora_auth_resolver`); err != nil {
				t.Errorf("drop disposable auth resolver role: %v", err)
			}
		}
	})

	_, sourceFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "db", "migrations")
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
			t.Fatalf("apply migration %s to disposable database: %v", name, err)
		}
	}

	grant := `GRANT USAGE ON SCHEMA identity, organization, platform TO nexora_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, organization TO nexora_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identity, organization TO nexora_app;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA platform TO nexora_app;`
	if _, err := migrationPool.Exec(ctx, grant); err != nil {
		t.Fatalf("grant application privileges in disposable database: %v", err)
	}

	var isSuperuser, bypassRLS bool
	if err := migrationPool.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'nexora_app'`).Scan(&isSuperuser, &bypassRLS); err != nil {
		t.Fatalf("inspect application role: %v", err)
	}
	if isSuperuser || bypassRLS {
		t.Fatalf("application role must be non-superuser and NOBYPASSRLS (superuser=%t bypassrls=%t)", isSuperuser, bypassRLS)
	}
	var appOwnsAuthTable bool
	if err := migrationPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_roles r ON r.oid = c.relowner
			WHERE n.nspname = 'identity'
			  AND c.relname IN ('session','refresh_token','user_account','role','user_scope')
			  AND r.rolname = 'nexora_app'
		)`).Scan(&appOwnsAuthTable); err != nil {
		t.Fatalf("inspect application role table ownership: %v", err)
	}
	if appOwnsAuthTable {
		t.Fatal("application role must not own tenant-scoped authentication tables")
	}
	var resolverCanLogin, resolverIsSuperuser, resolverBypassesRLS bool
	if err := migrationPool.QueryRow(ctx, `SELECT rolcanlogin, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'nexora_auth_resolver'`).Scan(&resolverCanLogin, &resolverIsSuperuser, &resolverBypassesRLS); err != nil {
		t.Fatalf("inspect resolver role: %v", err)
	}
	if resolverCanLogin || resolverIsSuperuser || resolverBypassesRLS {
		t.Fatalf("resolver role must be NOLOGIN/NOSUPERUSER/NOBYPASSRLS (login=%t superuser=%t bypassrls=%t)", resolverCanLogin, resolverIsSuperuser, resolverBypassesRLS)
	}
	var appIsResolverMember bool
	if err := migrationPool.QueryRow(ctx, `SELECT pg_has_role('nexora_app', 'nexora_auth_resolver', 'MEMBER')`).Scan(&appIsResolverMember); err != nil {
		t.Fatalf("inspect resolver role membership: %v", err)
	}
	if appIsResolverMember {
		t.Fatal("application role must not be a member of the authentication resolver role")
	}
	var forcedRLS bool
	if err := migrationPool.QueryRow(ctx, `SELECT bool_and(c.relrowsecurity AND c.relforcerowsecurity) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'identity' AND c.relname IN ('session','refresh_token','user_account','role','user_scope')`).Scan(&forcedRLS); err != nil {
		t.Fatalf("inspect authentication table RLS: %v", err)
	}
	if !forcedRLS {
		t.Fatal("all tenant-scoped authentication tables must have enabled and forced RLS")
	}

	return migrationPool, appPool
}

func TestAuthenticationWithActualMigrationRLS(t *testing.T) {
	adminPool, appPool := newMigratedAuthPools(t)
	ctx := context.Background()
	store := auth.NewPgxSessionStore(appPool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	appTM := postgres.NewTxManager(appPool, newDiscardLogger())

	tenantA, userA, sessionA := seedAuthIdentity(ctx, t, adminPool, "tenant_a")
	tenantB, userB, sessionB := seedAuthIdentity(ctx, t, adminPool, "tenant_b")
	_, accessHashA, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	_, accessHashB, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedAuthSession(ctx, adminPool, tenantA, userA, sessionA, accessHashA); err != nil {
		t.Fatal(err)
	}
	if err := seedAuthSession(ctx, adminPool, tenantB, userB, sessionB, accessHashB); err != nil {
		t.Fatal(err)
	}

	var visibleWithoutContext int
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM identity.session`).Scan(&visibleWithoutContext); err != nil {
		t.Fatalf("read sessions without RLS context: %v", err)
	}
	if visibleWithoutContext != 0 {
		t.Fatalf("application role saw %d sessions without tenant context", visibleWithoutContext)
	}

	session, err := store.GetSessionByTokenHash(ctx, accessHashA)
	if err != nil {
		t.Fatalf("resolve and load valid session under RLS: %v", err)
	}
	if session.TenantID != tenantA || session.ID != sessionA {
		t.Fatalf("session resolver returned unexpected identity: tenant=%s session=%s", session.TenantID, session.ID)
	}
	if _, err := store.GetSessionByTokenHash(ctx, auth.HashToken("unknown-access-token")); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("expected unknown session to fail closed, got %v", err)
	}
	principal, err := store.GetUserPrincipal(ctx, tenantA, userA)
	if err != nil || principal.TenantID != tenantA || principal.UserID != userA {
		t.Fatalf("resolve tenant-scoped principal: principal=%+v error=%v", principal, err)
	}

	if err := appTM.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
		if _, err := store.GetSessionByTokenHash(txCtx, accessHashB); !errors.Is(err, postgres.ErrTenantContextMismatch) {
			return fmt.Errorf("cross-tenant session lookup should be denied, got %v", err)
		}
		tx, _ := postgres.TxFromContext(txCtx)
		var count int
		if err := tx.QueryRow(txCtx, `SELECT count(*) FROM identity.session WHERE id = $1`, sessionB).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("tenant A RLS context exposed tenant B session")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	refreshRaw, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	familyID := uuid.New()
	if err := seedRefreshToken(ctx, adminPool, tenantA, userA, sessionA, refreshHash, familyID, time.Now().Add(30*24*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	result, err := store.RotateRefreshToken(ctx, refreshHash, &tenantA, 15*time.Minute, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("rotate valid refresh token under RLS: %v", err)
	}
	if result.RawAccessToken == "" || result.RawRefreshToken == "" || result.Principal.TenantID != tenantA {
		t.Fatalf("unexpected refresh rotation result: %+v", result)
	}
	if _, err := store.RotateRefreshToken(ctx, refreshHash, &tenantA, 15*time.Minute, 30*24*time.Hour); !errors.Is(err, auth.ErrRefreshTokenReplayDetected) {
		t.Fatalf("replayed token should be detected, got %v", err)
	}
	if err := appTM.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		var revokedAt *time.Time
		if err := tx.QueryRow(txCtx, `SELECT revoked_at FROM identity.session WHERE id = $1`, sessionA).Scan(&revokedAt); err != nil {
			return err
		}
		if revokedAt == nil {
			return errors.New("replayed refresh token did not revoke its session")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateRefreshToken(ctx, auth.HashToken("unknown-refresh-token"), nil, 15*time.Minute, 30*24*time.Hour); !errors.Is(err, auth.ErrRefreshTokenNotFound) {
		t.Fatalf("unknown refresh token should fail closed, got %v", err)
	}
	_ = refreshRaw

	wrongTenantRaw, wrongTenantHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedRefreshToken(ctx, adminPool, tenantB, userB, sessionB, wrongTenantHash, uuid.New(), time.Now().Add(30*24*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateRefreshToken(ctx, wrongTenantHash, &tenantA, 15*time.Minute, 30*24*time.Hour); !errors.Is(err, auth.ErrRefreshTokenTenantMismatch) {
		t.Fatalf("client tenant mismatch should be rejected against resolved tenant, got %v", err)
	}
	_ = wrongTenantRaw

	expiredRaw, expiredHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedRefreshToken(ctx, adminPool, tenantB, userB, sessionB, expiredHash, uuid.New(), time.Now().Add(-time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateRefreshToken(ctx, expiredHash, &tenantB, 15*time.Minute, 30*24*time.Hour); !errors.Is(err, auth.ErrRefreshTokenExpired) {
		t.Fatalf("expired refresh token should be rejected, got %v", err)
	}
	_ = expiredRaw

	revokedRaw, revokedHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	revokedAt := time.Now().Add(-time.Minute)
	if err := seedRefreshToken(ctx, adminPool, tenantB, userB, sessionB, revokedHash, uuid.New(), time.Now().Add(30*24*time.Hour), &revokedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateRefreshToken(ctx, revokedHash, &tenantB, 15*time.Minute, 30*24*time.Hour); !errors.Is(err, auth.ErrRefreshTokenReplayDetected) {
		t.Fatalf("revoked refresh token should trigger replay handling, got %v", err)
	}
	_ = revokedRaw
}

func TestRefreshTokenConcurrentRotationWithActualMigrationRLS(t *testing.T) {
	adminPool, appPool := newMigratedAuthPools(t)
	ctx := context.Background()
	store := auth.NewPgxSessionStore(appPool, newDiscardLogger())
	tenantID, userID, sessionID := seedAuthIdentity(ctx, t, adminPool, "concurrent")
	_, accessHash, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedAuthSession(ctx, adminPool, tenantID, userID, sessionID, accessHash); err != nil {
		t.Fatal(err)
	}
	_, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedRefreshToken(ctx, adminPool, tenantID, userID, sessionID, refreshHash, uuid.New(), time.Now().Add(30*24*time.Hour), nil); err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var successes atomic.Int64
	var replays atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.RotateRefreshToken(ctx, refreshHash, &tenantID, 15*time.Minute, 30*24*time.Hour)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, auth.ErrRefreshTokenReplayDetected):
				replays.Add(1)
			default:
				t.Errorf("unexpected concurrent rotation error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || replays.Load() != workers-1 {
		t.Fatalf("expected one successful rotation and %d replay detections, got success=%d replay=%d", workers-1, successes.Load(), replays.Load())
	}
}

func seedAuthIdentity(ctx context.Context, t *testing.T, db *pgxpool.Pool, label string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	tenantID, userID, sessionID := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO identity.user_account (id, tenant_id, username, display_name, status) VALUES ($1,$2,$3,$4,'ACTIVE')`, userID, tenantID, label+"_"+userID.String(), label); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return tenantID, userID, sessionID
}

func seedAuthSession(ctx context.Context, db *pgxpool.Pool, tenantID, userID, sessionID uuid.UUID, tokenHash string) error {
	_, err := db.Exec(ctx, `INSERT INTO identity.session (id, tenant_id, user_id, token_hash, issued_at, expires_at) VALUES ($1,$2,$3,$4,now(),now()+interval '1 hour')`, sessionID, tenantID, userID, tokenHash)
	return err
}

func seedRefreshToken(ctx context.Context, db *pgxpool.Pool, tenantID, userID, sessionID uuid.UUID, tokenHash string, familyID uuid.UUID, expiresAt time.Time, revokedAt *time.Time) error {
	_, err := db.Exec(ctx, `INSERT INTO identity.refresh_token (tenant_id,user_id,session_id,token_hash,family_id,issued_at,expires_at,revoked_at) VALUES ($1,$2,$3,$4,$5,now()-interval '2 hours',$6,$7)`, tenantID, userID, sessionID, tokenHash, familyID, expiresAt, revokedAt)
	return err
}
