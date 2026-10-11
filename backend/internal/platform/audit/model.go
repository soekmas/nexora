package audit

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Audit log status constants matching database CHECK constraint:
// CHECK (status IN ('SUCCESS','FAILED','DENIED'))
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
	StatusDenied  = "DENIED"
)

// Security event severity constants matching database CHECK constraint:
// CHECK (severity IN ('INFO','LOW','MEDIUM','HIGH','CRITICAL'))
const (
	SecuritySeverityInfo     = "INFO"
	SecuritySeverityLow      = "LOW"
	SecuritySeverityMedium   = "MEDIUM"
	SecuritySeverityHigh     = "HIGH"
	SecuritySeverityCritical = "CRITICAL"
)

// Log represents a canonical audit record matching audit.audit_log schema.
type Log struct {
	ID             int64      `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	CompanyID      *uuid.UUID `json:"company_id,omitempty"`
	StoreID        *uuid.UUID `json:"store_id,omitempty"`
	TerminalID     *uuid.UUID `json:"terminal_id,omitempty"`
	UserID         *uuid.UUID `json:"user_id,omitempty"`
	Module         string     `json:"module"`
	Action         string     `json:"action"`
	ResourceType   *string    `json:"resource_type,omitempty"`
	ResourceID     *uuid.UUID `json:"resource_id,omitempty"`
	BusinessNumber *string    `json:"business_number,omitempty"`
	Status         string     `json:"status"`
	RequestID      *uuid.UUID `json:"request_id,omitempty"`
	TraceID        *string    `json:"trace_id,omitempty"`
	IPAddress      *string    `json:"ip_address,omitempty"`
	UserAgent      *string    `json:"user_agent,omitempty"`
	Reason         *string    `json:"reason,omitempty"`
	Metadata       []byte     `json:"metadata"`
	CreatedAt      time.Time  `json:"created_at"`
}

// DataChange represents a field-level modification record matching audit.data_change schema.
type DataChange struct {
	ID          int64      `json:"id"`
	AuditLogID  int64      `json:"audit_log_id"`
	TableSchema string     `json:"table_schema"`
	TableName   string     `json:"table_name"`
	RowID       *uuid.UUID `json:"row_id,omitempty"`
	FieldName   string     `json:"field_name"`
	OldValue    []byte     `json:"old_value,omitempty"`
	NewValue    []byte     `json:"new_value,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SecurityEvent represents a security telemetry record matching audit.security_event schema.
type SecurityEvent struct {
	ID         int64      `json:"id"`
	TenantID   *uuid.UUID `json:"tenant_id,omitempty"`
	UserID     *uuid.UUID `json:"user_id,omitempty"`
	EventType  string     `json:"event_type"`
	Severity   string     `json:"severity"`
	IPAddress  *string    `json:"ip_address,omitempty"`
	UserAgent  *string    `json:"user_agent,omitempty"`
	RequestID  *uuid.UUID `json:"request_id,omitempty"`
	TraceID    *string    `json:"trace_id,omitempty"`
	Metadata   []byte     `json:"metadata"`
	OccurredAt time.Time  `json:"occurred_at"`
}

// LoginHistory represents an authentication attempt record matching audit.login_history schema.
type LoginHistory struct {
	ID            int64      `json:"id"`
	TenantID      *uuid.UUID `json:"tenant_id,omitempty"`
	UserID        *uuid.UUID `json:"user_id,omitempty"`
	Username      string     `json:"username"`
	Success       bool       `json:"success"`
	FailureReason *string    `json:"failure_reason,omitempty"`
	IPAddress     *string    `json:"ip_address,omitempty"`
	UserAgent     *string    `json:"user_agent,omitempty"`
	OccurredAt    time.Time  `json:"occurred_at"`
}

// Filter specifies criteria for tenant-scoped audit querying with bounded pagination.
type Filter struct {
	TenantID     uuid.UUID
	UserID       *uuid.UUID
	Module       *string
	Action       *string
	ResourceType *string
	ResourceID   *uuid.UUID
	Status       *string
	From         *time.Time
	To           *time.Time
	Limit        int
	Offset       int
}

// ValidateLog validates a Log record against the approved catalog and schema constraints.
func ValidateLog(log *Log) error {
	if log == nil {
		return fmt.Errorf("audit log cannot be nil")
	}

	if log.TenantID == uuid.Nil {
		return ErrMissingTenantContext
	}

	// Module length validation (varchar(50))
	if len(log.Module) > 50 {
		return fmt.Errorf("%w: module exceeds 50 characters", ErrFieldLengthExceeded)
	}

	// Action validation against catalog
	actionDef, err := ValidateAction(log.Action)
	if err != nil {
		return err
	}

	// If Module not specified, derive from catalog ActionDef
	if log.Module == "" {
		log.Module = actionDef.Module
	}

	// Status validation (map FAILURE to FAILED for catalog compatibility)
	normStatus := strings.ToUpper(strings.TrimSpace(log.Status))
	if normStatus == "FAILURE" {
		normStatus = StatusFailed
	}
	if normStatus == "" {
		normStatus = StatusSuccess
	}
	if normStatus != StatusSuccess && normStatus != StatusFailed && normStatus != StatusDenied {
		return fmt.Errorf("%w: '%s'", ErrInvalidStatus, log.Status)
	}
	log.Status = normStatus

	// Reason requirement check
	if actionDef.RequiresReason {
		if log.Reason == nil || strings.TrimSpace(*log.Reason) == "" {
			return fmt.Errorf("%w for action %s", ErrReasonRequired, actionDef.Code)
		}
	}

	// String length bounds
	if log.ResourceType != nil && len(*log.ResourceType) > 100 {
		return fmt.Errorf("%w: resource_type exceeds 100 characters", ErrFieldLengthExceeded)
	}
	if log.BusinessNumber != nil && len(*log.BusinessNumber) > 100 {
		return fmt.Errorf("%w: business_number exceeds 100 characters", ErrFieldLengthExceeded)
	}
	if log.TraceID != nil && len(*log.TraceID) > 100 {
		return fmt.Errorf("%w: trace_id exceeds 100 characters", ErrFieldLengthExceeded)
	}

	// IP address validation if present
	if log.IPAddress != nil && strings.TrimSpace(*log.IPAddress) != "" {
		if net.ParseIP(strings.TrimSpace(*log.IPAddress)) == nil {
			return fmt.Errorf("audit: invalid IP address format")
		}
	}

	// Metadata sanitization and scrubbing
	scrubbed, err := ScrubMetadata(log.Metadata)
	if err != nil {
		return err
	}
	log.Metadata = scrubbed

	return nil
}

// ValidateDataChange validates a DataChange entry against schema bounds.
func ValidateDataChange(change *DataChange) error {
	if change == nil {
		return fmt.Errorf("data change cannot be nil")
	}
	if strings.TrimSpace(change.TableSchema) == "" || len(change.TableSchema) > 100 {
		return fmt.Errorf("%w: invalid table_schema", ErrFieldLengthExceeded)
	}
	if strings.TrimSpace(change.TableName) == "" || len(change.TableName) > 100 {
		return fmt.Errorf("%w: invalid table_name", ErrFieldLengthExceeded)
	}
	if strings.TrimSpace(change.FieldName) == "" || len(change.FieldName) > 150 {
		return fmt.Errorf("%w: invalid field_name", ErrFieldLengthExceeded)
	}

	// Scrub OldValue and NewValue if provided
	if len(change.OldValue) > 0 {
		scrubbed, err := ScrubMetadata(change.OldValue)
		if err != nil {
			return fmt.Errorf("old_value: %w", err)
		}
		change.OldValue = scrubbed
	}
	if len(change.NewValue) > 0 {
		scrubbed, err := ScrubMetadata(change.NewValue)
		if err != nil {
			return fmt.Errorf("new_value: %w", err)
		}
		change.NewValue = scrubbed
	}

	return nil
}

// ValidateSecurityEvent validates a SecurityEvent entry.
func ValidateSecurityEvent(evt *SecurityEvent) error {
	if evt == nil {
		return fmt.Errorf("security event cannot be nil")
	}
	if strings.TrimSpace(evt.EventType) == "" || len(evt.EventType) > 80 {
		return fmt.Errorf("%w: invalid event_type", ErrFieldLengthExceeded)
	}

	normSev := strings.ToUpper(strings.TrimSpace(evt.Severity))
	if normSev == "" {
		normSev = SecuritySeverityInfo
	}
	switch normSev {
	case SecuritySeverityInfo, SecuritySeverityLow, SecuritySeverityMedium, SecuritySeverityHigh, SecuritySeverityCritical:
		evt.Severity = normSev
	default:
		return fmt.Errorf("%w: '%s'", ErrInvalidSeverity, evt.Severity)
	}

	if evt.TraceID != nil && len(*evt.TraceID) > 100 {
		return fmt.Errorf("%w: trace_id exceeds 100 characters", ErrFieldLengthExceeded)
	}

	if evt.IPAddress != nil && strings.TrimSpace(*evt.IPAddress) != "" {
		if net.ParseIP(strings.TrimSpace(*evt.IPAddress)) == nil {
			return fmt.Errorf("audit: invalid IP address format")
		}
	}

	scrubbed, err := ScrubMetadata(evt.Metadata)
	if err != nil {
		return err
	}
	evt.Metadata = scrubbed

	return nil
}

// ValidateLoginHistory validates a LoginHistory entry.
func ValidateLoginHistory(lh *LoginHistory) error {
	if lh == nil {
		return fmt.Errorf("login history cannot be nil")
	}
	if strings.TrimSpace(lh.Username) == "" {
		return fmt.Errorf("login history username cannot be empty")
	}
	if lh.FailureReason != nil && len(*lh.FailureReason) > 100 {
		return fmt.Errorf("%w: failure_reason exceeds 100 characters", ErrFieldLengthExceeded)
	}
	if lh.IPAddress != nil && strings.TrimSpace(*lh.IPAddress) != "" {
		if net.ParseIP(strings.TrimSpace(*lh.IPAddress)) == nil {
			return fmt.Errorf("audit: invalid IP address format")
		}
	}
	return nil
}
