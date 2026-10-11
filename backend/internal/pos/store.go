// Package pos provides persistence operations for the POS domain.
package pos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

// Store defines persistence operations for POS aggregates and entities.
type Store interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error)
	GetSessionByIDForUpdate(ctx context.Context, id uuid.UUID) (*Session, error)
	UpdateSession(ctx context.Context, session *Session, expectedVersion int64) error
	CreateCashMovement(ctx context.Context, movement *CashMovement) error
	ListCashMovements(ctx context.Context, sessionID uuid.UUID) ([]*CashMovement, error)
	GetTerminalStore(ctx context.Context, terminalID uuid.UUID) (storeID, companyID uuid.UUID, terminalStatus, storeStatus string, err error)
	GetUserStatus(ctx context.Context, userID uuid.UUID) (string, error)
	GetSessionSummaryTotals(ctx context.Context, sessionID uuid.UUID) (cashIn, cashOut float64, err error)
}

// PgxStore implements Store using pgxpool and PostgreSQL under forced RLS.
type PgxStore struct {
	pool *pgxpool.Pool
}

// NewPgxStore constructs a new PgxStore.
func NewPgxStore(pool *pgxpool.Pool) *PgxStore {
	return &PgxStore{pool: pool}
}

func (s *PgxStore) exec(ctx context.Context) postgres.DBTX {
	return postgres.GetDBTX(ctx, s.pool)
}

// CreateSession inserts a new shift session into pos.session.
func (s *PgxStore) CreateSession(ctx context.Context, session *Session) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	session.TenantID = pc.TenantID
	if session.Version == 0 {
		session.Version = 1
	}
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}
	if session.OpenedAt.IsZero() {
		session.OpenedAt = now
	}

	query := `
		INSERT INTO pos.session (
			id, tenant_id, company_id, store_id, terminal_id, cashier_user_id,
			session_number, opened_at, opening_cash, expected_cash, status,
			created_at, updated_at, version
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14
		)
	`

	expectedCashVal := session.OpeningCash
	if session.ExpectedCash != nil {
		expectedCashVal = *session.ExpectedCash
	}

	_, err := s.exec(ctx).Exec(ctx, query,
		session.ID,
		session.TenantID,
		session.CompanyID,
		session.StoreID,
		session.TerminalID,
		session.CashierUserID,
		session.SessionNumber,
		session.OpenedAt,
		session.OpeningCash,
		expectedCashVal,
		session.Status,
		session.CreatedAt,
		session.UpdatedAt,
		session.Version,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				if pgErr.ConstraintName == "ux_pos_session_terminal_open" {
					return ErrTerminalSessionActive
				}
			}
		}
		return fmt.Errorf("insert pos.session: %w", postgres.MapError(err))
	}

	return nil
}

// GetSessionByID retrieves a session by its unique ID.
func (s *PgxStore) GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	return s.querySession(ctx, id, false)
}

// GetSessionByIDForUpdate retrieves a session and places a row-level lock on it (SELECT ... FOR UPDATE).
func (s *PgxStore) GetSessionByIDForUpdate(ctx context.Context, id uuid.UUID) (*Session, error) {
	return s.querySession(ctx, id, true)
}

func (s *PgxStore) querySession(ctx context.Context, id uuid.UUID, forUpdate bool) (*Session, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	lockClause := ""
	if forUpdate {
		lockClause = "FOR UPDATE"
	}

	query := fmt.Sprintf(`
		SELECT
			id, tenant_id, company_id, store_id, terminal_id, cashier_user_id,
			session_number, opened_at, closed_at, opening_cash, expected_cash,
			actual_cash, variance, status, variance_approved_by, variance_approved_at,
			variance_reason, created_at, updated_at, version
		FROM pos.session
		WHERE id = $1
		%s
	`, lockClause)

	var (
		sess               Session
		expectedCashVal    *float64
		actualCashVal      *float64
		varianceVal        *float64
		varianceAppByVal   *uuid.UUID
		varianceAppAtVal   *time.Time
		varianceReasonVal  *string
	)

	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&sess.ID,
		&sess.TenantID,
		&sess.CompanyID,
		&sess.StoreID,
		&sess.TerminalID,
		&sess.CashierUserID,
		&sess.SessionNumber,
		&sess.OpenedAt,
		&sess.ClosedAt,
		&sess.OpeningCash,
		&expectedCashVal,
		&actualCashVal,
		&varianceVal,
		&sess.Status,
		&varianceAppByVal,
		&varianceAppAtVal,
		&varianceReasonVal,
		&sess.CreatedAt,
		&sess.UpdatedAt,
		&sess.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("query pos.session: %w", postgres.MapError(err))
	}

	sess.ExpectedCash = expectedCashVal
	sess.ActualCash = actualCashVal
	sess.ClosingCash = actualCashVal
	sess.Variance = varianceVal
	sess.VarianceApprovedBy = varianceAppByVal
	sess.VarianceApprovedAt = varianceAppAtVal
	sess.VarianceReason = varianceReasonVal

	return &sess, nil
}

// UpdateSession updates an existing session with optimistic concurrency control.
func (s *PgxStore) UpdateSession(ctx context.Context, session *Session, expectedVersion int64) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	now := time.Now().UTC()
	newVersion := expectedVersion + 1

	expectedCashVal := float64(0)
	if session.ExpectedCash != nil {
		expectedCashVal = *session.ExpectedCash
	}

	actualCashVal := session.ActualCash
	if actualCashVal == nil && session.ClosingCash != nil {
		actualCashVal = session.ClosingCash
	}

	query := `
		UPDATE pos.session SET
			expected_cash = $1,
			actual_cash = $2,
			variance = $3,
			status = $4,
			closed_at = $5,
			variance_approved_by = $6,
			variance_approved_at = $7,
			variance_reason = $8,
			updated_at = $9,
			version = $10
		WHERE id = $11 AND version = $12
	`

	cmdTag, err := s.exec(ctx).Exec(ctx, query,
		expectedCashVal,
		actualCashVal,
		session.Variance,
		session.Status,
		session.ClosedAt,
		session.VarianceApprovedBy,
		session.VarianceApprovedAt,
		session.VarianceReason,
		now,
		newVersion,
		session.ID,
		expectedVersion,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23514" {
				if pgErr.ConstraintName == "pos_session_expected_cash_check" {
					return ErrInsufficientDrawerCash
				}
			}
		}
		return fmt.Errorf("update pos.session: %w", postgres.MapError(err))
	}

	if cmdTag.RowsAffected() == 0 {
		var exists bool
		_ = s.exec(ctx).QueryRow(ctx, `SELECT true FROM pos.session WHERE id = $1`, session.ID).Scan(&exists)
		if !exists {
			return ErrSessionNotFound
		}
		return ErrSessionVersionConflict
	}

	session.Version = newVersion
	session.UpdatedAt = now
	return nil
}

// CreateCashMovement inserts a cash movement record into pos.cash_movement.
func (s *PgxStore) CreateCashMovement(ctx context.Context, movement *CashMovement) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	movement.TenantID = pc.TenantID
	if movement.CreatedAt.IsZero() {
		movement.CreatedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO pos.cash_movement (
			id, tenant_id, session_id, movement_type, amount, reason,
			reference_type, reference_id, created_at, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10
		)
	`

	_, err := s.exec(ctx).Exec(ctx, query,
		movement.ID,
		movement.TenantID,
		movement.SessionID,
		movement.Type,
		movement.Amount,
		movement.Reason,
		movement.ReferenceType,
		movement.ReferenceID,
		movement.CreatedAt,
		movement.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("insert pos.cash_movement: %w", postgres.MapError(err))
	}

	return nil
}

// ListCashMovements returns all movements for a session ordered by creation time.
func (s *PgxStore) ListCashMovements(ctx context.Context, sessionID uuid.UUID) ([]*CashMovement, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT
			id, tenant_id, session_id, movement_type, amount, reason,
			reference_type, reference_id, created_at, created_by
		FROM pos.cash_movement
		WHERE session_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.exec(ctx).Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query pos.cash_movement: %w", postgres.MapError(err))
	}
	defer rows.Close()

	var movements []*CashMovement
	for rows.Next() {
		var m CashMovement
		var reasonVal *string
		if err := rows.Scan(
			&m.ID,
			&m.TenantID,
			&m.SessionID,
			&m.Type,
			&m.Amount,
			&reasonVal,
			&m.ReferenceType,
			&m.ReferenceID,
			&m.CreatedAt,
			&m.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("scan cash movement: %w", postgres.MapError(err))
		}
		if reasonVal != nil {
			m.Reason = *reasonVal
		}
		movements = append(movements, &m)
	}

	return movements, nil
}

// GetTerminalStore retrieves store and terminal status and bindings.
func (s *PgxStore) GetTerminalStore(ctx context.Context, terminalID uuid.UUID) (storeID, companyID uuid.UUID, terminalStatus, storeStatus string, err error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return uuid.Nil, uuid.Nil, "", "", postgres.ErrMissingTenantContext
	}

	query := `
		SELECT t.store_id, t.company_id, t.status, s.status
		FROM organization.terminal t
		JOIN organization.store s ON t.store_id = s.id
		WHERE t.id = $1
	`

	err = s.exec(ctx).QueryRow(ctx, query, terminalID).Scan(&storeID, &companyID, &terminalStatus, &storeStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, "", "", ErrTerminalNotFound
		}
		return uuid.Nil, uuid.Nil, "", "", fmt.Errorf("query terminal: %w", postgres.MapError(err))
	}

	return storeID, companyID, terminalStatus, storeStatus, nil
}

// GetUserStatus checks the operational status of a user in identity.user_account.
func (s *PgxStore) GetUserStatus(ctx context.Context, userID uuid.UUID) (string, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return "", postgres.ErrMissingTenantContext
	}

	query := `SELECT status FROM identity.user_account WHERE id = $1`
	var status string
	err := s.exec(ctx).QueryRow(ctx, query, userID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCashierInactive
		}
		return "", fmt.Errorf("query user status: %w", postgres.MapError(err))
	}

	return status, nil
}

// GetSessionSummaryTotals aggregates cash-in and cash-out totals from pos.cash_movement.
func (s *PgxStore) GetSessionSummaryTotals(ctx context.Context, sessionID uuid.UUID) (cashIn, cashOut float64, err error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return 0, 0, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT
			COALESCE(SUM(CASE WHEN movement_type = 'CASH_IN' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN movement_type = 'CASH_OUT' THEN amount ELSE 0 END), 0)
		FROM pos.cash_movement
		WHERE session_id = $1
	`

	err = s.exec(ctx).QueryRow(ctx, query, sessionID).Scan(&cashIn, &cashOut)
	if err != nil {
		return 0, 0, fmt.Errorf("query cash totals: %w", postgres.MapError(err))
	}

	return cashIn, cashOut, nil
}
