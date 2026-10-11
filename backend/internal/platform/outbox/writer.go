package outbox

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/postgres"
)

// Writer defines the transactional outbox write port.
type Writer interface {
	Write(ctx context.Context, event *Event) error
	WriteBatch(ctx context.Context, events []*Event) error
}

// PgxWriter implements Writer using pgx and the active PostgreSQL transaction.
type PgxWriter struct {
	logger *slog.Logger
}

// NewPgxWriter constructs a new PgxWriter.
func NewPgxWriter(logger *slog.Logger) *PgxWriter {
	if logger == nil {
		logger = slog.Default()
	}
	return &PgxWriter{logger: logger}
}

// Write inserts an outbox event into integration.outbox_event within the active transaction.
// It fails closed if no transaction exists on the context, ensuring atomicity with business writes.
func (w *PgxWriter) Write(ctx context.Context, event *Event) error {
	return w.WriteBatch(ctx, []*Event{event})
}

// WriteBatch inserts multiple outbox events within the active transaction.
func (w *PgxWriter) WriteBatch(ctx context.Context, events []*Event) error {
	tx, ok := postgres.TxFromContext(ctx)
	if !ok || tx == nil {
		return ErrNoActiveTransaction
	}

	if len(events) == 0 {
		return nil
	}

	// Verify ambient tenant context matches event tenant IDs if established
	pc, hasTenantCtx := postgres.FromContext(ctx)
	expectedTenantID := pc.TenantID

	const insertSQL = `
		INSERT INTO integration.outbox_event (
			id, tenant_id, aggregate_type, aggregate_id, event_type,
			event_version, payload, status, occurred_at, retry_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	for _, evt := range events {
		if err := Validate(evt); err != nil {
			return err
		}

		if hasTenantCtx && expectedTenantID != uuid.Nil && evt.TenantID != expectedTenantID {
			return fmt.Errorf("%w: event tenant %s != context tenant %s",
				ErrTenantMismatch, evt.TenantID, expectedTenantID)
		}

		_, err := tx.Exec(ctx, insertSQL,
			evt.ID,
			evt.TenantID,
			evt.AggregateType,
			evt.AggregateID,
			evt.EventType,
			evt.EventVersion,
			evt.Payload,
			evt.Status,
			evt.OccurredAt,
			evt.RetryCount,
		)
		if err != nil {
			return fmt.Errorf("insert outbox event: %w", postgres.MapError(err))
		}
	}

	return nil
}
