// Package pricing provides the pure deterministic rule evaluation engine.
package pricing

import (
	"bytes"
	"math/big"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ResolveItemInput represents the parameters required to price a single line item.
type ResolveItemInput struct {
	VariantID   uuid.UUID
	ProductID   uuid.UUID
	Quantity    float64
	QuantityRat *big.Rat
}

// GetQuantityRat returns the quantity as an exact rational.
func (in ResolveItemInput) GetQuantityRat() *big.Rat {
	if in.QuantityRat != nil {
		return new(big.Rat).Set(in.QuantityRat)
	}
	if r, ok := new(big.Rat).SetString(strconv.FormatFloat(in.Quantity, 'f', -1, 64)); ok {
		return r
	}
	return new(big.Rat).SetFloat64(in.Quantity)
}

// LinePriceResult represents the winning price rule and line pricing calculation.
type LinePriceResult struct {
	WinningRule  *PriceRule
	Tier         int
	UnitPrice    float64
	Quantity     float64
	LineTotal    float64
	LineTotalRat *big.Rat
}

// Engine evaluates candidate pricing rules deterministically.
type Engine struct{}

// NewEngine constructs a new pure pricing Engine.
func NewEngine() *Engine {
	return &Engine{}
}

// DetermineTier calculates the specificity tier (1 to 8) for a candidate rule.
func DetermineTier(rule *PriceRule) int {
	hasStore := rule.StoreID != nil
	hasCustomerGroup := rule.CustomerGroupID != nil
	hasVariant := rule.VariantID != nil

	switch {
	case hasStore && hasCustomerGroup && hasVariant:
		return TierStoreCustomerGroupVariant // 1
	case hasStore && !hasCustomerGroup && hasVariant:
		return TierStoreVariant // 2
	case !hasStore && hasCustomerGroup && hasVariant:
		return TierCustomerGroupVariant // 3
	case !hasStore && !hasCustomerGroup && hasVariant:
		return TierBaseListVariant // 4
	case hasStore && hasCustomerGroup && !hasVariant:
		return TierStoreCustomerGroupProduct // 5
	case hasStore && !hasCustomerGroup && !hasVariant:
		return TierStoreProduct // 6
	case !hasStore && hasCustomerGroup && !hasVariant:
		return TierCustomerGroupProduct // 7
	default:
		return TierBaseListProduct // 8
	}
}

// IsEligible checks whether a candidate rule matches the context, item, and reference time.
func IsEligible(rule *PriceRule, ctx PricingContext, item ResolveItemInput, refTime time.Time) bool {
	if rule == nil {
		return false
	}
	// Status must be ACTIVE
	if rule.Status != PriceRuleStatusActive {
		return false
	}
	// Tenant must match
	if rule.TenantID != ctx.TenantID {
		return false
	}
	// Product must match
	if rule.ProductID != item.ProductID {
		return false
	}
	// Quantity break: min_qty <= requested quantity
	if item.Quantity < rule.MinQty {
		return false
	}
	// Effective time period check: effective_from <= refTime < effective_to (if effective_to is non-null)
	if refTime.Before(rule.EffectiveFrom) {
		return false
	}
	if rule.EffectiveTo != nil && !refTime.Before(*rule.EffectiveTo) {
		return false
	}
	// Store matching: if rule targets a store, it must match ctx.StoreID
	if rule.StoreID != nil && *rule.StoreID != ctx.StoreID {
		return false
	}
	// Customer group matching: if rule targets a customer group, context must have it and match
	if rule.CustomerGroupID != nil {
		if ctx.CustomerGroupID == nil || *rule.CustomerGroupID != *ctx.CustomerGroupID {
			return false
		}
	}
	// Variant matching: if rule targets a specific variant, it must match item.VariantID
	if rule.VariantID != nil && *rule.VariantID != item.VariantID {
		return false
	}

	return true
}

// CompareRules implements deterministic tie-breaking within the same specificity tier:
// 1. min_qty DESC
// 2. priority DESC
// 3. effective_from DESC
// 4. id ASC
func CompareRules(a, b *PriceRule) int {
	if a.MinQty != b.MinQty {
		if a.MinQty > b.MinQty {
			return -1 // a wins (higher min_qty)
		}
		return 1 // b wins
	}
	if a.Priority != b.Priority {
		if a.Priority > b.Priority {
			return -1 // a wins (higher priority)
		}
		return 1 // b wins
	}
	if !a.EffectiveFrom.Equal(b.EffectiveFrom) {
		if a.EffectiveFrom.After(b.EffectiveFrom) {
			return -1 // a wins (newer effective_from)
		}
		return 1 // b wins
	}
	// Final deterministic tie-breaker: ID ASC
	cmp := bytes.Compare(a.ID[:], b.ID[:])
	if cmp < 0 {
		return -1 // a wins (lower ID)
	} else if cmp > 0 {
		return 1 // b wins
	}
	return 0
}

// ResolveLine finds the winning rule for a single item from a list of candidates.
// Returns nil if no eligible rule matches.
func (e *Engine) ResolveLine(ctx PricingContext, item ResolveItemInput, candidates []*PriceRule, refTime time.Time) *LinePriceResult {
	if item.Quantity <= 0 {
		return nil
	}

	// Filter eligible rules and bucket by specificity tier
	tierBuckets := make(map[int][]*PriceRule)
	for _, rule := range candidates {
		if IsEligible(rule, ctx, item, refTime) {
			tier := DetermineTier(rule)
			tierBuckets[tier] = append(tierBuckets[tier], rule)
		}
	}

	// Search tiers in order of precedence: Tier 1 to Tier 8
	for tier := TierStoreCustomerGroupVariant; tier <= TierBaseListProduct; tier++ {
		rules := tierBuckets[tier]
		if len(rules) == 0 {
			continue
		}

		// Sort rules in this tier using canonical tie-breakers
		sort.SliceStable(rules, func(i, j int) bool {
			return CompareRules(rules[i], rules[j]) < 0
		})

		winning := rules[0]
		unitPrice := RoundTo4(winning.UnitPrice)
		unitPriceRat := winning.GetUnitPriceRat()
		quantityRat := item.GetQuantityRat()
		lineTotalRat, lineTotal := MultiplyExactRat(unitPriceRat, quantityRat)

		return &LinePriceResult{
			WinningRule:  winning,
			Tier:         tier,
			UnitPrice:    unitPrice,
			Quantity:     item.Quantity,
			LineTotal:    lineTotal,
			LineTotalRat: lineTotalRat,
		}
	}

	return nil
}

// MultiplyExactRat performs exact multiplication of unit price and quantity using math/big.Rat,
// and applies canonical half-up rounding to 4 decimal places without any floating-point intermediate.
func MultiplyExactRat(unitPriceRat, quantityRat *big.Rat) (*big.Rat, float64) {
	if unitPriceRat == nil || quantityRat == nil {
		return new(big.Rat), 0.0
	}
	product := new(big.Rat).Mul(unitPriceRat, quantityRat)

	scale := big.NewRat(10000, 1)
	scaled := new(big.Rat).Mul(product, scale)

	half := big.NewRat(1, 2)
	rounded := new(big.Rat).Add(scaled, half)

	intVal := new(big.Int).Quo(rounded.Num(), rounded.Denom())
	finalRat := new(big.Rat).SetFrac(intVal, big.NewInt(10000))
	res, _ := finalRat.Float64()
	return finalRat, res
}

// MultiplyExactMoney calculates unitPrice * quantity using exact rational arithmetic (math/big.Rat)
// and rounds half-up to 4 decimal places.
func MultiplyExactMoney(unitPrice float64, quantity float64) float64 {
	rPrice, ok1 := new(big.Rat).SetString(strconv.FormatFloat(unitPrice, 'f', -1, 64))
	rQty, ok2 := new(big.Rat).SetString(strconv.FormatFloat(quantity, 'f', -1, 64))
	if !ok1 || !ok2 {
		rPrice = new(big.Rat).SetFloat64(unitPrice)
		rQty = new(big.Rat).SetFloat64(quantity)
	}
	_, res := MultiplyExactRat(rPrice, rQty)
	return res
}

// RoundTo4 rounds a float64 to exactly 4 decimal places using exact rational arithmetic.
func RoundTo4(val float64) float64 {
	rVal, ok := new(big.Rat).SetString(strconv.FormatFloat(val, 'f', -1, 64))
	if !ok {
		rVal = new(big.Rat).SetFloat64(val)
	}
	scale := big.NewRat(10000, 1)
	scaled := new(big.Rat).Mul(rVal, scale)
	half := big.NewRat(1, 2)
	rounded := new(big.Rat).Add(scaled, half)
	intVal := new(big.Int).Quo(rounded.Num(), rounded.Denom())
	finalRat := new(big.Rat).SetFrac(intVal, big.NewInt(10000))
	res, _ := finalRat.Float64()
	return res
}
