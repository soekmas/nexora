// Package pos provides Point of Sale session lifecycle and cash management capabilities.
package pos

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Supported POS session lifecycle statuses.
const (
	SessionStatusOpening        = "OPENING"
	SessionStatusOpen           = "OPEN"
	SessionStatusActive         = "ACTIVE"
	SessionStatusClosing        = "CLOSING"
	SessionStatusClosed         = "CLOSED"
	SessionStatusVarianceReview = "VARIANCE_REVIEW"
)

// Supported cash movement types.
const (
	MovementTypeCashIn     = "CASH_IN"
	MovementTypeCashOut    = "CASH_OUT"
	MovementTypeSale       = "SALE"
	MovementTypeRefund     = "REFUND"
	MovementTypeOpening    = "OPENING"
	MovementTypeClosing    = "CLOSING"
	MovementTypeAdjustment = "ADJUSTMENT"
)

// Canonical domain events matching docs/architecture/12-event-catalog.md.
const (
	EventSessionOpened          = "pos.session.opened.v1"
	EventCashMovementCreated    = "pos.cash.movement_created.v1"
	EventSessionClosed          = "pos.session.closed.v1"
	EventSessionVarianceFlagged = "pos.session.variance_flagged.v1"
)

// Canonical audit actions matching audit.audit_action_catalog (seeded in migration 000010).
const (
	AuditActionSessionOpened           = "POS_SESSION_OPENED"
	AuditActionCashMovementCreated     = "POS_CASH_MOVEMENT_CREATED"
	AuditActionCashIn                  = "POS_CASH_IN"
	AuditActionCashOut                 = "POS_CASH_OUT"
	AuditActionSessionClosing          = "POS_SESSION_CLOSING"
	AuditActionSessionVarianceDetected = "POS_SESSION_VARIANCE_DETECTED"
	AuditActionSessionVarianceApproved = "POS_SESSION_VARIANCE_APPROVED"
	AuditActionSessionClosed           = "POS_SESSION_CLOSED"
	AuditActionSessionForceClosed      = "POS_SESSION_FORCE_CLOSED"
)

// Canonical permissions matching identity.permission.
const (
	PermSessionRead            = "session.read"
	PermSessionOpen            = "session.open"
	PermSessionClose           = "session.close"
	PermSessionVarianceReview  = "session.variance.review"
	PermSessionVarianceApprove = "session.variance.approve"
	PermCashMovementCreate     = "cash_movement.create"
	PermCashMovementApprove    = "cash_movement.approve"
)

// Standard domain errors.
var (
	ErrSessionNotFound          = errors.New("pos session not found")
	ErrTerminalNotFound         = errors.New("terminal not found")
	ErrStoreNotFound            = errors.New("store not found")
	ErrTerminalInactive         = errors.New("terminal is not active")
	ErrStoreInactive            = errors.New("store is not active")
	ErrCashierInactive          = errors.New("cashier account is not active")
	ErrTerminalSessionActive    = errors.New("terminal already has an active session")
	ErrInvalidSessionStatus     = errors.New("invalid session status for operation")
	ErrNegativeOpeningCash      = errors.New("opening cash must be non-negative")
	ErrNegativeClosingCash      = errors.New("closing cash must be non-negative")
	ErrInvalidAmount            = errors.New("amount must be strictly positive")
	ErrInsufficientDrawerCash   = errors.New("cash out exceeds available drawer cash")
	ErrReasonRequired           = errors.New("reason is required")
	ErrReasonTooShort           = errors.New("reason must be at least 3 characters")
	ErrApprovalReasonTooShort   = errors.New("approval reason must be at least 5 characters")
	ErrSessionVersionConflict   = errors.New("session has been modified concurrently")
	ErrSoDViolation             = errors.New("cashiers cannot approve variance for their own shift")
	ErrBlindCountRestricted     = errors.New("session summary is restricted to supervisors during active shift")
	ErrCurrencyRequired         = errors.New("currency is required")
	ErrInvalidCurrency          = errors.New("unsupported currency code")
)

// Session represents an operational POS shift session.
type Session struct {
	ID                 uuid.UUID  `json:"id"`
	TenantID           uuid.UUID  `json:"tenant_id,omitempty"`
	CompanyID          uuid.UUID  `json:"company_id,omitempty"`
	StoreID            uuid.UUID  `json:"store_id,omitempty"`
	TerminalID         uuid.UUID  `json:"terminal_id"`
	CashierUserID      uuid.UUID  `json:"cashier_user_id,omitempty"`
	SessionNumber      string     `json:"session_number,omitempty"`
	Status             string     `json:"status"`
	OpenedAt           time.Time  `json:"opened_at"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
	OpeningCash        float64    `json:"opening_cash"`
	ClosingCash        *float64   `json:"closing_cash,omitempty"`
	ActualCash         *float64   `json:"actual_cash,omitempty"`
	ExpectedCash       *float64   `json:"expected_cash,omitempty"`
	Variance           *float64   `json:"variance,omitempty"`
	Currency           string     `json:"currency,omitempty"`
	VarianceApprovedBy *uuid.UUID `json:"variance_approved_by,omitempty"`
	VarianceApprovedAt *time.Time `json:"variance_approved_at,omitempty"`
	VarianceReason     *string    `json:"variance_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at,omitempty"`
	Version            int64      `json:"version,omitempty"`
}

// CashMovement represents a mid-shift cash adjustment or drop in pos.cash_movement.
type CashMovement struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id,omitempty"`
	SessionID     uuid.UUID  `json:"session_id"`
	Type          string     `json:"type"` // maps to movement_type in DB
	Amount        float64    `json:"amount"`
	Reason        string     `json:"reason,omitempty"`
	ReferenceType *string    `json:"reference_type,omitempty"`
	ReferenceID   *uuid.UUID `json:"reference_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     *uuid.UUID `json:"created_by,omitempty"`
}

// SessionSummary encapsulates key cash totals and metrics for a shift.
type SessionSummary struct {
	Session      *Session `json:"session"`
	SalesTotal   float64  `json:"sales_total"`
	PaymentTotal float64  `json:"payment_total"`
	CashIn       float64  `json:"cash_in"`
	CashOut      float64  `json:"cash_out"`
	RefundTotal  float64  `json:"refund_total"`
}

// OpenSessionRequest defines parameters for opening a new POS shift.
type OpenSessionRequest struct {
	TerminalID  uuid.UUID `json:"terminal_id"`
	OpeningCash float64   `json:"opening_cash"`
	Currency    string    `json:"currency"`
}

// CashMovementRequest defines parameters for cash-in or cash-out operations.
type CashMovementRequest struct {
	Amount    float64 `json:"amount"`
	Reason    string  `json:"reason"`
	Reference string  `json:"reference,omitempty"`
}

// CloseSessionRequest defines parameters for submitting a blind shift close.
type CloseSessionRequest struct {
	ClosingCash float64 `json:"closing_cash"`
	Comment     string  `json:"comment,omitempty"`
}

// ApproveVarianceRequest defines parameters for supervisor variance approval.
type ApproveVarianceRequest struct {
	Reason string `json:"reason"`
}

// IsNonTerminalStatus checks if the given session status represents an active/non-terminal state.
func IsNonTerminalStatus(status string) bool {
	switch status {
	case SessionStatusOpening, SessionStatusOpen, SessionStatusActive, SessionStatusClosing, SessionStatusVarianceReview:
		return true
	default:
		return false
	}
}

// MaskBlindCountFields applies blind-count confidentiality to the session response.
// Sensitive drawer balances (expected_cash, closing_cash, actual_cash, variance) are cleared so cashiers
// cannot infer cash drawer balances during active shifts.
func (s *Session) MaskBlindCountFields() *Session {
	if s == nil {
		return nil
	}
	cp := *s
	cp.ExpectedCash = nil
	cp.ClosingCash = nil
	cp.ActualCash = nil
	cp.Variance = nil
	return &cp
}
