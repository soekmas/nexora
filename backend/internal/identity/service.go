package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// CreateUserRequest models user account creation inputs.
type CreateUserRequest struct {
	Username    string     `json:"username"`
	Email       *string    `json:"email,omitempty"`
	Password    string     `json:"password"`
	DisplayName string     `json:"display_name"`
	Status      UserStatus `json:"status,omitempty"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
}

// UpdateUserRequest models updating mutable user properties.
type UpdateUserRequest struct {
	DisplayName *string     `json:"display_name,omitempty"`
	Email       *string     `json:"email,omitempty"`
	Status      *UserStatus `json:"status,omitempty"`
	UpdatedBy   *uuid.UUID  `json:"updated_by,omitempty"`
}

// LoginRequest models authentication inputs.
type LoginRequest struct {
	Username  string  `json:"username"`
	Password  string  `json:"password"`
	IPAddress *string `json:"ip_address,omitempty"`
	UserAgent *string `json:"user_agent,omitempty"`
}

// ChangePasswordRequest models password change operations.
type ChangePasswordRequest struct {
	OldPassword string     `json:"old_password"`
	NewPassword string     `json:"new_password"`
	UpdatedBy   *uuid.UUID `json:"updated_by,omitempty"`
}

// AssignScopeRequest models binding a scope to a user.
type AssignScopeRequest struct {
	ScopeType   ScopeType  `json:"scope_type"`
	CompanyID   *uuid.UUID `json:"company_id,omitempty"`
	RegionID    *uuid.UUID `json:"region_id,omitempty"`
	StoreID     *uuid.UUID `json:"store_id,omitempty"`
	WarehouseID *uuid.UUID `json:"warehouse_id,omitempty"`
	TerminalID  *uuid.UUID `json:"terminal_id,omitempty"`
}

// Service provides identity and access control domain capabilities.
type Service interface {
	CreateUser(ctx context.Context, req CreateUserRequest) (*UserAccount, error)
	GetUser(ctx context.Context, id uuid.UUID) (*UserWithDetails, error)
	UpdateUser(ctx context.Context, id uuid.UUID, req UpdateUserRequest) (*UserAccount, error)
	ListUsers(ctx context.Context, limit, offset int) ([]UserAccount, int64, error)
	AuthenticateUser(ctx context.Context, req LoginRequest) (*UserWithDetails, error)
	ChangePassword(ctx context.Context, id uuid.UUID, req ChangePasswordRequest) error

	ListRoles(ctx context.Context) ([]Role, error)
	AssignRole(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error
	RevokeRole(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error

	ListPermissions(ctx context.Context) ([]Permission, error)

	AssignScope(ctx context.Context, actorID, targetUserID uuid.UUID, req AssignScopeRequest) (*UserScope, error)
	RevokeScope(ctx context.Context, actorID, scopeID uuid.UUID) error
	GetUserScopes(ctx context.Context, targetUserID uuid.UUID) ([]UserScope, error)
}

// DomainService implements Service.
type DomainService struct {
	store        Store
	hasher       PasswordHasher
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	logger       *slog.Logger
}

// NewService constructs a new DomainService.
func NewService(
	store Store,
	hasher PasswordHasher,
	txManager postgres.TxManager,
	auditStore audit.Store,
	outboxWriter outbox.Writer,
	logger *slog.Logger,
) *DomainService {
	if hasher == nil {
		hasher = NewArgon2idHasher()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DomainService{
		store:        store,
		hasher:       hasher,
		txManager:    txManager,
		auditStore:   auditStore,
		outboxWriter: outboxWriter,
		logger:       logger,
	}
}

// CreateUser creates a new user account with Argon2id password hashing, audit logging, and outbox event dispatch.
func (s *DomainService) CreateUser(ctx context.Context, req CreateUserRequest) (*UserAccount, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		return nil, errors.New("username is required")
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		return nil, errors.New("display_name is required")
	}
	if len(req.Password) < 8 {
		return nil, ErrInvalidPassword
	}

	status := req.Status
	if status == "" {
		status = StatusActive
	}

	hash, err := s.hasher.HashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &UserAccount{
		ID:               uuid.New(),
		TenantID:         pc.TenantID,
		Username:         req.Username,
		Email:            req.Email,
		PasswordHash:     &hash,
		DisplayName:      req.DisplayName,
		Status:           status,
		FailedLoginCount: 0,
		CreatedBy:        req.CreatedBy,
		Version:          1,
	}

	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreateUser(txCtx, user); err != nil {
			return err
		}

		// Audit logging: USER_CREATED (Severity NOTICE, after-state captured)
		if s.auditStore != nil {
			metaBytes, _ := json.Marshal(map[string]any{
				"username":     user.Username,
				"display_name": user.DisplayName,
				"status":       user.Status,
			})
			resourceType := "user_account"
			auditLog := &audit.Log{
				TenantID:     user.TenantID,
				UserID:       req.CreatedBy,
				Module:       "identity",
				Action:       AuditActionUserCreated,
				ResourceType: &resourceType,
				ResourceID:   &user.ID,
				Status:       audit.StatusSuccess,
				Metadata:     metaBytes,
				CreatedAt:    time.Now().UTC(),
			}
			if err := s.auditStore.Record(txCtx, auditLog, nil); err != nil {
				return fmt.Errorf("audit user created: %w", err)
			}
		}

		// Transactional outbox event: identity.user.created.v1
		if s.outboxWriter != nil {
			payload := map[string]any{
				"user_id":      user.ID,
				"tenant_id":    user.TenantID,
				"username":     user.Username,
				"display_name": user.DisplayName,
				"status":       user.Status,
				"created_at":   user.CreatedAt,
			}
			evt, err := outbox.NewEvent(user.TenantID, "user_account", user.ID, EventUserCreated, 1, payload)
			if err != nil {
				return fmt.Errorf("new outbox event: %w", err)
			}
			if err := s.outboxWriter.Write(txCtx, evt); err != nil {
				return fmt.Errorf("write outbox event: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetUser returns user details including assigned roles, permissions, and organizational scopes.
func (s *DomainService) GetUser(ctx context.Context, id uuid.UUID) (*UserWithDetails, error) {
	var details *UserWithDetails
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		user, err := s.store.GetUserByID(txCtx, id)
		if err != nil {
			return err
		}

		roles, err := s.store.GetUserRoles(txCtx, id)
		if err != nil {
			return err
		}

		perms, err := s.store.GetUserPermissions(txCtx, id)
		if err != nil {
			return err
		}

		scopes, err := s.store.GetUserScopes(txCtx, id)
		if err != nil {
			return err
		}

		details = &UserWithDetails{
			User:        *user,
			Roles:       roles,
			Permissions: perms,
			Scopes:      scopes,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return details, nil
}

// UpdateUser modifies user attributes with optimistic concurrency and audit logging.
func (s *DomainService) UpdateUser(ctx context.Context, id uuid.UUID, req UpdateUserRequest) (*UserAccount, error) {
	var updatedUser *UserAccount
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		user, err := s.store.GetUserByID(txCtx, id)
		if err != nil {
			return err
		}

		beforeStatus := user.Status
		if req.DisplayName != nil && *req.DisplayName != "" {
			user.DisplayName = *req.DisplayName
		}
		if req.Email != nil {
			user.Email = req.Email
		}
		if req.Status != nil {
			user.Status = *req.Status
		}
		user.UpdatedBy = req.UpdatedBy

		if err := s.store.UpdateUser(txCtx, user); err != nil {
			return err
		}
		updatedUser = user

		if s.auditStore != nil {
			action := AuditActionUserUpdated
			if beforeStatus != user.Status {
				switch user.Status {
				case StatusDisabled:
					action = AuditActionUserDisabled
				case StatusActive:
					action = AuditActionUserEnabled
				}
			}

			resourceType := "user_account"
			auditLog := &audit.Log{
				TenantID:     user.TenantID,
				UserID:       req.UpdatedBy,
				Module:       "identity",
				Action:       action,
				ResourceType: &resourceType,
				ResourceID:   &user.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}
			if err := s.auditStore.Record(txCtx, auditLog, nil); err != nil {
				return fmt.Errorf("audit user update: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updatedUser, nil
}

// ListUsers lists user accounts with pagination.
func (s *DomainService) ListUsers(ctx context.Context, limit, offset int) ([]UserAccount, int64, error) {
	var users []UserAccount
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		users, total, err = s.store.ListUsers(txCtx, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// AuthenticateUser executes the canonical login flow enforcing the 5-failure 15-minute lockout policy.
func (s *DomainService) AuthenticateUser(ctx context.Context, req LoginRequest) (*UserWithDetails, error) {
	pc, _ := postgres.FromContext(ctx)
	tenantID := pc.TenantID
	now := time.Now().UTC()

	var user *UserAccount
	var lookupErr error
	_ = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		u, err := s.store.GetUserByUsername(txCtx, req.Username)
		if err != nil {
			lookupErr = err
			return nil
		}
		user = u
		return nil
	})

	if lookupErr != nil || user == nil {
		if lookupErr != nil && !errors.Is(lookupErr, ErrUserNotFound) {
			return nil, lookupErr
		}
		// Record failed attempt for nonexistent username if tenant context available
		if tenantID != uuid.Nil {
			failReason := "USER_NOT_FOUND"
			_ = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
				return s.store.RecordLoginAttempt(txCtx, &LoginAttempt{
					TenantID:      &tenantID,
					Username:      req.Username,
					Success:       false,
					FailureReason: &failReason,
					IPAddress:     req.IPAddress,
					UserAgent:     req.UserAgent,
					OccurredAt:    now,
				})
			})
		}
		return nil, ErrInvalidCredentials
	}

	// If account is disabled or pending, immediately reject without touching lockout counter
	if user.Status == StatusDisabled {
		failReason := "ACCOUNT_DISABLED"
		_ = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
			return s.store.RecordLoginAttempt(txCtx, &LoginAttempt{
				TenantID:      &user.TenantID,
				UserID:        &user.ID,
				Username:      user.Username,
				Success:       false,
				FailureReason: &failReason,
				IPAddress:     req.IPAddress,
				UserAgent:     req.UserAgent,
				OccurredAt:    now,
			})
		})
		return nil, ErrAccountDisabled
	}
	if user.Status == StatusPending {
		failReason := "ACCOUNT_PENDING"
		_ = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
			return s.store.RecordLoginAttempt(txCtx, &LoginAttempt{
				TenantID:      &user.TenantID,
				UserID:        &user.ID,
				Username:      user.Username,
				Success:       false,
				FailureReason: &failReason,
				IPAddress:     req.IPAddress,
				UserAgent:     req.UserAgent,
				OccurredAt:    now,
			})
		})
		return nil, ErrAccountPending
	}

	// Check if account is currently locked
	if user.IsLocked(now) {
		failReason := "ACCOUNT_LOCKED"
		_ = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
			return s.store.RecordLoginAttempt(txCtx, &LoginAttempt{
				TenantID:      &user.TenantID,
				UserID:        &user.ID,
				Username:      user.Username,
				Success:       false,
				FailureReason: &failReason,
				IPAddress:     req.IPAddress,
				UserAgent:     req.UserAgent,
				OccurredAt:    now,
			})
		})
		return nil, ErrAccountLocked
	}

	// Verify password hash
	if user.PasswordHash == nil {
		return nil, ErrInvalidCredentials
	}
	passwordMatch, err := s.hasher.VerifyPassword(req.Password, *user.PasswordHash)
	if err != nil {
		s.logger.Error("password verification error", slog.String("error", err.Error()), slog.String("user_id", user.ID.String()))
		return nil, ErrInvalidCredentials
	}

	if !passwordMatch {
		// Failed authentication: increment counter within transaction
		var newCount int
		var isLocked bool
		lockErr := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
			lockUntil := now.Add(LockoutDuration)
			var err error
			newCount, isLocked, err = s.store.UpdateLoginFailure(txCtx, user.ID, &lockUntil)
			if err != nil {
				return err
			}

			failReason := "INVALID_CREDENTIALS"
			if isLocked {
				failReason = "ACCOUNT_LOCKED_THRESHOLD"
			}

			_ = s.store.RecordLoginAttempt(txCtx, &LoginAttempt{
				TenantID:      &user.TenantID,
				UserID:        &user.ID,
				Username:      user.Username,
				Success:       false,
				FailureReason: &failReason,
				IPAddress:     req.IPAddress,
				UserAgent:     req.UserAgent,
				OccurredAt:    now,
			})

			if s.auditStore != nil {
				action := AuditActionLoginFailure
				resourceType := "user_account"
				_ = s.auditStore.Record(txCtx, &audit.Log{
					TenantID:     user.TenantID,
					UserID:       &user.ID,
					Module:       "identity",
					Action:       action,
					ResourceType: &resourceType,
					ResourceID:   &user.ID,
					Status:       audit.StatusFailed,
					IPAddress:    req.IPAddress,
					UserAgent:    req.UserAgent,
					CreatedAt:    now,
				}, nil)

				if isLocked {
					reason := "consecutive failed login threshold reached (5 attempts)"
					_ = s.auditStore.Record(txCtx, &audit.Log{
						TenantID:     user.TenantID,
						UserID:       &user.ID,
						Module:       "identity",
						Action:       AuditActionUserLocked,
						ResourceType: &resourceType,
						ResourceID:   &user.ID,
						Status:       audit.StatusSuccess,
						Reason:       &reason,
						CreatedAt:    now,
					}, nil)
				}
			}

			if isLocked && s.outboxWriter != nil {
				payload := map[string]any{
					"user_id":            user.ID,
					"tenant_id":          user.TenantID,
					"failed_login_count": newCount,
					"locked_until":       lockUntil,
				}
				if evt, err := outbox.NewEvent(user.TenantID, "user_account", user.ID, EventUserLocked, 1, payload); err == nil {
					_ = s.outboxWriter.Write(txCtx, evt)
				}
			}

			return nil
		})

		if lockErr != nil {
			s.logger.Error("failed updating login failure counter", slog.String("error", lockErr.Error()))
		}

		if isLocked {
			return nil, ErrAccountLocked
		}
		return nil, ErrInvalidCredentials
	}

	// Successful authentication: reset failed count and record login
	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.UpdateLoginSuccess(txCtx, user.ID, now); err != nil {
			return err
		}

		_ = s.store.RecordLoginAttempt(txCtx, &LoginAttempt{
			TenantID:   &user.TenantID,
			UserID:     &user.ID,
			Username:   user.Username,
			Success:    true,
			IPAddress:  req.IPAddress,
			UserAgent:  req.UserAgent,
			OccurredAt: now,
		})

		if s.auditStore != nil {
			resourceType := "user_account"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     user.TenantID,
				UserID:       &user.ID,
				Module:       "identity",
				Action:       AuditActionLoginSuccess,
				ResourceType: &resourceType,
				ResourceID:   &user.ID,
				Status:       audit.StatusSuccess,
				IPAddress:    req.IPAddress,
				UserAgent:    req.UserAgent,
				CreatedAt:    now,
			}, nil)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("record login success: %w", err)
	}

	return s.GetUser(ctx, user.ID)
}

// ChangePassword updates a user's password after verifying the old password.
func (s *DomainService) ChangePassword(ctx context.Context, id uuid.UUID, req ChangePasswordRequest) error {
	if len(req.NewPassword) < 8 {
		return ErrInvalidPassword
	}

	return s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		user, err := s.store.GetUserByID(txCtx, id)
		if err != nil {
			return err
		}

		if user.PasswordHash != nil && req.OldPassword != "" {
			valid, err := s.hasher.VerifyPassword(req.OldPassword, *user.PasswordHash)
			if err != nil || !valid {
				return ErrInvalidCredentials
			}
		}

		newHash, err := s.hasher.HashPassword(req.NewPassword)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}

		user.PasswordHash = &newHash
		user.UpdatedBy = req.UpdatedBy

		if err := s.store.UpdateUser(txCtx, user); err != nil {
			return err
		}

		if s.auditStore != nil {
			resourceType := "user_account"
			reason := "user changed password"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     user.TenantID,
				UserID:       req.UpdatedBy,
				Module:       "identity",
				Action:       AuditActionPasswordReset,
				ResourceType: &resourceType,
				ResourceID:   &user.ID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		return nil
	})
}

// ListRoles queries tenant roles.
func (s *DomainService) ListRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		roles, err = s.store.ListRoles(txCtx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// AssignRole assigns a role to a user, checking segregation-of-duties rules.
func (s *DomainService) AssignRole(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error {
	return s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		role, err := s.store.GetRoleByID(txCtx, roleID)
		if err != nil {
			return err
		}

		// Segregation of Duties: Actor cannot grant administrative roles to themselves
		if actorID != uuid.Nil && actorID == targetUserID {
			codeUpper := strings.ToUpper(role.Code)
			if strings.Contains(codeUpper, "ADMIN") || strings.Contains(codeUpper, "SUPER") {
				return ErrSelfRoleAssignmentDenied
			}
		}

		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorPtr = &actorID
		}

		if err := s.store.AssignUserRole(txCtx, targetUserID, roleID, actorPtr); err != nil {
			return err
		}

		if s.auditStore != nil {
			resourceType := "user_role"
			reason := fmt.Sprintf("assigned role %s to user", role.Code)
			meta, _ := json.Marshal(map[string]any{
				"role_id":   role.ID,
				"role_code": role.Code,
				"user_id":   targetUserID,
			})
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     role.TenantID,
				UserID:       actorPtr,
				Module:       "identity",
				Action:       AuditActionUserRoleAssigned,
				ResourceType: &resourceType,
				ResourceID:   &targetUserID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"user_id":     targetUserID,
				"role_id":     role.ID,
				"role_code":   role.Code,
				"assigned_by": actorID,
			}
			if evt, err := outbox.NewEvent(role.TenantID, "user_account", targetUserID, EventRoleAssigned, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})
}

// RevokeRole revokes a role from a user.
func (s *DomainService) RevokeRole(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error {
	return s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		role, err := s.store.GetRoleByID(txCtx, roleID)
		if err != nil {
			return err
		}

		if err := s.store.RevokeUserRole(txCtx, targetUserID, roleID); err != nil {
			return err
		}

		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorPtr = &actorID
		}

		if s.auditStore != nil {
			resourceType := "user_role"
			reason := fmt.Sprintf("revoked role %s from user", role.Code)
			meta, _ := json.Marshal(map[string]any{
				"role_id":   role.ID,
				"role_code": role.Code,
				"user_id":   targetUserID,
			})
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     role.TenantID,
				UserID:       actorPtr,
				Module:       "identity",
				Action:       AuditActionUserRoleRevoked,
				ResourceType: &resourceType,
				ResourceID:   &targetUserID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"user_id":    targetUserID,
				"role_id":    role.ID,
				"role_code":  role.Code,
				"revoked_by": actorID,
			}
			if evt, err := outbox.NewEvent(role.TenantID, "user_account", targetUserID, EventRoleRevoked, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})
}

// ListPermissions queries all catalog permissions.
func (s *DomainService) ListPermissions(ctx context.Context) ([]Permission, error) {
	return s.store.ListPermissions(ctx)
}

// AssignScope binds an organizational scope to a user.
func (s *DomainService) AssignScope(ctx context.Context, actorID, targetUserID uuid.UUID, req AssignScopeRequest) (*UserScope, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	// Validate required entity reference per scope type
	switch req.ScopeType {
	case ScopeTypeCompany:
		if req.CompanyID == nil || *req.CompanyID == uuid.Nil {
			return nil, ErrInvalidScope
		}
	case ScopeTypeRegion:
		if req.RegionID == nil || *req.RegionID == uuid.Nil {
			return nil, ErrInvalidScope
		}
	case ScopeTypeStore:
		if req.StoreID == nil || *req.StoreID == uuid.Nil {
			return nil, ErrInvalidScope
		}
	case ScopeTypeWarehouse:
		if req.WarehouseID == nil || *req.WarehouseID == uuid.Nil {
			return nil, ErrInvalidScope
		}
	case ScopeTypeTerminal:
		if req.TerminalID == nil || *req.TerminalID == uuid.Nil {
			return nil, ErrInvalidScope
		}
	default:
		return nil, ErrInvalidScope
	}

	scope := &UserScope{
		ID:          uuid.New(),
		TenantID:    pc.TenantID,
		UserID:      targetUserID,
		CompanyID:   req.CompanyID,
		RegionID:    req.RegionID,
		StoreID:     req.StoreID,
		WarehouseID: req.WarehouseID,
		TerminalID:  req.TerminalID,
		ScopeType:   req.ScopeType,
		CreatedAt:   time.Now().UTC(),
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.AssignUserScope(txCtx, scope); err != nil {
			return err
		}

		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorPtr = &actorID
		}

		if s.auditStore != nil {
			resourceType := "user_scope"
			reason := fmt.Sprintf("bound scope %s to user", req.ScopeType)
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     pc.TenantID,
				UserID:       actorPtr,
				Module:       "identity",
				Action:       AuditActionUserScopeChanged,
				ResourceType: &resourceType,
				ResourceID:   &targetUserID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"scope_id":   scope.ID,
				"user_id":    targetUserID,
				"scope_type": scope.ScopeType,
			}
			if evt, err := outbox.NewEvent(pc.TenantID, "user_scope", scope.ID, EventScopeAssigned, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return scope, nil
}

// RevokeScope deletes an existing organizational scope binding.
func (s *DomainService) RevokeScope(ctx context.Context, actorID, scopeID uuid.UUID) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	return s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.RevokeUserScope(txCtx, scopeID); err != nil {
			return err
		}

		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorPtr = &actorID
		}

		if s.auditStore != nil {
			resourceType := "user_scope"
			reason := "revoked user scope binding"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     pc.TenantID,
				UserID:       actorPtr,
				Module:       "identity",
				Action:       AuditActionUserScopeChanged,
				ResourceType: &resourceType,
				ResourceID:   &scopeID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"scope_id": scopeID,
			}
			if evt, err := outbox.NewEvent(pc.TenantID, "user_scope", scopeID, EventScopeRevoked, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})
}

// GetUserScopes lists all organizational scopes bound to a target user.
func (s *DomainService) GetUserScopes(ctx context.Context, targetUserID uuid.UUID) ([]UserScope, error) {
	return s.store.GetUserScopes(ctx, targetUserID)
}
