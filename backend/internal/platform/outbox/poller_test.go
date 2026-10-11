package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/outbox"
)

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name        string
		retryCount  int
		baseBackoff time.Duration
		expected    time.Duration
	}{
		{name: "retry 0 default base", retryCount: 0, baseBackoff: 1 * time.Second, expected: 1 * time.Second},
		{name: "retry 1 default base", retryCount: 1, baseBackoff: 1 * time.Second, expected: 2 * time.Second},
		{name: "retry 2 default base", retryCount: 2, baseBackoff: 1 * time.Second, expected: 4 * time.Second},
		{name: "retry 3 default base", retryCount: 3, baseBackoff: 1 * time.Second, expected: 8 * time.Second},
		{name: "retry 4 default base", retryCount: 4, baseBackoff: 1 * time.Second, expected: 16 * time.Second},
		{name: "retry 1 custom base 500ms", retryCount: 1, baseBackoff: 500 * time.Millisecond, expected: 1 * time.Second},
		{name: "retry 2 custom base 2s", retryCount: 2, baseBackoff: 2 * time.Second, expected: 8 * time.Second},
		{name: "non-positive base defaults to 1s", retryCount: 1, baseBackoff: 0, expected: 2 * time.Second},
		{name: "negative base defaults to 1s", retryCount: 2, baseBackoff: -5 * time.Second, expected: 4 * time.Second},
		{name: "large retry capped at 10m", retryCount: 15, baseBackoff: 1 * time.Second, expected: 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := outbox.CalculateBackoff(tt.retryCount, tt.baseBackoff)
			if actual != tt.expected {
				t.Errorf("CalculateBackoff(%d, %v) = %v; want %v", tt.retryCount, tt.baseBackoff, actual, tt.expected)
			}
		})
	}
}

func TestPollerConfig_DefaultsAndBounds(t *testing.T) {
	cfg := outbox.DefaultPollerConfig()
	if cfg.BatchSize != 5 {
		t.Fatalf("expected DefaultPollerConfig.BatchSize = 5, got %d", cfg.BatchSize)
	}
	if cfg.BaseBackoff != 1*time.Second {
		t.Fatalf("expected DefaultPollerConfig.BaseBackoff = 1s, got %v", cfg.BaseBackoff)
	}

	// Verify NewPoller bounds clamping
	dispatcher := outbox.NewDispatcher(nil)

	p1 := outbox.NewPoller(nil, dispatcher, outbox.PollerConfig{BatchSize: 0, BaseBackoff: 0}, nil)
	if p1 == nil {
		t.Fatal("expected non-nil poller")
	}

	p2 := outbox.NewPoller(nil, dispatcher, outbox.PollerConfig{BatchSize: 500, BaseBackoff: 2 * time.Hour}, nil)
	if p2 == nil {
		t.Fatal("expected non-nil poller")
	}
}

func TestNonRetryableError_Wrapping(t *testing.T) {
	rawErr := errors.New("upstream schema rejection: 400 bad request")
	wrapped := outbox.NewNonRetryableError(rawErr)

	if !errors.Is(wrapped, outbox.ErrNonRetryable) {
		t.Fatalf("expected wrapped error to match ErrNonRetryable")
	}
	if !errors.Is(wrapped, rawErr) {
		t.Fatalf("expected wrapped error to match raw error")
	}

	nilWrapped := outbox.NewNonRetryableError(nil)
	if !errors.Is(nilWrapped, outbox.ErrNonRetryable) {
		t.Fatalf("expected nil wrapped error to match ErrNonRetryable")
	}
}

func TestDispatcher(t *testing.T) {
	dispatcher := outbox.NewDispatcher(nil)
	tenantID := uuid.New()
	aggregateID := uuid.New()

	var dispatchedCount int
	handler := func(_ context.Context, e *outbox.Event) error {
		dispatchedCount++
		if e.EventType != "OrderPaid" {
			return errors.New("unexpected event type")
		}
		return nil
	}

	dispatcher.Register("OrderPaid", handler)

	if !dispatcher.HasHandler("OrderPaid") {
		t.Fatalf("expected HasHandler to report true for OrderPaid")
	}
	if dispatcher.HasHandler("UnknownEvent") {
		t.Fatalf("expected HasHandler to report false for UnknownEvent")
	}

	// 1. Dispatch registered event type
	evt, err := outbox.NewEvent(tenantID, "order", aggregateID, "OrderPaid", 1, map[string]any{"paid": true})
	if err != nil {
		t.Fatalf("unexpected NewEvent error: %v", err)
	}

	if err := dispatcher.Dispatch(context.Background(), evt); err != nil {
		t.Fatalf("unexpected dispatch error: %v", err)
	}
	if dispatchedCount != 1 {
		t.Fatalf("expected dispatchedCount = 1, got %d", dispatchedCount)
	}

	// 2. Dispatch unregistered event type without default handler returns ErrNoHandlerRegistered
	unregisteredEvt, _ := outbox.NewEvent(tenantID, "order", aggregateID, "OrderCancelled", 1, map[string]any{"ok": true})
	err = dispatcher.Dispatch(context.Background(), unregisteredEvt)
	if !errors.Is(err, outbox.ErrNoHandlerRegistered) {
		t.Fatalf("expected ErrNoHandlerRegistered, got %v", err)
	}

	// 3. Fallback default handler receives unregistered events
	var defaultCalled bool
	dispatcher.SetDefaultHandler(func(_ context.Context, _ *outbox.Event) error {
		defaultCalled = true
		return nil
	})

	err = dispatcher.Dispatch(context.Background(), unregisteredEvt)
	if err != nil {
		t.Fatalf("expected default handler to handle event, got %v", err)
	}
	if !defaultCalled {
		t.Fatalf("expected default handler to be called")
	}
}

type mockMetricsRecorder struct {
	mu           sync.Mutex
	claims       int
	dispatches   map[string]int
	cycles       int
	workerActive bool
}

func newMockMetricsRecorder() *mockMetricsRecorder {
	return &mockMetricsRecorder{
		dispatches: make(map[string]int),
	}
}

func (m *mockMetricsRecorder) RecordWorkerClaim(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims += count
}

func (m *mockMetricsRecorder) RecordWorkerDispatch(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatches[status]++
}

func (m *mockMetricsRecorder) RecordWorkerPollCycle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cycles++
}

func (m *mockMetricsRecorder) SetWorkerActive(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workerActive = active
}

func TestPoller_MetricsAndStatus(t *testing.T) {
	dispatcher := outbox.NewDispatcher(nil)
	cfg := outbox.PollerConfig{
		BatchSize:    5,
		PollInterval: 10 * time.Millisecond,
	}
	p := outbox.NewPoller(nil, dispatcher, cfg, nil)
	metrics := newMockMetricsRecorder()
	p.SetMetrics(metrics)

	status := p.Status()
	if status.Running {
		t.Errorf("expected initial running = false")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	metrics.mu.Lock()
	active := metrics.workerActive
	metrics.mu.Unlock()
	if !active {
		t.Errorf("expected workerActive = true after Start")
	}

	status = p.Status()
	if !status.Running {
		t.Errorf("expected status.Running = true after Start")
	}

	// Wait for at least one poll cycle
	time.Sleep(50 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	metrics.mu.Lock()
	activeAfterStop := metrics.workerActive
	cycles := metrics.cycles
	metrics.mu.Unlock()

	if activeAfterStop {
		t.Errorf("expected workerActive = false after Stop")
	}
	if cycles < 1 {
		t.Errorf("expected at least 1 poll cycle, got %d", cycles)
	}

	status = p.Status()
	if status.Running {
		t.Errorf("expected status.Running = false after Stop")
	}
	if status.LastPollTime.IsZero() {
		t.Errorf("expected non-zero LastPollTime")
	}
}
