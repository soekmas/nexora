// Package crm provides customer relationship management, customer groups, and address book capabilities.
package crm

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Supported lifecycle status constants.
const (
	StatusActive   = "ACTIVE"
	StatusInactive = "INACTIVE"
)

// Supported customer address types.
const (
	AddressTypeShipping = "SHIPPING"
	AddressTypeBilling  = "BILLING"
	AddressTypeOther    = "OTHER"
)

// Canonical domain event types.
const (
	EventCustomerCreated       = "crm.customer.created.v1"
	EventCustomerUpdated       = "crm.customer.updated.v1"
	EventCustomerDisabled      = "crm.customer.disabled.v1"
	EventAddressCreated        = "crm.customer.address_created.v1"
	EventAddressUpdated        = "crm.customer.address_updated.v1"
	EventAddressDeleted        = "crm.customer.address_deleted.v1"
	EventCustomerGroupCreated  = "crm.customer_group.created.v1"
	EventCustomerGroupUpdated  = "crm.customer_group.updated.v1"
)

// Canonical audit action codes matching audit.audit_action_catalog.
const (
	AuditActionCustomerCreated       = "CUSTOMER_CREATED"
	AuditActionCustomerUpdated       = "CUSTOMER_UPDATED"
	AuditActionCustomerDisabled      = "CUSTOMER_DISABLED"
	AuditActionAddressCreated        = "CUSTOMER_ADDRESS_CREATED"
	AuditActionAddressUpdated        = "CUSTOMER_ADDRESS_UPDATED"
	AuditActionAddressDeleted        = "CUSTOMER_ADDRESS_DELETED"
	AuditActionCustomerGroupChanged  = "CUSTOMER_GROUP_CHANGED"
	AuditActionCustomerGroupCreated  = "CUSTOMER_GROUP_CREATED"
	AuditActionCustomerGroupUpdated  = "CUSTOMER_GROUP_UPDATED"
)

// Canonical permissions matching identity.permission.
const (
	PermCustomerRead          = "customer.read"
	PermCustomerCreate        = "customer.create"
	PermCustomerUpdate        = "customer.update"
	PermCustomerAddressManage = "customer.address.manage"
	PermCustomerGroupRead     = "customer_group.read"
	PermCustomerGroupCreate   = "customer_group.create"
	PermCustomerGroupUpdate   = "customer_group.update"
)

// Standard domain errors.
var (
	ErrCustomerNotFound           = errors.New("customer not found")
	ErrCustomerGroupNotFound      = errors.New("customer group not found")
	ErrAddressNotFound            = errors.New("customer address not found")
	ErrCustomerNumberConflict     = errors.New("customer number already exists in tenant")
	ErrCustomerGroupCodeConflict  = errors.New("customer group code already exists in tenant")
	ErrCustomerVersionConflict    = errors.New("customer has been modified by another transaction")
	ErrCustomerInactive           = errors.New("cannot modify or add addresses for an inactive customer")
	ErrCustomerGroupInactive      = errors.New("customer group is inactive and cannot be assigned")
	ErrInvalidCustomerNumber      = errors.New("invalid customer number format")
	ErrInvalidEmail               = errors.New("invalid email format")
	ErrInvalidPhone               = errors.New("invalid phone format")
	ErrCustomerNameRequired       = errors.New("customer name is required")
	ErrCustomerGroupNameRequired  = errors.New("customer group name is required")
	ErrCustomerGroupCodeRequired  = errors.New("customer group code is required")
	ErrAddressLineRequired        = errors.New("address line is required")
	ErrAddressRecipientRequired   = errors.New("recipient name is required")
	ErrInvalidAddressType         = errors.New("invalid address type")
	ErrCustomerNumberExhausted    = errors.New("failed to generate unique customer number after maximum retries")
)

// CustomerGroup represents a customer segment/tier.
type CustomerGroup struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Customer represents a customer profile in NEXORA.
type Customer struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	CustomerNo      string     `json:"customer_no"`
	CustomerGroupID *uuid.UUID `json:"group_id,omitempty"`
	Name            string     `json:"name"`
	Email           *string    `json:"email,omitempty"`
	Phone           *string    `json:"phone,omitempty"`
	TaxID           *string    `json:"tax_id,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatedBy       *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy       *uuid.UUID `json:"updated_by,omitempty"`
	Version         int64      `json:"version"`
}

// CustomerAddress represents a physical address in the customer address book.
type CustomerAddress struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	CustomerID    uuid.UUID `json:"customer_id"`
	AddressType   string    `json:"address_type"`
	RecipientName *string   `json:"recipient_name,omitempty"`
	Phone         *string   `json:"phone,omitempty"`
	AddressLine   string    `json:"address_line"`
	City          *string   `json:"city,omitempty"`
	Province      *string   `json:"province,omitempty"`
	PostalCode    *string   `json:"postal_code,omitempty"`
	CountryCode   string    `json:"country_code"`
	IsDefault     bool      `json:"is_default"`
}

// CreateCustomerGroupRequest carries payload for creating a customer group.
type CreateCustomerGroupRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// UpdateCustomerGroupRequest carries payload for updating a customer group.
type UpdateCustomerGroupRequest struct {
	Code   *string `json:"code,omitempty"`
	Name   *string `json:"name,omitempty"`
	Status *string `json:"status,omitempty"`
}

// CreateCustomerRequest carries payload for creating a customer profile.
type CreateCustomerRequest struct {
	CustomerNo string     `json:"customer_no,omitempty"`
	Name       string     `json:"name"`
	Email      *string    `json:"email,omitempty"`
	Phone      *string    `json:"phone,omitempty"`
	TaxID      *string    `json:"tax_id,omitempty"`
	GroupID    *uuid.UUID `json:"group_id,omitempty"`
}

// UpdateCustomerRequest carries payload for updating a customer profile.
type UpdateCustomerRequest struct {
	Name            *string    `json:"name,omitempty"`
	Email           *string    `json:"email,omitempty"`
	Phone           *string    `json:"phone,omitempty"`
	TaxID           *string    `json:"tax_id,omitempty"`
	GroupID         *uuid.UUID `json:"group_id,omitempty"`
	ExpectedVersion *int64     `json:"expected_version,omitempty"`
}

// CustomerResponse formats customer profile for API clients with OpenAPI compatibility.
type CustomerResponse struct {
	ID         uuid.UUID  `json:"id"`
	CustomerNo string     `json:"customer_no"`
	Name       string     `json:"name"`
	Email      *string    `json:"email,omitempty"`
	Phone      *string    `json:"phone,omitempty"`
	GroupID    *uuid.UUID `json:"group_id,omitempty"`
	TaxID      *string    `json:"tax_id,omitempty"`
	Status     string     `json:"status"`
	Active     bool       `json:"active"` // OpenAPI compatibility
	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ToResponse maps a Customer entity to CustomerResponse.
func (c *Customer) ToResponse() CustomerResponse {
	return CustomerResponse{
		ID:         c.ID,
		CustomerNo: c.CustomerNo,
		Name:       c.Name,
		Email:      c.Email,
		Phone:      c.Phone,
		GroupID:    c.CustomerGroupID,
		TaxID:      c.TaxID,
		Status:     c.Status,
		Active:     c.Status == StatusActive,
		Version:    c.Version,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}

// CreateAddressRequest carries payload for adding a customer address.
type CreateAddressRequest struct {
	AddressType   string  `json:"address_type,omitempty"`
	Label         string  `json:"label,omitempty"` // OpenAPI alias for AddressType
	RecipientName *string `json:"recipient_name,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	AddressLine   string  `json:"address_line,omitempty"`
	AddressLine1  string  `json:"address_line1,omitempty"` // OpenAPI alias for AddressLine
	City          *string `json:"city,omitempty"`
	Province      *string `json:"province,omitempty"`
	PostalCode    *string `json:"postal_code,omitempty"`
	CountryCode   string  `json:"country_code,omitempty"`
	IsDefault     bool    `json:"is_default,omitempty"`
}

// UpdateAddressRequest carries payload for modifying a customer address.
type UpdateAddressRequest struct {
	AddressType   *string `json:"address_type,omitempty"`
	Label         *string `json:"label,omitempty"`
	RecipientName *string `json:"recipient_name,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	AddressLine   *string `json:"address_line,omitempty"`
	AddressLine1  *string `json:"address_line1,omitempty"`
	City          *string `json:"city,omitempty"`
	Province      *string `json:"province,omitempty"`
	PostalCode    *string `json:"postal_code,omitempty"`
	CountryCode   *string `json:"country_code,omitempty"`
	IsDefault     *bool   `json:"is_default,omitempty"`
}

// AddressResponse formats address for API clients with OpenAPI compatibility.
type AddressResponse struct {
	ID            uuid.UUID `json:"id"`
	CustomerID    uuid.UUID `json:"customer_id"`
	AddressType   string    `json:"address_type"`
	Label         string    `json:"label"` // OpenAPI compatibility
	RecipientName *string   `json:"recipient_name,omitempty"`
	Phone         *string   `json:"phone,omitempty"`
	AddressLine   string    `json:"address_line"`
	AddressLine1  string    `json:"address_line1"` // OpenAPI compatibility
	City          *string   `json:"city,omitempty"`
	Province      *string   `json:"province,omitempty"`
	PostalCode    *string   `json:"postal_code,omitempty"`
	CountryCode   string    `json:"country_code"`
	IsDefault     bool      `json:"is_default"`
}

// ToResponse maps a CustomerAddress entity to AddressResponse.
func (a *CustomerAddress) ToResponse() AddressResponse {
	return AddressResponse{
		ID:            a.ID,
		CustomerID:    a.CustomerID,
		AddressType:   a.AddressType,
		Label:         a.AddressType,
		RecipientName: a.RecipientName,
		Phone:         a.Phone,
		AddressLine:   a.AddressLine,
		AddressLine1:  a.AddressLine,
		City:          a.City,
		Province:      a.Province,
		PostalCode:    a.PostalCode,
		CountryCode:   a.CountryCode,
		IsDefault:     a.IsDefault,
	}
}

// ListCustomersFilter defines query parameters for searching and listing customers.
type ListCustomersFilter struct {
	Query    string     `json:"query,omitempty"`
	Email    string     `json:"email,omitempty"`
	Phone    string     `json:"phone,omitempty"`
	GroupID  *uuid.UUID `json:"group_id,omitempty"`
	Status   string     `json:"status,omitempty"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// ListCustomerGroupsFilter defines query parameters for listing customer groups.
type ListCustomerGroupsFilter struct {
	Status   string `json:"status,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// Validation helpers

var (
	rePhone        = regexp.MustCompile(`^\+?[0-9\s\-\(\)]{7,50}$`)
	reCustomerNo   = regexp.MustCompile(`^[A-Za-z0-9\-_]{3,50}$`)
	reGroupCode    = regexp.MustCompile(`^[A-Za-z0-9\-_]{2,50}$`)
)

// ValidateEmail validates and normalizes an email address.
func ValidateEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(addr.Address), nil
}

// ValidatePhone validates and normalizes a phone number.
func ValidatePhone(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !rePhone.MatchString(trimmed) {
		return "", ErrInvalidPhone
	}
	// Strip superfluous spaces and hyphens for consistency
	normalized := strings.ReplaceAll(trimmed, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "(", "")
	normalized = strings.ReplaceAll(normalized, ")", "")
	return normalized, nil
}

// ValidateCustomerNumber validates custom-provided customer numbers.
func ValidateCustomerNumber(no string) error {
	trimmed := strings.TrimSpace(no)
	if !reCustomerNo.MatchString(trimmed) {
		return ErrInvalidCustomerNumber
	}
	return nil
}

// ValidateGroupCode validates customer group codes.
func ValidateGroupCode(code string) error {
	trimmed := strings.TrimSpace(code)
	if !reGroupCode.MatchString(trimmed) {
		return fmt.Errorf("%w: code must be 2-50 alphanumeric characters", ErrInvalidInput)
	}
	return nil
}

// ErrInvalidInput represents generic client validation error.
var ErrInvalidInput = errors.New("invalid input")

// Masking helpers for PII in audit snapshots

// MaskEmail masks the local part of an email address.
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}
	user := parts[0]
	domain := parts[1]
	if len(user) <= 1 {
		return "*@" + domain
	}
	return string(user[0]) + "***@" + domain
}

// MaskPhone masks middle digits of a phone number.
func MaskPhone(phone string) string {
	if len(phone) < 6 {
		return "***"
	}
	prefix := phone[:3]
	suffix := phone[len(phone)-3:]
	return prefix + "****" + suffix
}

// MaskTaxID masks all but the last 4 digits of a tax identifier.
func MaskTaxID(taxID string) string {
	if len(taxID) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(taxID)-4) + taxID[len(taxID)-4:]
}
