package audit_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/audit"
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
		DBMaxConns:          10,
		DBMinConns:          2,
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
	return cfg
}

var (
	setupOnce sync.Once
	setupErr  error
)

func connectLivePool(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adminCfg := getAdminTestConfig()
	adminPool, err := postgres.NewPool(ctx, adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("Live PostgreSQL container not reachable: %v (skipping integration tests)", err)
		return nil, nil
	}

	setupOnce.Do(func() {
		if err := testfixture.SetupTestTable(ctx, adminPool); err != nil {
			setupErr = fmt.Errorf("failed to setup test tables: %w", err)
			return
		}

		if err := testfixture.SetupAuditTables(ctx, adminPool); err != nil {
			setupErr = fmt.Errorf("failed to setup audit tables: %w", err)
			return
		}
	})

	if setupErr != nil {
		adminPool.Close()
		t.Fatalf("%v", setupErr)
	}

	appCfg := getAppTestConfig()
	appPool, err := postgres.NewPool(ctx, appCfg, newDiscardLogger())
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect as nexora_app: %v", err)
	}

	return adminPool, appPool
}

// 5.1 Append-Only Behavior & DB Triggers
func TestAudit_Integration_AppendOnlyAndImmutability(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	var insertedLogID int64
	var insertedChangeID int64

	// Insert audit record with a data change in transaction
	err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
		log := &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_COMPLETED",
			Status:   audit.StatusSuccess,
			Metadata: []byte(`{"order_no":"SO-001"}`),
		}
		ch := &audit.DataChange{
			TableSchema: "sales",
			TableName:   "sales_order",
			FieldName:   "status",
			OldValue:    []byte(`"PENDING"`),
			NewValue:    []byte(`"COMPLETED"`),
		}
		if err := store.Record(txCtx, log, []*audit.DataChange{ch}); err != nil {
			return err
		}
		insertedLogID = log.ID
		insertedChangeID = ch.ID
		return nil
	})
	if err != nil {
		t.Fatalf("failed to insert audit record: %v", err)
	}

	if insertedLogID == 0 || insertedChangeID == 0 {
		t.Fatalf("expected non-zero IDs, got log=%d change=%d", insertedLogID, insertedChangeID)
	}

	// Verify database-level trigger prevents UPDATE on audit.audit_log
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `UPDATE audit.audit_log SET status = 'FAILED' WHERE id = $1`, insertedLogID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected UPDATE on audit.audit_log to fail due to trigger trg_audit_log_immutable, got nil")
	}
	if !strings.Contains(err.Error(), "Immutable record: audit.audit_log") {
		t.Errorf("expected immutable record trigger error, got: %v", err)
	}

	// Verify database-level trigger prevents DELETE on audit.audit_log
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `DELETE FROM audit.audit_log WHERE id = $1`, insertedLogID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected DELETE on audit.audit_log to fail due to trigger trg_audit_log_immutable, got nil")
	}
	if !strings.Contains(err.Error(), "Immutable record: audit.audit_log") {
		t.Errorf("expected immutable record trigger error, got: %v", err)
	}

	// Verify database-level trigger prevents UPDATE on audit.data_change
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `UPDATE audit.data_change SET field_name = 'tampered' WHERE id = $1`, insertedChangeID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected UPDATE on audit.data_change to fail due to trigger trg_audit_data_change_immutable, got nil")
	}
	if !strings.Contains(err.Error(), "Immutable record: audit.data_change") {
		t.Errorf("expected immutable record trigger error, got: %v", err)
	}

	// Verify database-level trigger prevents DELETE on audit.data_change
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `DELETE FROM audit.data_change WHERE id = $1`, insertedChangeID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected DELETE on audit.data_change to fail due to trigger trg_audit_data_change_immutable, got nil")
	}
	if !strings.Contains(err.Error(), "Immutable record: audit.data_change") {
		t.Errorf("expected immutable record trigger error, got: %v", err)
	}
}

// 5.2 Tenant Isolation and RLS Verification
func TestAudit_Integration_TenantIsolationAndRLS(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantA := uuid.New()
	tenantB := uuid.New()

	var tenantALogID int64

	// Tenant A inserts audit record
	err := tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
		log := &audit.Log{
			TenantID: tenantA,
			Action:   "PRICE_CREATED",
			Status:   audit.StatusSuccess,
			Metadata: []byte(`{"price":1000}`),
		}
		ch := &audit.DataChange{
			TableSchema: "pricing",
			TableName:   "price_rule",
			FieldName:   "amount",
			NewValue:    []byte(`1000`),
		}
		if err := store.Record(txCtx, log, []*audit.DataChange{ch}); err != nil {
			return err
		}
		tenantALogID = log.ID
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant A failed to insert audit record: %v", err)
	}

	// Tenant B attempts to read Tenant A's audit record via FindAuditLogs under RLS
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantB), func(txCtx context.Context) error {
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{})
		if err != nil {
			return err
		}
		if total != 0 || len(logs) != 0 {
			t.Fatalf("Tenant B saw %d records; expected 0 due to RLS tenant isolation", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant B query failed: %v", err)
	}

	// Tenant B attempts to access Tenant A's data changes via GetDataChanges
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantB), func(txCtx context.Context) error {
		changes, err := store.GetDataChanges(txCtx, tenantALogID)
		if err != nil {
			return err
		}
		if len(changes) != 0 {
			t.Fatalf("Tenant B was able to read %d data changes of Tenant A", len(changes))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GetDataChanges failed: %v", err)
	}

	// Tenant A attempts to forge Tenant B's tenant_id in Record
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
		forgedLog := &audit.Log{
			TenantID: tenantB, // Forged!
			Action:   "PRICE_CREATED",
			Status:   audit.StatusSuccess,
		}
		return store.Record(txCtx, forgedLog, nil)
	})
	if err == nil {
		t.Fatalf("expected forged tenant ID to be rejected, got nil")
	}
	if !errors.Is(err, audit.ErrTenantMismatch) {
		t.Errorf("expected ErrTenantMismatch, got: %v", err)
	}
}

// 5.3 Transaction Atomicity & Rollback
func TestAudit_Integration_TransactionAtomicityAndRollback(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	// Case 1: Transaction rolls back -> Audit record must be rolled back too!
	forcedErr := errors.New("simulated business write failure")
	err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)

		// Business write simulation using same active transaction
		if err := testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
			ID:       uuid.New(),
			TenantID: tenantID,
			Name:     "Rollback Item",
		}); err != nil {
			return err
		}

		log := &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_CREATED",
			Status:   audit.StatusSuccess,
		}
		if err := store.Record(txCtx, log, nil); err != nil {
			return err
		}

		// Force error to rollback
		return forcedErr
	})

	if !errors.Is(err, forcedErr) {
		t.Fatalf("expected simulated error, got %v", err)
	}

	// Verify neither test item nor audit log was committed
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		count, err := testfixture.CountTestItems(txCtx, tx)
		if err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("expected 0 test items after rollback, got %d", count)
		}

		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{})
		if err != nil {
			return err
		}
		if total != 0 || len(logs) != 0 {
			t.Errorf("expected 0 audit logs after rollback, got %d", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verification query failed: %v", err)
	}

	// Case 2: Commit persists both business item and audit log
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		if err := testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
			ID:       uuid.New(),
			TenantID: tenantID,
			Name:     "Committed Item",
		}); err != nil {
			return err
		}

		log := &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_COMPLETED",
			Status:   audit.StatusSuccess,
		}
		return store.Record(txCtx, log, nil)
	})
	if err != nil {
		t.Fatalf("committed transaction failed: %v", err)
	}

	// Verify both committed
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		count, err := testfixture.CountTestItems(txCtx, tx)
		if err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("expected 1 test item, got %d", count)
		}

		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{})
		if err != nil {
			return err
		}
		if total != 1 || len(logs) != 1 {
			t.Errorf("expected 1 audit log, got %d", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verification query failed: %v", err)
	}

	// Case 3: Calling Record outside an active transaction fails closed
	err = store.Record(txCtx, &audit.Log{
		TenantID: tenantID,
		Action:   "SALE_CREATED",
		Status:   audit.StatusSuccess,
	}, nil)
	if !errors.Is(err, audit.ErrNoActiveTransaction) {
		t.Fatalf("expected ErrNoActiveTransaction, got %v", err)
	}
}

// 5.4 Sensitive Metadata Protection
func TestAudit_Integration_SensitiveMetadataScrubbing(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	var logID int64

	// Write audit log containing sensitive passwords and card numbers
	err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
		log := &audit.Log{
			TenantID: tenantID,
			Action:   "PAYMENT_CREATED",
			Status:   audit.StatusSuccess,
			Metadata: []byte(`{
				"merchant": "Store 1",
				"password": "plainSecret123",
				"card_number": "4111222233334444",
				"cvv": "123",
				"customer_id": "cust-888"
			}`),
		}
		if err := store.Record(txCtx, log, nil); err != nil {
			return err
		}
		logID = log.ID
		return nil
	})
	if err != nil {
		t.Fatalf("failed to record audit log: %v", err)
	}

	// Retrieve audit log and inspect stored metadata in PostgreSQL
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{})
		if err != nil {
			return err
		}
		if total != 1 || logs[0].ID != logID {
			t.Fatalf("expected to find audit log %d", logID)
		}

		storedJSON := string(logs[0].Metadata)
		if strings.Contains(storedJSON, "plainSecret123") {
			t.Errorf("CRITICAL SECURITY LEAK: plaintext password stored in audit metadata: %s", storedJSON)
		}
		if strings.Contains(storedJSON, "4111222233334444") {
			t.Errorf("CRITICAL SECURITY LEAK: plaintext card_number stored in audit metadata: %s", storedJSON)
		}
		if strings.Contains(storedJSON, `"cvv":"123"`) {
			t.Errorf("CRITICAL SECURITY LEAK: plaintext cvv stored in audit metadata: %s", storedJSON)
		}
		if !strings.Contains(storedJSON, audit.RedactedPlaceholder) {
			t.Errorf("expected [REDACTED] in stored metadata, got %s", storedJSON)
		}
		if !strings.Contains(storedJSON, "cust-888") {
			t.Errorf("expected non-sensitive customer_id to be preserved, got %s", storedJSON)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to verify scrubbed metadata: %v", err)
	}
}

// 5.5 Concurrency and Multi-Tenant Pool Contention
func TestAudit_Integration_ConcurrentMultiTenantWrites(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenant1 := uuid.New()
	tenant2 := uuid.New()

	const iterations = 10
	var wg sync.WaitGroup
	errCh := make(chan error, iterations*2)

	for i := 0; i < iterations; i++ {
		wg.Add(2)

		// Tenant 1 goroutine
		go func(idx int) {
			defer wg.Done()
			txCtx := postgres.WithTenantID(ctx, tenant1)
			err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
				log := &audit.Log{
					TenantID: tenant1,
					Action:   "SALE_COMPLETED",
					Status:   audit.StatusSuccess,
					Metadata: []byte(fmt.Sprintf(`{"tx":%d}`, idx)),
				}
				return store.Record(txCtx, log, nil)
			})
			if err != nil {
				errCh <- fmt.Errorf("tenant1 idx %d: %w", idx, err)
			}
		}(i)

		// Tenant 2 goroutine
		go func(idx int) {
			defer wg.Done()
			txCtx := postgres.WithTenantID(ctx, tenant2)
			err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
				log := &audit.Log{
					TenantID: tenant2,
					Action:   "PRICE_CREATED",
					Status:   audit.StatusSuccess,
					Metadata: []byte(fmt.Sprintf(`{"tx":%d}`, idx)),
				}
				return store.Record(txCtx, log, nil)
			})
			if err != nil {
				errCh <- fmt.Errorf("tenant2 idx %d: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent write failure: %v", err)
	}

	// Verify Tenant 1 records count
	_ = tm.WithinTx(postgres.WithTenantID(ctx, tenant1), func(txCtx context.Context) error {
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{Limit: 100})
		if err != nil {
			t.Fatalf("failed to query tenant 1: %v", err)
		}
		if total != iterations || len(logs) != iterations {
			t.Errorf("expected %d records for Tenant 1, got %d", iterations, total)
		}
		for _, l := range logs {
			if l.TenantID != tenant1 {
				t.Errorf("cross-tenant leakage! record has tenant %s, expected %s", l.TenantID, tenant1)
			}
		}
		return nil
	})

	// Verify Tenant 2 records count
	_ = tm.WithinTx(postgres.WithTenantID(ctx, tenant2), func(txCtx context.Context) error {
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{Limit: 100})
		if err != nil {
			t.Fatalf("failed to query tenant 2: %v", err)
		}
		if total != iterations || len(logs) != iterations {
			t.Errorf("expected %d records for Tenant 2, got %d", iterations, total)
		}
		for _, l := range logs {
			if l.TenantID != tenant2 {
				t.Errorf("cross-tenant leakage! record has tenant %s, expected %s", l.TenantID, tenant2)
			}
		}
		return nil
	})
}

// Standalone, Security Events & Login History
func TestAudit_Integration_StandaloneAndSecurityEvents(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	// Test RecordStandalone
	standaloneLog := &audit.Log{
		TenantID: tenantID,
		Action:   "PERMISSION_DENIED",
		Status:   audit.StatusDenied,
		Metadata: []byte(`{"resource":"order_approval"}`),
	}
	if err := store.RecordStandalone(txCtx, standaloneLog); err != nil {
		t.Fatalf("RecordStandalone failed: %v", err)
	}
	if standaloneLog.ID == 0 {
		t.Fatalf("expected generated ID for standalone log")
	}

	// Test RecordSecurityEvent
	secEvt := &audit.SecurityEvent{
		TenantID:  &tenantID,
		EventType: "RATE_LIMIT_TRIGGERED",
		Severity:  "HIGH",
		Metadata:  []byte(`{"ip":"192.168.1.100","secret":"leaked"}`),
	}
	if err := store.RecordSecurityEvent(txCtx, secEvt); err != nil {
		t.Fatalf("RecordSecurityEvent failed: %v", err)
	}
	if secEvt.ID == 0 {
		t.Fatalf("expected generated ID for security event")
	}
	if strings.Contains(string(secEvt.Metadata), "leaked") {
		t.Errorf("expected sensitive secret to be scrubbed from security event metadata")
	}

	// Test RecordLoginHistory
	loginEntry := &audit.LoginHistory{
		TenantID: &tenantID,
		Username: "cashier_john",
		Success:  true,
	}
	if err := store.RecordLoginHistory(txCtx, loginEntry); err != nil {
		t.Fatalf("RecordLoginHistory failed: %v", err)
	}
	if loginEntry.ID == 0 {
		t.Fatalf("expected generated ID for login history")
	}
}

// 5.6 Filtering, Bounded Pagination and Deterministic Ordering
func TestAudit_Integration_FilteringAndPagination(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	actions := []string{"SALE_CREATED", "SALE_COMPLETED", "PAYMENT_CREATED", "PAYMENT_SUCCESS", "PRICE_CREATED"}

	// Seed 5 records with 5ms sleep to ensure distinct timestamps
	for _, act := range actions {
		err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
			return store.Record(txCtx, &audit.Log{
				TenantID: tenantID,
				Action:   act,
				Status:   audit.StatusSuccess,
			}, nil)
		})
		if err != nil {
			t.Fatalf("seeding failed for %s: %v", act, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Test Filter by Action
	_ = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		actionFilter := "SALE_COMPLETED"
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{
			Action: &actionFilter,
		})
		if err != nil {
			t.Fatalf("FindAuditLogs by action failed: %v", err)
		}
		if total != 1 || len(logs) != 1 || logs[0].Action != "SALE_COMPLETED" {
			t.Fatalf("expected 1 record for SALE_COMPLETED, got %d", total)
		}
		return nil
	})

	// Test Filter by Module
	_ = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		moduleFilter := "SALES"
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{
			Module: &moduleFilter,
		})
		if err != nil {
			t.Fatalf("FindAuditLogs by module failed: %v", err)
		}
		if total != 2 || len(logs) != 2 {
			t.Fatalf("expected 2 records for SALES module, got %d", total)
		}
		return nil
	})

	// Test Pagination & Deterministic Ordering (created_at DESC, id DESC)
	_ = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		// Limit 2, Offset 0
		page1, total, err := store.FindAuditLogs(txCtx, audit.Filter{
			Limit:  2,
			Offset: 0,
		})
		if err != nil {
			t.Fatalf("page 1 failed: %v", err)
		}
		if total != 5 || len(page1) != 2 {
			t.Fatalf("expected total 5 and page1 len 2, got total=%d len=%d", total, len(page1))
		}

		// Limit 2, Offset 2
		page2, total, err := store.FindAuditLogs(txCtx, audit.Filter{
			Limit:  2,
			Offset: 2,
		})
		if err != nil {
			t.Fatalf("page 2 failed: %v", err)
		}
		if total != 5 || len(page2) != 2 {
			t.Fatalf("expected total 5 and page2 len 2, got total=%d len=%d", total, len(page2))
		}

		// Ensure no overlap between page 1 and page 2
		if page1[0].ID == page2[0].ID || page1[1].ID == page2[0].ID {
			t.Errorf("pagination overlap detected between page 1 and page 2")
		}

		// Ensure DESC order
		if page1[0].CreatedAt.Before(page1[1].CreatedAt) {
			t.Errorf("expected descending chronological order, got %v before %v", page1[0].CreatedAt, page1[1].CreatedAt)
		}

		return nil
	})

	// Test Limit Bounding (limit > 100 capped to 100, limit <= 0 defaulted to 20)
	_ = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		logs, total, err := store.FindAuditLogs(txCtx, audit.Filter{
			Limit: 200, // Should cap to 100
		})
		if err != nil {
			t.Fatalf("bounded limit test failed: %v", err)
		}
		if total != 5 || len(logs) != 5 {
			t.Errorf("expected 5 records, got total=%d len=%d", total, len(logs))
		}
		return nil
	})
}

// Validation failures on store.Record
func TestAudit_Integration_ValidationFailures(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	// Subtest 1: Unrecognized action
	err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
		return store.Record(txCtx, &audit.Log{
			TenantID: tenantID,
			Action:   "FORBIDDEN_HACK_ACTION",
			Status:   audit.StatusSuccess,
		}, nil)
	})
	if !errors.Is(err, audit.ErrInvalidAction) {
		t.Errorf("expected ErrInvalidAction, got: %v", err)
	}

	// Subtest 2: Reason required but missing
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		return store.Record(txCtx, &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_VOIDED", // Requires reason!
			Status:   audit.StatusSuccess,
		}, nil)
	})
	if !errors.Is(err, audit.ErrReasonRequired) {
		t.Errorf("expected ErrReasonRequired, got: %v", err)
	}

	// Subtest 3: Invalid status
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		return store.Record(txCtx, &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_CREATED",
			Status:   "BOGUS_STATUS",
		}, nil)
	})
	if !errors.Is(err, audit.ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got: %v", err)
	}
}

// Child Table Database RLS Verification (audit.data_change, audit.security_event, audit.login_history)
func TestAudit_Integration_ChildTableRLS(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantA := uuid.New()
	tenantB := uuid.New()

	var tenantAChangeID int64
	var tenantASecEvtID int64
	var tenantALoginID int64

	// Tenant A inserts records in all three audit tables
	err := tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
		// 1. audit.data_change via store.Record
		log := &audit.Log{
			TenantID: tenantA,
			Action:   "PRICE_CREATED",
			Status:   audit.StatusSuccess,
		}
		ch := &audit.DataChange{
			TableSchema: "pricing",
			TableName:   "price_list",
			FieldName:   "base_price",
			NewValue:    []byte(`5000`),
		}
		if err := store.Record(txCtx, log, []*audit.DataChange{ch}); err != nil {
			return err
		}
		tenantAChangeID = ch.ID

		// 2. audit.security_event
		secEvt := &audit.SecurityEvent{
			TenantID:  &tenantA,
			EventType: "SUSPICIOUS_AUTH_ACTIVITY",
			Severity:  "HIGH",
			Metadata:  []byte(`{"attempt":1}`),
		}
		if err := store.RecordSecurityEvent(txCtx, secEvt); err != nil {
			return err
		}
		tenantASecEvtID = secEvt.ID

		// 3. audit.login_history
		loginEntry := &audit.LoginHistory{
			TenantID: &tenantA,
			Username: "admin_tenant_a",
			Success:  true,
		}
		if err := store.RecordLoginHistory(txCtx, loginEntry); err != nil {
			return err
		}
		tenantALoginID = loginEntry.ID

		return nil
	})
	if err != nil {
		t.Fatalf("Tenant A failed to insert records: %v", err)
	}

	// Tenant B attempts direct database queries on Tenant A's child records under nexora_app RLS
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantB), func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)

		// 1. Verify RLS on audit.data_change
		var changeCount int
		if err := tx.QueryRow(txCtx, `SELECT count(*) FROM audit.data_change WHERE id = $1`, tenantAChangeID).Scan(&changeCount); err != nil {
			return fmt.Errorf("query data_change: %w", err)
		}
		if changeCount != 0 {
			t.Fatalf("CRITICAL RLS LEAK: Tenant B saw %d data changes of Tenant A", changeCount)
		}

		// 2. Verify RLS on audit.security_event
		var secEvtCount int
		if err := tx.QueryRow(txCtx, `SELECT count(*) FROM audit.security_event WHERE id = $1`, tenantASecEvtID).Scan(&secEvtCount); err != nil {
			return fmt.Errorf("query security_event: %w", err)
		}
		if secEvtCount != 0 {
			t.Fatalf("CRITICAL RLS LEAK: Tenant B saw %d security events of Tenant A", secEvtCount)
		}

		// 3. Verify RLS on audit.login_history
		var loginCount int
		if err := tx.QueryRow(txCtx, `SELECT count(*) FROM audit.login_history WHERE id = $1`, tenantALoginID).Scan(&loginCount); err != nil {
			return fmt.Errorf("query login_history: %w", err)
		}
		if loginCount != 0 {
			t.Fatalf("CRITICAL RLS LEAK: Tenant B saw %d login history records of Tenant A", loginCount)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Tenant B RLS verification query failed: %v", err)
	}

	// Tenant A verifies their own records are visible
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)

		var c1, c2, c3 int
		_ = tx.QueryRow(txCtx, `SELECT count(*) FROM audit.data_change WHERE id = $1`, tenantAChangeID).Scan(&c1)
		_ = tx.QueryRow(txCtx, `SELECT count(*) FROM audit.security_event WHERE id = $1`, tenantASecEvtID).Scan(&c2)
		_ = tx.QueryRow(txCtx, `SELECT count(*) FROM audit.login_history WHERE id = $1`, tenantALoginID).Scan(&c3)

		if c1 != 1 || c2 != 1 || c3 != 1 {
			t.Fatalf("expected Tenant A to see all own records, got changes=%d, secEvts=%d, logins=%d", c1, c2, c3)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant A verification query failed: %v", err)
	}
}

// Database Immutability Trigger Verification on audit.login_history
func TestAudit_Integration_LoginHistoryImmutability(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	store := audit.NewPgxStore(appPool, tm, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	var loginID int64
	err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
		entry := &audit.LoginHistory{
			TenantID: &tenantID,
			Username: "security_test_user",
			Success:  false,
		}
		if err := store.RecordLoginHistory(txCtx, entry); err != nil {
			return err
		}
		loginID = entry.ID
		return nil
	})
	if err != nil {
		t.Fatalf("failed to insert login history: %v", err)
	}

	// Verify UPDATE on audit.login_history is blocked by trg_login_history_immutable
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `UPDATE audit.login_history SET success = true WHERE id = $1`, loginID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected UPDATE on audit.login_history to fail due to trigger trg_login_history_immutable, got nil")
	}
	if !strings.Contains(err.Error(), "Immutable record: audit.login_history") {
		t.Errorf("expected immutable record trigger error, got: %v", err)
	}

	// Verify DELETE on audit.login_history is blocked by trg_login_history_immutable
	err = tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `DELETE FROM audit.login_history WHERE id = $1`, loginID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected DELETE on audit.login_history to fail due to trigger trg_login_history_immutable, got nil")
	}
	if !strings.Contains(err.Error(), "Immutable record: audit.login_history") {
		t.Errorf("expected immutable record trigger error, got: %v", err)
	}
}

// Foreign Key Constraint on audit.audit_log(action) referencing audit.audit_action_catalog(code)
func TestAudit_Integration_CatalogForeignKeyConstraint(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	// Case 1: Valid canonical action seeded in catalog succeeds at database level
	canonicalActions := []string{"USER_UPDATED", "PAYMENT_COMPLETED", "SALE_HELD", "STOCK_RESERVED"}
	for _, act := range canonicalActions {
		err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			_, execErr := tx.Exec(txCtx, `
				INSERT INTO audit.audit_log (tenant_id, module, action, status)
				VALUES ($1, 'TEST', $2, 'SUCCESS')
			`, tenantID, act)
			return execErr
		})
		if err != nil {
			t.Fatalf("expected canonical action %q to succeed FK constraint, got: %v", act, err)
		}
	}

	// Case 2: Unregistered/bogus action code directly executed against DB must violate fk_audit_log_action
	err := tm.WithinTx(txCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `
			INSERT INTO audit.audit_log (tenant_id, module, action, status)
			VALUES ($1, 'SECURITY', 'BOGUS_NON_EXISTENT_ACTION_123', 'SUCCESS')
		`, tenantID)
		return execErr
	})
	if err == nil {
		t.Fatalf("expected foreign key constraint violation fk_audit_log_action, got nil")
	}
	if !strings.Contains(err.Error(), "fk_audit_log_action") {
		t.Errorf("expected fk_audit_log_action foreign key error, got: %v", err)
	}
}
