package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

// PollerConfig contains configuration settings for the background outbox poller.
type PollerConfig struct {
	WorkerID        string
	BatchSize       int
	PollInterval    time.Duration
	MaxRetries      int
	DispatchTimeout time.Duration
	LeaseDuration   time.Duration
	TargetSystem    string
	BaseBackoff     time.Duration
	TenantID        *uuid.UUID // Optional: if non-nil, binds tenant RLS context per poll
}

// DefaultPollerConfig returns safe production defaults for outbox polling.
func DefaultPollerConfig() PollerConfig {
	return PollerConfig{
		WorkerID:        uuid.New().String(),
		BatchSize:       5,
		PollInterval:    500 * time.Millisecond,
		MaxRetries:      5,
		DispatchTimeout: 5 * time.Second,
		LeaseDuration:   30 * time.Second,
		TargetSystem:    "internal_dispatcher",
		BaseBackoff:     1 * time.Second,
	}
}

// MetricsRecorder defines the hook for tracking outbox worker metrics.
type MetricsRecorder interface {
	RecordWorkerClaim(count int)
	RecordWorkerDispatch(status string)
	RecordWorkerPollCycle()
	SetWorkerActive(active bool)
}

// PollerStatus models operational diagnostics for the background poller.
type PollerStatus struct {
	Running             bool      `json:"running"`
	LastPollTime        time.Time `json:"last_poll_time"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastError           string    `json:"last_error,omitempty"`
}

// Poller coordinates background polling, claiming, and dispatching of outbox events.
type Poller struct {
	pool       *pgxpool.Pool
	tm         postgres.TxManager
	dispatcher Dispatcher
	cfg        PollerConfig
	logger     *slog.Logger
	metrics    MetricsRecorder

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	statusMu            sync.RWMutex
	lastPollTime        time.Time
	consecutiveFailures int
	lastError           string
}

// NewPoller constructs a new background Outbox Poller.
func NewPoller(
	pool *pgxpool.Pool,
	dispatcher Dispatcher,
	cfg PollerConfig,
	logger *slog.Logger,
) *Poller {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = uuid.New().String()
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5
	} else if cfg.BatchSize > 100 {
		cfg.BatchSize = 100
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	if cfg.DispatchTimeout <= 0 {
		cfg.DispatchTimeout = 5 * time.Second
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 30 * time.Second
	} else if cfg.LeaseDuration < cfg.DispatchTimeout {
		cfg.LeaseDuration = cfg.DispatchTimeout * 2
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 1 * time.Second
	} else if cfg.BaseBackoff > 10*time.Minute {
		cfg.BaseBackoff = 10 * time.Minute
	}
	if cfg.TargetSystem == "" {
		cfg.TargetSystem = "internal_dispatcher"
	}

	return &Poller{
		pool:       pool,
		tm:         postgres.NewTxManager(pool, logger),
		dispatcher: dispatcher,
		cfg:        cfg,
		logger:     logger,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// SetMetrics configures an optional metrics recorder for operational tracking.
func (p *Poller) SetMetrics(m MetricsRecorder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metrics = m
}

// Status returns the current operational health and diagnostics for the poller.
func (p *Poller) Status() PollerStatus {
	p.statusMu.RLock()
	defer p.statusMu.RUnlock()
	p.mu.Lock()
	running := p.running
	p.mu.Unlock()

	return PollerStatus{
		Running:             running,
		LastPollTime:        p.lastPollTime,
		ConsecutiveFailures: p.consecutiveFailures,
		LastError:           p.lastError,
	}
}

// Start launches the background polling loop.
func (p *Poller) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return errors.New("outbox poller is already running")
	}
	p.running = true
	p.stopCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	if p.metrics != nil {
		p.metrics.SetWorkerActive(true)
	}
	p.mu.Unlock()

	p.logger.Info("Starting transactional outbox poller",
		slog.Int("batch_size", p.cfg.BatchSize),
		slog.Duration("poll_interval", p.cfg.PollInterval),
		slog.Int("max_retries", p.cfg.MaxRetries),
	)

	go p.runLoop(ctx)
	return nil
}

// Stop signals the poller to stop and waits for in-flight polling to complete.
func (p *Poller) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	if p.metrics != nil {
		p.metrics.SetWorkerActive(false)
	}
	close(p.stopCh)
	p.mu.Unlock()

	p.logger.Info("Stopping outbox poller, waiting for in-flight work...")
	select {
	case <-p.doneCh:
		p.logger.Info("Outbox poller stopped successfully")
		return nil
	case <-ctx.Done():
		p.logger.Warn("Outbox poller shutdown timed out or canceled", slog.String("error", ctx.Err().Error()))
		return ctx.Err()
	}
}

func (p *Poller) runLoop(daemonCtx context.Context) {
	defer close(p.doneCh)

	for {
		select {
		case <-daemonCtx.Done():
			return
		case <-p.stopCh:
			return
		default:
		}

		processed, err := p.ProcessNextBatch(daemonCtx)
		p.statusMu.Lock()
		p.lastPollTime = time.Now()
		if err != nil {
			p.consecutiveFailures++
			p.lastError = err.Error()
		} else {
			p.consecutiveFailures = 0
			p.lastError = ""
		}
		p.statusMu.Unlock()

		p.mu.Lock()
		m := p.metrics
		p.mu.Unlock()
		if m != nil {
			m.RecordWorkerPollCycle()
		}

		if err != nil {
			if !errors.Is(err, context.Canceled) {
				p.logger.Error("Error processing outbox batch", slog.String("error", err.Error()))
			}
		}

		// If no items were processed or an error occurred, wait for PollInterval before next poll
		if processed == 0 || err != nil {
			select {
			case <-daemonCtx.Done():
				return
			case <-p.stopCh:
				return
			case <-time.After(p.cfg.PollInterval):
			}
		}
	}
}

func (p *Poller) claimBatch(ctx context.Context) ([]*Event, error) {
	pollCtx := ctx
	if p.cfg.TenantID != nil && *p.cfg.TenantID != uuid.Nil {
		pollCtx = postgres.WithTenantID(ctx, *p.cfg.TenantID)
	}

	leaseSec := p.cfg.LeaseDuration.Seconds()
	if leaseSec <= 0 {
		leaseSec = 30.0
	}

	var claimedEvents []*Event
	err := p.tm.WithinTx(pollCtx, func(txCtx context.Context) error {
		tx, ok := postgres.TxFromContext(txCtx)
		if !ok || tx == nil {
			return errors.New("failed to acquire transaction for claiming outbox events")
		}

		const claimSQL = `
			WITH claimable AS (
				SELECT id
				FROM integration.outbox_event
				WHERE status = 'PENDING'
				   OR (status = 'FAILED' AND retry_count < $1 AND (next_attempt_at IS NULL OR next_attempt_at <= now()))
				   OR (status = 'PROCESSING' AND lease_expires_at IS NOT NULL AND lease_expires_at < now())
				ORDER BY occurred_at ASC
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE integration.outbox_event e
			SET status = 'PROCESSING',
			    claimed_by = $3,
			    lease_expires_at = now() + ($4::float8 * interval '1 second')
			FROM claimable c
			WHERE e.id = c.id
			RETURNING e.id, e.tenant_id, e.aggregate_type, e.aggregate_id, e.event_type,
			          e.event_version, e.payload, e.status, e.occurred_at, e.retry_count;
		`

		rows, err := tx.Query(txCtx, claimSQL, p.cfg.MaxRetries, p.cfg.BatchSize, p.cfg.WorkerID, leaseSec)
		if err != nil {
			return fmt.Errorf("claim outbox events: %w", postgres.MapError(err))
		}
		defer rows.Close()

		for rows.Next() {
			var evt Event
			var statusStr string
			if err := rows.Scan(
				&evt.ID,
				&evt.TenantID,
				&evt.AggregateType,
				&evt.AggregateID,
				&evt.EventType,
				&evt.EventVersion,
				&evt.Payload,
				&statusStr,
				&evt.OccurredAt,
				&evt.RetryCount,
			); err != nil {
				return fmt.Errorf("scan claimed outbox event: %w", postgres.MapError(err))
			}
			evt.Status = Status(statusStr)
			claimedEvents = append(claimedEvents, &evt)
		}
		return rows.Err()
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return claimedEvents, nil
}

func (p *Poller) releaseEvents(ctx context.Context, events []*Event) {
	if len(events) == 0 {
		return
	}
	pollCtx := ctx
	if p.cfg.TenantID != nil && *p.cfg.TenantID != uuid.Nil {
		pollCtx = postgres.WithTenantID(ctx, *p.cfg.TenantID)
	}

	ids := make([]uuid.UUID, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}

	_ = p.tm.WithinTx(pollCtx, func(txCtx context.Context) error {
		tx, ok := postgres.TxFromContext(txCtx)
		if !ok || tx == nil {
			return nil
		}
		const releaseSQL = `
			UPDATE integration.outbox_event
			SET status = 'PENDING',
			    claimed_by = NULL,
			    lease_expires_at = NULL
			WHERE id = ANY($1) AND status = 'PROCESSING';
		`
		_, _ = tx.Exec(txCtx, releaseSQL, ids)
		return nil
	})
}

func (p *Poller) finalizeEvent(ctx context.Context, evt *Event, dispatchErr error) error {
	pollCtx := ctx
	if p.cfg.TenantID != nil && *p.cfg.TenantID != uuid.Nil {
		pollCtx = postgres.WithTenantID(ctx, *p.cfg.TenantID)
	}

	return p.tm.WithinTx(pollCtx, func(txCtx context.Context) error {
		tx, ok := postgres.TxFromContext(txCtx)
		if !ok || tx == nil {
			return errors.New("failed to acquire transaction to finalize outbox event")
		}

		if dispatchErr == nil {
			const updatePubSQL = `
				UPDATE integration.outbox_event
				SET status = 'PUBLISHED',
				    published_at = now(),
				    claimed_by = NULL,
				    lease_expires_at = NULL
				WHERE id = $1;
			`
			if _, err := tx.Exec(txCtx, updatePubSQL, evt.ID); err != nil {
				return fmt.Errorf("mark event published: %w", postgres.MapError(err))
			}

			const insertSuccessAttemptSQL = `
				INSERT INTO integration.delivery_attempt (
					event_id, target_system, attempt_no, status, response_code, attempted_at
				)
				SELECT $1::uuid, $2::varchar(100), COALESCE(max(attempt_no), 0) + 1, 'SUCCESS', '200', now()
				FROM integration.delivery_attempt
				WHERE event_id = $1::uuid AND target_system = $2::varchar(100);
			`
			if _, err := tx.Exec(txCtx, insertSuccessAttemptSQL, evt.ID, p.cfg.TargetSystem); err != nil {
				return fmt.Errorf("record success attempt: %w", postgres.MapError(err))
			}
			return nil
		}

		// Failure handling
		isNonRetryable := errors.Is(dispatchErr, ErrNonRetryable)
		newRetryCount := evt.RetryCount + 1
		newStatus := StatusFailed
		respCode := "ERROR"

		backoff := CalculateBackoff(newRetryCount, p.cfg.BaseBackoff)
		backoffSec := backoff.Seconds()

		if isNonRetryable || newRetryCount >= p.cfg.MaxRetries {
			newStatus = StatusDeadLetter
			if isNonRetryable {
				respCode = "PERMANENT_ERROR"
				p.logger.WarnContext(txCtx, "Outbox event transitioned to DEAD_LETTER immediately due to permanent/non-retryable error",
					slog.String("event_id", evt.ID.String()),
					slog.String("event_type", evt.EventType),
					slog.String("error", dispatchErr.Error()),
				)
			} else {
				p.logger.WarnContext(txCtx, "Outbox event transitioned to DEAD_LETTER after max retries",
					slog.String("event_id", evt.ID.String()),
					slog.String("event_type", evt.EventType),
					slog.Int("attempts", newRetryCount),
					slog.String("error", dispatchErr.Error()),
				)
			}
		}

		errMsg := dispatchErr.Error()
		if newStatus == StatusDeadLetter {
			const updateDeadLetterSQL = `
				UPDATE integration.outbox_event
				SET status = 'DEAD_LETTER',
				    retry_count = $1,
				    last_error = $2,
				    claimed_by = NULL,
				    lease_expires_at = NULL,
				    next_attempt_at = NULL
				WHERE id = $3;
			`
			if _, err := tx.Exec(txCtx, updateDeadLetterSQL, newRetryCount, errMsg, evt.ID); err != nil {
				return fmt.Errorf("update event dead letter: %w", postgres.MapError(err))
			}
		} else {
			const updateFailSQL = `
				UPDATE integration.outbox_event
				SET status = 'FAILED',
				    retry_count = $1,
				    last_error = $2,
				    claimed_by = NULL,
				    lease_expires_at = NULL,
				    next_attempt_at = now() + ($3::float8 * interval '1 second')
				WHERE id = $4;
			`
			if _, err := tx.Exec(txCtx, updateFailSQL, newRetryCount, errMsg, backoffSec, evt.ID); err != nil {
				return fmt.Errorf("update event failure: %w", postgres.MapError(err))
			}
		}

		const insertFailAttemptSQL = `
			INSERT INTO integration.delivery_attempt (
				event_id, target_system, attempt_no, status, response_code, response_body, attempted_at
			)
			SELECT $1::uuid, $2::varchar(100), COALESCE(max(attempt_no), 0) + 1, 'FAILED', $3, $4, now()
			FROM integration.delivery_attempt
			WHERE event_id = $1::uuid AND target_system = $2::varchar(100);
		`
		if _, err := tx.Exec(txCtx, insertFailAttemptSQL, evt.ID, p.cfg.TargetSystem, respCode, errMsg); err != nil {
			return fmt.Errorf("record fail attempt: %w", postgres.MapError(err))
		}

		return nil
	})
}

// ProcessNextBatch claims, dispatches, and updates a single batch of eligible outbox events.
// Concurrency is safe across multiple worker processes via PostgreSQL FOR UPDATE SKIP LOCKED
// and persistent leases. External dispatch is decoupled outside database transactions.
func (p *Poller) ProcessNextBatch(ctx context.Context) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	claimedEvents, err := p.claimBatch(ctx)
	if err != nil {
		return 0, err
	}
	if len(claimedEvents) == 0 {
		return 0, nil
	}

	p.mu.Lock()
	metrics := p.metrics
	p.mu.Unlock()

	if metrics != nil {
		metrics.RecordWorkerClaim(len(claimedEvents))
	}

	var processedCount int
	for idx, evt := range claimedEvents {
		if ctx.Err() != nil {
			// Context canceled before dispatching this event
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			p.releaseEvents(cleanupCtx, claimedEvents[idx:])
			cancel()
			return processedCount, ctx.Err()
		}

		// Dispatch outside database transaction
		dispatchCtx, cancel := context.WithTimeout(ctx, p.cfg.DispatchTimeout)
		dispatchErr := p.dispatcher.Dispatch(dispatchCtx, evt)
		cancel()

		if ctx.Err() != nil {
			// Context was canceled during dispatch!
			// Release this event and remaining events back to PENDING so they are immediately retryable
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
			p.releaseEvents(cleanupCtx, claimedEvents[idx:])
			cancelCleanup()
			return processedCount, ctx.Err()
		}

		// Finalize event in short transaction
		finErr := p.finalizeEvent(ctx, evt, dispatchErr)
		if finErr != nil {
			p.logger.Error("Failed to finalize outbox event",
				slog.String("event_id", evt.ID.String()),
				slog.String("error", finErr.Error()),
			)
			return processedCount, finErr
		}

		if dispatchErr == nil {
			if metrics != nil {
				metrics.RecordWorkerDispatch("success")
			}
		} else {
			isNonRetryable := errors.Is(dispatchErr, ErrNonRetryable)
			if isNonRetryable || evt.RetryCount+1 >= p.cfg.MaxRetries {
				if metrics != nil {
					metrics.RecordWorkerDispatch("dead_letter")
				}
			} else {
				if metrics != nil {
					metrics.RecordWorkerDispatch("retry")
				}
			}
		}

		processedCount++
	}

	return processedCount, nil
}

// CalculateBackoff computes exponential backoff for an event retry attempt.
func CalculateBackoff(retryCount int, baseBackoff time.Duration) time.Duration {
	if baseBackoff <= 0 {
		baseBackoff = 1 * time.Second
	}
	if retryCount <= 0 {
		return baseBackoff
	}
	cappedRetry := retryCount
	if cappedRetry > 10 {
		cappedRetry = 10
	}
	multiplier := math.Pow(2, float64(cappedRetry))
	backoff := time.Duration(multiplier) * baseBackoff
	if backoff > 10*time.Minute {
		return 10 * time.Minute
	}
	return backoff
}
