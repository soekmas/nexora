// Package pricing defines repository and external port contracts.
package pricing

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PriceListFilter defines query parameters for listing price lists.
type PriceListFilter struct {
	Currency *string
	Channel  *string
	Status   *string
	Page     int
	PageSize int
}

// PriceRuleFilter defines query parameters for listing price rules.
type PriceRuleFilter struct {
	PriceListID     *uuid.UUID
	ProductID       *uuid.UUID
	VariantID       *uuid.UUID
	StoreID         *uuid.UUID
	CustomerGroupID *uuid.UUID
	Status          *string
	Page            int
	PageSize        int
}

// Store defines persistence operations for pricing aggregates and lookup helpers.
type Store interface {
	// Price List operations
	CreatePriceList(ctx context.Context, pl *PriceList) error
	GetPriceListByID(ctx context.Context, id uuid.UUID) (*PriceList, error)
	GetPriceListByCode(ctx context.Context, code string) (*PriceList, error)
	UpdatePriceList(ctx context.Context, pl *PriceList, expectedVersion int64) error
	ListPriceLists(ctx context.Context, filter PriceListFilter) ([]*PriceList, int, error)
	GetDefaultPriceList(ctx context.Context, currency, channel string) (*PriceList, error)

	// Price Rule operations
	CreatePriceRule(ctx context.Context, pr *PriceRule) error
	GetPriceRuleByID(ctx context.Context, id uuid.UUID) (*PriceRule, error)
	GetPriceRuleByIDForUpdate(ctx context.Context, id uuid.UUID) (*PriceRule, error)
	UpdatePriceRule(ctx context.Context, pr *PriceRule, expectedVersion int64) error
	ListPriceRules(ctx context.Context, filter PriceRuleFilter) ([]*PriceRule, int, error)
	GetCandidateRules(ctx context.Context, priceListID uuid.UUID, productIDs []uuid.UUID, variantIDs []uuid.UUID, refTime time.Time) ([]*PriceRule, error)

	// Context lookups
	GetStore(ctx context.Context, storeID uuid.UUID) (companyID uuid.UUID, status string, err error)
	GetCustomerGroup(ctx context.Context, customerID uuid.UUID) (customerGroupID *uuid.UUID, status string, err error)
	GetVariants(ctx context.Context, variantIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
}
