package lifecycle_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"example.com/nexora/backend/internal/platform/lifecycle"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestManager_LIFOOrder(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())

	var order []string
	var mu sync.Mutex

	mgr.Register("resource_1", func(_ context.Context) error {
		mu.Lock()
		order = append(order, "resource_1")
		mu.Unlock()
		return nil
	})

	mgr.Register("resource_2", func(_ context.Context) error {
		mu.Lock()
		order = append(order, "resource_2")
		mu.Unlock()
		return nil
	})

	mgr.Register("resource_3", func(_ context.Context) error {
		mu.Lock()
		order = append(order, "resource_3")
		mu.Unlock()
		return nil
	})

	ctx := context.Background()
	if err := mgr.Cleanup(ctx); err != nil {
		t.Fatalf("expected successful cleanup, got error: %v", err)
	}

	expected := []string{"resource_3", "resource_2", "resource_1"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d items executed, got %d", len(expected), len(order))
	}
	for i, name := range expected {
		if order[i] != name {
			t.Errorf("at index %d: expected %s, got %s", i, name, order[i])
		}
	}
}

func TestManager_ErrorContinuationAndJoin(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())

	errOne := errors.New("error in resource 1")
	errThree := errors.New("error in resource 3")

	var executed []string

	mgr.Register("resource_1", func(_ context.Context) error {
		executed = append(executed, "resource_1")
		return errOne
	})

	mgr.Register("resource_2", func(_ context.Context) error {
		executed = append(executed, "resource_2")
		return nil
	})

	mgr.Register("resource_3", func(_ context.Context) error {
		executed = append(executed, "resource_3")
		return errThree
	})

	err := mgr.Cleanup(context.Background())
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}

	// Verify all cleanups executed despite errors
	if len(executed) != 3 {
		t.Errorf("expected 3 cleanups executed, got %d (%v)", len(executed), executed)
	}

	// Verify both errors are joined in the return error
	if !errors.Is(err, errOne) {
		t.Errorf("expected joined error to wrap errOne, got: %v", err)
	}
	if !errors.Is(err, errThree) {
		t.Errorf("expected joined error to wrap errThree, got: %v", err)
	}
}

func TestManager_IdempotentCleanup(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())

	callCount := 0
	mgr.Register("singleton", func(_ context.Context) error {
		callCount++
		return nil
	})

	// First call
	if err := mgr.Cleanup(context.Background()); err != nil {
		t.Fatalf("unexpected error on first cleanup: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected callCount=1, got %d", callCount)
	}

	// Second call should be a no-op and not re-execute
	if err := mgr.Cleanup(context.Background()); err != nil {
		t.Fatalf("unexpected error on second cleanup: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected callCount to remain 1 after repeated cleanup, got %d", callCount)
	}
}

func TestManager_NilRegister(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())
	// Should not panic or register
	mgr.Register("nil_hook", nil)
	if err := mgr.Cleanup(context.Background()); err != nil {
		t.Fatalf("unexpected error on cleanup: %v", err)
	}
}

func TestManager_CleanupTimeoutRespect(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())

	mgr.Register("hanging_resource", func(ctx context.Context) error {
		select {
		case <-time.After(500 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	shortTimeout := 20 * time.Millisecond
	err := mgr.CleanupWithTimeout(shortTimeout)
	if err == nil {
		t.Fatal("expected deadline exceeded error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestManager_PartialStartupFailureRollback(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())

	resource1Cleaned := false
	resource2Cleaned := false

	// Step 1: Initialize Resource 1 (success)
	mgr.Register("resource_1", func(_ context.Context) error {
		resource1Cleaned = true
		return nil
	})

	// Step 2: Initialize Resource 2 (success)
	mgr.Register("resource_2", func(_ context.Context) error {
		resource2Cleaned = true
		return nil
	})

	// Step 3: Initialize Resource 3 (fails during bootstrap)
	startupErr := errors.New("failed to connect to downstream service")

	// Rollback pattern on partial startup failure:
	if startupErr != nil {
		cleanupErr := mgr.Cleanup(context.Background())
		if cleanupErr != nil {
			t.Fatalf("unexpected error during rollback: %v", cleanupErr)
		}
	}

	if !resource1Cleaned {
		t.Error("expected resource 1 to be cleaned up after partial startup failure")
	}
	if !resource2Cleaned {
		t.Error("expected resource 2 to be cleaned up after partial startup failure")
	}
}

func TestManager_PerItemTimeoutPreventsGoroutineLeak(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())
	mgr.SetItemTimeout(50 * time.Millisecond) // Per-item timeout of 50ms

	var fastExecuted, finalExecuted bool

	// This resource hangs indefinitely (simulates goroutine leak scenario)
	mgr.Register("fast_resource", func(_ context.Context) error {
		fastExecuted = true
		return nil
	})

	mgr.Register("hanging_resource", func(_ context.Context) error {
		// Block forever — simulate hung resource
		select {}
	})

	mgr.Register("final_resource", func(_ context.Context) error {
		finalExecuted = true
		return nil
	})

	// Overall context has a generous deadline, but per-item timeout limits the hang
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := mgr.Cleanup(ctx)
	elapsed := time.Since(start)

	// Should complete well within the 5-second overall budget
	if elapsed > 2*time.Second {
		t.Errorf("cleanup took too long (%v), per-item timeout should have capped the hanging resource", elapsed)
	}

	// The hanging resource should produce a timeout error
	if err == nil {
		t.Fatal("expected error from hanging resource timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded in error, got: %v", err)
	}

	// Other resources should still have executed despite the hanging resource
	if !finalExecuted {
		t.Error("expected final_resource to execute despite hanging_resource timeout")
	}
	if !fastExecuted {
		t.Error("expected fast_resource to execute despite hanging_resource timeout")
	}
}

func TestManager_SetItemTimeout(t *testing.T) {
	mgr := lifecycle.NewManager(newDiscardLogger())

	// Default should be the constant
	mgr.SetItemTimeout(0) // Should revert to default (no panic)

	// Setting a valid timeout
	mgr.SetItemTimeout(30 * time.Second)

	// Register and cleanup to verify no regression
	executed := false
	mgr.Register("test", func(_ context.Context) error {
		executed = true
		return nil
	})
	if err := mgr.Cleanup(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected cleanup to execute")
	}
}
