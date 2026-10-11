package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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

func connectTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminCfg := getAdminTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	adminPool, err := postgres.NewPool(ctx, adminCfg, newDiscardLogger())
	if err != nil {
		t.Skipf("PostgreSQL test instance unavailable at %s:%d (%v); skipping live DB integration test", adminCfg.DBHost, adminCfg.DBPort, err)
		return nil
	}
	defer adminPool.Close()

	// Run fixture setup under admin pool
	adminTM := postgres.NewTxManager(adminPool, newDiscardLogger())
	if setupErr := adminTM.WithinUnscopedTx(ctx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		return testfixture.SetupTestTable(txCtx, tx)
	}); setupErr != nil {
		t.Fatalf("failed to setup test fixture table: %v", setupErr)
	}

	// Now connect as non-superuser application role nexora_app so RLS policies are strictly enforced
	appCfg := getAppTestConfig()
	appPool, err := postgres.NewPool(ctx, appCfg, newDiscardLogger())
	if err != nil {
		t.Fatalf("failed to connect as application role: %v", err)
	}
	return appPool
}

func TestNewPool_NilConfig(t *testing.T) {
	_, err := postgres.NewPool(context.Background(), nil, newDiscardLogger())
	if err == nil {
		t.Fatal("expected error on nil config, got nil")
	}
}

func TestContext_PrincipalContext(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	companyID := uuid.New()
	storeID := uuid.New()
	warehouseID := uuid.New()
	terminalID := uuid.New()

	ctx := context.Background()
	ctx = postgres.WithPrincipalContext(ctx, postgres.PrincipalContext{
		TenantID:    tenantID,
		UserID:      userID,
		CompanyID:   companyID,
		StoreID:     storeID,
		WarehouseID: warehouseID,
		TerminalID:  terminalID,
	})

	pc, ok := postgres.FromContext(ctx)
	if !ok {
		t.Fatal("expected PrincipalContext to be present in context")
	}
	if pc.TenantID != tenantID || pc.UserID != userID || pc.CompanyID != companyID ||
		pc.StoreID != storeID || pc.WarehouseID != warehouseID || pc.TerminalID != terminalID {
		t.Errorf("extracted PrincipalContext does not match expected values: %+v", pc)
	}

	// Test WithTenantID override
	newTenantID := uuid.New()
	ctx2 := postgres.WithTenantID(ctx, newTenantID)
	pc2, ok := postgres.FromContext(ctx2)
	if !ok || pc2.TenantID != newTenantID {
		t.Errorf("expected tenant ID to update to %s, got %s", newTenantID, pc2.TenantID)
	}
	if pc2.UserID != userID {
		t.Errorf("expected user ID to remain intact after WithTenantID: %s", pc2.UserID)
	}
}

func TestMapError(t *testing.T) {
	tests := []struct {
		name        string
		pgCode      string
		expectedErr error
	}{
		{"unique violation", "23505", postgres.ErrUniqueViolation},
		{"foreign key violation", "23503", postgres.ErrForeignKeyViolation},
		{"check violation", "23514", postgres.ErrCheckViolation},
		{"not null violation", "23502", postgres.ErrNotNullViolation},
		{"rls policy violation", "42501", postgres.ErrRLSPolicyViolation},
		{"serialization failure", "40001", postgres.ErrSerializationFailure},
		{"deadlock detected", "40P01", postgres.ErrDeadlockDetected},
		{"statement timeout", "57014", postgres.ErrStatementTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := &pgconn.PgError{Code: tc.pgCode, Message: "test db error"}
			mapped := postgres.MapError(input)
			if !errors.Is(mapped, tc.expectedErr) {
				t.Errorf("expected error wrapping %v, got %v", tc.expectedErr, mapped)
			}
		})
	}

	// Normal non-pg error should be unmodified
	stdErr := errors.New("generic error")
	if postgres.MapError(stdErr) != stdErr {
		t.Errorf("expected generic error to remain unmodified")
	}
	if postgres.MapError(nil) != nil {
		t.Errorf("expected nil for nil input")
	}
}

func TestTxManager_MissingTenantContextFailClosed(t *testing.T) {
	// A dummy pool is not even called because validation fails closed before SQL execution
	tm := postgres.NewTxManager(nil, newDiscardLogger())

	// 1. Completely empty context
	err := tm.WithinTx(context.Background(), func(_ context.Context) error {
		t.Fatal("callback should not execute when tenant context is missing")
		return nil
	})
	if !errors.Is(err, postgres.ErrMissingTenantContext) {
		t.Errorf("expected ErrMissingTenantContext on empty context, got: %v", err)
	}

	// 2. Context with zero UUID (uuid.Nil)
	ctx := postgres.WithTenantID(context.Background(), uuid.Nil)
	err = tm.WithinTx(ctx, func(_ context.Context) error {
		t.Fatal("callback should not execute when tenant ID is uuid.Nil")
		return nil
	})
	if !errors.Is(err, postgres.ErrMissingTenantContext) {
		t.Errorf("expected ErrMissingTenantContext on uuid.Nil, got: %v", err)
	}
}

func TestTxManager_NestedTransactionBehavior(t *testing.T) {
	pool := connectTestPool(t)
	defer pool.Close()

	tm := postgres.NewTxManager(pool, newDiscardLogger())
	tenant1 := uuid.New()
	tenant2 := uuid.New()

	ctx1 := postgres.WithTenantID(context.Background(), tenant1)

	// 1. Nested with same tenant should succeed and reuse transaction
	err := tm.WithinTx(ctx1, func(txCtx1 context.Context) error {
		tx1, ok1 := postgres.TxFromContext(txCtx1)
		if !ok1 {
			t.Fatal("expected tx1 in context")
		}

		return tm.WithinTx(txCtx1, func(txCtx2 context.Context) error {
			tx2, ok2 := postgres.TxFromContext(txCtx2)
			if !ok2 {
				t.Fatal("expected tx2 in context")
			}
			if tx1 != tx2 {
				t.Errorf("expected same transaction in nested call")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("expected nested transaction with same tenant to succeed, got: %v", err)
	}

	// 2. Nested with mismatched tenant should fail with ErrTenantContextMismatch
	err = tm.WithinTx(ctx1, func(txCtx1 context.Context) error {
		// Attempt to switch tenant inside transaction
		ctx2 := postgres.WithTenantID(txCtx1, tenant2)
		return tm.WithinTx(ctx2, func(_ context.Context) error {
			t.Fatal("nested callback should not execute with mismatched tenant")
			return nil
		})
	})
	if !errors.Is(err, postgres.ErrTenantContextMismatch) {
		t.Errorf("expected ErrTenantContextMismatch on mismatched tenant, got: %v", err)
	}
}

func TestIntegration_RLSAndIsolation(t *testing.T) {
	pool := connectTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	logger := newDiscardLogger()
	tm := postgres.NewTxManager(pool, logger)

	cleanFixtureTable := func() {
		_ = tm.WithinUnscopedTx(ctx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			_, err := tx.Exec(txCtx, `TRUNCATE TABLE platform_test.test_items`)
			return err
		})
	}

	t.Run("sequential connection reuse without context leakage", func(t *testing.T) {
		cleanFixtureTable()
		tenantA := uuid.New()
		tenantB := uuid.New()

		// Tenant A creates row
		err := tm.WithinTx(postgres.WithTenantID(ctx, tenantA), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			return testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantA,
				Name:     "item-for-tenant-a",
			})
		})
		if err != nil {
			t.Fatalf("failed to insert item for Tenant A: %v", err)
		}

		// Re-acquire connection from pool under Tenant B context
		err = tm.WithinTx(postgres.WithTenantID(ctx, tenantB), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, countErr := testfixture.CountTestItems(txCtx, tx)
			if countErr != nil {
				return countErr
			}
			if count != 0 {
				t.Errorf("Tenant B observed %d rows of Tenant A data! Cross-tenant isolation violated", count)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("error checking Tenant B view: %v", err)
		}
	})

	t.Run("rollback state cleanup and residue disposal", func(t *testing.T) {
		cleanFixtureTable()
		tenantC := uuid.New()
		expectedErr := errors.New("simulated business error requiring rollback")

		// Transaction fails
		err := tm.WithinTx(postgres.WithTenantID(ctx, tenantC), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			if insertErr := testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantC,
				Name:     "aborted-item",
			}); insertErr != nil {
				return insertErr
			}
			return expectedErr
		})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}

		// Verify row does not exist
		err = tm.WithinTx(postgres.WithTenantID(ctx, tenantC), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, countErr := testfixture.CountTestItems(txCtx, tx)
			if countErr != nil {
				return countErr
			}
			if count != 0 {
				t.Errorf("rolled back item was persisted! count=%d", count)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("verification error: %v", err)
		}
	})

	t.Run("rollback on panic", func(t *testing.T) {
		cleanFixtureTable()
		tenantD := uuid.New()

		err := tm.WithinTx(postgres.WithTenantID(ctx, tenantD), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			if insertErr := testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantD,
				Name:     "panicking-item",
			}); insertErr != nil {
				return insertErr
			}
			panic("critical unexpected panic inside transaction")
		})
		if err == nil {
			t.Fatal("expected error after panic recovery, got nil")
		}

		// Verify row does not exist
		err = tm.WithinTx(postgres.WithTenantID(ctx, tenantD), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, countErr := testfixture.CountTestItems(txCtx, tx)
			if countErr != nil {
				return countErr
			}
			if count != 0 {
				t.Errorf("row persisted despite panic! count=%d", count)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("verification error: %v", err)
		}
	})

	t.Run("cross-tenant write policy rejection", func(t *testing.T) {
		cleanFixtureTable()
		tenantE := uuid.New()
		tenantF := uuid.New()

		// Tenant E context attempts to insert a row tagged with Tenant F's ID
		err := tm.WithinTx(postgres.WithTenantID(ctx, tenantE), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			return testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantF, // Spoofed tenant ID!
				Name:     "spoofed-item",
			})
		})

		if err == nil {
			t.Fatal("expected RLS policy violation on cross-tenant insert, got nil")
		}
		if !errors.Is(err, postgres.ErrRLSPolicyViolation) {
			t.Errorf("expected ErrRLSPolicyViolation, got: %v", err)
		}
	})

	t.Run("concurrent multi-tenant isolation under pool contention", func(t *testing.T) {
		cleanFixtureTable()
		tenant1 := uuid.New()
		tenant2 := uuid.New()
		const iterations = 20

		var wg sync.WaitGroup
		errs := make(chan error, iterations*2)

		for i := 0; i < iterations; i++ {
			wg.Add(2)

			// Worker 1: Tenant 1
			go func() {
				defer wg.Done()
				tCtx := postgres.WithTenantID(ctx, tenant1)
				e := tm.WithinTx(tCtx, func(txCtx context.Context) error {
					tx, _ := postgres.TxFromContext(txCtx)
					return testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
						ID:       uuid.New(),
						TenantID: tenant1,
						Name:     "tenant-1-concurrent",
					})
				})
				if e != nil {
					errs <- e
				}
			}()

			// Worker 2: Tenant 2
			go func() {
				defer wg.Done()
				tCtx := postgres.WithTenantID(ctx, tenant2)
				e := tm.WithinTx(tCtx, func(txCtx context.Context) error {
					tx, _ := postgres.TxFromContext(txCtx)
					return testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
						ID:       uuid.New(),
						TenantID: tenant2,
						Name:     "tenant-2-concurrent",
					})
				})
				if e != nil {
					errs <- e
				}
			}()
		}

		wg.Wait()
		close(errs)

		for e := range errs {
			t.Fatalf("concurrent insert error: %v", e)
		}

		// Verify exact count for Tenant 1
		err := tm.WithinTx(postgres.WithTenantID(ctx, tenant1), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, countErr := testfixture.CountTestItems(txCtx, tx)
			if countErr != nil {
				return countErr
			}
			if count != iterations {
				t.Errorf("expected Tenant 1 count=%d, got %d", iterations, count)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("verification error for Tenant 1: %v", err)
		}

		// Verify exact count for Tenant 2
		err = tm.WithinTx(postgres.WithTenantID(ctx, tenant2), func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, countErr := testfixture.CountTestItems(txCtx, tx)
			if countErr != nil {
				return countErr
			}
			if count != iterations {
				t.Errorf("expected Tenant 2 count=%d, got %d", iterations, count)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("verification error for Tenant 2: %v", err)
		}
	})

	t.Run("savepoint operations: commit, partial rollback, and panic recovery", func(t *testing.T) {
		cleanFixtureTable()
		tenantG := uuid.New()
		tCtx := postgres.WithTenantID(ctx, tenantG)

		// 1. Calling WithinSavepoint without an active transaction returns ErrNoActiveTransaction
		err := tm.WithinSavepoint(tCtx, "sp_no_tx", func(_ context.Context) error {
			return nil
		})
		if !errors.Is(err, postgres.ErrNoActiveTransaction) {
			t.Errorf("expected ErrNoActiveTransaction, got %v", err)
		}

		// 2. Partial rollback: inner savepoint fails, outer transaction commits outer items
		err = tm.WithinTx(tCtx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)

			// Insert outer item 1
			if err := testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantG,
				Name:     "outer-item-1",
			}); err != nil {
				return err
			}

			// Inner savepoint that fails
			spErr := tm.WithinSavepoint(txCtx, "sp_fail", func(spCtx context.Context) error {
				spTx, _ := postgres.TxFromContext(spCtx)
				if err := testfixture.InsertTestItem(spCtx, spTx, testfixture.TestItem{
					ID:       uuid.New(),
					TenantID: tenantG,
					Name:     "inner-failed-item",
				}); err != nil {
					return err
				}
				return errors.New("simulated business error in savepoint")
			})
			if spErr == nil {
				t.Fatal("expected savepoint error")
			}

			// Insert outer item 2 (outer transaction recovers from savepoint error)
			return testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantG,
				Name:     "outer-item-2",
			});
		})
		if err != nil {
			t.Fatalf("outer transaction commit failed: %v", err)
		}

		// Verify that outer items were committed, but the savepoint item was rolled back
		_ = tm.WithinTx(tCtx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, _ := testfixture.CountTestItems(txCtx, tx)
			if count != 2 {
				t.Errorf("expected 2 items committed after savepoint partial rollback, got %d", count)
			}
			return nil
		})

		// 3. Savepoint panic recovery
		cleanFixtureTable()
		err = tm.WithinTx(tCtx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)

			// Insert outer item
			if err := testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantG,
				Name:     "outer-item-pre-panic",
			}); err != nil {
				return err
			}

			// Inner savepoint panics
			spErr := tm.WithinSavepoint(txCtx, "sp_panic", func(spCtx context.Context) error {
				spTx, _ := postgres.TxFromContext(spCtx)
				_ = testfixture.InsertTestItem(spCtx, spTx, testfixture.TestItem{
					ID:       uuid.New(),
					TenantID: tenantG,
					Name:     "inner-panicking-item",
				})
				panic("simulated savepoint panic")
			})
			if spErr == nil {
				t.Fatal("expected error from recovered savepoint panic")
			}

			// Outer transaction continues and commits
			return testfixture.InsertTestItem(txCtx, tx, testfixture.TestItem{
				ID:       uuid.New(),
				TenantID: tenantG,
				Name:     "outer-item-post-panic",
			})
		})
		if err != nil {
			t.Fatalf("outer transaction commit failed after savepoint panic recovery: %v", err)
		}

		_ = tm.WithinTx(tCtx, func(txCtx context.Context) error {
			tx, _ := postgres.TxFromContext(txCtx)
			count, _ := testfixture.CountTestItems(txCtx, tx)
			if count != 2 {
				t.Errorf("expected 2 items committed after savepoint panic, got %d", count)
			}
			return nil
		})
	})
}
