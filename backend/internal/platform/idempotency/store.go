package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

var (
	// ErrRecordNotFound is returned when an idempotency record does not exist.
	ErrRecordNotFound = errors.New("idempotency record not found")

	// ErrReservationLost is returned when completing an idempotency record fails because it does not exist,
	// was already completed, or reservation ownership was lost.
	ErrReservationLost = errors.New("idempotency reservation not found, already completed, or ownership lost")

	// DefaultTTL is the default time-to-live for completed idempotency records (24 hours).
	DefaultTTL = 24 * time.Hour

	// InProgressStaleThreshold defines the reference duration after which an uncompleted record
	// is considered aged. Note: stale records are never deleted synchronously without proven fencing.
	InProgressStaleThreshold = 5 * time.Minute
)

type recordContextKey struct{}

// WithRecord injects the active idempotency record into the context.
func WithRecord(ctx context.Context, rec *Record) context.Context {
	return context.WithValue(ctx, recordContextKey{}, rec)
}

// RecordFromContext retrieves the active idempotency record from the context if present.
func RecordFromContext(ctx context.Context) (*Record, bool) {
	rec, ok := ctx.Value(recordContextKey{}).(*Record)
	return rec, ok && rec != nil
}

// Record represents a persisted idempotency key record from platform.idempotency_key.
type Record struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Key            string
	Operation      string
	RequestHash    string
	ResponseStatus *int
	ResponseBody   []byte
	CreatedAt      time.Time
	ExpiresAt      *time.Time
}

// IsCompleted reports whether the operation associated with this key completed successfully.
func (r *Record) IsCompleted() bool {
	return r != nil && r.ResponseStatus != nil
}

// IsInProgress reports whether the operation is still executing.
func (r *Record) IsInProgress() bool {
	return r != nil && r.ResponseStatus == nil
}

// AcquireResult captures the outcome of an atomic AcquireOrGet operation.
type AcquireResult struct {
	// Acquired indicates that the caller won the race and should proceed with executing the operation.
	Acquired bool

	// Record holds the existing or newly initialized idempotency record.
	Record *Record

	// HashMismatch indicates that the key was previously used with a different request payload.
	HashMismatch bool
}

// Store defines the persistence contract for idempotency tracking.
type Store interface {
	AcquireOrGet(ctx context.Context, tenantID uuid.UUID, key, operation, requestHash string, ttl time.Duration) (*AcquireResult, error)
	Complete(ctx context.Context, recordID, tenantID uuid.UUID, status int, responseBody []byte) error
	Release(ctx context.Context, recordID, tenantID uuid.UUID) error
	Get(ctx context.Context, tenantID uuid.UUID, key, operation string) (*Record, error)
	Delete(ctx context.Context, tenantID uuid.UUID, key, operation string) error

	// CleanupExpired safely prunes expired, completed records in batches using FOR UPDATE SKIP LOCKED.
	// Active / in-progress records (response_status IS NULL) are NEVER pruned.
	CleanupExpired(ctx context.Context, batchSize int) (int64, error)

	// FindStuckReservations identifies in-progress records older than staleThreshold for diagnostic/recovery review.
	FindStuckReservations(ctx context.Context, tenantID uuid.UUID, staleThreshold time.Duration, limit int) ([]*Record, error)

	// MarkStuckAsFailed provides bounded administrative fencing to resolve a crash-stuck reservation.
	MarkStuckAsFailed(ctx context.Context, tenantID, recordID uuid.UUID, staleThreshold time.Duration, failStatus int, failBody []byte) error
}

// PgxStore implements Store backed by PostgreSQL platform.idempotency_key.
type PgxStore struct {
	dbtx   postgres.DBTX
	tm     postgres.TxManager
	logger *slog.Logger
}

// NewPgxStore constructs a new PostgreSQL-backed Store.
func NewPgxStore(dbtx postgres.DBTX, logger *slog.Logger) *PgxStore {
	if logger == nil {
		logger = slog.Default()
	}

	var tm postgres.TxManager
	if pool, ok := dbtx.(*pgxpool.Pool); ok {
		tm = postgres.NewTxManager(pool, logger)
	}

	return &PgxStore{
		dbtx:   dbtx,
		tm:     tm,
		logger: logger,
	}
}

// WithTxManager sets an explicit TxManager on the store.
func (s *PgxStore) WithTxManager(tm postgres.TxManager) *PgxStore {
	return &PgxStore{
		dbtx:   s.dbtx,
		tm:     tm,
		logger: s.logger,
	}
}

// WithDBTX returns a shallow clone of the store using a transactional executor.
func (s *PgxStore) WithDBTX(dbtx postgres.DBTX) *PgxStore {
	return &PgxStore{
		dbtx:   dbtx,
		tm:     s.tm,
		logger: s.logger,
	}
}

func (s *PgxStore) execInTx(ctx context.Context, tenantID uuid.UUID, fn func(c context.Context, dbtx postgres.DBTX) error) error {
	// If context already carries an active transaction, execute directly on it
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return fn(ctx, tx)
	}

	// If a TxManager is configured, wrap execution in a transaction with tenant RLS context
	if s.tm != nil {
		txCtx := postgres.WithTenantID(ctx, tenantID)
		return s.tm.WithinTx(txCtx, func(c context.Context) error {
			tx, _ := postgres.TxFromContext(c)
			return fn(c, tx)
		})
	}

	// Fallback to direct dbtx executor
	return fn(ctx, s.dbtx)
}

func (s *PgxStore) execUnscoped(ctx context.Context, fn func(c context.Context, dbtx postgres.DBTX) error) error {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return fn(ctx, tx)
	}
	if s.tm != nil {
		return s.tm.WithinUnscopedTx(ctx, func(c context.Context) error {
			tx, _ := postgres.TxFromContext(c)
			return fn(c, tx)
		})
	}
	return fn(ctx, s.dbtx)
}

// AcquireOrGet attempts to atomically insert a new in-progress record for the given (tenant_id, key, operation).
// If the key already exists, it fetches the existing record and checks for payload hash equivalence.
func (s *PgxStore) AcquireOrGet(
	ctx context.Context,
	tenantID uuid.UUID,
	key, operation, requestHash string,
	ttl time.Duration,
) (*AcquireResult, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	var result *AcquireResult
	err := s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		// 1. Attempt atomic insertion. Database unique constraint on (tenant_id, key, operation)
		// guarantees exactly one concurrent process succeeds.
		// In-progress records are NEVER automatically deleted synchronously on an old timestamp
		// alone, preventing concurrent duplicate execution if a handler is still running.
		recordID := uuid.New()
		ttlSeconds := int(ttl.Seconds())
		insertQuery := `
			INSERT INTO platform.idempotency_key (
				id, tenant_id, key, operation, request_hash, created_at, expires_at
			) VALUES (
				$1, $2, $3, $4, $5, now(), now() + make_interval(secs => $6)
			)
			ON CONFLICT (tenant_id, key, operation) DO NOTHING
			RETURNING id, created_at, expires_at;
		`

		var createdAt time.Time
		var expiresAt *time.Time
		err := executor.QueryRow(c, insertQuery, recordID, tenantID, key, operation, requestHash, ttlSeconds).Scan(
			&recordID,
			&createdAt,
			&expiresAt,
		)

		if err == nil {
			// Won the race: record acquired in-progress
			result = &AcquireResult{
				Acquired: true,
				Record: &Record{
					ID:          recordID,
					TenantID:    tenantID,
					Key:         key,
					Operation:   operation,
					RequestHash: requestHash,
					CreatedAt:   createdAt,
					ExpiresAt:   expiresAt,
				},
			}
			return nil
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("acquire idempotency key: %w", postgres.MapError(err))
		}

		// 2. Lost the race: key already exists. Fetch the existing record.
		existing, err := s.getWithExecutor(c, executor, tenantID, key, operation)
		if err != nil {
			return fmt.Errorf("fetch existing idempotency record: %w", err)
		}

		// 3. Verify request hash match
		if existing.RequestHash != requestHash {
			result = &AcquireResult{
				Acquired:     false,
				Record:       existing,
				HashMismatch: true,
			}
			return nil
		}

		result = &AcquireResult{
			Acquired:     false,
			Record:       existing,
			HashMismatch: false,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// Complete updates an in-progress record with the final HTTP status code and response payload.
// It verifies that exactly one in-progress row was updated. If the record does not exist,
// was already completed, or ownership was lost, ErrReservationLost is returned.
func (s *PgxStore) Complete(
	ctx context.Context,
	recordID, tenantID uuid.UUID,
	status int,
	responseBody []byte,
) error {
	var bodyJSON any
	if len(responseBody) > 0 {
		var raw any
		if err := json.Unmarshal(responseBody, &raw); err == nil {
			bodyJSON = raw
		} else {
			bodyJSON = string(responseBody)
		}
	}

	query := `
		UPDATE platform.idempotency_key
		SET response_status = $1, response_body = $2
		WHERE id = $3 AND tenant_id = $4 AND response_status IS NULL;
	`

	return s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		tag, err := executor.Exec(c, query, status, bodyJSON, recordID, tenantID)
		if err != nil {
			return fmt.Errorf("complete idempotency record: %w", postgres.MapError(err))
		}
		if tag.RowsAffected() == 0 {
			return ErrReservationLost
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("expected 1 row updated, got %d", tag.RowsAffected())
		}
		return nil
	})
}

// Release removes an in-progress record so that the caller can retry upon failure.
// Completed records are not removed by Release.
func (s *PgxStore) Release(ctx context.Context, recordID, tenantID uuid.UUID) error {
	query := `
		DELETE FROM platform.idempotency_key
		WHERE id = $1 AND tenant_id = $2 AND response_status IS NULL;
	`

	return s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		_, err := executor.Exec(c, query, recordID, tenantID)
		if err != nil {
			return fmt.Errorf("release idempotency record: %w", postgres.MapError(err))
		}
		return nil
	})
}

// Get retrieves an existing idempotency record by (tenant_id, key, operation).
func (s *PgxStore) Get(ctx context.Context, tenantID uuid.UUID, key, operation string) (*Record, error) {
	var rec *Record
	err := s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		var err error
		rec, err = s.getWithExecutor(c, executor, tenantID, key, operation)
		return err
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *PgxStore) getWithExecutor(
	ctx context.Context,
	executor postgres.DBTX,
	tenantID uuid.UUID,
	key, operation string,
) (*Record, error) {
	query := `
		SELECT id, tenant_id, key, operation, request_hash, response_status, response_body, created_at, expires_at
		FROM platform.idempotency_key
		WHERE tenant_id = $1 AND key = $2 AND operation = $3;
	`

	var rec Record
	var rawBody []byte
	err := executor.QueryRow(ctx, query, tenantID, key, operation).Scan(
		&rec.ID,
		&rec.TenantID,
		&rec.Key,
		&rec.Operation,
		&rec.RequestHash,
		&rec.ResponseStatus,
		&rawBody,
		&rec.CreatedAt,
		&rec.ExpiresAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("get idempotency record: %w", postgres.MapError(err))
	}

	rec.ResponseBody = rawBody
	return &rec, nil
}

// Delete permanently removes an idempotency record.
func (s *PgxStore) Delete(ctx context.Context, tenantID uuid.UUID, key, operation string) error {
	query := `
		DELETE FROM platform.idempotency_key
		WHERE tenant_id = $1 AND key = $2 AND operation = $3;
	`

	return s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		_, err := executor.Exec(c, query, tenantID, key, operation)
		if err != nil {
			return fmt.Errorf("delete idempotency record: %w", postgres.MapError(err))
		}
		return nil
	})
}

// CleanupExpired prunes expired completed idempotency records up to batchSize rows.
// Active / in-progress operations (response_status IS NULL) are strictly protected and never pruned.
// Uses a CTE with FOR UPDATE SKIP LOCKED for concurrent safety across multiple cleanup workers.
func (s *PgxStore) CleanupExpired(ctx context.Context, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 100
	}

	query := `
		WITH to_delete AS (
			SELECT id
			FROM platform.idempotency_key
			WHERE response_status IS NOT NULL
			  AND expires_at IS NOT NULL
			  AND expires_at < now()
			ORDER BY expires_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM platform.idempotency_key k
		USING to_delete d
		WHERE k.id = d.id;
	`

	var rowsAffected int64
	execFn := func(c context.Context, executor postgres.DBTX) error {
		tag, err := executor.Exec(c, query, batchSize)
		if err != nil {
			return fmt.Errorf("cleanup expired idempotency records: %w", postgres.MapError(err))
		}
		rowsAffected = tag.RowsAffected()
		return nil
	}

	var err error
	if pc, ok := postgres.FromContext(ctx); ok && pc.TenantID != uuid.Nil {
		err = s.execInTx(ctx, pc.TenantID, execFn)
	} else {
		err = s.execUnscoped(ctx, execFn)
	}
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

// FindStuckReservations finds in-progress records older than staleThreshold for review or recovery.
func (s *PgxStore) FindStuckReservations(ctx context.Context, tenantID uuid.UUID, staleThreshold time.Duration, limit int) ([]*Record, error) {
	if limit <= 0 {
		limit = 50
	}
	thresholdSeconds := int(staleThreshold.Seconds())
	if thresholdSeconds <= 0 {
		thresholdSeconds = 300 // default 5 minutes
	}

	query := `
		SELECT id, tenant_id, key, operation, request_hash, response_status, response_body, created_at, expires_at
		FROM platform.idempotency_key
		WHERE tenant_id = $1
		  AND response_status IS NULL
		  AND created_at < now() - ($2::int * interval '1 second')
		ORDER BY created_at ASC
		LIMIT $3;
	`

	var records []*Record
	err := s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		rows, err := executor.Query(c, query, tenantID, thresholdSeconds, limit)
		if err != nil {
			return fmt.Errorf("find stuck reservations: %w", postgres.MapError(err))
		}
		defer rows.Close()

		for rows.Next() {
			var rec Record
			var rawBody []byte
			if err := rows.Scan(
				&rec.ID,
				&rec.TenantID,
				&rec.Key,
				&rec.Operation,
				&rec.RequestHash,
				&rec.ResponseStatus,
				&rawBody,
				&rec.CreatedAt,
				&rec.ExpiresAt,
			); err != nil {
				return fmt.Errorf("scan stuck record: %w", postgres.MapError(err))
			}
			rec.ResponseBody = rawBody
			records = append(records, &rec)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// MarkStuckAsFailed atomically fences and marks a crash-stuck in-progress reservation as failed.
// Returns ErrReservationLost if the reservation was already completed by another worker or process.
func (s *PgxStore) MarkStuckAsFailed(ctx context.Context, tenantID, recordID uuid.UUID, staleThreshold time.Duration, failStatus int, failBody []byte) error {
	thresholdSeconds := int(staleThreshold.Seconds())
	if thresholdSeconds <= 0 {
		thresholdSeconds = 300
	}

	var bodyJSON any
	if len(failBody) > 0 {
		var raw any
		if err := json.Unmarshal(failBody, &raw); err == nil {
			bodyJSON = raw
		} else {
			bodyJSON = string(failBody)
		}
	}

	query := `
		UPDATE platform.idempotency_key
		SET response_status = $1, response_body = $2
		WHERE id = $3
		  AND tenant_id = $4
		  AND response_status IS NULL
		  AND created_at < now() - ($5::int * interval '1 second');
	`

	return s.execInTx(ctx, tenantID, func(c context.Context, executor postgres.DBTX) error {
		tag, err := executor.Exec(c, query, failStatus, bodyJSON, recordID, tenantID, thresholdSeconds)
		if err != nil {
			return fmt.Errorf("mark stuck reservation failed: %w", postgres.MapError(err))
		}
		if tag.RowsAffected() == 0 {
			return ErrReservationLost
		}
		return nil
	})
}

// ExecuteAtomic coordinates business mutation and idempotency response completion within a single database transaction.
// Transaction ownership:
// 1. The transaction is initiated by tm.WithinTx with mandatory ambient tenant context.
// 2. The business mutation function 'fn' executes using the transactional context (accessible via postgres.TxFromContext).
// 3. Upon successful execution of 'fn', store.Complete is executed within the SAME database transaction.
// 4. If 'fn' returns an error, the transaction rolls back, leaving the business entities untouched and the in-progress
//    reservation in PostgreSQL (fail-closed) so subsequent retries cannot silently re-execute duplicate actions.
// 5. If 'fn' panics, tm.WithinTx recovers the panic and rolls back both the business mutation and completion.
func ExecuteAtomic(
	ctx context.Context,
	tm postgres.TxManager,
	store Store,
	record *Record,
	fn func(txCtx context.Context) (status int, body []byte, err error),
) error {
	if record == nil {
		return errors.New("idempotency record must not be nil")
	}
	if tm == nil {
		return errors.New("TxManager must not be nil")
	}

	return tm.WithinTx(ctx, func(txCtx context.Context) error {
		status, body, err := fn(txCtx)
		if err != nil {
			return err
		}

		if compErr := store.Complete(txCtx, record.ID, record.TenantID, status, body); compErr != nil {
			return fmt.Errorf("atomic idempotency completion: %w", compErr)
		}

		return nil
	})
}
