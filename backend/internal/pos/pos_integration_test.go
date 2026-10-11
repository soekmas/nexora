package pos_test

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/idempotency"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
	"example.com/nexora/backend/internal/pos"
)

func newIntegrationDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func getIntegrationAdminTestConfig() config.Config {
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

func setupPosIntegrationDatabase(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()

	adminCfg := getIntegrationAdminTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := postgres.NewPool(ctx, &adminCfg, newIntegrationDiscardLogger())
	if err != nil {
		t.Skipf("PostgreSQL unavailable at %s:%d (%v); skipping integration test", adminCfg.DBHost, adminCfg.DBPort, err)
		return nil, nil
	}

	dbName := fmt.Sprintf("nexora_pos_integ_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		adminPool.Close()
		t.Fatalf("create disposable database: %v", err)
	}

	migrationCfg := adminCfg
	migrationCfg.DBName = dbName
	migrationPool, err := postgres.NewPool(ctx, &migrationCfg, newIntegrationDiscardLogger())
	if err != nil {
		_, _ = adminPool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		adminPool.Close()
		t.Fatalf("connect migration pool: %v", err)
	}

	appCfg := adminCfg
	appCfg.DBName = dbName
	appCfg.DBUser = "nexora_app"
	appCfg.DBPassword = "app_secret_pass"

	appPool, err := postgres.NewPool(ctx, &appCfg, newIntegrationDiscardLogger())
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

type testOrgHierarchy struct {
	TenantID   uuid.UUID
	CompanyID  uuid.UUID
	StoreID    uuid.UUID
	TerminalID uuid.UUID
	CashierID  uuid.UUID
	ManagerID  uuid.UUID
}

func seedHierarchy(t *testing.T, pool *pgxpool.Pool, prefix string) testOrgHierarchy {
	t.Helper()
	ctx := context.Background()

	h := testOrgHierarchy{
		TenantID:   uuid.New(),
		CompanyID:  uuid.New(),
		StoreID:    uuid.New(),
		TerminalID: uuid.New(),
		CashierID:  uuid.New(),
		ManagerID:  uuid.New(),
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO organization.company (id, tenant_id, code, name, base_currency)
		VALUES ($1, $2, $3, $4, 'IDR');
	`, h.CompanyID, h.TenantID, prefix+"-CMP", prefix+" Company"); err != nil {
		t.Fatalf("seed company: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO organization.store (id, tenant_id, company_id, code, name, status)
		VALUES ($1, $2, $3, $4, $5, 'ACTIVE');
	`, h.StoreID, h.TenantID, h.CompanyID, prefix+"-STR", prefix+" Store"); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO organization.terminal (id, tenant_id, company_id, store_id, code, name, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'ACTIVE');
	`, h.TerminalID, h.TenantID, h.CompanyID, h.StoreID, prefix+"-TRM", prefix+" Terminal"); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, display_name, status)
		VALUES ($1, $2, $3, $4, $5, 'ACTIVE');
	`, h.CashierID, h.TenantID, prefix+"_cashier", prefix+"_cashier@test.org", prefix+" Cashier"); err != nil {
		t.Fatalf("seed cashier: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO identity.user_account (id, tenant_id, username, email, display_name, status)
		VALUES ($1, $2, $3, $4, $5, 'ACTIVE');
	`, h.ManagerID, h.TenantID, prefix+"_mgr", prefix+"_mgr@test.org", prefix+" Manager"); err != nil {
		t.Fatalf("seed manager: %v", err)
	}

	return h
}

func newPrincipal(tenantID, userID uuid.UUID, permissions []string) *auth.AuthenticatedPrincipal {
	return &auth.AuthenticatedPrincipal{
		SessionID:   uuid.New(),
		TenantID:    tenantID,
		UserID:      userID,
		Username:    "testuser",
		DisplayName: "Test User",
		Roles:       []string{"CASHIER"},
		Permissions: permissions,
	}
}

func contextWithPrincipal(ctx context.Context, p *auth.AuthenticatedPrincipal) context.Context {
	ctx = auth.WithPrincipal(ctx, p)
	ctx = postgres.WithPrincipalContext(ctx, p.ToPrincipalContext())
	return ctx
}

func setupTestRouter(handler *pos.Handler, principal *auth.AuthenticatedPrincipal) chi.Router {
	r := chi.NewRouter()
	r.Use(platformmw.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			if principal != nil {
				ctx = auth.WithPrincipal(ctx, principal)
				ctx = postgres.WithPrincipalContext(ctx, principal.ToPrincipalContext())
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Mount("/api/v1/pos", handler.Routes())
	r.Mount("/pos", handler.CompatibilityRoutes())
	return r
}

// 1. Session Opening, Float Validation, and Automatic Opening Cash Movement
func TestPOS_Integration_OpenSession_SuccessAndFloatMovement(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "OPN")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	principal := newPrincipal(h.TenantID, h.CashierID, []string{pos.PermSessionOpen, pos.PermSessionRead})
	callCtx := contextWithPrincipal(ctx, principal)

	// Open session with positive opening float
	session, err := svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("OpenSession failed: %v", err)
	}

	if session.Status != pos.SessionStatusOpen {
		t.Errorf("expected session status OPEN, got %s", session.Status)
	}
	if session.OpeningCash != 100000.0 {
		t.Errorf("expected opening cash 100000.0, got %f", session.OpeningCash)
	}
	if session.ExpectedCash == nil || *session.ExpectedCash != 100000.0 {
		t.Fatalf("expected ExpectedCash 100000.0, got %v", session.ExpectedCash)
	}

	// Verify pos.cash_movement has OPENING movement
	var movements []*pos.CashMovement
	err = tm.WithinTx(callCtx, func(txCtx context.Context) error {
		var err error
		movements, err = store.ListCashMovements(txCtx, session.ID)
		return err
	})
	if err != nil {
		t.Fatalf("ListCashMovements: %v", err)
	}
	if len(movements) != 1 {
		t.Fatalf("expected 1 movement, got %d", len(movements))
	}
	if movements[0].Type != pos.MovementTypeOpening {
		t.Errorf("expected movement type OPENING, got %s", movements[0].Type)
	}
	if movements[0].Amount != 100000.0 {
		t.Errorf("expected movement amount 100000.0, got %f", movements[0].Amount)
	}

	// Verify Audit Log entry POS_SESSION_OPENED
	var auditCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE action = 'POS_SESSION_OPENED' AND resource_id = $1
	`, session.ID).Scan(&auditCount)
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("expected 1 audit log entry for POS_SESSION_OPENED, got %d", auditCount)
	}

	// Verify Outbox Event pos.session.opened.v1
	var outboxCount int
	err = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM integration.outbox_event
		WHERE event_type = 'pos.session.opened.v1' AND aggregate_id = $1
	`, session.ID).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("expected 1 outbox event for pos.session.opened.v1, got %d", outboxCount)
	}

	// Negative Float Test: must fail
	_, err = svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: -500.0,
		Currency:    "IDR",
	})
	if !errors.Is(err, pos.ErrNegativeOpeningCash) {
		t.Fatalf("expected ErrNegativeOpeningCash, got %v", err)
	}
}

// 2. Blind Cash Count Confidentiality Across All Response Paths (CORR-POS-001)
func TestPOS_Integration_BlindCountConfidentiality(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "BLD")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)
	handler := pos.NewHandler(svc, nil, logger)

	cashierPrincipal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead, pos.PermCashMovementCreate, pos.PermSessionClose,
	})
	supervisorPrincipal := newPrincipal(h.TenantID, h.ManagerID, []string{
		pos.PermSessionRead, pos.PermSessionVarianceReview, pos.PermSessionVarianceApprove,
	})

	// Open session with 100,000 float, then Cash In 50,000 -> Expected Cash = 150,000
	initCtx := contextWithPrincipal(ctx, cashierPrincipal)
	session, err := svc.OpenSession(initCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	_, err = svc.CashIn(initCtx, session.ID, pos.CashMovementRequest{
		Amount: 50000.0,
		Reason: "Shift top-up",
	})
	if err != nil {
		t.Fatalf("CashIn: %v", err)
	}

	// Test Vector 1: GET /sessions/{id} by Cashier during OPEN -> ExpectedCash must be NULL
	cashierRouter := setupTestRouter(handler, cashierPrincipal)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/pos/sessions/%s", session.ID), nil)
	rec := httptest.NewRecorder()
	cashierRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	var cashierResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cashierResp); err != nil {
		t.Fatalf("unmarshal cashier response: %v", err)
	}
	if cashierResp["expected_cash"] != nil {
		t.Errorf("SECURITY LEAK: expected_cash exposed to cashier: %v", cashierResp["expected_cash"])
	}
	if cashierResp["variance"] != nil {
		t.Errorf("SECURITY LEAK: variance exposed to cashier: %v", cashierResp["variance"])
	}

	// Test Vector 2: GET /sessions/{id}/summary by Cashier during OPEN -> 403 BLIND_COUNT_RESTRICTED
	reqSumm := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/pos/sessions/%s/summary", session.ID), nil)
	recSumm := httptest.NewRecorder()
	cashierRouter.ServeHTTP(recSumm, reqSumm)

	if recSumm.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cashier summary, got %d: %s", recSumm.Code, recSumm.Body.String())
	}
	var errResp map[string]any
	_ = json.Unmarshal(recSumm.Body.Bytes(), &errResp)
	errObj, _ := errResp["error"].(map[string]any)
	if errObj["code"] != "BLIND_COUNT_RESTRICTED" {
		t.Errorf("expected error code BLIND_COUNT_RESTRICTED, got %v", errObj["code"])
	}

	// Test Vector 3: Supervisor can read full figures
	supervisorRouter := setupTestRouter(handler, supervisorPrincipal)
	reqSup := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/pos/sessions/%s", session.ID), nil)
	recSup := httptest.NewRecorder()
	supervisorRouter.ServeHTTP(recSup, reqSup)

	if recSup.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for supervisor, got %d: %s", recSup.Code, recSup.Body.String())
	}
	var supResp map[string]any
	_ = json.Unmarshal(recSup.Body.Bytes(), &supResp)
	if supResp["expected_cash"] == nil || supResp["expected_cash"].(float64) != 150000.0 {
		t.Errorf("expected supervisor to see 150000.0 expected_cash, got %v", supResp["expected_cash"])
	}

	// Test Vector 4: Over-withdrawal cash-out error -> NO numeric drawer balance disclosure
	overwithdrawBody, _ := json.Marshal(pos.CashMovementRequest{
		Amount: 9999999.0,
		Reason: "Excessive payout",
	})
	reqOver := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/pos/sessions/%s/cash-out", session.ID), bytes.NewReader(overwithdrawBody))
	recOver := httptest.NewRecorder()
	cashierRouter.ServeHTTP(recOver, reqOver)

	if recOver.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", recOver.Code, recOver.Body.String())
	}
	rawErrBody := recOver.Body.String()
	if bytes.Contains([]byte(rawErrBody), []byte("150000")) || bytes.Contains([]byte(rawErrBody), []byte("150000.0")) {
		t.Errorf("SECURITY LEAK: error response leaked drawer balance 150000: %s", rawErrBody)
	}
}

// 3. Mid-shift Cash In and Cash Out Movements with Concurrency Protection
func TestPOS_Integration_CashInAndCashOut(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "MOV")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	principal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead, pos.PermCashMovementCreate,
	})
	callCtx := contextWithPrincipal(ctx, principal)

	session, err := svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Cash In 50,000 -> Expected Cash 150,000
	inMv, err := svc.CashIn(callCtx, session.ID, pos.CashMovementRequest{
		Amount: 50000.0,
		Reason: "Mid-day float addition",
	})
	if err != nil {
		t.Fatalf("CashIn failed: %v", err)
	}
	if inMv.Amount != 50000.0 || inMv.Type != pos.MovementTypeCashIn {
		t.Errorf("unexpected cash in movement: %+v", inMv)
	}

	// Cash Out 30,000 -> Expected Cash 120,000
	outMv, err := svc.CashOut(callCtx, session.ID, pos.CashMovementRequest{
		Amount: 30000.0,
		Reason: "Safe cash drop",
	})
	if err != nil {
		t.Fatalf("CashOut failed: %v", err)
	}
	if outMv.Amount != 30000.0 || outMv.Type != pos.MovementTypeCashOut {
		t.Errorf("unexpected cash out movement: %+v", outMv)
	}

	// Verify authoritative expected_cash in database
	sessDB, err := svc.GetSession(callCtx, session.ID, true)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sessDB.ExpectedCash == nil || *sessDB.ExpectedCash != 120000.0 {
		t.Fatalf("expected authoritative expected_cash 120000.0, got %v", sessDB.ExpectedCash)
	}

	// Over-withdrawal: Cash out 150,000 (exceeds 120,000)
	_, err = svc.CashOut(callCtx, session.ID, pos.CashMovementRequest{
		Amount: 150000.0,
		Reason: "Attempt over-withdrawal",
	})
	if !errors.Is(err, pos.ErrInsufficientDrawerCash) {
		t.Fatalf("expected ErrInsufficientDrawerCash, got %v", err)
	}
}

// 4. Balanced Shift Close (variance == 0 -> CLOSED)
func TestPOS_Integration_BalancedClose(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "BAL")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	principal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead, pos.PermCashMovementCreate, pos.PermSessionClose,
	})
	callCtx := contextWithPrincipal(ctx, principal)

	session, err := svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Close with exactly 100,000 (variance = 0)
	closedSession, err := svc.CloseSession(callCtx, session.ID, pos.CloseSessionRequest{
		ClosingCash: 100000.0,
		Comment:     "Exact count at shift end",
	}, false)
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	if closedSession.Status != pos.SessionStatusClosed {
		t.Fatalf("expected status CLOSED, got %s", closedSession.Status)
	}
	if closedSession.ClosedAt == nil {
		t.Errorf("expected non-nil ClosedAt")
	}
	if closedSession.Variance == nil || *closedSession.Variance != 0.0 {
		t.Errorf("expected variance 0.0, got %v", closedSession.Variance)
	}

	// Verify audit log POS_SESSION_CLOSED
	var auditCount int
	_ = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE action = 'POS_SESSION_CLOSED' AND resource_id = $1
	`, session.ID).Scan(&auditCount)
	if auditCount != 1 {
		t.Errorf("expected 1 audit log entry for POS_SESSION_CLOSED, got %d", auditCount)
	}

	// Verify outbox pos.session.closed.v1
	var outboxCount int
	_ = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM integration.outbox_event
		WHERE event_type = 'pos.session.closed.v1' AND aggregate_id = $1
	`, session.ID).Scan(&outboxCount)
	if outboxCount != 1 {
		t.Errorf("expected 1 outbox event for pos.session.closed.v1, got %d", outboxCount)
	}

	// Mutating closed session must fail with ErrInvalidSessionStatus
	_, err = svc.CashIn(callCtx, session.ID, pos.CashMovementRequest{
		Amount: 10000.0,
		Reason: "Late cash in",
	})
	if !errors.Is(err, pos.ErrInvalidSessionStatus) {
		t.Fatalf("expected ErrInvalidSessionStatus on closed session, got %v", err)
	}
}

// 5. Variance Review Lifecycle and Segregation of Duties (SoD)
func TestPOS_Integration_VarianceReviewAndApproval(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "VAR")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	cashierPrincipal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead, pos.PermSessionClose, pos.PermSessionVarianceApprove,
	})
	supervisorPrincipal := newPrincipal(h.TenantID, h.ManagerID, []string{
		pos.PermSessionRead, pos.PermSessionVarianceReview, pos.PermSessionVarianceApprove,
	})

	cashierCtx := contextWithPrincipal(ctx, cashierPrincipal)
	session, err := svc.OpenSession(cashierCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Cashier counts 90,000 (shortage of 10,000) -> Status becomes VARIANCE_REVIEW
	// Caller lacks supervisor privilege, so response masks expected cash and variance
	varSession, err := svc.CloseSession(cashierCtx, session.ID, pos.CloseSessionRequest{
		ClosingCash: 90000.0,
		Comment:     "Physical bill count discrepancy",
	}, false)
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	if varSession.Status != pos.SessionStatusVarianceReview {
		t.Fatalf("expected status VARIANCE_REVIEW, got %s", varSession.Status)
	}
	if varSession.ExpectedCash != nil {
		t.Errorf("CONFIDENTIALITY LEAK: expected_cash returned in cashier close response: %v", *varSession.ExpectedCash)
	}
	if varSession.Variance != nil {
		t.Errorf("CONFIDENTIALITY LEAK: variance returned in cashier close response: %v", *varSession.Variance)
	}

	// Verify Audit Log POS_SESSION_VARIANCE_DETECTED
	var auditVarCount int
	_ = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE action = 'POS_SESSION_VARIANCE_DETECTED' AND resource_id = $1
	`, session.ID).Scan(&auditVarCount)
	if auditVarCount != 1 {
		t.Errorf("expected 1 POS_SESSION_VARIANCE_DETECTED audit log, got %d", auditVarCount)
	}

	// Segregation of Duties (SoD): Cashier attempts to approve their own shift variance
	_, err = svc.ApproveVariance(cashierCtx, session.ID, pos.ApproveVarianceRequest{
		Reason: "I approve my own shortage",
	})
	if !errors.Is(err, pos.ErrSoDViolation) {
		t.Fatalf("expected ErrSoDViolation on self-approval, got %v", err)
	}

	// Supervisor approves variance with valid justification
	supCtx := contextWithPrincipal(ctx, supervisorPrincipal)
	approvedSession, err := svc.ApproveVariance(supCtx, session.ID, pos.ApproveVarianceRequest{
		Reason: "Approved discrepancy due to documented change float adjustment",
	})
	if err != nil {
		t.Fatalf("Supervisor ApproveVariance failed: %v", err)
	}

	if approvedSession.Status != pos.SessionStatusClosed {
		t.Fatalf("expected session status CLOSED after approval, got %s", approvedSession.Status)
	}
	if approvedSession.VarianceApprovedBy == nil || *approvedSession.VarianceApprovedBy != h.ManagerID {
		t.Errorf("expected VarianceApprovedBy %s, got %v", h.ManagerID, approvedSession.VarianceApprovedBy)
	}
	if approvedSession.VarianceApprovedAt == nil {
		t.Errorf("expected non-nil VarianceApprovedAt")
	}
	if approvedSession.VarianceReason == nil || *approvedSession.VarianceReason != "Approved discrepancy due to documented change float adjustment" {
		t.Errorf("expected VarianceReason populated, got %v", approvedSession.VarianceReason)
	}

	// Verify Audit Log POS_SESSION_VARIANCE_APPROVED with reason
	var auditApproveCount int
	_ = migrationPool.QueryRow(ctx, `
		SELECT count(*) FROM audit.audit_log
		WHERE action = 'POS_SESSION_VARIANCE_APPROVED' AND resource_id = $1 AND reason IS NOT NULL
	`, session.ID).Scan(&auditApproveCount)
	if auditApproveCount != 1 {
		t.Errorf("expected 1 POS_SESSION_VARIANCE_APPROVED audit log with reason, got %d", auditApproveCount)
	}
}

// 6. Terminal Exclusivity (Exactly One Active Session per Terminal)
func TestPOS_Integration_TerminalExclusivity(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "EXC")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	principal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead, pos.PermSessionClose,
	})
	callCtx := contextWithPrincipal(ctx, principal)

	// 1. Open first session
	session1, err := svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("open first session: %v", err)
	}

	// 2. Attempt to open second session on same terminal -> must fail with ErrTerminalSessionActive
	_, err = svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 50000.0,
		Currency:    "IDR",
	})
	if !errors.Is(err, pos.ErrTerminalSessionActive) {
		t.Fatalf("expected ErrTerminalSessionActive on concurrent open, got %v", err)
	}

	// 3. Close first session
	_, err = svc.CloseSession(callCtx, session1.ID, pos.CloseSessionRequest{
		ClosingCash: 100000.0,
	}, false)
	if err != nil {
		t.Fatalf("close first session: %v", err)
	}

	// 4. Opening a new session on the terminal now succeeds
	session2, err := svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("open session after close failed: %v", err)
	}
	if session2.ID == session1.ID {
		t.Fatalf("expected new session ID, got identical ID")
	}
}

// 7. Concurrent Cash Out Requests Under Heavy Race (Triple-Layer Defense)
func TestPOS_Integration_ConcurrentCashOutRaces(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	h := seedHierarchy(t, migrationPool, "RCE")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	principal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead, pos.PermCashMovementCreate,
	})
	callCtx := contextWithPrincipal(ctx, principal)

	// Opening cash: 100,000
	session, err := svc.OpenSession(callCtx, pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// 10 concurrent goroutines attempt to cash-out 20,000 each (total 200,000)
	// Exactly 5 should succeed (5 * 20,000 = 100,000) and 5 should fail with ErrInsufficientDrawerCash
	const concurrency = 10
	var successCount int64
	var failureCount int64

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := svc.CashOut(callCtx, session.ID, pos.CashMovementRequest{
				Amount: 20000.0,
				Reason: fmt.Sprintf("Concurrent payout #%d", idx),
			})
			if err == nil {
				atomic.AddInt64(&successCount, 1)
			} else if errors.Is(err, pos.ErrInsufficientDrawerCash) {
				atomic.AddInt64(&failureCount, 1)
			} else {
				t.Errorf("unexpected error in concurrent cash out: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if successCount != 5 {
		t.Errorf("expected exactly 5 successful cash-outs, got %d", successCount)
	}
	if failureCount != 5 {
		t.Errorf("expected exactly 5 failed cash-outs, got %d", failureCount)
	}

	// Authoritative balance in DB must be exactly 0
	finalSession, err := svc.GetSession(callCtx, session.ID, true)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if finalSession.ExpectedCash == nil || *finalSession.ExpectedCash != 0.0 {
		t.Fatalf("expected exactly 0.0 drawer cash, got %v", finalSession.ExpectedCash)
	}
}

// 8. Idempotency Key Replay and Conflict Protection
func TestPOS_Integration_IdempotencyReplay(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}

	h := seedHierarchy(t, migrationPool, "IDM")
	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	idemStore := idempotency.NewPgxStore(appPool, logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)
	handler := pos.NewHandler(svc, idemStore, logger)

	principal := newPrincipal(h.TenantID, h.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead,
	})
	router := setupTestRouter(handler, principal)

	openPayload := pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	}
	body, _ := json.Marshal(openPayload)

	idemKey := "key-pos-open-" + uuid.New().String()

	// 1. First execution
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/pos/sessions/open", bytes.NewReader(body))
	req1.Header.Set("Idempotency-Key", idemKey)
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first execution failed: code %d, body: %s", rec1.Code, rec1.Body.String())
	}
	var sess1 map[string]any
	_ = json.Unmarshal(rec1.Body.Bytes(), &sess1)

	// 2. Replay with identical key and payload -> must return exact same response
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/pos/sessions/open", bytes.NewReader(body))
	req2.Header.Set("Idempotency-Key", idemKey)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("replayed execution failed: code %d, body: %s", rec2.Code, rec2.Body.String())
	}
	var sess2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &sess2)

	if sess1["id"] != sess2["id"] {
		t.Errorf("expected identical session ID on idempotent replay: %v != %v", sess1["id"], sess2["id"])
	}

	// 3. Replay with identical key but DIFFERENT payload -> 409 IDEMPOTENCY_KEY_REUSED
	diffPayload, _ := json.Marshal(pos.OpenSessionRequest{
		TerminalID:  h.TerminalID,
		OpeningCash: 250000.0,
		Currency:    "IDR",
	})
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/pos/sessions/open", bytes.NewReader(diffPayload))
	req3.Header.Set("Idempotency-Key", idemKey)
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on payload mismatch, got %d: %s", rec3.Code, rec3.Body.String())
	}
}

// 9. Cross-Tenant Isolation under Forced RLS with nexora_app (NOBYPASSRLS)
func TestPOS_Integration_RLSTenantIsolation(t *testing.T) {
	migrationPool, appPool := setupPosIntegrationDatabase(t)
	if appPool == nil {
		return
	}
	ctx := context.Background()

	hA := seedHierarchy(t, migrationPool, "TNA")
	hB := seedHierarchy(t, migrationPool, "TNB")

	logger := newIntegrationDiscardLogger()
	tm := postgres.NewTxManager(appPool, logger)
	store := pos.NewPgxStore(appPool)
	auditStore := audit.NewPgxStore(appPool, tm, logger)
	outboxWriter := outbox.NewPgxWriter(logger)
	svc := pos.NewService(store, tm, auditStore, outboxWriter, logger)

	principalA := newPrincipal(hA.TenantID, hA.CashierID, []string{
		pos.PermSessionOpen, pos.PermSessionRead,
	})
	ctxA := contextWithPrincipal(ctx, principalA)

	// Tenant A opens a session
	sessionA, err := svc.OpenSession(ctxA, pos.OpenSessionRequest{
		TerminalID:  hA.TerminalID,
		OpeningCash: 100000.0,
		Currency:    "IDR",
	})
	if err != nil {
		t.Fatalf("Tenant A OpenSession: %v", err)
	}

	// Tenant B attempts to read Tenant A's session -> must return ErrSessionNotFound under RLS
	principalB := newPrincipal(hB.TenantID, hB.CashierID, []string{
		pos.PermSessionRead,
	})
	ctxB := contextWithPrincipal(ctx, principalB)

	_, err = svc.GetSession(ctxB, sessionA.ID, true)
	if !errors.Is(err, pos.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound for cross-tenant access, got %v", err)
	}

	// Direct PostgreSQL query check under nexora_app role
	txB, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer txB.Rollback(ctx)

	if _, err := txB.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", hB.TenantID.String()); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}

	var foundCount int
	err = txB.QueryRow(ctx, "SELECT count(*) FROM pos.session WHERE id = $1", sessionA.ID).Scan(&foundCount)
	if err != nil {
		t.Fatalf("query session under tenant B: %v", err)
	}
	if foundCount != 0 {
		t.Fatalf("CRITICAL RLS LEAK: Tenant B can see Tenant A session row!")
	}
}
