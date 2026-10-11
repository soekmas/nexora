package audit_test

import (
	"errors"
	"testing"

	"example.com/nexora/backend/internal/platform/audit"
)

func TestAuditCatalog_ApprovedActions(t *testing.T) {
	tests := []struct {
		name           string
		action         string
		expectedModule string
		requiresReason bool
	}{
		// Identity & Access
		{"USER_CREATED", "USER_CREATED", "IDENTITY", false},
		{"USER_DISABLED", "USER_DISABLED", "IDENTITY", true},
		{"USER_ROLE_ASSIGNED", "USER_ROLE_ASSIGNED", "IDENTITY", true},
		{"USER_ROLE_CHANGED_baseline_alias", "USER_ROLE_CHANGED", "IDENTITY", true},
		{"PERMISSION_DENIED", "PERMISSION_DENIED", "SECURITY", false},
		{"LOGIN_SUCCESS", "LOGIN_SUCCESS", "SECURITY", false},
		{"LOGIN_FAILURE", "LOGIN_FAILURE", "SECURITY", false},

		// Sales
		{"SALE_CREATED", "SALE_CREATED", "SALES", false},
		{"SALE_COMPLETED", "SALE_COMPLETED", "SALES", false},
		{"SALE_CANCELLED", "SALE_CANCELLED", "SALES", true},
		{"SALE_VOIDED", "SALE_VOIDED", "SALES", true},
		{"SALE_REFUNDED_baseline_alias", "SALE_REFUNDED", "SALES", true},

		// Payment
		{"PAYMENT_CREATED", "PAYMENT_CREATED", "PAYMENT", false},
		{"PAYMENT_COMPLETED", "PAYMENT_COMPLETED", "PAYMENT", false},
		{"PAYMENT_SUCCESS_baseline_alias", "PAYMENT_SUCCESS", "PAYMENT", false},
		{"PAYMENT_FAILED", "PAYMENT_FAILED", "PAYMENT", false},
		{"REFUND_APPROVED", "REFUND_APPROVED", "PAYMENT", true},

		// Inventory
		{"STOCK_ADJUSTED", "STOCK_ADJUSTED", "INVENTORY", true},
		{"STOCK_TRANSFER_CREATED", "STOCK_TRANSFER_CREATED", "INVENTORY", false},
		{"STOCK_TRANSFERRED_baseline_alias", "STOCK_TRANSFERRED", "INVENTORY", false},
		{"STOCK_OPNAME_POSTED", "STOCK_OPNAME_POSTED", "INVENTORY", true},

		// Pricing & Promotion
		{"PRICE_CREATED", "PRICE_CREATED", "PRICING", false},
		{"PRICE_CHANGED_baseline_alias", "PRICE_CHANGED", "PRICING", true},
		{"PROMOTION_APPROVED", "PROMOTION_APPROVED", "PROMOTION", true},

		// Security & Offline
		{"RLS_CONTEXT_MISSING", "RLS_CONTEXT_MISSING", "SECURITY", false},
		{"TENANT_ACCESS_DENIED", "TENANT_ACCESS_DENIED", "SECURITY", false},
		{"IDEMPOTENCY_KEY_REUSED", "IDEMPOTENCY_KEY_REUSED", "PLATFORM", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def, err := audit.ValidateAction(tc.action)
			if err != nil {
				t.Fatalf("expected action %q to be valid, got err: %v", tc.action, err)
			}
			if def.Module != tc.expectedModule {
				t.Errorf("expected module %q, got %q", tc.expectedModule, def.Module)
			}
			if def.RequiresReason != tc.requiresReason {
				t.Errorf("expected requiresReason=%v, got %v", tc.requiresReason, def.RequiresReason)
			}
		})
	}
}

func TestAuditCatalog_InvalidActions(t *testing.T) {
	invalidActions := []string{
		"UNKNOWN_ACTION",
		"UPDATE_STATUS",
		"DELETE_RECORD",
		"HACK_THE_PLANET",
		"",
		"   ",
	}

	for _, act := range invalidActions {
		t.Run("invalid_"+act, func(t *testing.T) {
			_, err := audit.ValidateAction(act)
			if err == nil {
				t.Fatalf("expected error for invalid action %q, got nil", act)
			}
			if !errors.Is(err, audit.ErrInvalidAction) {
				t.Errorf("expected ErrInvalidAction, got %v", err)
			}
		})
	}
}
