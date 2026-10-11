package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// ErrNonRetryable indicates a permanent, non-retryable delivery failure
// that should transition immediately to DEAD_LETTER without exhausting remaining retries.
var ErrNonRetryable = errors.New("non-retryable delivery failure")

// NewNonRetryableError wraps an underlying error with ErrNonRetryable.
func NewNonRetryableError(err error) error {
	if err == nil {
		return ErrNonRetryable
	}
	return fmt.Errorf("%w: %w", ErrNonRetryable, err)
}

// Handler represents an asynchronous consumer callback for a specific event type.
type Handler func(ctx context.Context, event *Event) error

// Dispatcher routes committed outbox events to registered consumer handlers.
type Dispatcher interface {
	Register(eventType string, handler Handler)
	Dispatch(ctx context.Context, event *Event) error
	HasHandler(eventType string) bool
}

// DefaultDispatcher is an in-memory thread-safe event dispatcher.
type DefaultDispatcher struct {
	mu             sync.RWMutex
	handlers       map[string]Handler
	defaultHandler Handler
	logger         *slog.Logger
}

// NewDispatcher constructs a new DefaultDispatcher.
func NewDispatcher(logger *slog.Logger) *DefaultDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultDispatcher{
		handlers: make(map[string]Handler),
		logger:   logger,
	}
}

// SetDefaultHandler sets a fallback handler for event types without an explicit registration.
func (d *DefaultDispatcher) SetDefaultHandler(handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.defaultHandler = handler
}

// Register binds a consumer handler to a specific canonical event type.
func (d *DefaultDispatcher) Register(eventType string, handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[eventType] = handler
}

// HasHandler reports whether a handler is registered for the specified event type.
func (d *DefaultDispatcher) HasHandler(eventType string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.handlers[eventType]
	return ok || d.defaultHandler != nil
}

// Dispatch invokes the registered consumer handler for the given event.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, event *Event) error {
	if event == nil {
		return fmt.Errorf("%w: nil event", ErrInvalidEvent)
	}

	d.mu.RLock()
	handler, ok := d.handlers[event.EventType]
	if !ok {
		handler = d.defaultHandler
	}
	d.mu.RUnlock()

	if handler == nil {
		d.logger.WarnContext(ctx, "No outbox handler registered for event type",
			slog.String("event_type", event.EventType),
			slog.String("event_id", event.ID.String()),
		)
		return fmt.Errorf("%w: %s", ErrNoHandlerRegistered, event.EventType)
	}

	d.logger.DebugContext(ctx, "Dispatching outbox event",
		slog.String("event_type", event.EventType),
		slog.String("event_id", event.ID.String()),
		slog.String("tenant_id", event.TenantID.String()),
		slog.Int("attempt", event.RetryCount+1),
	)

	return handler(ctx, event)
}
