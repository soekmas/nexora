package identity

import (
	"time"

	"github.com/google/uuid"
)

// UserStatus represents the lifecycle state of a user account.
type UserStatus string

// Supported lifecycle states for user accounts.
const (
	StatusActive   UserStatus = "ACTIVE"
	StatusLocked   UserStatus = "LOCKED"
	StatusDisabled UserStatus = "DISABLED"
	StatusPending  UserStatus = "PENDING"
)

// ScopeType represents the organizational boundary bound to a user.
type ScopeType string

// Supported organizational boundary types.
const (
	ScopeTypeCompany   ScopeType = "COMPANY"
	ScopeTypeRegion    ScopeType = "REGION"
	ScopeTypeStore     ScopeType = "STORE"
	ScopeTypeWarehouse ScopeType = "WAREHOUSE"
	ScopeTypeTerminal  ScopeType = "TERMINAL"
)

// Lockout policy configuration per DEC-B1-005.
const (
	MaxFailedLoginAttempts = 5
	LockoutDuration        = 15 * time.Minute
)

// Canonical domain event types for the Identity module.
const (
	EventUserCreated = "identity.user.created.v1"
	EventUserUpdated = "identity.user.updated.v1"
	EventUserLocked  = "identity.user.locked.v1"
	EventRoleAssigned = "identity.role.assigned.v1"
	EventRoleRevoked  = "identity.role.revoked.v1"
	EventScopeAssigned = "identity.scope.assigned.v1"
	EventScopeRevoked  = "identity.scope.revoked.v1"
)

// Canonical audit actions for the Identity module per catalog section 11.
const (
	AuditActionUserCreated      = "USER_CREATED"
	AuditActionUserUpdated      = "USER_UPDATED"
	AuditActionUserDisabled     = "USER_DISABLED"
	AuditActionUserEnabled      = "USER_ENABLED"
	AuditActionUserLocked       = "USER_LOCKED"
	AuditActionUserRoleAssigned = "USER_ROLE_ASSIGNED"
	AuditActionUserRoleRevoked  = "USER_ROLE_REVOKED"
	AuditActionUserScopeChanged = "USER_SCOPE_CHANGED"
	AuditActionLoginSuccess     = "LOGIN_SUCCESS"
	AuditActionLoginFailure     = "LOGIN_FAILURE"
	AuditActionPasswordReset    = "PASSWORD_RESET"
)

// UserAccount represents the entity in identity.user_account.
type UserAccount struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	Username         string     `json:"username"`
	Email            *string    `json:"email,omitempty"`
	PasswordHash     *string    `json:"-"`
	DisplayName      string     `json:"display_name"`
	Status           UserStatus `json:"status"`
	FailedLoginCount int        `json:"failed_login_count"`
	LockedUntil      *time.Time `json:"locked_until,omitempty"`
	LastLoginAt      *time.Time `json:"last_login_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedBy        *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy        *uuid.UUID `json:"updated_by,omitempty"`
	Version          int64      `json:"version"`
}

// IsLocked checks whether the user account is currently locked.
// If locked_until is set but is in the past, the lock is considered expired.
func (u *UserAccount) IsLocked(now time.Time) bool {
	if u.Status == StatusLocked {
		if u.LockedUntil != nil && now.After(*u.LockedUntil) {
			return false // Lockout duration has expired
		}
		return true
	}
	if u.LockedUntil != nil && now.Before(*u.LockedUntil) {
		return true
	}
	return false
}

// Role represents the entity in identity.role.
type Role struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	IsSystem    bool       `json:"is_system"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy   *uuid.UUID `json:"updated_by,omitempty"`
	Version     int64      `json:"version"`
	Permissions []string   `json:"permissions,omitempty"`
}

// Permission represents the catalog entity in identity.permission.
type Permission struct {
	ID     uuid.UUID `json:"id"`
	Code   string    `json:"code"`
	Name   string    `json:"name"`
	Module string    `json:"module"`
	Action string    `json:"action"`
}

// UserRole represents a junction record between a user and a role.
type UserRole struct {
	UserID     uuid.UUID  `json:"user_id"`
	RoleID     uuid.UUID  `json:"role_id"`
	AssignedAt time.Time  `json:"assigned_at"`
	AssignedBy *uuid.UUID `json:"assigned_by,omitempty"`
}

// UserScope represents an organizational boundary binding in identity.user_scope.
type UserScope struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	UserID      uuid.UUID  `json:"user_id"`
	CompanyID   *uuid.UUID `json:"company_id,omitempty"`
	RegionID    *uuid.UUID `json:"region_id,omitempty"`
	StoreID     *uuid.UUID `json:"store_id,omitempty"`
	WarehouseID *uuid.UUID `json:"warehouse_id,omitempty"`
	TerminalID  *uuid.UUID `json:"terminal_id,omitempty"`
	ScopeType   ScopeType  `json:"scope_type"`
	CreatedAt   time.Time  `json:"created_at"`
}

// LoginAttempt represents an entry in identity.login_attempt.
type LoginAttempt struct {
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

// UserWithDetails aggregates account data with its assigned roles, scopes, and aggregated permissions.
type UserWithDetails struct {
	User        UserAccount `json:"user"`
	Roles       []Role      `json:"roles"`
	Scopes      []UserScope `json:"scopes"`
	Permissions []string    `json:"permissions"`
}
