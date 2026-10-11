// Package pricing implements the NEXORA Pricing Engine domain models,
// pure deterministic rule resolution, persistence, and application services.
package pricing

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Permitted Price List Statuses
const (
	PriceListStatusActive   = "ACTIVE"
	PriceListStatusInactive = "INACTIVE"
	PriceListStatusArchived = "ARCHIVED"
)

// Permitted Price Rule Statuses (canonical state machine)
const (
	PriceRuleStatusDraft    = "DRAFT"
	PriceRuleStatusApproved = "APPROVED"
	PriceRuleStatusActive   = "ACTIVE"
	PriceRuleStatusInactive = "INACTIVE"
	PriceRuleStatusExpired  = "EXPIRED"
)

// Canonical Permissions
const (
	PermPriceRead           = "price.read"
	PermPriceListCreate     = "price_list.create"
	PermPriceListUpdate     = "price_list.update"
	PermPriceRuleRead       = "price_rule.read"
	PermPriceRuleCreate     = "price_rule.create"
	PermPriceRuleUpdate     = "price_rule.update"
	PermPriceRuleSubmit     = "price_rule.submit"
	PermPriceRuleApprove    = "price_rule.approve"
	PermPriceRuleActivate   = "price_rule.activate"
	PermPriceRuleDeactivate = "price_rule.deactivate"
)

// Canonical Audit Actions
const (
	AuditActionPriceCreated     = "PRICE_CREATED"
	AuditActionPriceUpdated     = "PRICE_UPDATED"
	AuditActionPriceChanged     = "PRICE_CHANGED" // Legacy alias
	AuditActionPriceSubmitted   = "PRICE_SUBMITTED"
	AuditActionPriceApproved    = "PRICE_APPROVED"
	AuditActionPriceRejected    = "PRICE_REJECTED"
	AuditActionPriceActivated   = "PRICE_ACTIVATED"
	AuditActionPriceDeactivated = "PRICE_DEACTIVATED"
	AuditActionPriceExpired     = "PRICE_EXPIRED"
)

// Canonical Outbox Events matching docs/architecture/33-phase2-batch2-pre-implementation-review.md
const (
	EventPriceCreated     = "pricing.price_rule.created.v1"
	EventPriceApproved    = "pricing.price_rule.approved.v1"
	EventPriceActivated   = "pricing.price_rule.activated.v1"
	EventPriceDeactivated = "pricing.price_rule.deactivated.v1"
	EventPriceExpired     = "pricing.price_rule.expired.v1"
)

// Specificity Tiers (DEC-P2-018: 1 to 8)
const (
	TierStoreCustomerGroupVariant = 1 // Store + Customer Group + Variant
	TierStoreVariant              = 2 // Store + Variant
	TierCustomerGroupVariant      = 3 // Customer Group + Variant
	TierBaseListVariant           = 4 // Base List + Variant
	TierStoreCustomerGroupProduct = 5 // Store + Customer Group + Product
	TierStoreProduct              = 6 // Store + Product
	TierCustomerGroupProduct      = 7 // Customer Group + Product
	TierBaseListProduct           = 8 // Base List + Product
)

// PriceList represents a tenant-isolated pricing catalog.
type PriceList struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	CompanyID *uuid.UUID `json:"company_id,omitempty"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Currency  string     `json:"currency"`
	Channel   string     `json:"channel"`
	Status    string     `json:"status"`
	IsDefault bool       `json:"is_default"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Validate validates PriceList attributes.
func (pl *PriceList) Validate() error {
	if pl.Code == "" || len(pl.Code) > 50 {
		return errors.New("price list code is required and must not exceed 50 characters")
	}
	if pl.Name == "" || len(pl.Name) > 150 {
		return errors.New("price list name is required and must not exceed 150 characters")
	}
	if len(pl.Currency) != 3 {
		return errors.New("price list currency must be a 3-character ISO code")
	}
	if pl.Channel == "" || len(pl.Channel) > 30 {
		return errors.New("price list channel is required and must not exceed 30 characters")
	}
	switch pl.Status {
	case PriceListStatusActive, PriceListStatusInactive, PriceListStatusArchived:
	default:
		return fmt.Errorf("invalid price list status: %s", pl.Status)
	}
	if pl.IsDefault && pl.CompanyID != nil {
		return errors.New("default price list must be tenant-wide (company_id must be null)")
	}
	return nil
}

// PriceRule represents a specific pricing rule within a price list.
type PriceRule struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	PriceListID     uuid.UUID  `json:"price_list_id"`
	ProductID       uuid.UUID  `json:"product_id"`
	VariantID       *uuid.UUID `json:"variant_id,omitempty"`
	CustomerGroupID *uuid.UUID `json:"customer_group_id,omitempty"`
	StoreID         *uuid.UUID `json:"store_id,omitempty"`
	MinQty          float64    `json:"min_qty"`
	UnitPrice       float64    `json:"unit_price"`
	Priority        int        `json:"priority"`
	EffectiveFrom   time.Time  `json:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to,omitempty"`
	Status          string     `json:"status"`
	Version         int64      `json:"version"`

	// Exact rational representations preserving database precision without binary float intermediate
	unitPriceRat *big.Rat
	minQtyRat    *big.Rat
}

// SetScannedDecimals sets exact rational values parsed directly from database numeric text.
func (pr *PriceRule) SetScannedDecimals(minQtyStr, unitPriceStr string) {
	pr.MinQty, _ = strconv.ParseFloat(minQtyStr, 64)
	pr.UnitPrice, _ = strconv.ParseFloat(unitPriceStr, 64)
	if r, ok := new(big.Rat).SetString(minQtyStr); ok {
		pr.minQtyRat = r
	}
	if r, ok := new(big.Rat).SetString(unitPriceStr); ok {
		pr.unitPriceRat = r
	}
}

// GetUnitPriceRat returns the exact rational unit price.
func (pr *PriceRule) GetUnitPriceRat() *big.Rat {
	if pr.unitPriceRat != nil {
		return new(big.Rat).Set(pr.unitPriceRat)
	}
	if r, ok := new(big.Rat).SetString(strconv.FormatFloat(pr.UnitPrice, 'f', 4, 64)); ok {
		pr.unitPriceRat = r
		return new(big.Rat).Set(r)
	}
	r := new(big.Rat).SetFloat64(pr.UnitPrice)
	pr.unitPriceRat = r
	return new(big.Rat).Set(r)
}

// SetUnitPriceRat assigns an exact rational unit price.
func (pr *PriceRule) SetUnitPriceRat(r *big.Rat) {
	if r != nil {
		pr.unitPriceRat = new(big.Rat).Set(r)
		pr.UnitPrice, _ = r.Float64()
	}
}

// GetMinQtyRat returns the exact rational min quantity.
func (pr *PriceRule) GetMinQtyRat() *big.Rat {
	if pr.minQtyRat != nil {
		return new(big.Rat).Set(pr.minQtyRat)
	}
	if r, ok := new(big.Rat).SetString(strconv.FormatFloat(pr.MinQty, 'f', 6, 64)); ok {
		pr.minQtyRat = r
		return new(big.Rat).Set(r)
	}
	r := new(big.Rat).SetFloat64(pr.MinQty)
	pr.minQtyRat = r
	return new(big.Rat).Set(r)
}

// SetMinQtyRat assigns an exact rational min quantity.
func (pr *PriceRule) SetMinQtyRat(r *big.Rat) {
	if r != nil {
		pr.minQtyRat = new(big.Rat).Set(r)
		pr.MinQty, _ = r.Float64()
	}
}

// Validate validates PriceRule attributes.
func (pr *PriceRule) Validate() error {
	if pr.PriceListID == uuid.Nil {
		return errors.New("price_list_id is required")
	}
	if pr.ProductID == uuid.Nil {
		return errors.New("product_id is required")
	}
	if pr.MinQty <= 0 {
		return errors.New("min_qty must be greater than zero")
	}
	if pr.UnitPrice < 0 {
		return errors.New("unit_price must be non-negative")
	}
	if pr.EffectiveFrom.IsZero() {
		return errors.New("effective_from is required")
	}
	if pr.EffectiveTo != nil && !pr.EffectiveTo.After(pr.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	switch pr.Status {
	case PriceRuleStatusDraft, PriceRuleStatusApproved, PriceRuleStatusActive, PriceRuleStatusInactive, PriceRuleStatusExpired:
	default:
		return fmt.Errorf("invalid price rule status: %s", pr.Status)
	}
	return nil
}

// CartItem represents an item to be quoted.
type CartItem struct {
	VariantID uuid.UUID `json:"variant_id"`
	Quantity  float64   `json:"quantity"`
}

// GetQuantityRat returns the quantity as an exact rational number.
func (c CartItem) GetQuantityRat() *big.Rat {
	if r, ok := new(big.Rat).SetString(strconv.FormatFloat(c.Quantity, 'f', -1, 64)); ok {
		return r
	}
	return new(big.Rat).SetFloat64(c.Quantity)
}

// PriceQuoteRequest represents the request body for quote calculation.
type PriceQuoteRequest struct {
	StoreID     uuid.UUID  `json:"store_id"`
	CustomerID  *uuid.UUID `json:"customer_id,omitempty"`
	Currency    string     `json:"currency"`
	Channel     string     `json:"channel,omitempty"`
	PriceListID *uuid.UUID `json:"price_list_id,omitempty"`
	Items       []CartItem `json:"items"`
}

// Validate validates PriceQuoteRequest.
func (r *PriceQuoteRequest) Validate() error {
	if r.StoreID == uuid.Nil {
		return errors.New("store_id is required")
	}
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if len(r.Currency) != 3 {
		return errors.New("currency must be a valid 3-letter ISO code")
	}
	if len(r.Items) == 0 {
		return errors.New("items must contain at least one item")
	}
	if len(r.Items) > 500 {
		return errors.New("items must not exceed 500 items")
	}
	for i, it := range r.Items {
		if it.VariantID == uuid.Nil {
			return fmt.Errorf("items[%d].variant_id is required", i)
		}
		if it.Quantity <= 0 {
			return fmt.Errorf("items[%d].quantity must be positive", i)
		}
	}
	if r.Channel == "" {
		r.Channel = "POS"
	}
	return nil
}

// PriceQuoteItem represents the calculated line in a quote.
type PriceQuoteItem struct {
	VariantID uuid.UUID `json:"variant_id"`
	Quantity  float64   `json:"quantity"`
	UnitPrice float64   `json:"unit_price"`
	Discount  float64   `json:"discount"`
	NetAmount float64   `json:"net_amount"`
}

// PriceQuote represents the full calculated quote response.
type PriceQuote struct {
	Items         []PriceQuoteItem `json:"items"`
	Subtotal      float64          `json:"subtotal"`
	DiscountTotal float64          `json:"discount_total"`
	TaxTotal      float64          `json:"tax_total"`
	GrandTotal    float64          `json:"grand_total"`
	Currency      string           `json:"currency"`
}

// UnpricedItemDetail represents an unpriced cart item in 422 error details.
type UnpricedItemDetail struct {
	Index     int    `json:"index"`
	VariantID string `json:"variant_id"`
}

// PricingContext provides contextual data for rule resolution.
type PricingContext struct {
	TenantID        uuid.UUID
	StoreID         uuid.UUID
	CompanyID       *uuid.UUID
	CustomerGroupID *uuid.UUID
	Currency        string
	Channel         string
	ReferenceTime   time.Time
}

// CreatePriceListRequest models the payload to create a price list.
type CreatePriceListRequest struct {
	CompanyID *uuid.UUID `json:"company_id,omitempty"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Currency  string     `json:"currency"`
	Channel   string     `json:"channel"`
	Status    string     `json:"status,omitempty"`
	IsDefault bool       `json:"is_default"`
}

// UpdatePriceListRequest models the payload to update a price list.
type UpdatePriceListRequest struct {
	Name      *string `json:"name,omitempty"`
	Status    *string `json:"status,omitempty"`
	IsDefault *bool   `json:"is_default,omitempty"`
	Version   int64   `json:"version"`
}

// CreatePriceRuleRequest models the payload to create a draft price rule.
type CreatePriceRuleRequest struct {
	PriceListID     uuid.UUID  `json:"price_list_id"`
	ProductID       uuid.UUID  `json:"product_id"`
	VariantID       *uuid.UUID `json:"variant_id,omitempty"`
	CustomerGroupID *uuid.UUID `json:"customer_group_id,omitempty"`
	StoreID         *uuid.UUID `json:"store_id,omitempty"`
	MinQty          float64    `json:"min_qty"`
	UnitPrice       float64    `json:"unit_price"`
	Priority        int        `json:"priority"`
	EffectiveFrom   time.Time  `json:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to,omitempty"`
}

// UpdatePriceRuleRequest models the payload to update an editable price rule.
type UpdatePriceRuleRequest struct {
	CustomerGroupID *uuid.UUID `json:"customer_group_id,omitempty"`
	StoreID         *uuid.UUID `json:"store_id,omitempty"`
	MinQty          *float64   `json:"min_qty,omitempty"`
	UnitPrice       *float64   `json:"unit_price,omitempty"`
	Priority        *int       `json:"priority,omitempty"`
	EffectiveFrom   *time.Time `json:"effective_from,omitempty"`
	EffectiveTo     *time.Time `json:"effective_to,omitempty"`
	Reason          string     `json:"reason"`
	Version         int64      `json:"version"`
}

// RuleTransitionRequest models action requests (approve, reject, activate, deactivate).
type RuleTransitionRequest struct {
	Reason  string `json:"reason,omitempty"`
	Version int64  `json:"version"`
}
