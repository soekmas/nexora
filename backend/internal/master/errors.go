// Package master provides product catalog, brand, category, unit of measure, and barcode management.
package master

import (
	"errors"
	"net/http"

	"example.com/nexora/backend/internal/platform/apperr"
)

// Sentinel domain errors for the Master Data package.
var (
	ErrBrandNotFound             = errors.New("brand not found")
	ErrBrandCodeExists           = errors.New("brand code already exists for this tenant")
	ErrBrandCodeRequired         = errors.New("brand code is required")
	ErrBrandNameRequired         = errors.New("brand name is required")
	ErrCategoryNotFound          = errors.New("category not found")
	ErrCategoryCodeExists        = errors.New("category code already exists for this tenant")
	ErrCategoryCodeRequired      = errors.New("category code is required")
	ErrCategoryNameRequired      = errors.New("category name is required")
	ErrCategorySelfParent        = errors.New("category cannot be its own parent")
	ErrCategoryCycleDetected     = errors.New("category hierarchy cannot contain cycles")
	ErrUOMNotFound               = errors.New("unit of measure not found")
	ErrUOMCodeExists             = errors.New("unit of measure code already exists for this tenant")
	ErrUOMCodeRequired           = errors.New("unit of measure code is required")
	ErrUOMNameRequired           = errors.New("unit of measure name is required")
	ErrInvalidUOMPrecision       = errors.New("invalid unit of measure precision scale: must be between 0 and 6")
	ErrProductNotFound           = errors.New("product not found")
	ErrProductSKUExists          = errors.New("product SKU already exists for this tenant")
	ErrProductSKURequired        = errors.New("product SKU is required")
	ErrProductNameRequired       = errors.New("product name is required")
	ErrProductBaseUOMRequired    = errors.New("base UOM is required")
	ErrInvalidProductType        = errors.New("invalid product type")
	ErrVariantNotFound           = errors.New("product variant not found")
	ErrVariantSKUExists          = errors.New("variant SKU already exists for this tenant")
	ErrVariantSKURequired        = errors.New("variant SKU is required")
	ErrVariantNameRequired       = errors.New("variant name is required")
	ErrBarcodeNotFound           = errors.New("barcode not found")
	ErrBarcodeExists             = errors.New("barcode already exists for this tenant")
	ErrBarcodeRequired           = errors.New("barcode value is required")
	ErrSerialNotFound            = errors.New("serial number not found")
	ErrSerialNumberNotFound      = errors.New("serial number not found")
	ErrSerialNumberExists        = errors.New("serial number already exists for this tenant")
	ErrSerialNumberRequired      = errors.New("serial number is required")
	ErrSerialTrackingNotEnabled  = errors.New("serial tracking is not enabled for this product")
	ErrCompanyCrossTenant        = errors.New("referenced company belongs to a different tenant or does not exist")
	ErrInvalidStatus             = errors.New("invalid status: must be ACTIVE or INACTIVE")
	ErrInvalidStatusTransition   = errors.New("invalid status transition")
	ErrProductDiscontinued       = errors.New("discontinued products cannot be modified or reactivated")
	ErrOptimisticLockConflict    = errors.New("optimistic lock conflict: record has been updated by another transaction")
	ErrValidation                = errors.New("validation failed")
)

// MapToAppError maps sentinel master errors to canonical platform HTTP AppError envelopes.
func MapToAppError(err error) *apperr.AppError {
	if err == nil {
		return nil
	}

	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		return appErr
	}

	switch {
	case errors.Is(err, ErrBrandNotFound):
		return apperr.NewAppError(http.StatusNotFound, "BRAND_NOT_FOUND", "Brand not found")
	case errors.Is(err, ErrCategoryNotFound):
		return apperr.NewAppError(http.StatusNotFound, "CATEGORY_NOT_FOUND", "Category not found")
	case errors.Is(err, ErrUOMNotFound):
		return apperr.NewAppError(http.StatusNotFound, "UOM_NOT_FOUND", "Unit of measure not found")
	case errors.Is(err, ErrProductNotFound):
		return apperr.NewAppError(http.StatusNotFound, "PRODUCT_NOT_FOUND", "Product not found")
	case errors.Is(err, ErrVariantNotFound):
		return apperr.NewAppError(http.StatusNotFound, "VARIANT_NOT_FOUND", "Product variant not found")
	case errors.Is(err, ErrBarcodeNotFound):
		return apperr.NewAppError(http.StatusNotFound, "BARCODE_NOT_FOUND", "Barcode not found")
	case errors.Is(err, ErrSerialNotFound), errors.Is(err, ErrSerialNumberNotFound):
		return apperr.NewAppError(http.StatusNotFound, "SERIAL_NOT_FOUND", "Serial number not found")

	case errors.Is(err, ErrBrandCodeExists):
		return apperr.NewConflictError("BRAND_CODE_EXISTS", "Brand code already exists")
	case errors.Is(err, ErrCategoryCodeExists):
		return apperr.NewConflictError("CATEGORY_CODE_EXISTS", "Category code already exists")
	case errors.Is(err, ErrUOMCodeExists):
		return apperr.NewConflictError("UOM_CODE_EXISTS", "Unit of measure code already exists")
	case errors.Is(err, ErrProductSKUExists):
		return apperr.NewConflictError("PRODUCT_SKU_EXISTS", "Product SKU already exists")
	case errors.Is(err, ErrVariantSKUExists):
		return apperr.NewConflictError("VARIANT_SKU_EXISTS", "Variant SKU already exists")
	case errors.Is(err, ErrBarcodeExists):
		return apperr.NewConflictError("BARCODE_EXISTS", "Barcode already exists")
	case errors.Is(err, ErrSerialNumberExists):
		return apperr.NewConflictError("SERIAL_EXISTS", "Serial number already exists")
	case errors.Is(err, ErrOptimisticLockConflict):
		return apperr.NewConflictError("OPTIMISTIC_LOCK_CONFLICT", "The record was modified by another operation. Please reload and retry.")
	case errors.Is(err, ErrInvalidStatusTransition):
		return apperr.NewConflictError("INVALID_STATUS_TRANSITION", err.Error())
	case errors.Is(err, ErrProductDiscontinued):
		return apperr.NewConflictError("PRODUCT_DISCONTINUED", "Discontinued products cannot be reactivated or updated")

	case errors.Is(err, ErrCategorySelfParent):
		return apperr.NewBadRequestError("Category cannot be its own parent")
	case errors.Is(err, ErrCategoryCycleDetected):
		return apperr.NewBadRequestError("Category hierarchy cannot contain cycles")
	case errors.Is(err, ErrInvalidUOMPrecision):
		return apperr.NewBadRequestError("Precision scale must be between 0 and 6")
	case errors.Is(err, ErrBrandCodeRequired), errors.Is(err, ErrBrandNameRequired),
		errors.Is(err, ErrCategoryCodeRequired), errors.Is(err, ErrCategoryNameRequired),
		errors.Is(err, ErrUOMCodeRequired), errors.Is(err, ErrUOMNameRequired),
		errors.Is(err, ErrProductSKURequired), errors.Is(err, ErrProductNameRequired),
		errors.Is(err, ErrProductBaseUOMRequired), errors.Is(err, ErrInvalidProductType),
		errors.Is(err, ErrVariantSKURequired), errors.Is(err, ErrVariantNameRequired),
		errors.Is(err, ErrBarcodeRequired), errors.Is(err, ErrSerialNumberRequired),
		errors.Is(err, ErrSerialTrackingNotEnabled), errors.Is(err, ErrInvalidStatus):
		return apperr.NewBadRequestError(err.Error())
	case errors.Is(err, ErrCompanyCrossTenant):
		return apperr.NewBadRequestError("Referenced company belongs to a different tenant or does not exist")
	case errors.Is(err, ErrValidation):
		return apperr.NewValidationError("request", err.Error())

	default:
		return apperr.NewAppError(http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected master data processing error occurred")
	}
}
