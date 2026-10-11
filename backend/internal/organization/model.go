package organization

import (
	"time"

	"github.com/google/uuid"
)

// Common lifecycle states for organizational entities.
const (
	StatusActive   = "ACTIVE"
	StatusInactive = "INACTIVE"
)

// Terminal lifecycle states per state machine catalog.
const (
	TerminalStatusActive      = "ACTIVE"
	TerminalStatusInactive    = "INACTIVE"
	TerminalStatusMaintenance = "MAINTENANCE"
	TerminalStatusDisabled    = "DISABLED"
	TerminalStatusRetired     = "RETIRED"
)

// Warehouse types.
const (
	WarehouseTypeStore   = "STORE"
	WarehouseTypeCentral = "CENTRAL"
	WarehouseTypeTransit = "TRANSIT"
	WarehouseTypeVirtual = "VIRTUAL"
	WarehouseTypeService = "SERVICE"
)

// Canonical domain event types for Organization.
const (
	EventCompanyCreated   = "org.company.created.v1"
	EventCompanyUpdated   = "org.company.updated.v1"
	EventRegionCreated    = "org.region.created.v1"
	EventRegionUpdated    = "org.region.updated.v1"
	EventStoreCreated     = "org.store.created.v1"
	EventStoreUpdated     = "org.store.updated.v1"
	EventStoreDisabled    = "org.store.disabled.v1"
	EventStoreEnabled     = "org.store.enabled.v1"
	EventWarehouseCreated = "org.warehouse.created.v1"
	EventWarehouseUpdated = "org.warehouse.updated.v1"
	EventTerminalRegistered = "org.terminal.registered.v1"
	EventTerminalUpdated    = "org.terminal.updated.v1"
	EventTerminalDisabled   = "org.terminal.disabled.v1"
	EventTerminalRetired    = "org.terminal.retired.v1"
	EventDeviceRegistered   = "org.device.registered.v1"
	EventDeviceRevoked      = "org.device.revoked.v1"
)

// Canonical audit actions for Organization.
const (
	AuditActionCompanyCreated    = "COMPANY_CREATED"
	AuditActionCompanyUpdated    = "COMPANY_UPDATED"
	AuditActionRegionCreated     = "REGION_CREATED"
	AuditActionRegionUpdated     = "REGION_UPDATED"
	AuditActionStoreCreated      = "STORE_CREATED"
	AuditActionStoreUpdated      = "STORE_UPDATED"
	AuditActionStoreEnabled      = "STORE_ENABLED"
	AuditActionStoreDisabled     = "STORE_DISABLED"
	AuditActionWarehouseCreated  = "WAREHOUSE_CREATED"
	AuditActionWarehouseUpdated  = "WAREHOUSE_UPDATED"
	AuditActionTerminalRegistered = "TERMINAL_REGISTERED"
	AuditActionTerminalEnabled   = "TERMINAL_ENABLED"
	AuditActionTerminalDisabled  = "TERMINAL_DISABLED"
	AuditActionTerminalRetired   = "TERMINAL_RETIRED"
	AuditActionDeviceRegistered  = "DEVICE_REGISTERED"
	AuditActionDeviceSuspended   = "DEVICE_SUSPENDED"
	AuditActionDeviceRevoked     = "DEVICE_REVOKED"
)

// Company represents organization.company.
type Company struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	Code         string     `json:"code"`
	Name         string     `json:"name"`
	LegalName    *string    `json:"legal_name,omitempty"`
	TaxID        *string    `json:"tax_id,omitempty"`
	BaseCurrency string     `json:"base_currency"`
	Timezone     string     `json:"timezone"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CreatedBy    *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy    *uuid.UUID `json:"updated_by,omitempty"`
	Version      int64      `json:"version"`
}

// Region represents organization.region.
type Region struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	CompanyID uuid.UUID  `json:"company_id"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	Version   int64      `json:"version"`
}

// Store represents organization.store.
type Store struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	CompanyID uuid.UUID  `json:"company_id"`
	RegionID  *uuid.UUID `json:"region_id,omitempty"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Address   *string    `json:"address,omitempty"`
	Timezone  string     `json:"timezone"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	Version   int64      `json:"version"`
}

// Warehouse represents organization.warehouse.
type Warehouse struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	CompanyID     uuid.UUID  `json:"company_id"`
	StoreID       *uuid.UUID `json:"store_id,omitempty"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	WarehouseType string     `json:"warehouse_type"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy     *uuid.UUID `json:"updated_by,omitempty"`
	Version       int64      `json:"version"`
}

// Terminal represents organization.terminal.
type Terminal struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	CompanyID        uuid.UUID  `json:"company_id"`
	StoreID          uuid.UUID  `json:"store_id"`
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	DeviceIdentifier *string    `json:"device_identifier,omitempty"` // Stores SHA-256 hash of device token
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedBy        *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy        *uuid.UUID `json:"updated_by,omitempty"`
	Version          int64      `json:"version"`
}

// Request and response DTOs

// CreateCompanyRequest specifies payload for company creation.
type CreateCompanyRequest struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	LegalName    *string `json:"legal_name,omitempty"`
	TaxID        *string `json:"tax_id,omitempty"`
	BaseCurrency string  `json:"base_currency,omitempty"`
	Timezone     string  `json:"timezone,omitempty"`
}

// UpdateCompanyRequest specifies payload for company updates.
type UpdateCompanyRequest struct {
	Name         *string `json:"name,omitempty"`
	LegalName    *string `json:"legal_name,omitempty"`
	TaxID        *string `json:"tax_id,omitempty"`
	BaseCurrency *string `json:"base_currency,omitempty"`
	Timezone     *string `json:"timezone,omitempty"`
	Status       *string `json:"status,omitempty"`
}

// CreateRegionRequest specifies payload for region creation.
type CreateRegionRequest struct {
	CompanyID uuid.UUID `json:"company_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
}

// UpdateRegionRequest specifies payload for region updates.
type UpdateRegionRequest struct {
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

// CreateStoreRequest specifies payload for store creation.
type CreateStoreRequest struct {
	CompanyID uuid.UUID  `json:"company_id"`
	RegionID  *uuid.UUID `json:"region_id,omitempty"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Address   *string    `json:"address,omitempty"`
	Timezone  string     `json:"timezone,omitempty"`
}

// UpdateStoreRequest specifies payload for store updates.
type UpdateStoreRequest struct {
	RegionID *uuid.UUID `json:"region_id,omitempty"`
	Name     *string    `json:"name,omitempty"`
	Address  *string    `json:"address,omitempty"`
	Timezone *string    `json:"timezone,omitempty"`
	Status   *string    `json:"status,omitempty"`
}

// CreateWarehouseRequest specifies payload for warehouse creation.
type CreateWarehouseRequest struct {
	CompanyID     uuid.UUID  `json:"company_id"`
	StoreID       *uuid.UUID `json:"store_id,omitempty"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	WarehouseType string     `json:"warehouse_type,omitempty"`
}

// UpdateWarehouseRequest specifies payload for warehouse updates.
type UpdateWarehouseRequest struct {
	StoreID       *uuid.UUID `json:"store_id,omitempty"`
	Name          *string    `json:"name,omitempty"`
	WarehouseType *string    `json:"warehouse_type,omitempty"`
	Status        *string    `json:"status,omitempty"`
}

// RegisterTerminalRequest specifies payload for terminal registration.
type RegisterTerminalRequest struct {
	CompanyID uuid.UUID `json:"company_id"`
	StoreID   uuid.UUID `json:"store_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
}

// UpdateTerminalRequest specifies payload for terminal updates.
type UpdateTerminalRequest struct {
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

// TerminalRegistrationResult is returned only once when a terminal/device is registered or rotated.
type TerminalRegistrationResult struct {
	Terminal    Terminal `json:"terminal"`
	DeviceToken string   `json:"device_token"` // Plaintext token returned ONCE
}
