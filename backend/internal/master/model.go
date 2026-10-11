// Package master provides product catalog, brand, category, unit of measure, and barcode management.
package master

import (
	"time"

	"github.com/google/uuid"
)

// Supported lifecycle status constants.
const (
	StatusActive       = "ACTIVE"
	StatusInactive     = "INACTIVE"
	StatusDiscontinued = "DISCONTINUED"
)

// Supported product types.
const (
	ProductTypeStockable  = "STOCKABLE"
	ProductTypeService    = "SERVICE"
	ProductTypeConsumable = "CONSUMABLE"
	ProductTypeBundle     = "BUNDLE"
)

// Canonical domain event types.
const (
	EventProductCreated     = "catalog.product.created.v1"
	EventProductUpdated     = "catalog.product.updated.v1"
	EventProductActivated   = "catalog.product.activated.v1"
	EventProductDeactivated = "catalog.product.deactivated.v1"
	EventVariantCreated     = "catalog.variant.created.v1"
	EventVariantUpdated     = "catalog.variant.updated.v1"
	EventBarcodeAssigned    = "catalog.barcode.assigned.v1"
)

// Canonical audit action codes matching audit.audit_action_catalog.
const (
	AuditActionProductCreated     = "PRODUCT_CREATED"
	AuditActionProductUpdated     = "PRODUCT_UPDATED"
	AuditActionProductActivated   = "PRODUCT_ACTIVATED"
	AuditActionProductDeactivated = "PRODUCT_DEACTIVATED"
	AuditActionVariantCreated     = "PRODUCT_VARIANT_CREATED"
	AuditActionVariantUpdated     = "PRODUCT_VARIANT_UPDATED"
	AuditActionBarcodeCreated     = "BARCODE_CREATED"
	AuditActionBarcodeChanged     = "BARCODE_CHANGED"
	AuditActionCategoryCreated    = "CATEGORY_CREATED"
	AuditActionCategoryUpdated    = "CATEGORY_UPDATED"
	AuditActionUOMCreated         = "UOM_CREATED"
	AuditActionUOMUpdated         = "UOM_UPDATED"
	AuditActionBrandCreated       = "BRAND_CREATED"
	AuditActionBrandUpdated       = "BRAND_UPDATED"
)

// Brand represents a product brand.
type Brand struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Category represents a hierarchical product category.
type Category struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// UOM represents a unit of measure with decimal precision scale.
type UOM struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	PrecisionScale int16     `json:"precision_scale"`
}

// Product represents a core product master aggregate.
type Product struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	CompanyID   *uuid.UUID `json:"company_id,omitempty"`
	SKU         string     `json:"sku"`
	Name        string     `json:"name"`
	BrandID     *uuid.UUID `json:"brand_id,omitempty"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	BaseUOMID   uuid.UUID  `json:"base_uom_id"`
	ProductType string     `json:"product_type"`
	TrackSerial bool       `json:"track_serial"`
	TrackLot    bool       `json:"track_lot"`
	TaxCategory *string    `json:"tax_category,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy   *uuid.UUID `json:"updated_by,omitempty"`
	Version     int64      `json:"version"`
}

// ProductVariant represents a distinct variant (size, color, etc.) of a product.
type ProductVariant struct {
	ID         uuid.UUID      `json:"id"`
	TenantID   uuid.UUID      `json:"tenant_id"`
	ProductID  uuid.UUID      `json:"product_id"`
	SKU        string         `json:"sku"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ProductBarcode represents a barcode assigned to a product or variant.
type ProductBarcode struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	Barcode     string     `json:"barcode"`
	BarcodeType string     `json:"barcode_type"`
	IsPrimary   bool       `json:"is_primary"`
	Status      string     `json:"status"`
}

// SerialNumber represents an individual tracked serial unit.
type SerialNumber struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	ProductID uuid.UUID  `json:"product_id"`
	VariantID *uuid.UUID `json:"variant_id,omitempty"`
	SerialNo  string     `json:"serial_no"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
}

// ProductDetails combines a product with its variants and barcodes for detail responses.
type ProductDetails struct {
	Product  Product          `json:"product"`
	Variants []ProductVariant `json:"variants"`
	Barcodes []ProductBarcode `json:"barcodes"`
}

// DTOs for API requests

// CreateBrandRequest specifies the payload for brand creation.
type CreateBrandRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// UpdateBrandRequest specifies the payload for brand updates.
type UpdateBrandRequest struct {
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

// CreateCategoryRequest specifies the payload for category creation.
type CreateCategoryRequest struct {
	ParentID *uuid.UUID `json:"parent_id,omitempty"`
	Code     string     `json:"code"`
	Name     string     `json:"name"`
}

// UpdateCategoryRequest specifies the payload for category updates.
type UpdateCategoryRequest struct {
	ParentID *uuid.UUID `json:"parent_id,omitempty"`
	Name     *string    `json:"name,omitempty"`
	Status   *string    `json:"status,omitempty"`
}

// CreateUOMRequest specifies the payload for unit of measure creation.
type CreateUOMRequest struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	PrecisionScale *int16 `json:"precision_scale,omitempty"`
}

// CreateProductRequest specifies the payload for product creation.
type CreateProductRequest struct {
	CompanyID   *uuid.UUID             `json:"company_id,omitempty"`
	SKU         string                 `json:"sku"`
	Name        string                 `json:"name"`
	BrandID     *uuid.UUID             `json:"brand_id,omitempty"`
	CategoryID  *uuid.UUID             `json:"category_id,omitempty"`
	BaseUOMID   uuid.UUID              `json:"base_uom_id"`
	UOMID       *uuid.UUID             `json:"uom_id,omitempty"`
	ProductType string                 `json:"product_type,omitempty"`
	TrackSerial bool                   `json:"track_serial,omitempty"`
	TrackLot    bool                   `json:"track_lot,omitempty"`
	TaxCategory *string                `json:"tax_category,omitempty"`
	Variants    []CreateVariantRequest `json:"variants,omitempty"`
}

// UpdateProductRequest specifies the payload for updating an existing product.
type UpdateProductRequest struct {
	Name        *string    `json:"name,omitempty"`
	BrandID     *uuid.UUID `json:"brand_id,omitempty"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	BaseUOMID   *uuid.UUID `json:"base_uom_id,omitempty"`
	UOMID       *uuid.UUID `json:"uom_id,omitempty"`
	ProductType *string    `json:"product_type,omitempty"`
	TaxCategory *string    `json:"tax_category,omitempty"`
}

// CreateVariantRequest specifies the payload for variant creation.
type CreateVariantRequest struct {
	SKU        string         `json:"sku"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// CreateBarcodeRequest specifies the payload for assigning a barcode.
type CreateBarcodeRequest struct {
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	Barcode     string     `json:"barcode"`
	BarcodeType string     `json:"barcode_type,omitempty"`
	IsPrimary   bool       `json:"is_primary,omitempty"`
}

// CreateSerialNumberRequest specifies the payload for creating a catalog serial number.
type CreateSerialNumberRequest struct {
	VariantID *uuid.UUID `json:"variant_id,omitempty"`
	SerialNo  string     `json:"serial_no"`
}

// ReasonRequest specifies a payload with an audit reason (used for activation/deactivation).
type ReasonRequest struct {
	Reason string `json:"reason"`
}

// Response models

// PaginationMeta provides standard pagination metadata.
type PaginationMeta struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// ProductVariantResponse represents a product variant response.
type ProductVariantResponse struct {
	ID         uuid.UUID      `json:"id"`
	SKU        string         `json:"sku"`
	Name       string         `json:"name"`
	ProductID  uuid.UUID      `json:"product_id"`
	Active     bool           `json:"active"`
	Status     string         `json:"status"`
	Attributes map[string]any `json:"attributes,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ProductResponse represents a product response with variants and compatibility fields.
type ProductResponse struct {
	ID          uuid.UUID                `json:"id"`
	SKU         string                   `json:"sku"`
	Name        string                   `json:"name"`
	BrandID     *uuid.UUID               `json:"brand_id,omitempty"`
	CategoryID  *uuid.UUID               `json:"category_id,omitempty"`
	BaseUOMID   uuid.UUID                `json:"base_uom_id"`
	UOMID       uuid.UUID                `json:"uom_id"`
	CompanyID   *uuid.UUID               `json:"company_id,omitempty"`
	ProductType string                   `json:"product_type"`
	TrackSerial bool                     `json:"track_serial"`
	TrackLot    bool                     `json:"track_lot"`
	TaxCategory *string                  `json:"tax_category,omitempty"`
	Status      string                   `json:"status"`
	Active      bool                     `json:"active"`
	Version     int64                    `json:"version"`
	Variants    []ProductVariantResponse `json:"variants"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// ProductListResponse represents a paginated list of products.
type ProductListResponse struct {
	Items  []ProductResponse `json:"items"`
	Meta   PaginationMeta    `json:"meta"`
	Total  int64             `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

// BarcodeResponse represents a barcode response.
type BarcodeResponse struct {
	ID               uuid.UUID  `json:"id"`
	Barcode          string     `json:"barcode"`
	Type             string     `json:"type"`
	ProductID        uuid.UUID  `json:"product_id"`
	ProductVariantID *uuid.UUID `json:"product_variant_id,omitempty"`
	IsPrimary        bool       `json:"is_primary"`
	Status           string     `json:"status"`
}

// BarcodeListResponse represents a paginated list of barcodes.
type BarcodeListResponse struct {
	Items  []BarcodeResponse `json:"items"`
	Meta   PaginationMeta    `json:"meta"`
	Total  int64             `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

// SerialNumberResponse represents a serial number response.
type SerialNumberResponse struct {
	ID        uuid.UUID  `json:"id"`
	ProductID uuid.UUID  `json:"product_id"`
	VariantID *uuid.UUID `json:"variant_id,omitempty"`
	SerialNo  string     `json:"serial_no"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
}

// SerialNumberListResponse represents a paginated list of serial numbers.
type SerialNumberListResponse struct {
	Items  []SerialNumberResponse `json:"items"`
	Meta   PaginationMeta         `json:"meta"`
	Total  int64                  `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

