// Package organization provides company, region, store, warehouse, and POS terminal lifecycle management.
package organization

import (
	"errors"
	"net/http"

	"example.com/nexora/backend/internal/platform/apperr"
)

// Sentinel errors for the Organization domain.
var (
	ErrCompanyNotFound         = errors.New("company not found")
	ErrCompanyCodeExists       = errors.New("company code already exists for this tenant")
	ErrRegionNotFound          = errors.New("region not found")
	ErrRegionCodeExists        = errors.New("region code already exists for this company")
	ErrStoreNotFound           = errors.New("store not found")
	ErrStoreCodeExists         = errors.New("store code already exists for this company")
	ErrWarehouseNotFound       = errors.New("warehouse not found")
	ErrWarehouseCodeExists     = errors.New("warehouse code already exists for this company")
	ErrTerminalNotFound        = errors.New("terminal not found")
	ErrTerminalCodeExists      = errors.New("terminal code already exists for this store")
	ErrParentNotFound          = errors.New("parent entity not found")
	ErrParentTenantMismatch    = errors.New("parent entity belongs to a different tenant or company")
	ErrInvalidDeviceToken      = errors.New("invalid or mismatched terminal device token")
	ErrDeviceTokenRevoked      = errors.New("terminal device token has been revoked")
	ErrTerminalDisabled        = errors.New("terminal is disabled")
	ErrTerminalRetired         = errors.New("terminal is retired and cannot be used")
	ErrOptimisticLockConflict  = errors.New("organization record update conflict: concurrent modification detected")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
	ErrValidation              = errors.New("validation failed")
)

// MapToAppError maps organization domain errors to standard API error responses.
func MapToAppError(err error) error {
	if err == nil {
		return nil
	}

	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		return appErr
	}

	switch {
	case errors.Is(err, ErrCompanyNotFound):
		return apperr.NewAppError(http.StatusNotFound, "COMPANY_NOT_FOUND", "Company not found")
	case errors.Is(err, ErrCompanyCodeExists):
		return apperr.NewAppError(http.StatusConflict, "COMPANY_CODE_ALREADY_EXISTS", "Company code already exists")
	case errors.Is(err, ErrRegionNotFound):
		return apperr.NewAppError(http.StatusNotFound, "REGION_NOT_FOUND", "Region not found")
	case errors.Is(err, ErrRegionCodeExists):
		return apperr.NewAppError(http.StatusConflict, "REGION_CODE_ALREADY_EXISTS", "Region code already exists in this company")
	case errors.Is(err, ErrStoreNotFound):
		return apperr.NewAppError(http.StatusNotFound, "STORE_NOT_FOUND", "Store not found")
	case errors.Is(err, ErrStoreCodeExists):
		return apperr.NewAppError(http.StatusConflict, "STORE_CODE_ALREADY_EXISTS", "Store code already exists in this company")
	case errors.Is(err, ErrWarehouseNotFound):
		return apperr.NewAppError(http.StatusNotFound, "WAREHOUSE_NOT_FOUND", "Warehouse not found")
	case errors.Is(err, ErrWarehouseCodeExists):
		return apperr.NewAppError(http.StatusConflict, "WAREHOUSE_CODE_ALREADY_EXISTS", "Warehouse code already exists in this company")
	case errors.Is(err, ErrTerminalNotFound):
		return apperr.NewAppError(http.StatusNotFound, "TERMINAL_NOT_FOUND", "Terminal not found")
	case errors.Is(err, ErrTerminalCodeExists):
		return apperr.NewAppError(http.StatusConflict, "TERMINAL_CODE_ALREADY_EXISTS", "Terminal code already exists in this store")
	case errors.Is(err, ErrParentNotFound):
		return apperr.NewAppError(http.StatusBadRequest, "PARENT_NOT_FOUND", "Referenced parent entity does not exist or is inactive")
	case errors.Is(err, ErrParentTenantMismatch):
		return apperr.NewAppError(http.StatusBadRequest, "PARENT_TENANT_MISMATCH", "Referenced parent entity belongs to a different organizational boundary")
	case errors.Is(err, ErrInvalidDeviceToken):
		return apperr.NewAppError(http.StatusUnauthorized, "INVALID_DEVICE_TOKEN", "Invalid or expired terminal device token")
	case errors.Is(err, ErrDeviceTokenRevoked):
		return apperr.NewAppError(http.StatusForbidden, "DEVICE_TOKEN_REVOKED", "Terminal device token has been revoked")
	case errors.Is(err, ErrTerminalDisabled):
		return apperr.NewAppError(http.StatusForbidden, "TERMINAL_DISABLED", "Terminal is disabled")
	case errors.Is(err, ErrTerminalRetired):
		return apperr.NewAppError(http.StatusForbidden, "TERMINAL_RETIRED", "Terminal has been retired")
	case errors.Is(err, ErrOptimisticLockConflict):
		return apperr.NewAppError(http.StatusConflict, "OPTIMISTIC_LOCK_CONFLICT", "Record was modified by another operation; retry with the latest version")
	case errors.Is(err, ErrInvalidStatusTransition):
		return apperr.NewAppError(http.StatusBadRequest, "INVALID_STATUS_TRANSITION", "Requested status transition is not permitted")
	case errors.Is(err, ErrValidation):
		return apperr.NewAppError(http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
	default:
		return apperr.NewAppError(http.StatusInternalServerError, "INTERNAL_ERROR", "Internal organization processing error")
	}
}
