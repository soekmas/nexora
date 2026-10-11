package audit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
)

func TestValidateLog(t *testing.T) {
	tenantID := uuid.New()
	reason := "Customer requested refund after item return"

	t.Run("valid audit log", func(t *testing.T) {
		log := &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_COMPLETED",
			Status:   audit.StatusSuccess,
			Metadata: []byte(`{"order_id":"ord-123"}`),
		}
		if err := audit.ValidateLog(log); err != nil {
			t.Fatalf("expected valid log, got err: %v", err)
		}
		if log.Module != "SALES" {
			t.Errorf("expected module SALES to be populated from catalog, got %s", log.Module)
		}
	})

	t.Run("missing tenant ID fails", func(t *testing.T) {
		log := &audit.Log{
			Action: "SALE_COMPLETED",
			Status: audit.StatusSuccess,
		}
		err := audit.ValidateLog(log)
		if !errors.Is(err, audit.ErrMissingTenantContext) {
			t.Errorf("expected ErrMissingTenantContext, got %v", err)
		}
	})

	t.Run("requires reason failure", func(t *testing.T) {
		log := &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_VOIDED", // Requires reason
			Status:   audit.StatusSuccess,
		}
		err := audit.ValidateLog(log)
		if !errors.Is(err, audit.ErrReasonRequired) {
			t.Errorf("expected ErrReasonRequired, got %v", err)
		}
	})

	t.Run("requires reason success with reason", func(t *testing.T) {
		log := &audit.Log{
			TenantID: tenantID,
			Action:   "SALE_VOIDED",
			Status:   audit.StatusSuccess,
			Reason:   &reason,
		}
		if err := audit.ValidateLog(log); err != nil {
			t.Fatalf("expected valid log with reason, got: %v", err)
		}
	})

	t.Run("status normalization and rejection", func(t *testing.T) {
		log := &audit.Log{
			TenantID: tenantID,
			Action:   "LOGIN_FAILURE",
			Status:   "FAILURE", // Should normalize to FAILED
		}
		if err := audit.ValidateLog(log); err != nil {
			t.Fatalf("expected FAILURE to normalize to FAILED, got err: %v", err)
		}
		if log.Status != audit.StatusFailed {
			t.Errorf("expected status FAILED, got %s", log.Status)
		}

		// Invalid status
		log.Status = "INVALID_STATUS"
		err := audit.ValidateLog(log)
		if !errors.Is(err, audit.ErrInvalidStatus) {
			t.Errorf("expected ErrInvalidStatus, got %v", err)
		}
	})

	t.Run("field length bounds", func(t *testing.T) {
		longStr := strings.Repeat("A", 105)
		log := &audit.Log{
			TenantID:     tenantID,
			Action:       "SALE_COMPLETED",
			Status:       audit.StatusSuccess,
			ResourceType: &longStr,
		}
		err := audit.ValidateLog(log)
		if !errors.Is(err, audit.ErrFieldLengthExceeded) {
			t.Errorf("expected ErrFieldLengthExceeded, got %v", err)
		}
	})
}

func TestValidateDataChange(t *testing.T) {
	rowID := uuid.New()
	change := &audit.DataChange{
		TableSchema: "sales",
		TableName:   "sales_order",
		RowID:       &rowID,
		FieldName:   "status",
		OldValue:    []byte(`{"status":"PENDING"}`),
		NewValue:    []byte(`{"status":"COMPLETED"}`),
	}

	if err := audit.ValidateDataChange(change); err != nil {
		t.Fatalf("expected valid data change, got: %v", err)
	}

	// Invalid empty schema
	change.TableSchema = ""
	if err := audit.ValidateDataChange(change); err == nil {
		t.Errorf("expected error for empty table schema, got nil")
	}
}

func TestValidateSecurityEvent(t *testing.T) {
	evt := &audit.SecurityEvent{
		EventType: "RATE_LIMIT_TRIGGERED",
		Severity:  "MEDIUM",
		Metadata:  []byte(`{"endpoint":"/auth/login"}`),
	}
	if err := audit.ValidateSecurityEvent(evt); err != nil {
		t.Fatalf("expected valid security event, got: %v", err)
	}

	// Invalid severity
	evt.Severity = "CRAZY_HIGH"
	if err := audit.ValidateSecurityEvent(evt); !errors.Is(err, audit.ErrInvalidSeverity) {
		t.Errorf("expected ErrInvalidSeverity, got %v", err)
	}
}

func TestValidateLoginHistory(t *testing.T) {
	lh := &audit.LoginHistory{
		Username: "cashier1",
		Success:  true,
	}
	if err := audit.ValidateLoginHistory(lh); err != nil {
		t.Fatalf("expected valid login history, got: %v", err)
	}

	lh.Username = ""
	if err := audit.ValidateLoginHistory(lh); err == nil {
		t.Errorf("expected error for empty username, got nil")
	}
}
