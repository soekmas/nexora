// Package identity provides user account lifecycle, authentication, role and permission management, and scope binding.
package identity

import (
	"errors"
	"net/http"

	"example.com/nexora/backend/internal/platform/apperr"
)

// Sentinel domain errors for the Identity package.
var (
	ErrUserNotFound             = errors.New("user account not found")
	ErrUsernameExists           = errors.New("username already exists for this tenant")
	ErrEmailExists              = errors.New("email already exists for this tenant")
	ErrInvalidCredentials       = errors.New("invalid username or password")
	ErrAccountLocked            = errors.New("user account is locked due to consecutive failed login attempts")
	ErrAccountDisabled          = errors.New("user account is disabled")
	ErrAccountPending           = errors.New("user account is pending activation")
	ErrRoleNotFound             = errors.New("role not found")
	ErrRoleExists               = errors.New("role code already exists for this tenant")
	ErrPermissionNotFound       = errors.New("permission not found")
	ErrUserRoleExists           = errors.New("user already has this role")
	ErrUserRoleNotFound         = errors.New("user does not have this role")
	ErrInvalidScope             = errors.New("invalid scope configuration or missing required entity identifier")
	ErrSelfRoleAssignmentDenied = errors.New("segregation of duties: actor cannot grant administrative roles to themselves")
	ErrSystemRoleProtected      = errors.New("system roles cannot be modified or deleted")
	ErrInvalidPassword          = errors.New("password does not meet policy requirements")
	ErrMalformedPasswordHash    = errors.New("stored password hash format is invalid or corrupted")
	ErrUnsupportedHashVersion   = errors.New("unsupported password hash version or algorithm")
)

// MapToAppError maps identity domain errors to standard API error responses.
func MapToAppError(err error) error {
	if err == nil {
		return nil
	}

	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		return appErr
	}

	switch {
	case errors.Is(err, ErrUserNotFound):
		return apperr.NewAppError(http.StatusNotFound, "USER_NOT_FOUND", "User account not found")
	case errors.Is(err, ErrUsernameExists):
		return apperr.NewAppError(http.StatusConflict, "USERNAME_ALREADY_EXISTS", "Username is already registered")
	case errors.Is(err, ErrEmailExists):
		return apperr.NewAppError(http.StatusConflict, "EMAIL_ALREADY_EXISTS", "Email is already registered")
	case errors.Is(err, ErrInvalidCredentials):
		return apperr.NewAppError(http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
	case errors.Is(err, ErrAccountLocked):
		return apperr.NewAppError(http.StatusForbidden, "ACCOUNT_LOCKED", "Account is locked due to consecutive failed attempts; try again later")
	case errors.Is(err, ErrAccountDisabled):
		return apperr.NewAppError(http.StatusForbidden, "ACCOUNT_DISABLED", "Account is disabled; contact your administrator")
	case errors.Is(err, ErrAccountPending):
		return apperr.NewAppError(http.StatusForbidden, "ACCOUNT_PENDING", "Account activation is pending")
	case errors.Is(err, ErrRoleNotFound):
		return apperr.NewAppError(http.StatusNotFound, "ROLE_NOT_FOUND", "Role not found")
	case errors.Is(err, ErrRoleExists):
		return apperr.NewAppError(http.StatusConflict, "ROLE_ALREADY_EXISTS", "Role code already exists")
	case errors.Is(err, ErrPermissionNotFound):
		return apperr.NewAppError(http.StatusNotFound, "PERMISSION_NOT_FOUND", "Permission not found")
	case errors.Is(err, ErrUserRoleExists):
		return apperr.NewAppError(http.StatusConflict, "USER_ROLE_ALREADY_ASSIGNED", "User already has this role")
	case errors.Is(err, ErrUserRoleNotFound):
		return apperr.NewAppError(http.StatusNotFound, "USER_ROLE_NOT_FOUND", "User role assignment not found")
	case errors.Is(err, ErrInvalidScope):
		return apperr.NewAppError(http.StatusBadRequest, "INVALID_SCOPE", "Invalid scope configuration")
	case errors.Is(err, ErrSelfRoleAssignmentDenied):
		return apperr.NewAppError(http.StatusForbidden, "SOD_VIOLATION", "Actor cannot grant administrative roles to themselves")
	case errors.Is(err, ErrSystemRoleProtected):
		return apperr.NewAppError(http.StatusForbidden, "SYSTEM_ROLE_PROTECTED", "System roles cannot be modified")
	case errors.Is(err, ErrInvalidPassword):
		return apperr.NewAppError(http.StatusBadRequest, "INVALID_PASSWORD", "Password must be at least 8 characters long")
	default:
		return apperr.NewAppError(http.StatusInternalServerError, "INTERNAL_ERROR", "Internal identity processing error")
	}
}
