package idempotency_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/idempotency"
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
	cfg.DBMaxConns = 25
	cfg.DBMinConns = 5
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
		if err := testfixture.SetupIdentityTables(txCtx, tx); err != nil {
			return err
		}
		return testfixture.SetupIdempotencyTable(txCtx, tx)
	}); setupErr != nil {
		adminPool.Close()
		t.Fatalf("failed to setup idempotency test table: %v", setupErr)
	}

	appCfg := getAppTestConfig()
	appPool, err := postgres.NewPool(ctx, appCfg, newDiscardLogger())
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect as application role: %v", err)
	}

	return adminPool, appPool
}

func TestStore_Integration_LifecycleAndPayloadMismatch(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	store := idempotency.NewPgxStore(appPool, newDiscardLogger())

	tenantID := uuid.New()
	key := "ord-key-" + uuid.New().String()[:8]
	op := "POST /api/v1/pos/orders"

	p1 := []byte(`{"qty": 5, "item": "SKU-001"}`)
	hash1 := idempotency.HashPayload(p1)

	p2 := []byte(`{"qty": 10, "item": "SKU-001"}`)
	hash2 := idempotency.HashPayload(p2)

	// 1. First request acquires in-progress record
	res1, err := store.AcquireOrGet(ctx, tenantID, key, op, hash1, 1*time.Hour)
	if err != nil {
		t.Fatalf("AcquireOrGet failed: %v", err)
	}
	if !res1.Acquired || res1.HashMismatch || res1.Record == nil {
		t.Fatalf("expected acquired=true, got %+v", res1)
	}
	if !res1.Record.IsInProgress() {
		t.Fatalf("expected record to be in-progress")
	}

	// 2. Duplicate while in-progress returns Acquired=false, HashMismatch=false
	resInProg, err := store.AcquireOrGet(ctx, tenantID, key, op, hash1, 1*time.Hour)
	if err != nil {
		t.Fatalf("AcquireOrGet failed: %v", err)
	}
	if resInProg.Acquired || resInProg.HashMismatch {
		t.Fatalf("expected in-progress conflict, got %+v", resInProg)
	}
	if !resInProg.Record.IsInProgress() {
		t.Fatalf("expected existing record to be in-progress")
	}

	// 3. Different payload with same key returns HashMismatch=true
	resMismatch, err := store.AcquireOrGet(ctx, tenantID, key, op, hash2, 1*time.Hour)
	if err != nil {
		t.Fatalf("AcquireOrGet failed: %v", err)
	}
	if !resMismatch.HashMismatch {
		t.Fatalf("expected HashMismatch=true for different payload, got %+v", resMismatch)
	}

	// 4. Complete the operation
	respBody := []byte(`{"order_id":"ord-123","status":"COMPLETED"}`)
	err = store.Complete(ctx, res1.Record.ID, tenantID, 201, respBody)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	// 5. Subsequent request with same key and original payload returns completed record
	resReplay, err := store.AcquireOrGet(ctx, tenantID, key, op, hash1, 1*time.Hour)
	if err != nil {
		t.Fatalf("AcquireOrGet failed: %v", err)
	}
	if resReplay.Acquired || resReplay.HashMismatch {
		t.Fatalf("expected replay match, got %+v", resReplay)
	}
	if !resReplay.Record.IsCompleted() {
		t.Fatalf("expected record to be completed")
	}
	if *resReplay.Record.ResponseStatus != 201 {
		t.Fatalf("expected status 201, got %d", *resReplay.Record.ResponseStatus)
	}
}

func TestStore_Integration_ConcurrentAcquisition(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	store := idempotency.NewPgxStore(appPool, newDiscardLogger())

	tenantID := uuid.New()
	key := "concurrent-key-" + uuid.New().String()[:8]
	op := "POST /api/v1/payments"
	hash := idempotency.HashPayload([]byte(`{"amount": 250000}`))

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var acquiredCount int32
	var inProgressCount int32
	var errorCount int32

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			<-startBarrier

			res, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}
			if res.Acquired {
				atomic.AddInt32(&acquiredCount, 1)
			} else if res.Record != nil && res.Record.IsInProgress() {
				atomic.AddInt32(&inProgressCount, 1)
			}
		}()
	}

	// Unleash all 20 goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	if errorCount != 0 {
		t.Fatalf("expected 0 errors across concurrent attempts, got %d", errorCount)
	}
	if acquiredCount != 1 {
		t.Fatalf("CRITICAL CONCURRENCY VIOLATION: expected exactly 1 acquisition, got %d", acquiredCount)
	}
	if inProgressCount != concurrency-1 {
		t.Fatalf("expected %d goroutines to encounter in-progress state, got %d", concurrency-1, inProgressCount)
	}

	// Verify exactly 1 row exists in platform.idempotency_key for this key
	var rowCount int
	err := adminPool.QueryRow(ctx, `
		SELECT count(*) FROM platform.idempotency_key
		WHERE tenant_id = $1 AND key = $2 AND operation = $3
	`, tenantID, key, op).Scan(&rowCount)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly 1 database row, got %d", rowCount)
	}
}

func TestStore_Integration_MultiTenantIsolation(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	store := idempotency.NewPgxStore(appPool, newDiscardLogger())

	tenantA := uuid.New()
	tenantB := uuid.New()
	sharedKey := "shared-idem-key"
	op := "POST /api/v1/pos/orders"

	hashA := idempotency.HashPayload([]byte(`{"tenant": "A"}`))
	hashB := idempotency.HashPayload([]byte(`{"tenant": "B"}`))

	// Tenant A acquires key
	resA, err := store.AcquireOrGet(ctx, tenantA, sharedKey, op, hashA, 1*time.Hour)
	if err != nil || !resA.Acquired {
		t.Fatalf("Tenant A acquisition failed: %v, %+v", err, resA)
	}

	// Tenant B acquires the exact same key with different payload:
	// Because of tenant isolation, Tenant B MUST NOT conflict with Tenant A!
	resB, err := store.AcquireOrGet(ctx, tenantB, sharedKey, op, hashB, 1*time.Hour)
	if err != nil || !resB.Acquired {
		t.Fatalf("Tenant B acquisition failed (expected independent key per tenant): %v, %+v", err, resB)
	}

	if resA.Record.ID == resB.Record.ID {
		t.Fatalf("expected distinct records for distinct tenants, got identical ID %s", resA.Record.ID)
	}
}

func TestStore_Integration_TransactionRollbackAtomicity(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	appTM := postgres.NewTxManager(appPool, newDiscardLogger())

	tenantID := uuid.New()
	key := "rollback-key-" + uuid.New().String()[:8]
	op := "POST /api/v1/orders/commit"
	hash := idempotency.HashPayload([]byte(`{"item": "test"}`))

	principalCtx := postgres.PrincipalContext{TenantID: tenantID}
	txCtx := postgres.WithPrincipalContext(ctx, principalCtx)

	// Simulate transactional execution where business failure triggers rollback
	err := appTM.WithinTx(txCtx, func(c context.Context) error {
		tx, ok := postgres.TxFromContext(c)
		if !ok {
			t.Fatal("expected tx in context")
		}

		txStore := idempotency.NewPgxStore(tx, newDiscardLogger())
		res, err := txStore.AcquireOrGet(c, tenantID, key, op, hash, 1*time.Hour)
		if err != nil || !res.Acquired {
			return err
		}

		// Insert test item
		_ = testfixture.InsertTestItem(c, tx, testfixture.TestItem{
			ID:       uuid.New(),
			TenantID: tenantID,
			Name:     "Transactional Item",
		})

		// Return error to force transaction rollback
		return postgres.ErrSerializationFailure
	})

	if err == nil {
		t.Fatal("expected error to force rollback")
	}

	// Verify that because the transaction rolled back, the idempotency record was ALSO rolled back
	store := idempotency.NewPgxStore(appPool, newDiscardLogger())
	rec, err := store.Get(ctx, tenantID, key, op)
	if err == nil || rec != nil {
		t.Fatalf("expected idempotency record to be rolled back with transaction, but found: %+v", rec)
	}

	// Retry is permitted now because the rolled back record left no residue
	resRetry, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
	if err != nil || !resRetry.Acquired {
		t.Fatalf("expected retry acquisition to succeed after rollback: %v, %+v", err, resRetry)
	}
}

func TestMiddleware_Integration_LivePostgreSQL_ColdRestartAndReplay(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	userID := uuid.New()

	var executionCount int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&executionCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"order_id":"ord-cold-restart-1","amount":75000}`))
	})

	store := idempotency.NewPgxStore(appPool, newDiscardLogger())
	pipeline := idempotency.RequireIdempotency(store, "POST /pos/orders", 1*time.Hour)(handler)

	key := "cold-restart-key-" + uuid.New().String()[:8]
	payload := []byte(`{"table_no": 4, "items": ["tea", "snack"]}`)

	// 1. Initial HTTP Request: executes handler, writes to PostgreSQL, returns 201 MISS
	req1 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req1.Header.Set("Idempotency-Key", key)
	authCtx := auth.WithPrincipal(ctx, &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
	req1 = req1.WithContext(authCtx)
	rec1 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on first request, got %d (body: %s)", rec1.Code, rec1.Body.String())
	}
	if rec1.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("expected X-Cache: MISS on first request, got %q", rec1.Header().Get("X-Cache"))
	}
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Fatalf("expected handler to execute 1 time, got %d", executionCount)
	}

	// 2. Simulate complete cold restart of the API daemon by constructing a completely new store instance
	restartedStore := idempotency.NewPgxStore(appPool, newDiscardLogger())
	restartedPipeline := idempotency.RequireIdempotency(restartedStore, "POST /pos/orders", 1*time.Hour)(handler)

	// Re-send Request 1 to the restarted pipeline: must retrieve cached response from PostgreSQL
	req2 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req2.Header.Set("Idempotency-Key", key)
	req2 = req2.WithContext(authCtx)
	rec2 := httptest.NewRecorder()

	restartedPipeline.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on replayed request after cold restart, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT on replayed request after cold restart, got %q", rec2.Header().Get("X-Cache"))
	}
	var b1, b2 any
	_ = json.Unmarshal(rec1.Body.Bytes(), &b1)
	_ = json.Unmarshal(rec2.Body.Bytes(), &b2)
	if !reflect.DeepEqual(b1, b2) {
		t.Fatalf("expected identical JSON content %+v, got %+v", b1, b2)
	}
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Fatalf("expected handler NOT to re-execute after cold restart, got %d", executionCount)
	}

	// 3. Different payload with same key must return 409 IDEMPOTENCY_KEY_REUSED
	diffPayload := []byte(`{"table_no": 99, "items": ["caviar"]}`)
	req3 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(diffPayload))
	req3.Header.Set("Idempotency-Key", key)
	req3 = req3.WithContext(authCtx)
	rec3 := httptest.NewRecorder()

	restartedPipeline.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on payload mismatch after cold restart, got %d", rec3.Code)
	}
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Fatalf("expected handler NOT to execute on key reuse conflict, got %d", executionCount)
	}
}

func TestStore_Integration_StaleTimestampCannotAuthoriseDuplicateAcquisition(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	key := "stale-ts-key-" + uuid.New().String()[:8]
	op := "POST /api/v1/pos/orders"
	hash := idempotency.HashPayload([]byte(`{"items": ["item1"]}`))

	store := idempotency.NewPgxStore(appPool, newDiscardLogger())

	// 1. Initial request acquires key
	res1, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
	if err != nil {
		t.Fatalf("initial AcquireOrGet failed: %v", err)
	}
	if !res1.Acquired {
		t.Fatalf("expected initial request to acquire key")
	}

	// 2. Artificially backdate the created_at timestamp in PostgreSQL to 1 hour ago (simulating a long-running execution)
	updateSQL := `
		UPDATE platform.idempotency_key
		SET created_at = now() - interval '1 hour'
		WHERE id = $1;
	`
	_, err = adminPool.Exec(ctx, updateSQL, res1.Record.ID)
	if err != nil {
		t.Fatalf("failed to backdate created_at: %v", err)
	}

	// 3. A second request arrives with identical key and payload:
	// Under remediated rules, an old timestamp alone MUST NOT authorize another request to acquire the key!
	res2, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
	if err != nil {
		t.Fatalf("second AcquireOrGet failed: %v", err)
	}
	if res2.Acquired {
		t.Fatalf("CRITICAL FLAW: second request acquired key despite first request still in-progress!")
	}
	if !res2.Record.IsInProgress() {
		t.Fatalf("expected record to remain in-progress, got completed")
	}
}

func TestStore_Integration_FencingLostReservationCannotComplete(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	key := "fencing-key-" + uuid.New().String()[:8]
	op := "POST /api/v1/pos/orders"
	hash := idempotency.HashPayload([]byte(`{"items": ["fenced"]}`))

	store := idempotency.NewPgxStore(appPool, newDiscardLogger())

	// 1. Acquire key
	res, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
	if err != nil || !res.Acquired {
		t.Fatalf("failed to acquire key: %v", err)
	}

	// 2. Complete once successfully
	err = store.Complete(ctx, res.Record.ID, tenantID, 200, []byte(`{"status":"ok"}`))
	if err != nil {
		t.Fatalf("first complete failed: %v", err)
	}

	// 3. Attempting to complete again with the same record ID must return ErrReservationLost (fenced)
	err = store.Complete(ctx, res.Record.ID, tenantID, 200, []byte(`{"status":"duplicate"}`))
	if !errors.Is(err, idempotency.ErrReservationLost) {
		t.Fatalf("expected ErrReservationLost on second completion, got %v", err)
	}

	// 4. Attempting to complete with a non-existent UUID must return ErrReservationLost
	err = store.Complete(ctx, uuid.New(), tenantID, 200, []byte(`{"status":"lost"}`))
	if !errors.Is(err, idempotency.ErrReservationLost) {
		t.Fatalf("expected ErrReservationLost on unknown UUID, got %v", err)
	}
}

func TestMiddleware_Integration_HandlerPanicLeavesRecordInProgress(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	userID := uuid.New()

	var executionCount int32
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&executionCount, 1)
		panic("database connection dropped mid-transaction")
	})

	store := idempotency.NewPgxStore(appPool, newDiscardLogger())
	pipeline := idempotency.RequireIdempotency(store, "POST /pos/orders", 1*time.Hour)(handler)

	key := "panic-key-" + uuid.New().String()[:8]
	payload := []byte(`{"table": 1}`)

	req1 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req1.Header.Set("Idempotency-Key", key)
	authCtx := auth.WithPrincipal(ctx, &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
	req1 = req1.WithContext(authCtx)
	rec1 := httptest.NewRecorder()

	// 1. Initial request panics
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic to propagate")
		}

		// 2. Verify record in PostgreSQL was NOT released; it remains in-progress
		rec, err := store.Get(ctx, tenantID, key, "POST /pos/orders")
		if err != nil || rec == nil {
			t.Fatalf("expected record to remain in PostgreSQL after panic, got: %v", err)
		}
		if rec.ResponseStatus != nil {
			t.Fatalf("expected response_status to remain nil (in-progress)")
		}

		// 3. Retry with same key must fail closed with 409 IDEMPOTENCY_IN_PROGRESS
		req2 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
		req2.Header.Set("Idempotency-Key", key)
		req2 = req2.WithContext(authCtx)
		rec2 := httptest.NewRecorder()

		pipeline.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusConflict {
			t.Fatalf("expected 409 on retry after panic, got %d", rec2.Code)
		}
		if atomic.LoadInt32(&executionCount) != 1 {
			t.Fatalf("expected handler NOT to re-execute after panic, got %d calls", executionCount)
		}
	}()

	pipeline.ServeHTTP(rec1, req1)
}

func TestIdempotency_ExecuteAtomic_Coordination(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	tm := postgres.NewTxManager(appPool, slog.Default())
	store := idempotency.NewPgxStore(appPool, slog.Default()).WithTxManager(tm)

	t.Run("atomic commit of business mutation and idempotency completion", func(t *testing.T) {
		key := "atomic-success-" + uuid.New().String()[:8]
		op := "POST /test/items"
		hash := "test-hash-success"

		res, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
		if err != nil || !res.Acquired {
			t.Fatalf("failed to acquire record: %v", err)
		}

		item := testfixture.TestItem{
			ID:       uuid.New(),
			TenantID: tenantID,
			Name:     "atomic-item-success",
		}

		// Execute business mutation and completion atomically
		tCtx := postgres.WithTenantID(ctx, tenantID)
		err = idempotency.ExecuteAtomic(tCtx, tm, store, res.Record, func(txCtx context.Context) (int, []byte, error) {
			tx, _ := postgres.TxFromContext(txCtx)
			if err := testfixture.InsertTestItem(txCtx, tx, item); err != nil {
				return 0, nil, err
			}
			return 201, []byte(`{"status":"created"}`), nil
		})
		if err != nil {
			t.Fatalf("ExecuteAtomic failed: %v", err)
		}

		// Verify both persisted
		rec, err := store.Get(ctx, tenantID, key, op)
		if err != nil || rec == nil || rec.ResponseStatus == nil || *rec.ResponseStatus != 201 {
			t.Fatalf("expected completed idempotency record with 201, got: %+v (err: %v)", rec, err)
		}

		_ = tm.WithinTx(tCtx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, _ := testfixture.CountTestItems(txCtx, tx)
			if count == 0 {
				t.Error("expected test item to be committed")
			}
			return nil
		})
	})

	t.Run("business error rolls back both mutation and completion, preserving in-progress fail-closed", func(t *testing.T) {
		key := "atomic-fail-" + uuid.New().String()[:8]
		op := "POST /test/items"
		hash := "test-hash-fail"

		res, err := store.AcquireOrGet(ctx, tenantID, key, op, hash, 1*time.Hour)
		if err != nil || !res.Acquired {
			t.Fatalf("failed to acquire record: %v", err)
		}

		item := testfixture.TestItem{
			ID:       uuid.New(),
			TenantID: tenantID,
			Name:     "atomic-item-fail",
		}

		tCtx := postgres.WithTenantID(ctx, tenantID)
		err = idempotency.ExecuteAtomic(tCtx, tm, store, res.Record, func(txCtx context.Context) (int, []byte, error) {
			tx, _ := postgres.TxFromContext(txCtx)
			_ = testfixture.InsertTestItem(txCtx, tx, item)
			return 0, nil, errors.New("business mutation failed")
		})
		if err == nil {
			t.Fatal("expected ExecuteAtomic to return business error")
		}

		// Verify idempotency record remains IN-PROGRESS (status IS NULL)
		rec, err := store.Get(ctx, tenantID, key, op)
		if err != nil || rec == nil {
			t.Fatalf("expected record to remain in DB, got: %v", err)
		}
		if rec.ResponseStatus != nil {
			t.Errorf("expected response_status to remain nil (in-progress), got %v", *rec.ResponseStatus)
		}
	})
}

func TestIdempotency_CleanupExpired(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	tm := postgres.NewTxManager(appPool, slog.Default())
	store := idempotency.NewPgxStore(appPool, slog.Default()).WithTxManager(tm)

	// 1. Insert expired completed record (expires_at in past)
	expiredKey := "expired-comp-" + uuid.New().String()[:8]
	res1, _ := store.AcquireOrGet(ctx, tenantID, expiredKey, "OP", "h1", 1*time.Hour)
	_ = store.Complete(ctx, res1.Record.ID, tenantID, 200, []byte(`{}`))
	// Backdate expires_at
	_, _ = adminPool.Exec(ctx, `UPDATE platform.idempotency_key SET expires_at = now() - interval '1 hour' WHERE id = $1`, res1.Record.ID)

	// 2. Insert active in-progress record (response_status IS NULL)
	activeKey := "active-in-prog-" + uuid.New().String()[:8]
	res2, _ := store.AcquireOrGet(ctx, tenantID, activeKey, "OP", "h2", 1*time.Hour)
	// Backdate expires_at to prove in-progress records are NEVER deleted even if old
	_, _ = adminPool.Exec(ctx, `UPDATE platform.idempotency_key SET expires_at = now() - interval '1 hour' WHERE id = $1`, res2.Record.ID)

	// 3. Insert valid unexpired completed record
	validKey := "valid-comp-" + uuid.New().String()[:8]
	res3, _ := store.AcquireOrGet(ctx, tenantID, validKey, "OP", "h3", 24*time.Hour)
	_ = store.Complete(ctx, res3.Record.ID, tenantID, 200, []byte(`{}`))

	// Run cleanup with tenant context
	pruned, err := store.CleanupExpired(postgres.WithTenantID(ctx, tenantID), 100)
	if err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}
	if pruned < 1 {
		t.Errorf("expected at least 1 record pruned, got %d", pruned)
	}

	// Verify expired completed record was deleted
	rec1, err := store.Get(ctx, tenantID, expiredKey, "OP")
	if err != idempotency.ErrRecordNotFound {
		t.Errorf("expected expired record deleted, got rec: %+v, err: %v", rec1, err)
	}

	// Verify active in-progress record was NOT deleted
	rec2, err := store.Get(ctx, tenantID, activeKey, "OP")
	if err != nil || rec2 == nil {
		t.Errorf("expected active in-progress record preserved, got: %v", err)
	}

	// Verify valid unexpired completed record was NOT deleted
	rec3, err := store.Get(ctx, tenantID, validKey, "OP")
	if err != nil || rec3 == nil {
		t.Errorf("expected valid unexpired record preserved, got: %v", err)
	}
}

func TestIdempotency_StuckReservationRecovery(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	tm := postgres.NewTxManager(appPool, slog.Default())
	store := idempotency.NewPgxStore(appPool, slog.Default()).WithTxManager(tm)

	key := "stuck-key-" + uuid.New().String()[:8]
	res, _ := store.AcquireOrGet(ctx, tenantID, key, "OP", "hash", 1*time.Hour)

	// Backdate created_at by 10 minutes to simulate process crash
	_, _ = adminPool.Exec(ctx, `UPDATE platform.idempotency_key SET created_at = now() - interval '10 minutes' WHERE id = $1`, res.Record.ID)

	// 1. FindStuckReservations identifies it
	stuckList, err := store.FindStuckReservations(ctx, tenantID, 5*time.Minute, 10)
	if err != nil {
		t.Fatalf("FindStuckReservations failed: %v", err)
	}
	var found bool
	for _, r := range stuckList {
		if r.ID == res.Record.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected stuck record %s in list, got %d records", res.Record.ID, len(stuckList))
	}

	// 2. MarkStuckAsFailed marks it with terminal error status 500
	failBody := []byte(`{"error":"operation timed out due to server crash"}`)
	err = store.MarkStuckAsFailed(ctx, tenantID, res.Record.ID, 5*time.Minute, 500, failBody)
	if err != nil {
		t.Fatalf("MarkStuckAsFailed failed: %v", err)
	}

	// 3. Subsequent MarkStuckAsFailed returns ErrReservationLost (fenced)
	err = store.MarkStuckAsFailed(ctx, tenantID, res.Record.ID, 5*time.Minute, 500, failBody)
	if !errors.Is(err, idempotency.ErrReservationLost) {
		t.Errorf("expected ErrReservationLost on second mark, got: %v", err)
	}
}
