// Package pos provides the business logic and orchestration for POS shift sessions.
package pos

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// Service defines the business operations for POS session and cash management.
type Service interface {
	OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error)
	GetSession(ctx context.Context, id uuid.UUID, callerCanReviewVariance bool) (*Session, error)
	CashIn(ctx context.Context, sessionID uuid.UUID, req CashMovementRequest) (*CashMovement, error)
	CashOut(ctx context.Context, sessionID uuid.UUID, req CashMovementRequest) (*CashMovement, error)
	CloseSession(ctx context.Context, sessionID uuid.UUID, req CloseSessionRequest, callerCanReviewVariance bool) (*Session, error)
	ApproveVariance(ctx context.Context, sessionID uuid.UUID, req ApproveVarianceRequest) (*Session, error)
	GetSessionSummary(ctx context.Context, sessionID uuid.UUID, callerCanReviewVariance bool) (*SessionSummary, error)
}

// posService coordinates domain logic, transactions, audit trails, and outbox publication.
type posService struct {
	store        Store
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	logger       *slog.Logger
}

// NewService constructs a new POS domain Service.
func NewService(
	store Store,
	txManager postgres.TxManager,
	auditStore audit.Store,
	outboxWriter outbox.Writer,
	logger *slog.Logger,
) Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &posService{
		store:        store,
		txManager:    txManager,
		auditStore:   auditStore,
		outboxWriter: outboxWriter,
		logger:       logger,
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// OpenSession starts a new POS shift session on a registered terminal.
func (s *posService) OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	if req.TerminalID == uuid.Nil {
		return nil, ErrTerminalNotFound
	}
	if req.OpeningCash < 0 {
		return nil, ErrNegativeOpeningCash
	}
	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		currency = "IDR"
	}

	var session *Session

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// 1. Validate terminal and store operational status under transaction-local RLS
		storeID, companyID, terminalStatus, storeStatus, err := s.store.GetTerminalStore(txCtx, req.TerminalID)
		if err != nil {
			return err
		}
		if terminalStatus != "ACTIVE" {
			return ErrTerminalInactive
		}
		if storeStatus != "ACTIVE" {
			return ErrStoreInactive
		}

		// 2. Validate cashier status
		userStatus, err := s.store.GetUserStatus(txCtx, actorID)
		if err != nil {
			return err
		}
		if userStatus != "ACTIVE" {
			return ErrCashierInactive
		}

		// 3. Generate collision-resistant session number
		sessNumber, err := GenerateSessionNumber(time.Now())
		if err != nil {
			return fmt.Errorf("generate session number: %w", err)
		}

		now := time.Now().UTC()
		session = &Session{
			ID:            uuid.New(),
			TenantID:      pc.TenantID,
			CompanyID:     companyID,
			StoreID:       storeID,
			TerminalID:    req.TerminalID,
			CashierUserID: actorID,
			SessionNumber: sessNumber,
			Status:        SessionStatusOpen,
			OpenedAt:      now,
			OpeningCash:   req.OpeningCash,
			ExpectedCash:  &req.OpeningCash,
			Currency:      currency,
			CreatedAt:     now,
			UpdatedAt:     now,
			Version:       1,
		}

		var openingMovement *CashMovement
		if req.OpeningCash > 0 {
			openingMovement = &CashMovement{
				ID:        uuid.New(),
				TenantID:  pc.TenantID,
				SessionID: session.ID,
				Type:      MovementTypeOpening,
				Amount:    req.OpeningCash,
				Reason:    "Opening cash float",
				CreatedAt: now,
				CreatedBy: uuidPtr(actorID),
			}
		}

		// Insert session (enforces partial unique index ux_pos_session_terminal_open)
		if err := s.store.CreateSession(txCtx, session); err != nil {
			return err
		}

		// Insert opening cash movement if positive float
		if openingMovement != nil {
			if err := s.store.CreateCashMovement(txCtx, openingMovement); err != nil {
				return err
			}
		}

		// Record canonical audit entry
		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"session_number": session.SessionNumber,
				"terminal_id":    session.TerminalID.String(),
				"store_id":       session.StoreID.String(),
				"opening_cash":   session.OpeningCash,
				"currency":       session.Currency,
			})
			resType := "pos_session"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     session.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "POS",
				Action:       AuditActionSessionOpened,
				ResourceType: &resType,
				ResourceID:   &session.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		// Publish canonical outbox domain event
		if s.outboxWriter != nil {
			payload := map[string]any{
				"session_id":     session.ID.String(),
				"session_number": session.SessionNumber,
				"terminal_id":    session.TerminalID.String(),
				"store_id":       session.StoreID.String(),
				"cashier_id":     session.CashierUserID.String(),
				"opening_cash":   session.OpeningCash,
				"opened_at":      session.OpenedAt.Format(time.RFC3339),
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal outbox payload: %w", err)
			}
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      session.TenantID,
				AggregateType: "pos_session",
				AggregateID:   session.ID,
				EventType:     EventSessionOpened,
				EventVersion:  1,
				Payload:       payloadBytes,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return session, nil
}

// GetSession retrieves shift session details, enforcing blind-count masking for cashiers during active shifts.
func (s *posService) GetSession(ctx context.Context, id uuid.UUID, callerCanReviewVariance bool) (*Session, error) {
	var session *Session
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		session, err = s.store.GetSessionByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Confidentiality: Mask expected cash and variance if caller lacks variance review permission
	// and the shift is still non-closed (CORR-POS-001).
	if !callerCanReviewVariance && IsNonTerminalStatus(session.Status) {
		return session.MaskBlindCountFields(), nil
	}

	return session, nil
}

// CashIn deposits mid-shift cash into the drawer with concurrency row locking.
func (s *posService) CashIn(ctx context.Context, sessionID uuid.UUID, req CashMovementRequest) (*CashMovement, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	actorID := pc.UserID
	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, ErrReasonRequired
	}
	if len(reason) < 3 {
		return nil, ErrReasonTooShort
	}

	now := time.Now().UTC()
	var movement *CashMovement

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// 1. Pessimistic row locking on pos.session
		session, err := s.store.GetSessionByIDForUpdate(txCtx, sessionID)
		if err != nil {
			return err
		}

		if session.Status != SessionStatusOpen && session.Status != SessionStatusActive {
			return ErrInvalidSessionStatus
		}

		currentExpected := float64(0)
		if session.ExpectedCash != nil {
			currentExpected = *session.ExpectedCash
		}
		newExpected := currentExpected + req.Amount
		session.ExpectedCash = &newExpected

		// 2. Update session balance under optimistic version check
		if err := s.store.UpdateSession(txCtx, session, session.Version); err != nil {
			return err
		}

		// 3. Create cash movement record
		var refType *string
		if req.Reference != "" {
			ref := "REFERENCE"
			refType = &ref
		}
		movement = &CashMovement{
			ID:            uuid.New(),
			TenantID:      pc.TenantID,
			SessionID:     sessionID,
			Type:          MovementTypeCashIn,
			Amount:        req.Amount,
			Reason:        reason,
			ReferenceType: refType,
			CreatedAt:     now,
			CreatedBy:     uuidPtr(actorID),
		}
		if err := s.store.CreateCashMovement(txCtx, movement); err != nil {
			return err
		}

		// 4. Audit entry
		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"session_id": sessionID.String(),
				"amount":     movement.Amount,
				"reason":     movement.Reason,
			})
			resType := "pos_cash_movement"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     session.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "POS",
				Action:       AuditActionCashIn,
				ResourceType: &resType,
				ResourceID:   &movement.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		// 5. Outbox domain event
		if s.outboxWriter != nil {
			payload := map[string]any{
				"movement_id": movement.ID.String(),
				"session_id":  sessionID.String(),
				"type":        movement.Type,
				"amount":      movement.Amount,
				"reason":      movement.Reason,
				"created_at":  now.Format(time.RFC3339),
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal outbox payload: %w", err)
			}
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      session.TenantID,
				AggregateType: "pos_cash_movement",
				AggregateID:   movement.ID,
				EventType:     EventCashMovementCreated,
				EventVersion:  1,
				Payload:       payloadBytes,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return movement, nil
}

// CashOut performs a mid-shift cash drop/payout with triple-layer over-withdrawal defense.
func (s *posService) CashOut(ctx context.Context, sessionID uuid.UUID, req CashMovementRequest) (*CashMovement, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	actorID := pc.UserID
	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, ErrReasonRequired
	}
	if len(reason) < 3 {
		return nil, ErrReasonTooShort
	}

	now := time.Now().UTC()
	var movement *CashMovement

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// 1. Layer 2: Pessimistic row locking on pos.session
		session, err := s.store.GetSessionByIDForUpdate(txCtx, sessionID)
		if err != nil {
			return err
		}

		if session.Status != SessionStatusOpen && session.Status != SessionStatusActive {
			return ErrInvalidSessionStatus
		}

		currentExpected := float64(0)
		if session.ExpectedCash != nil {
			currentExpected = *session.ExpectedCash
		}

		// 2. Layer 1: Application domain validation
		if currentExpected < req.Amount {
			return ErrInsufficientDrawerCash
		}
		newExpected := currentExpected - req.Amount
		session.ExpectedCash = &newExpected

		// 3. Layer 3: Database check constraint pos_session_expected_cash_check protects during write
		if err := s.store.UpdateSession(txCtx, session, session.Version); err != nil {
			return err
		}

		var refType *string
		if req.Reference != "" {
			ref := "REFERENCE"
			refType = &ref
		}
		movement = &CashMovement{
			ID:            uuid.New(),
			TenantID:      pc.TenantID,
			SessionID:     sessionID,
			Type:          MovementTypeCashOut,
			Amount:        req.Amount,
			Reason:        reason,
			ReferenceType: refType,
			CreatedAt:     now,
			CreatedBy:     uuidPtr(actorID),
		}
		if err := s.store.CreateCashMovement(txCtx, movement); err != nil {
			return err
		}

		// 4. Audit entry
		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"session_id": sessionID.String(),
				"amount":     movement.Amount,
				"reason":     movement.Reason,
			})
			resType := "pos_cash_movement"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     session.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "POS",
				Action:       AuditActionCashOut,
				ResourceType: &resType,
				ResourceID:   &movement.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		// 5. Outbox domain event
		if s.outboxWriter != nil {
			payload := map[string]any{
				"movement_id": movement.ID.String(),
				"session_id":  sessionID.String(),
				"type":        movement.Type,
				"amount":      movement.Amount,
				"reason":      movement.Reason,
				"created_at":  now.Format(time.RFC3339),
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal outbox payload: %w", err)
			}
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      session.TenantID,
				AggregateType: "pos_cash_movement",
				AggregateID:   movement.ID,
				EventType:     EventCashMovementCreated,
				EventVersion:  1,
				Payload:       payloadBytes,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return movement, nil
}

// CloseSession receives a blind cash count, calculates variance, and executes or routes the close transition.
func (s *posService) CloseSession(ctx context.Context, sessionID uuid.UUID, req CloseSessionRequest, callerCanReviewVariance bool) (*Session, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	actorID := pc.UserID
	if req.ClosingCash < 0 {
		return nil, ErrNegativeClosingCash
	}

	now := time.Now().UTC()
	var updatedSession *Session

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// 1. Pessimistic row locking on pos.session
		session, err := s.store.GetSessionByIDForUpdate(txCtx, sessionID)
		if err != nil {
			return err
		}

		if session.Status != SessionStatusOpen && session.Status != SessionStatusActive {
			return ErrInvalidSessionStatus
		}

		currentExpected := float64(0)
		if session.ExpectedCash != nil {
			currentExpected = *session.ExpectedCash
		}

		// 2. Authoritative variance calculation
		variance := req.ClosingCash - currentExpected
		session.ClosingCash = &req.ClosingCash
		session.ActualCash = &req.ClosingCash
		session.Variance = &variance

		var auditAction string
		var outboxEvent string
		isVarianceReview := variance != 0

		if isVarianceReview {
			// Cash variance detected: divert to VARIANCE_REVIEW
			session.Status = SessionStatusVarianceReview
			auditAction = AuditActionSessionVarianceDetected
			outboxEvent = EventSessionVarianceFlagged
		} else {
			// Balanced: close immediately
			session.Status = SessionStatusClosed
			session.ClosedAt = &now
			auditAction = AuditActionSessionClosed
			outboxEvent = EventSessionClosed
		}

		// 3. Update session in DB
		if err := s.store.UpdateSession(txCtx, session, session.Version); err != nil {
			return err
		}

		// 4. Create closing cash movement if closing cash > 0
		if req.ClosingCash > 0 {
			closingMovement := &CashMovement{
				ID:        uuid.New(),
				TenantID:  pc.TenantID,
				SessionID: sessionID,
				Type:      MovementTypeClosing,
				Amount:    req.ClosingCash,
				Reason:    req.Comment,
				CreatedAt: now,
				CreatedBy: uuidPtr(actorID),
			}
			if err := s.store.CreateCashMovement(txCtx, closingMovement); err != nil {
				return err
			}
		}

		// 5. Audit entry
		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"session_id":    sessionID.String(),
				"closing_cash":  req.ClosingCash,
				"expected_cash": currentExpected,
				"variance":      variance,
				"status":        session.Status,
			})
			resType := "pos_session"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     session.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "POS",
				Action:       auditAction,
				ResourceType: &resType,
				ResourceID:   &session.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		// 6. Outbox domain event
		if s.outboxWriter != nil {
			payload := map[string]any{
				"session_id":   session.ID.String(),
				"terminal_id":  session.TerminalID.String(),
				"cashier_id":   session.CashierUserID.String(),
				"closing_cash": req.ClosingCash,
				"status":       session.Status,
				"occurred_at":  now.Format(time.RFC3339),
			}
			if isVarianceReview {
				payload["variance"] = variance
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal outbox payload: %w", err)
			}
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      session.TenantID,
				AggregateType: "pos_session",
				AggregateID:   session.ID,
				EventType:     outboxEvent,
				EventVersion:  1,
				Payload:       payloadBytes,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		updatedSession = session
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Confidentiality: if session entered VARIANCE_REVIEW and caller lacks supervisor privilege,
	// mask expected cash and variance in response (CORR-POS-001).
	if updatedSession.Status == SessionStatusVarianceReview && !callerCanReviewVariance {
		return updatedSession.MaskBlindCountFields(), nil
	}

	return updatedSession, nil
}

// ApproveVariance finalizes a shift in VARIANCE_REVIEW, enforcing Segregation of Duties.
func (s *posService) ApproveVariance(ctx context.Context, sessionID uuid.UUID, req ApproveVarianceRequest) (*Session, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	actorID := pc.UserID
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || len(reason) < 5 {
		return nil, ErrApprovalReasonTooShort
	}

	now := time.Now().UTC()
	var updatedSession *Session

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// 1. Pessimistic row locking on pos.session
		session, err := s.store.GetSessionByIDForUpdate(txCtx, sessionID)
		if err != nil {
			return err
		}

		if session.Status != SessionStatusVarianceReview {
			return ErrInvalidSessionStatus
		}

		// 2. Segregation of Duties (SoD): shift cashier cannot approve their own variance
		if actorID == session.CashierUserID {
			return ErrSoDViolation
		}

		// 3. Transition to CLOSED with variance approval metadata
		session.Status = SessionStatusClosed
		session.ClosedAt = &now
		session.VarianceApprovedBy = &actorID
		session.VarianceApprovedAt = &now
		session.VarianceReason = &reason

		if err := s.store.UpdateSession(txCtx, session, session.Version); err != nil {
			return err
		}

		// 4. Audit entry: POS_SESSION_VARIANCE_APPROVED (Severity: HIGH, requires reason)
		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"session_id":  sessionID.String(),
				"approver_id": actorID.String(),
				"cashier_id":  session.CashierUserID.String(),
				"variance":    session.Variance,
				"reason":      reason,
			})
			resType := "pos_session"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     session.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "POS",
				Action:       AuditActionSessionVarianceApproved,
				ResourceType: &resType,
				ResourceID:   &session.ID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		// 5. Outbox domain event: pos.session.closed.v1
		if s.outboxWriter != nil {
			payload := map[string]any{
				"session_id":           session.ID.String(),
				"terminal_id":          session.TerminalID.String(),
				"cashier_id":           session.CashierUserID.String(),
				"closed_at":            now.Format(time.RFC3339),
				"variance_approved_by": actorID.String(),
				"variance_reason":      reason,
			}
			if session.Variance != nil {
				payload["variance"] = *session.Variance
			}
			if session.ActualCash != nil {
				payload["closing_cash"] = *session.ActualCash
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal outbox payload: %w", err)
			}
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      session.TenantID,
				AggregateType: "pos_session",
				AggregateID:   session.ID,
				EventType:     EventSessionClosed,
				EventVersion:  1,
				Payload:       payloadBytes,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		updatedSession = session
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updatedSession, nil
}

// GetSessionSummary computes session totals, enforcing blind-count confidentiality during active shifts.
func (s *posService) GetSessionSummary(ctx context.Context, sessionID uuid.UUID, callerCanReviewVariance bool) (*SessionSummary, error) {
	var summary *SessionSummary
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		session, err := s.store.GetSessionByID(txCtx, sessionID)
		if err != nil {
			return err
		}

		// CORR-POS-001: Cashiers are forbidden from retrieving session summary during active shift
		// to prevent mathematical derivation of drawer totals (opening_cash + cash_in - cash_out).
		if !callerCanReviewVariance && IsNonTerminalStatus(session.Status) {
			return ErrBlindCountRestricted
		}

		cashIn, cashOut, err := s.store.GetSessionSummaryTotals(txCtx, sessionID)
		if err != nil {
			return err
		}

		summary = &SessionSummary{
			Session:      session,
			SalesTotal:   0,
			PaymentTotal: 0,
			CashIn:       cashIn,
			CashOut:      cashOut,
			RefundTotal:  0,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return summary, nil
}
