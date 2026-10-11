package identity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/postgres"
)

func TestUserAccount_IsLocked(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-5 * time.Minute)
	future := now.Add(15 * time.Minute)

	tests := []struct {
		name     string
		user     UserAccount
		expected bool
	}{
		{
			name: "active user with no lock",
			user: UserAccount{
				Status:      StatusActive,
				LockedUntil: nil,
			},
			expected: false,
		},
		{
			name: "locked user with future locked_until",
			user: UserAccount{
				Status:      StatusLocked,
				LockedUntil: &future,
			},
			expected: true,
		},
		{
			name: "locked user with expired locked_until",
			user: UserAccount{
				Status:      StatusLocked,
				LockedUntil: &past,
			},
			expected: false, // Lockout period has elapsed
		},
		{
			name: "active status but future locked_until timestamp",
			user: UserAccount{
				Status:      StatusActive,
				LockedUntil: &future,
			},
			expected: true,
		},
		{
			name: "locked status without locked_until timestamp",
			user: UserAccount{
				Status:      StatusLocked,
				LockedUntil: nil,
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.user.IsLocked(now)
			if got != tc.expected {
				t.Fatalf("expected IsLocked=%v, got %v", tc.expected, got)
			}
		})
	}
}

func TestDomainService_ScopeValidation(t *testing.T) {
	svc := &DomainService{}
	tenantID := uuid.New()
	actorID := uuid.New()
	targetUserID := uuid.New()
	validID := uuid.New()

	ctx := postgres.WithTenantID(context.Background(), tenantID)

	// Missing foreign key for COMPANY
	_, err := svc.AssignScope(ctx, actorID, targetUserID, AssignScopeRequest{
		ScopeType: ScopeTypeCompany,
		CompanyID: nil,
	})
	if err != ErrInvalidScope {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}

	// Missing foreign key for STORE
	_, err = svc.AssignScope(ctx, actorID, targetUserID, AssignScopeRequest{
		ScopeType: ScopeTypeStore,
		StoreID:   nil,
	})
	if err != ErrInvalidScope {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}

	// Invalid scope type
	_, err = svc.AssignScope(ctx, actorID, targetUserID, AssignScopeRequest{
		ScopeType: ScopeType("INVALID"),
		CompanyID: &validID,
	})
	if err != ErrInvalidScope {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestDomainService_PasswordPolicyValidation(t *testing.T) {
	svc := &DomainService{}
	tenantID := uuid.New()
	ctx := postgres.WithTenantID(context.Background(), tenantID)

	// Password shorter than 8 chars
	_, err := svc.CreateUser(ctx, CreateUserRequest{
		Username:    "alice",
		DisplayName: "Alice",
		Password:    "short",
	})
	if err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}

	// Password change with short password
	err = svc.ChangePassword(ctx, uuid.New(), ChangePasswordRequest{
		NewPassword: "123",
	})
	if err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}
