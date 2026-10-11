// Package lifecycle provides deterministic resource cleanup and graceful application termination.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DefaultItemTimeout is the maximum time each individual cleanup hook may execute
// before being considered hung. This prevents goroutine leaks from blocking cleanup.
const DefaultItemTimeout = 10 * time.Second

// CleanupFunc defines the signature of a cleanup action for a resource.
type CleanupFunc func(ctx context.Context) error

type cleanupItem struct {
	name string
	fn   CleanupFunc
}

// Manager orchestrates registered cleanup callbacks in reverse order of registration (LIFO).
type Manager struct {
	logger      *slog.Logger
	mu          sync.Mutex
	items       []cleanupItem
	itemTimeout time.Duration
}

// NewManager returns a new initialized Manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger:      logger,
		itemTimeout: DefaultItemTimeout,
	}
}

// SetItemTimeout configures the per-item timeout for cleanup hooks.
// If d <= 0, DefaultItemTimeout is used. This caps the maximum time any single
// cleanup hook may run, preventing goroutine leaks from indefinitely blocked resources.
func (m *Manager) SetItemTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d <= 0 {
		d = DefaultItemTimeout
	}
	m.itemTimeout = d
}

// Register adds a named cleanup function to the manager.
// Cleanups will be invoked in LIFO order upon Cleanup.
// If fn is nil, Register is a no-op.
func (m *Manager) Register(name string, fn CleanupFunc) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, cleanupItem{name: name, fn: fn})
	m.logger.Debug("Registered lifecycle cleanup hook", slog.String("resource", name))
}

// Cleanup executes all registered cleanup functions in LIFO order within the provided context.
// Each cleanup hook is bounded by both the overall context deadline and a per-item timeout
// (whichever expires first), preventing any single hung hook from leaking goroutines or
// blocking the remaining cleanup sequence.
// Even if an individual cleanup returns an error or times out, the remaining cleanups will still execute.
// All returned errors are combined using errors.Join.
// Cleanup is idempotent: once executed, registered items are cleared so subsequent calls do not re-execute them.
func (m *Manager) Cleanup(ctx context.Context) error {
	m.mu.Lock()
	if len(m.items) == 0 {
		m.mu.Unlock()
		return nil
	}
	// Copy and clear items to guarantee idempotency and avoid re-execution
	items := make([]cleanupItem, len(m.items))
	copy(items, m.items)
	m.items = nil
	itemTimeout := m.itemTimeout
	m.mu.Unlock()

	var errs []error
	// Execute in reverse order of registration (LIFO)
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		m.logger.Debug("Executing cleanup hook", slog.String("resource", item.name))

		// Derive a per-item context bounded by both the overall context and the per-item timeout.
		// This ensures that a single hung hook does not consume the entire shutdown budget.
		itemCtx, itemCancel := context.WithTimeout(ctx, itemTimeout)

		done := make(chan error, 1)
		go func() {
			done <- item.fn(itemCtx)
		}()

		select {
		case err := <-done:
			if err != nil {
				m.logger.Error("Cleanup hook failed",
					slog.String("resource", item.name),
					slog.String("error", err.Error()),
				)
				errs = append(errs, fmt.Errorf("cleanup %s: %w", item.name, err))
			} else {
				m.logger.Debug("Cleanup hook completed", slog.String("resource", item.name))
			}
		case <-itemCtx.Done():
			m.logger.Error("Cleanup hook timed out (goroutine abandoned)",
				slog.String("resource", item.name),
				slog.Duration("item_timeout", itemTimeout),
				slog.String("error", itemCtx.Err().Error()),
			)
			errs = append(errs, fmt.Errorf("cleanup %s: %w", item.name, itemCtx.Err()))
		}

		itemCancel()
	}

	return errors.Join(errs...)
}

// CleanupWithTimeout executes Cleanup bounded by the specified timeout.
func (m *Manager) CleanupWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.Cleanup(ctx)
}
