package outbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/outbox"
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

func connectLivePool(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adminCfg := getAdminTestConfig()
	adminPool, err := postgres.NewPool(ctx, adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("Live PostgreSQL container not reachable: %v (skipping integration tests)", err)
		return nil, nil
	}

	if err := testfixture.SetupTestTable(ctx, adminPool); err != nil {
		adminPool.Close()
		t.Fatalf("failed to setup test tables: %v", err)
	}

	if err := testfixture.SetupOutboxTables(ctx, adminPool); err != nil {
		adminPool.Close()
		t.Fatalf("failed to setup outbox tables: %v", err)
	}

	appCfg := getAppTestConfig()
	appPool, err := postgres.NewPool(ctx, appCfg, newDiscardLogger())
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect as nexora_app: %v", err)
	}

	return adminPool, appPool
}

func TestOutbox_Integration_AtomicityAndRollback(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	aggregateID := uuid.New()

	// 1. Outbox write outside of transaction must fail closed with ErrNoActiveTransaction
	evtNoTx, err := outbox.NewEvent(tenantID, "order", aggregateID, "OrderCreated", 1, map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}
	err = writer.Write(ctx, evtNoTx)
	if !errors.Is(err, outbox.ErrNoActiveTransaction) {
		t.Fatalf("expected ErrNoActiveTransaction when called outside tx, got %v", err)
	}

	// 2. Transaction that rolls back must leave zero outbox events and zero business entities
	itemRollback := testfixture.TestItem{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "item-to-rollback",
	}
	evtRollback, _ := outbox.NewEvent(tenantID, "order", aggregateID, "OrderRollback", 1, map[string]any{"status": "cancelled"})

	txCtx := postgres.WithTenantID(ctx, tenantID)
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		if err := testfixture.InsertTestItem(c, tx, itemRollback); err != nil {
			return err
		}
		if err := writer.Write(c, evtRollback); err != nil {
			return err
		}
		// Deliberately trigger rollback
		return errors.New("simulated business error causing transaction rollback")
	})

	// Verify absence of both item and outbox event
	var countItems int
	_ = appPool.QueryRow(ctx, "SELECT count(*) FROM platform_test.test_items WHERE id = $1", itemRollback.ID).Scan(&countItems)
	if countItems != 0 {
		t.Fatalf("expected item to be absent after rollback, found %d", countItems)
	}

	var countEvents int
	_ = appPool.QueryRow(ctx, "SELECT count(*) FROM integration.outbox_event WHERE id = $1", evtRollback.ID).Scan(&countEvents)
	if countEvents != 0 {
		t.Fatalf("CRITICAL FLAW: outbox event visible after transaction rollback! count = %d", countEvents)
	}

	// 3. Transaction that commits successfully must persist both business entity and outbox event
	itemCommit := testfixture.TestItem{
		ID:       uuid.New(),
		TenantID: tenantID,
		Name:     "item-to-commit",
	}
	evtCommit, _ := outbox.NewEvent(tenantID, "order", aggregateID, "OrderCommitted", 1, map[string]any{"status": "confirmed"})

	err = tm.WithinTx(txCtx, func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		if err := testfixture.InsertTestItem(c, tx, itemCommit); err != nil {
			return err
		}
		return writer.Write(c, evtCommit)
	})
	if err != nil {
		t.Fatalf("expected WithinTx to succeed, got %v", err)
	}

	// Verify presence under tenant context
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		var c1, c2 int
		_ = tx.QueryRow(c, "SELECT count(*) FROM platform_test.test_items WHERE id = $1", itemCommit.ID).Scan(&c1)
		_ = tx.QueryRow(c, "SELECT count(*) FROM integration.outbox_event WHERE id = $1", evtCommit.ID).Scan(&c2)
		if c1 != 1 || c2 != 1 {
			t.Fatalf("expected 1 item and 1 event after commit, got items=%d, events=%d", c1, c2)
		}
		return nil
	})
}

func TestOutbox_Integration_MultiTenantRLSIsolation(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantA := uuid.New()
	tenantB := uuid.New()

	evtA, _ := outbox.NewEvent(tenantA, "order", uuid.New(), "OrderA", 1, map[string]any{"tenant": "A"})
	evtB, _ := outbox.NewEvent(tenantB, "order", uuid.New(), "OrderB", 1, map[string]any{"tenant": "B"})

	// 1. Insert Event A under Tenant A context
	err := tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(c context.Context) error {
		return writer.Write(c, evtA)
	})
	if err != nil {
		t.Fatalf("failed to insert Event A: %v", err)
	}

	// 2. Insert Event B under Tenant B context
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantB), func(c context.Context) error {
		return writer.Write(c, evtB)
	})
	if err != nil {
		t.Fatalf("failed to insert Event B: %v", err)
	}

	// 3. Tenant A must NOT be able to view Event B under RLS
	err = tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		var countA, countB int
		_ = tx.QueryRow(c, "SELECT count(*) FROM integration.outbox_event WHERE id = $1", evtA.ID).Scan(&countA)
		_ = tx.QueryRow(c, "SELECT count(*) FROM integration.outbox_event WHERE id = $1", evtB.ID).Scan(&countB)

		if countA != 1 {
			t.Fatalf("expected Tenant A to view Event A, got %d", countA)
		}
		if countB != 0 {
			t.Fatalf("CRITICAL RLS VIOLATION: Tenant A viewed Event B! count = %d", countB)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("query under Tenant A failed: %v", err)
	}
}

func TestOutbox_Integration_ConcurrentWorkerClaimsSkipLocked(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	// Create 10 pending events
	const eventCount = 10
	var events []*outbox.Event
	for i := 0; i < eventCount; i++ {
		evt, _ := outbox.NewEvent(tenantID, "item", uuid.New(), "ItemCreated", 1, map[string]any{"i": i})
		events = append(events, evt)
	}

	err := tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.WriteBatch(c, events)
	})
	if err != nil {
		t.Fatalf("failed to write batch: %v", err)
	}

	// Run 2 concurrent worker poller batches with dispatching
	dispatcher := outbox.NewDispatcher(newDiscardLogger())
	var processedEvents sync.Map
	var duplicateClaimCount int32

	dispatcher.Register("ItemCreated", func(_ context.Context, e *outbox.Event) error {
		if _, loaded := processedEvents.LoadOrStore(e.ID, true); loaded {
			atomic.AddInt32(&duplicateClaimCount, 1)
		}
		// Small delay to keep lock held across concurrent claims
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	cfg := outbox.DefaultPollerConfig()
	cfg.BatchSize = 5
	cfg.TenantID = &tenantID

	poller1 := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())
	poller2 := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = poller1.ProcessNextBatch(ctx)
	}()

	go func() {
		defer wg.Done()
		_, _ = poller2.ProcessNextBatch(ctx)
	}()

	wg.Wait()

	if dupes := atomic.LoadInt32(&duplicateClaimCount); dupes > 0 {
		t.Fatalf("CRITICAL CONCURRENCY VIOLATION: %d events claimed simultaneously by multiple workers!", dupes)
	}
}

func TestOutbox_Integration_DeliverySuccessAndDeadLetter(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	// 1. Test Successful Delivery
	evtSuccess, _ := outbox.NewEvent(tenantID, "order", uuid.New(), "OrderSuccess", 1, map[string]any{"ok": true})
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.Write(c, evtSuccess)
	})

	dispatcher := outbox.NewDispatcher(newDiscardLogger())
	dispatcher.Register("OrderSuccess", func(_ context.Context, _ *outbox.Event) error {
		return nil
	})

	cfg := outbox.DefaultPollerConfig()
	cfg.TenantID = &tenantID
	cfg.MaxRetries = 3

	poller := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())

	processed, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessNextBatch failed: %v", err)
	}
	if processed == 0 {
		t.Fatalf("expected at least 1 event processed")
	}

	// Verify event is marked PUBLISHED and delivery attempt recorded
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		var status string
		_ = tx.QueryRow(c, "SELECT status FROM integration.outbox_event WHERE id = $1", evtSuccess.ID).Scan(&status)
		if status != "PUBLISHED" {
			t.Fatalf("expected status PUBLISHED, got %s", status)
		}

		var attemptCount int
		_ = tx.QueryRow(c, "SELECT count(*) FROM integration.delivery_attempt WHERE event_id = $1 AND status = 'SUCCESS'", evtSuccess.ID).Scan(&attemptCount)
		if attemptCount != 1 {
			t.Fatalf("expected 1 successful delivery attempt, got %d", attemptCount)
		}
		return nil
	})

	// 2. Test Dead-Letter Queue Transition after MaxRetries
	evtFailing, _ := outbox.NewEvent(tenantID, "order", uuid.New(), "OrderFailing", 1, map[string]any{"fail": true})
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.Write(c, evtFailing)
	})

	dispatcher.Register("OrderFailing", func(_ context.Context, _ *outbox.Event) error {
		return errors.New("simulated external webhook timeout")
	})

	// Process until MaxRetries (3 times), backdating attempted_at and next_attempt_at between iterations to simulate exponential backoff elapse
	for i := 0; i < cfg.MaxRetries; i++ {
		_, _ = adminPool.Exec(ctx, "UPDATE integration.delivery_attempt SET attempted_at = now() - interval '1 hour' WHERE event_id = $1", evtFailing.ID)
		_, _ = adminPool.Exec(ctx, "UPDATE integration.outbox_event SET next_attempt_at = now() - interval '1 hour' WHERE id = $1", evtFailing.ID)
		_, _ = poller.ProcessNextBatch(ctx)
	}

	// Verify event transitioned to DEAD_LETTER
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		var status string
		var retryCount int
		_ = tx.QueryRow(c, "SELECT status, retry_count FROM integration.outbox_event WHERE id = $1", evtFailing.ID).Scan(&status, &retryCount)
		if status != "DEAD_LETTER" {
			t.Fatalf("expected event to be in DEAD_LETTER, got status=%s, retry_count=%d", status, retryCount)
		}
		if retryCount != cfg.MaxRetries {
			t.Fatalf("expected retry_count = %d, got %d", cfg.MaxRetries, retryCount)
		}
		return nil
	})
}

func TestOutbox_Integration_NonRetryableErrorImmediateDeadLetter(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	evtPermanent, err := outbox.NewEvent(tenantID, "order", uuid.New(), "OrderUnrecoverable", 1, map[string]any{"bad": true})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	err = tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.Write(c, evtPermanent)
	})
	if err != nil {
		t.Fatalf("failed to write event: %v", err)
	}

	dispatcher := outbox.NewDispatcher(newDiscardLogger())
	dispatcher.Register("OrderUnrecoverable", func(_ context.Context, _ *outbox.Event) error {
		return outbox.NewNonRetryableError(errors.New("unrecoverable schema error"))
	})

	cfg := outbox.DefaultPollerConfig()
	cfg.TenantID = &tenantID
	cfg.MaxRetries = 5

	poller := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())

	processed, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessNextBatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed event, got %d", processed)
	}

	// Verify immediate DEAD_LETTER transition on first attempt without 5 retries
	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		tx, _ := postgres.TxFromContext(c)
		var status string
		var retryCount int
		_ = tx.QueryRow(c, "SELECT status, retry_count FROM integration.outbox_event WHERE id = $1", evtPermanent.ID).Scan(&status, &retryCount)
		if status != "DEAD_LETTER" {
			t.Fatalf("expected immediate DEAD_LETTER status, got %s", status)
		}
		if retryCount != 1 {
			t.Fatalf("expected retry_count = 1 for immediate permanent failure, got %d", retryCount)
		}

		var attemptStatus, respCode string
		_ = tx.QueryRow(c, "SELECT status, response_code FROM integration.delivery_attempt WHERE event_id = $1", evtPermanent.ID).Scan(&attemptStatus, &respCode)
		if attemptStatus != "FAILED" || respCode != "PERMANENT_ERROR" {
			t.Fatalf("expected attempt status FAILED and PERMANENT_ERROR, got status=%s code=%s", attemptStatus, respCode)
		}
		return nil
	})
}

func TestOutbox_Integration_ConfigurableBaseBackoff(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	evt, err := outbox.NewEvent(tenantID, "order", uuid.New(), "OrderBackoffTest", 1, map[string]any{"try": 1})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	_ = tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.Write(c, evt)
	})

	var attemptCount int
	dispatcher := outbox.NewDispatcher(newDiscardLogger())
	dispatcher.Register("OrderBackoffTest", func(_ context.Context, _ *outbox.Event) error {
		attemptCount++
		if attemptCount == 1 {
			return errors.New("transient network blip")
		}
		return nil
	})

	// Configure poller with 10 seconds BaseBackoff
	cfg := outbox.DefaultPollerConfig()
	cfg.TenantID = &tenantID
	cfg.BaseBackoff = 10 * time.Second

	poller := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())

	// Batch 1: first attempt fails
	p1, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("first batch failed: %v", err)
	}
	if p1 != 1 {
		t.Fatalf("expected 1 event in first batch, got %d", p1)
	}

	// Batch 2 immediately afterwards: event must NOT be eligible due to 10s BaseBackoff
	p2, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("second batch failed: %v", err)
	}
	if p2 != 0 {
		t.Fatalf("expected 0 events claimed because 10s backoff has not elapsed, got %d", p2)
	}

	// Backdate delivery_attempt by 12 seconds: with retry_count=1 and BaseBackoff=10s, required delay is 20s.
	// 12s < 20s, so event must STILL not be claimed!
	_, err = adminPool.Exec(ctx, "UPDATE integration.delivery_attempt SET attempted_at = now() - interval '12 seconds' WHERE event_id = $1", evt.ID)
	if err != nil {
		t.Fatalf("failed to backdate delivery attempt: %v", err)
	}
	_, err = adminPool.Exec(ctx, "UPDATE integration.outbox_event SET next_attempt_at = now() + interval '8 seconds' WHERE id = $1", evt.ID)
	if err != nil {
		t.Fatalf("failed to update next_attempt_at: %v", err)
	}

	p2b, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("intermediate batch failed: %v", err)
	}
	if p2b != 0 {
		t.Fatalf("expected 0 events claimed because 12s < 20s backoff, got %d", p2b)
	}

	// Backdate delivery_attempt by 25 seconds: 25s > 20s, so backoff has now elapsed!
	_, err = adminPool.Exec(ctx, "UPDATE integration.delivery_attempt SET attempted_at = now() - interval '25 seconds' WHERE event_id = $1", evt.ID)
	if err != nil {
		t.Fatalf("failed to backdate delivery attempt: %v", err)
	}
	_, err = adminPool.Exec(ctx, "UPDATE integration.outbox_event SET next_attempt_at = now() - interval '5 seconds' WHERE id = $1", evt.ID)
	if err != nil {
		t.Fatalf("failed to update next_attempt_at: %v", err)
	}

	// Batch 3: event is now eligible and succeeds
	p3, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("third batch failed: %v", err)
	}
	if p3 != 1 {
		t.Fatalf("expected 1 event claimed after backoff elapsed, got %d", p3)
	}

	// Verify published
	var status string
	_ = adminPool.QueryRow(ctx, "SELECT status FROM integration.outbox_event WHERE id = $1", evt.ID).Scan(&status)
	if status != "PUBLISHED" {
		t.Fatalf("expected status PUBLISHED, got %s", status)
	}
}

func TestOutbox_Integration_BatchWriteOversizedPayloadRejection(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	evt1, _ := outbox.NewEvent(tenantID, "item", uuid.New(), "Item1", 1, map[string]any{"ok": true})
	evt2, _ := outbox.NewEvent(tenantID, "item", uuid.New(), "Item2", 1, map[string]any{"ok": true})

	// Make evt3 oversized (> 1 MiB)
	oversizedData := []byte(`{"data":"` + strings.Repeat("x", outbox.MaxPayloadSize+10) + `"}`)
	evt3 := &outbox.Event{
		ID:            uuid.New(),
		TenantID:      tenantID,
		AggregateType: "item",
		AggregateID:   uuid.New(),
		EventType:     "Item3",
		EventVersion:  1,
		Payload:       oversizedData,
		Status:        outbox.StatusPending,
		OccurredAt:    time.Now().UTC(),
	}

	// Batch write containing the oversized event must fail closed
	err := tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.WriteBatch(c, []*outbox.Event{evt1, evt2, evt3})
	})
	if !errors.Is(err, outbox.ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}

	// Verify atomicity: none of the batch was inserted
	var count int
	_ = appPool.QueryRow(ctx, "SELECT count(*) FROM integration.outbox_event WHERE id IN ($1, $2, $3)", evt1.ID, evt2.ID, evt3.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 events in DB after rejected batch, found %d", count)
	}
}

func TestOutbox_Integration_CancellationSafetyAndRecovery(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	evt1, _ := outbox.NewEvent(tenantID, "task", uuid.New(), "TaskStep1", 1, map[string]any{"step": 1})
	evt2, _ := outbox.NewEvent(tenantID, "task", uuid.New(), "TaskStep2", 1, map[string]any{"step": 2})

	err := tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.WriteBatch(c, []*outbox.Event{evt1, evt2})
	})
	if err != nil {
		t.Fatalf("failed to insert events: %v", err)
	}

	// 1. Cancellation before dispatch: passing a pre-canceled context aborts safely
	canceledCtx, cancelNow := context.WithCancel(ctx)
	cancelNow()

	dispatcher := outbox.NewDispatcher(newDiscardLogger())
	var dispatchedCount int
	dispatcher.Register("TaskStep1", func(_ context.Context, _ *outbox.Event) error {
		dispatchedCount++
		return nil
	})
	dispatcher.Register("TaskStep2", func(_ context.Context, _ *outbox.Event) error {
		dispatchedCount++
		return nil
	})

	cfg := outbox.DefaultPollerConfig()
	cfg.TenantID = &tenantID
	poller := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())

	_, err = poller.ProcessNextBatch(canceledCtx)
	if err == nil {
		t.Fatalf("expected error when processing with canceled context")
	}
	if dispatchedCount != 0 {
		t.Fatalf("expected 0 events dispatched with canceled context, got %d", dispatchedCount)
	}

	// 2. Cancellation during batch: if context is canceled after first dispatch,
	// second dispatch is prevented by context check and transaction rolls back
	cancelDuringCtx, cancelDuring := context.WithCancel(ctx)
	var step1Dispatched, step2Dispatched bool

	dispatcher2 := outbox.NewDispatcher(newDiscardLogger())
	dispatcher2.Register("TaskStep1", func(_ context.Context, _ *outbox.Event) error {
		step1Dispatched = true
		cancelDuring() // Cancel the batch context immediately during step 1
		return nil
	})
	dispatcher2.Register("TaskStep2", func(_ context.Context, _ *outbox.Event) error {
		step2Dispatched = true
		return nil
	})

	poller2 := outbox.NewPoller(appPool, dispatcher2, cfg, newDiscardLogger())
	_, err = poller2.ProcessNextBatch(cancelDuringCtx)
	if err == nil {
		t.Fatalf("expected error from canceled batch")
	}
	if !step1Dispatched {
		t.Fatalf("expected step 1 to have been dispatched")
	}
	if step2Dispatched {
		t.Fatalf("expected step 2 to NOT be dispatched after cancellation")
	}

	// 3. Restart after rollback: both events remain PENDING and are retried on clean batch
	var status1, status2 string
	_ = adminPool.QueryRow(ctx, "SELECT status FROM integration.outbox_event WHERE id = $1", evt1.ID).Scan(&status1)
	_ = adminPool.QueryRow(ctx, "SELECT status FROM integration.outbox_event WHERE id = $1", evt2.ID).Scan(&status2)
	if status1 != "PENDING" || status2 != "PENDING" {
		t.Fatalf("expected both events to remain PENDING after rollback, got status1=%s, status2=%s", status1, status2)
	}

	// Clean retry succeeds
	dispatcherClean := outbox.NewDispatcher(newDiscardLogger())
	dispatcherClean.Register("TaskStep1", func(_ context.Context, _ *outbox.Event) error { return nil })
	dispatcherClean.Register("TaskStep2", func(_ context.Context, _ *outbox.Event) error { return nil })
	pollerClean := outbox.NewPoller(appPool, dispatcherClean, cfg, newDiscardLogger())

	processed, err := pollerClean.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("clean retry failed: %v", err)
	}
	if processed != 2 {
		t.Fatalf("expected 2 events processed on clean restart, got %d", processed)
	}
}

// DeduplicatingConsumer verifies at-least-once transport delivery guarantees
// by idempotently deduplicating events by their UUID.
type DeduplicatingConsumer struct {
	mu           sync.Mutex
	seenCounts   map[uuid.UUID]int
	uniqueEvents map[uuid.UUID]*outbox.Event
}

func NewDeduplicatingConsumer() *DeduplicatingConsumer {
	return &DeduplicatingConsumer{
		seenCounts:   make(map[uuid.UUID]int),
		uniqueEvents: make(map[uuid.UUID]*outbox.Event),
	}
}

func (c *DeduplicatingConsumer) Handle(_ context.Context, e *outbox.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seenCounts[e.ID]++
	if _, exists := c.uniqueEvents[e.ID]; !exists {
		c.uniqueEvents[e.ID] = e
	}
	return nil
}

func (c *DeduplicatingConsumer) TotalReceipts(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seenCounts[id]
}

func (c *DeduplicatingConsumer) UniqueCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.uniqueEvents)
}

func TestOutbox_Integration_AtLeastOnceDeduplicationWithConsumer(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, newDiscardLogger())
	writer := outbox.NewPgxWriter(newDiscardLogger())

	tenantID := uuid.New()
	txCtx := postgres.WithTenantID(ctx, tenantID)

	evt, err := outbox.NewEvent(tenantID, "order", uuid.New(), "OrderDeduplicationTest", 1, map[string]any{"order_id": 999})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	err = tm.WithinTx(txCtx, func(c context.Context) error {
		return writer.Write(c, evt)
	})
	if err != nil {
		t.Fatalf("failed to write event: %v", err)
	}

	consumer := NewDeduplicatingConsumer()
	dispatcher := outbox.NewDispatcher(newDiscardLogger())
	dispatcher.Register("OrderDeduplicationTest", consumer.Handle)

	cfg := outbox.DefaultPollerConfig()
	cfg.TenantID = &tenantID
	poller := outbox.NewPoller(appPool, dispatcher, cfg, newDiscardLogger())

	// First delivery attempt
	processed, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("first poll failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed event, got %d", processed)
	}

	if consumer.TotalReceipts(evt.ID) != 1 {
		t.Fatalf("expected consumer to have received event once, got %d", consumer.TotalReceipts(evt.ID))
	}
	if consumer.UniqueCount() != 1 {
		t.Fatalf("expected 1 unique event, got %d", consumer.UniqueCount())
	}

	// Simulate at-least-once redelivery: e.g. event is reset to PENDING (simulating worker crash before ack or message redelivery)
	_, err = adminPool.Exec(ctx, "UPDATE integration.outbox_event SET status = 'PENDING', published_at = NULL WHERE id = $1", evt.ID)
	if err != nil {
		t.Fatalf("failed to reset event: %v", err)
	}

	// Second delivery attempt
	processed2, err := poller.ProcessNextBatch(ctx)
	if err != nil {
		t.Fatalf("second poll failed: %v", err)
	}
	if processed2 != 1 {
		t.Fatalf("expected 1 processed event on redelivery, got %d", processed2)
	}

	// Verify at-least-once delivery behavior:
	// The transport delivered the event 2 times, but DeduplicatingConsumer tracked exactly 1 unique event
	if consumer.TotalReceipts(evt.ID) != 2 {
		t.Errorf("expected 2 total deliveries for at-least-once transport, got %d", consumer.TotalReceipts(evt.ID))
	}
	if consumer.UniqueCount() != 1 {
		t.Errorf("expected exactly 1 unique business processed event, got %d", consumer.UniqueCount())
	}
}
