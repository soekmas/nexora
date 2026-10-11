package promotion

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CartItem represents an item line in the cart for promotion evaluation.
type CartItem struct {
	LineID       string     `json:"line_id,omitempty"`
	ProductID    uuid.UUID  `json:"product_id"`
	VariantID    *uuid.UUID `json:"variant_id,omitempty"`
	CategoryID   string     `json:"category_id,omitempty"`
	Name         string     `json:"name"`
	Quantity     float64    `json:"quantity"`
	UnitPrice    float64    `json:"unit_price"`
	quantityRat  *big.Rat
	unitPriceRat *big.Rat
}

// GetQuantityRat returns exact quantity as *big.Rat.
func (c *CartItem) GetQuantityRat() *big.Rat {
	if c.quantityRat != nil {
		return new(big.Rat).Set(c.quantityRat)
	}
	r := ratFromFloat(c.Quantity)
	c.quantityRat = new(big.Rat).Set(r)
	return r
}

// GetUnitPriceRat returns exact unit price as *big.Rat.
func (c *CartItem) GetUnitPriceRat() *big.Rat {
	if c.unitPriceRat != nil {
		return new(big.Rat).Set(c.unitPriceRat)
	}
	r := ratFromFloat(c.UnitPrice)
	c.unitPriceRat = new(big.Rat).Set(r)
	return r
}

// GetLineTotalRat returns exact line total (Quantity * UnitPrice) as *big.Rat.
func (c *CartItem) GetLineTotalRat() *big.Rat {
	q := c.GetQuantityRat()
	p := c.GetUnitPriceRat()
	return new(big.Rat).Mul(q, p)
}

// BonusItem explicitly details bonus/free items rewarded to the customer.
type BonusItem struct {
	PromotionID    uuid.UUID  `json:"promotion_id"`
	PromotionCode  string     `json:"promotion_code"`
	ProductID      uuid.UUID  `json:"product_id"`
	VariantID      *uuid.UUID `json:"variant_id,omitempty"`
	ItemName       string     `json:"item_name"`
	FreeQuantity   float64    `json:"free_quantity"`
	DiscountAmount float64    `json:"discount_amount"`
}

// AppliedPromotion details a promotion rule applied to the evaluation result.
type AppliedPromotion struct {
	PromotionID         uuid.UUID              `json:"promotion_id"`
	PromotionCode       string                 `json:"promotion_code"`
	PromotionName       string                 `json:"promotion_name"`
	StackingMode        string                 `json:"stacking_mode"`
	Priority            int                    `json:"priority"`
	RewardType          string                 `json:"reward_type"`
	DiscountAmount      float64                `json:"discount_amount"`
	BonusItems          []BonusItem            `json:"bonus_items,omitempty"`
	CalculationSnapshot map[string]interface{} `json:"calculation_snapshot,omitempty"`
	discountAmountRat   *big.Rat
}

// GetDiscountAmountRat returns exact discount amount as *big.Rat.
func (ap *AppliedPromotion) GetDiscountAmountRat() *big.Rat {
	if ap.discountAmountRat != nil {
		return new(big.Rat).Set(ap.discountAmountRat)
	}
	return ratFromFloat(ap.DiscountAmount)
}

// EvaluationInput provides the context and cart items for pure promotion calculation.
type EvaluationInput struct {
	TenantID        uuid.UUID   `json:"tenant_id"`
	StoreID         *uuid.UUID  `json:"store_id,omitempty"`
	Channel         string      `json:"channel"`
	CustomerTier    string      `json:"customer_tier,omitempty"`
	CustomerGroupID *uuid.UUID  `json:"customer_group_id,omitempty"`
	EvaluationTime  time.Time   `json:"evaluation_time"`
	Items           []CartItem  `json:"items"`
	Promotions      []Promotion `json:"promotions"`
}

// Validate validates the evaluation input.
func (in *EvaluationInput) Validate() error {
	if in.TenantID == uuid.Nil {
		return errors.New("tenant_id is required")
	}
	if len(in.Items) == 0 {
		return errors.New("items must contain at least one cart item")
	}
	for i, it := range in.Items {
		if it.ProductID == uuid.Nil {
			return fmt.Errorf("items[%d].product_id is required", i)
		}
		if it.Quantity <= 0 {
			return fmt.Errorf("items[%d].quantity must be positive", i)
		}
		if it.UnitPrice < 0 {
			return fmt.Errorf("items[%d].unit_price must be non-negative", i)
		}
	}
	if in.Channel == "" {
		in.Channel = "POS"
	}
	if in.EvaluationTime.IsZero() {
		in.EvaluationTime = time.Now().UTC()
	}
	return nil
}

// EvaluationOutput represents the pure deterministic evaluation result.
type EvaluationOutput struct {
	Subtotal          float64            `json:"subtotal"`
	DiscountTotal     float64            `json:"discount_total"`
	NetTotal          float64            `json:"net_total"`
	AppliedPromotions []AppliedPromotion `json:"applied_promotions"`
	BonusItems        []BonusItem        `json:"bonus_items,omitempty"`
	subtotalRat       *big.Rat
	discountTotalRat  *big.Rat
	netTotalRat       *big.Rat
}

// GetSubtotalRat returns exact subtotal as *big.Rat.
func (out *EvaluationOutput) GetSubtotalRat() *big.Rat {
	if out.subtotalRat != nil {
		return new(big.Rat).Set(out.subtotalRat)
	}
	return ratFromFloat(out.Subtotal)
}

// GetDiscountTotalRat returns exact discount total as *big.Rat.
func (out *EvaluationOutput) GetDiscountTotalRat() *big.Rat {
	if out.discountTotalRat != nil {
		return new(big.Rat).Set(out.discountTotalRat)
	}
	return ratFromFloat(out.DiscountTotal)
}

// GetNetTotalRat returns exact net total as *big.Rat.
func (out *EvaluationOutput) GetNetTotalRat() *big.Rat {
	if out.netTotalRat != nil {
		return new(big.Rat).Set(out.netTotalRat)
	}
	return ratFromFloat(out.NetTotal)
}

// ratFromFloat converts a float64 to exact *big.Rat via string representation to avoid precision drift.
func ratFromFloat(val float64) *big.Rat {
	str := strconv.FormatFloat(val, 'f', -1, 64)
	if r, ok := new(big.Rat).SetString(str); ok {
		return r
	}
	return new(big.Rat).SetFloat64(val)
}

// roundRatToFourDecimals rounds a big.Rat to 4 decimal places using Half-Up rounding.
func roundRatToFourDecimals(r *big.Rat) (*big.Rat, float64) {
	if r == nil {
		return new(big.Rat), 0.0
	}
	scale := big.NewRat(10000, 1)
	scaled := new(big.Rat).Mul(r, scale)

	half := big.NewRat(1, 2)
	var rounded *big.Rat
	if r.Sign() >= 0 {
		rounded = new(big.Rat).Add(scaled, half)
	} else {
		rounded = new(big.Rat).Sub(scaled, half)
	}

	num := rounded.Num()
	denom := rounded.Denom()
	intVal := new(big.Int).Quo(num, denom)

	finalRat := new(big.Rat).SetFrac(intVal, big.NewInt(10000))
	f, _ := finalRat.Float64()
	return finalRat, f
}

// candidateEval holds internal evaluation state for an eligible candidate promotion.
type candidateEval struct {
	promotion  Promotion
	discount   *big.Rat
	rewardType string
	bonusItems []BonusItem
	snapshot   map[string]interface{}
}

// Evaluate performs pure deterministic promotion calculation.
func Evaluate(input EvaluationInput) (*EvaluationOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// 1. Calculate Cart Subtotal using exact rational arithmetic
	subtotalRat := new(big.Rat)
	for i := range input.Items {
		lineTotal := input.Items[i].GetLineTotalRat()
		subtotalRat.Add(subtotalRat, lineTotal)
	}
	subtotalRatRounded, subtotalFloat := roundRatToFourDecimals(subtotalRat)

	// 2. Filter Active & Eligible Candidate Promotions
	var eligibleCandidates []candidateEval
	for _, promo := range input.Promotions {
		// Must be ACTIVE
		if promo.Status != StatusActive {
			continue
		}
		// Channel match
		if promo.Channel != "ALL" && !strings.EqualFold(promo.Channel, input.Channel) {
			continue
		}
		// Effective period match
		if !promo.IsEffectiveAt(input.EvaluationTime) {
			continue
		}
		// Usage limit check (pure read-only: does NOT consume/mutate usage_count)
		if !promo.HasUsageAvailable() {
			continue
		}
		// Conditions match (AND logic)
		if !evaluateConditions(promo.Conditions, input.Items, subtotalRat) {
			continue
		}

		// Calculate discount and bonus items for this candidate
		cand, err := calculateCandidateReward(promo, input.Items, subtotalRat)
		if err != nil {
			return nil, fmt.Errorf("calculate reward for promo %s: %w", promo.Code, err)
		}
		if cand.discount.Sign() > 0 || len(cand.bonusItems) > 0 {
			eligibleCandidates = append(eligibleCandidates, *cand)
		}
	}

	// 3. Sort Eligible Candidates Deterministically:
	// Priority DESC, then EffectiveFrom ASC, then CreatedAt ASC, then ID ASC
	sort.SliceStable(eligibleCandidates, func(i, j int) bool {
		pi, pj := eligibleCandidates[i].promotion, eligibleCandidates[j].promotion
		if pi.Priority != pj.Priority {
			return pi.Priority > pj.Priority
		}
		if !pi.EffectiveFrom.Equal(pj.EffectiveFrom) {
			return pi.EffectiveFrom.Before(pj.EffectiveFrom)
		}
		if !pi.CreatedAt.Equal(pj.CreatedAt) {
			return pi.CreatedAt.Before(pj.CreatedAt)
		}
		return pi.ID.String() < pj.ID.String()
	})

	// 4. Resolve Stacking Policies (STACKABLE, EXCLUSIVE, BEST_ONLY)
	selectedCandidates := resolveStacking(eligibleCandidates, subtotalRat)

	// 5. Accumulate Applied Discounts with Subtotal Floor & Cap Guarantee
	appliedPromotions := make([]AppliedPromotion, 0, len(selectedCandidates))
	var allBonusItems []BonusItem
	discountTotalRat := new(big.Rat)

	for _, cand := range selectedCandidates {
		// Calculate remaining budget
		remainingRat := new(big.Rat).Sub(subtotalRat, discountTotalRat)
		if remainingRat.Sign() <= 0 && len(cand.bonusItems) == 0 {
			// Subtotal fully discounted; further monetary discounts capped to zero
			continue
		}

		actualPromoDiscount := new(big.Rat).Set(cand.discount)
		if actualPromoDiscount.Cmp(remainingRat) > 0 {
			actualPromoDiscount.Set(remainingRat)
		}
		if actualPromoDiscount.Sign() < 0 {
			actualPromoDiscount.SetInt64(0)
		}

		discountTotalRat.Add(discountTotalRat, actualPromoDiscount)
		_, discFloat := roundRatToFourDecimals(actualPromoDiscount)

		appliedPromotions = append(appliedPromotions, AppliedPromotion{
			PromotionID:         cand.promotion.ID,
			PromotionCode:       cand.promotion.Code,
			PromotionName:       cand.promotion.Name,
			StackingMode:        cand.promotion.StackingMode,
			Priority:            cand.promotion.Priority,
			RewardType:          cand.rewardType,
			DiscountAmount:      discFloat,
			BonusItems:          cand.bonusItems,
			CalculationSnapshot: cand.snapshot,
			discountAmountRat:   new(big.Rat).Set(actualPromoDiscount),
		})
		allBonusItems = append(allBonusItems, cand.bonusItems...)
	}

	// Guarantee discountTotal does not exceed subtotal and is not negative
	if discountTotalRat.Cmp(subtotalRat) > 0 {
		discountTotalRat.Set(subtotalRat)
	}
	if discountTotalRat.Sign() < 0 {
		discountTotalRat.SetInt64(0)
	}

	discountTotalRounded, discountTotalFloat := roundRatToFourDecimals(discountTotalRat)
	netTotalRat := new(big.Rat).Sub(subtotalRatRounded, discountTotalRounded)
	if netTotalRat.Sign() < 0 {
		netTotalRat.SetInt64(0)
	}
	netTotalRounded, netTotalFloat := roundRatToFourDecimals(netTotalRat)

	return &EvaluationOutput{
		Subtotal:          subtotalFloat,
		DiscountTotal:     discountTotalFloat,
		NetTotal:          netTotalFloat,
		AppliedPromotions: appliedPromotions,
		BonusItems:        allBonusItems,
		subtotalRat:       subtotalRatRounded,
		discountTotalRat:  discountTotalRounded,
		netTotalRat:       netTotalRounded,
	}, nil
}

// evaluateConditions evaluates all conditions for a promotion (AND logic).
func evaluateConditions(conditions []Condition, items []CartItem, subtotalRat *big.Rat) bool {
	for _, cond := range conditions {
		switch cond.Type {
		case ConditionTypeMinOrderAmount:
			var cfg MinOrderAmountConfig
			if err := json.Unmarshal(cond.Config, &cfg); err != nil {
				return false
			}
			minRat := ratFromFloat(cfg.MinAmount)
			if subtotalRat.Cmp(minRat) < 0 {
				return false
			}

		case ConditionTypeMinItemQty:
			var cfg MinItemQtyConfig
			if err := json.Unmarshal(cond.Config, &cfg); err != nil {
				return false
			}
			minQtyRat := ratFromFloat(cfg.MinQty)
			matchingQtyRat := new(big.Rat)

			hasProductFilter := len(cfg.ProductIDs) > 0
			hasCategoryFilter := len(cfg.CategoryIDs) > 0

			for _, it := range items {
				match := true
				if hasProductFilter {
					match = false
					for _, pid := range cfg.ProductIDs {
						if it.ProductID == pid {
							match = true
							break
						}
					}
				}
				if match && hasCategoryFilter {
					match = false
					for _, cid := range cfg.CategoryIDs {
						if strings.EqualFold(it.CategoryID, cid) {
							match = true
							break
						}
					}
				}
				if match {
					matchingQtyRat.Add(matchingQtyRat, it.GetQuantityRat())
				}
			}
			if matchingQtyRat.Cmp(minQtyRat) < 0 {
				return false
			}

		case ConditionTypeProductInList:
			var cfg ProductInListConfig
			if err := json.Unmarshal(cond.Config, &cfg); err != nil {
				return false
			}
			matched := false
			for _, it := range items {
				for _, pid := range cfg.ProductIDs {
					if it.ProductID == pid {
						matched = true
						break
					}
				}
				if !matched && len(cfg.VariantIDs) > 0 && it.VariantID != nil {
					for _, vid := range cfg.VariantIDs {
						if *it.VariantID == vid {
							matched = true
							break
						}
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				return false
			}

		case ConditionTypeCategoryInList:
			var cfg CategoryInListConfig
			if err := json.Unmarshal(cond.Config, &cfg); err != nil {
				return false
			}
			matched := false
			for _, it := range items {
				for _, cid := range cfg.CategoryIDs {
					if strings.EqualFold(it.CategoryID, cid) {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				return false
			}

		default:
			// Unknown condition type: fail safe
			return false
		}
	}
	return true
}

// calculateCandidateReward calculates the discount and bonus items for a candidate promotion.
func calculateCandidateReward(promo Promotion, items []CartItem, subtotalRat *big.Rat) (*candidateEval, error) {
	totalDiscountRat := new(big.Rat)
	var bonusItems []BonusItem
	snapshot := make(map[string]interface{})
	var primaryRewardType string

	for _, rew := range promo.Rewards {
		primaryRewardType = rew.Type
		switch rew.Type {
		case RewardTypePercentageDiscount:
			var cfg PercentageDiscountConfig
			if err := json.Unmarshal(rew.Config, &cfg); err != nil {
				return nil, err
			}
			pctRat := ratFromFloat(cfg.Percentage / 100.0)
			disc := new(big.Rat).Mul(subtotalRat, pctRat)

			if cfg.MaxDiscountAmount != nil && *cfg.MaxDiscountAmount > 0 {
				capRat := ratFromFloat(*cfg.MaxDiscountAmount)
				if disc.Cmp(capRat) > 0 {
					disc.Set(capRat)
				}
			}
			totalDiscountRat.Add(totalDiscountRat, disc)
			snapshot["percentage"] = cfg.Percentage
			if cfg.MaxDiscountAmount != nil {
				snapshot["max_discount_cap"] = *cfg.MaxDiscountAmount
			}

		case RewardTypeFixedAmountDiscount:
			var cfg FixedAmountDiscountConfig
			if err := json.Unmarshal(rew.Config, &cfg); err != nil {
				return nil, err
			}
			disc := ratFromFloat(cfg.Amount)
			totalDiscountRat.Add(totalDiscountRat, disc)
			snapshot["fixed_amount"] = cfg.Amount

		case RewardTypeBuyXGetY:
			var cfg BuyXGetYConfig
			if err := json.Unmarshal(rew.Config, &cfg); err != nil {
				return nil, err
			}
			if cfg.BuyQty <= 0 || cfg.GetQty <= 0 {
				continue
			}

			// Identify qualifying cart items based on conditions or reward config
			var qualifyingItems []CartItem
			qualifyingQty := 0.0

			// Extract product/category filters from promo conditions
			var filterProductIDs []uuid.UUID
			var filterCategoryIDs []string
			for _, c := range promo.Conditions {
				if c.Type == ConditionTypeProductInList {
					var pcfg ProductInListConfig
					_ = json.Unmarshal(c.Config, &pcfg)
					filterProductIDs = append(filterProductIDs, pcfg.ProductIDs...)
				}
				if c.Type == ConditionTypeCategoryInList {
					var ccfg CategoryInListConfig
					_ = json.Unmarshal(c.Config, &ccfg)
					filterCategoryIDs = append(filterCategoryIDs, ccfg.CategoryIDs...)
				}
			}

			for _, it := range items {
				match := true
				if len(filterProductIDs) > 0 {
					match = false
					for _, pid := range filterProductIDs {
						if it.ProductID == pid {
							match = true
							break
						}
					}
				}
				if match && len(filterCategoryIDs) > 0 {
					match = false
					for _, cid := range filterCategoryIDs {
						if strings.EqualFold(it.CategoryID, cid) {
							match = true
							break
						}
					}
				}
				if match {
					qualifyingItems = append(qualifyingItems, it)
					qualifyingQty += it.Quantity
				}
			}

			// Number of trigger sets
			sets := int(math.Floor(qualifyingQty / cfg.BuyQty))
			if cfg.MaxApplications != nil && *cfg.MaxApplications > 0 && sets > *cfg.MaxApplications {
				sets = *cfg.MaxApplications
			}
			if sets <= 0 {
				continue
			}

			totalBonusQty := float64(sets) * cfg.GetQty

			// Determine bonus product details & unit price
			var bonusProdID uuid.UUID
			var bonusVariantID *uuid.UUID
			var bonusItemName string
			bonusUnitPriceRat := new(big.Rat)

			if cfg.RewardProductID != nil {
				bonusProdID = *cfg.RewardProductID
				bonusVariantID = cfg.RewardVariantID
				bonusItemName = cfg.RewardItemName
				// Check if the bonus product is in the cart
				for _, it := range items {
					if it.ProductID == bonusProdID {
						bonusUnitPriceRat = it.GetUnitPriceRat()
						if bonusItemName == "" {
							bonusItemName = it.Name
						}
						break
					}
				}
			} else if len(qualifyingItems) > 0 {
				// Bonus item is the same as the qualifying item
				first := qualifyingItems[0]
				bonusProdID = first.ProductID
				bonusVariantID = first.VariantID
				bonusItemName = first.Name
				bonusUnitPriceRat = first.GetUnitPriceRat()
			}

			if bonusItemName == "" {
				bonusItemName = fmt.Sprintf("Bonus Item (Buy %v Get %v)", cfg.BuyQty, cfg.GetQty)
			}

			discountPct := cfg.DiscountPercentage
			if discountPct <= 0 {
				discountPct = 100.0 // Default 100% free
			}

			bonusQtyRat := ratFromFloat(totalBonusQty)
			discountPctRat := ratFromFloat(discountPct / 100.0)
			buyXGetYDiscountRat := new(big.Rat).Mul(bonusQtyRat, bonusUnitPriceRat)
			buyXGetYDiscountRat.Mul(buyXGetYDiscountRat, discountPctRat)

			totalDiscountRat.Add(totalDiscountRat, buyXGetYDiscountRat)
			_, bonusDiscFloat := roundRatToFourDecimals(buyXGetYDiscountRat)

			bonusItem := BonusItem{
				PromotionID:    promo.ID,
				PromotionCode:  promo.Code,
				ProductID:      bonusProdID,
				VariantID:      bonusVariantID,
				ItemName:       bonusItemName,
				FreeQuantity:   totalBonusQty,
				DiscountAmount: bonusDiscFloat,
			}
			bonusItems = append(bonusItems, bonusItem)
			snapshot["buy_x_get_y"] = map[string]interface{}{
				"sets":            sets,
				"bonus_quantity":  totalBonusQty,
				"discount_amount": bonusDiscFloat,
			}
		}
	}

	return &candidateEval{
		promotion:  promo,
		discount:   totalDiscountRat,
		rewardType: primaryRewardType,
		bonusItems: bonusItems,
		snapshot:   snapshot,
	}, nil
}

// resolveStacking resolves conflicts between STACKABLE, EXCLUSIVE, and BEST_ONLY promotions deterministically.
func resolveStacking(candidates []candidateEval, subtotalRat *big.Rat) []candidateEval {
	if len(candidates) == 0 {
		return nil
	}

	// Group candidates by stacking mode
	var stackableCandidates []candidateEval
	var exclusiveCandidates []candidateEval
	var bestOnlyCandidates []candidateEval

	for _, cand := range candidates {
		switch cand.promotion.StackingMode {
		case StackingModeExclusive:
			exclusiveCandidates = append(exclusiveCandidates, cand)
		case StackingModeBestOnly:
			bestOnlyCandidates = append(bestOnlyCandidates, cand)
		default: // STACKABLE
			stackableCandidates = append(stackableCandidates, cand)
		}
	}

	// 1. If the highest-priority overall candidate is EXCLUSIVE, it strictly takes exclusivity.
	topCandidate := candidates[0]
	if topCandidate.promotion.StackingMode == StackingModeExclusive {
		return []candidateEval{topCandidate}
	}

	// 2. Prepare Option A: Bundle of all eligible STACKABLE promotions
	stackableTotalRat := new(big.Rat)
	for _, cand := range stackableCandidates {
		stackableTotalRat.Add(stackableTotalRat, cand.discount)
	}

	// 3. Prepare Option B: Best candidate among BEST_ONLY promotions
	var bestOnlyChoice *candidateEval
	if len(bestOnlyCandidates) > 0 {
		best := bestOnlyCandidates[0]
		for i := 1; i < len(bestOnlyCandidates); i++ {
			curr := bestOnlyCandidates[i]
			if curr.discount.Cmp(best.discount) > 0 {
				best = curr
			}
		}
		bestOnlyChoice = &best
	}

	// 4. Prepare Option C: Best candidate among EXCLUSIVE promotions
	var bestExclusiveChoice *candidateEval
	if len(exclusiveCandidates) > 0 {
		best := exclusiveCandidates[0]
		for i := 1; i < len(exclusiveCandidates); i++ {
			curr := exclusiveCandidates[i]
			if curr.discount.Cmp(best.discount) > 0 {
				best = curr
			}
		}
		bestExclusiveChoice = &best
	}

	// Compare candidate sets to select the optimal discount for the customer
	// while respecting the respective exclusivity rules:
	bestBundle := stackableCandidates
	bestDiscount := stackableTotalRat

	if bestOnlyChoice != nil {
		if bestOnlyChoice.discount.Cmp(bestDiscount) > 0 {
			bestBundle = []candidateEval{*bestOnlyChoice}
			bestDiscount = bestOnlyChoice.discount
		}
	}

	if bestExclusiveChoice != nil {
		if bestExclusiveChoice.discount.Cmp(bestDiscount) > 0 {
			bestBundle = []candidateEval{*bestExclusiveChoice}
		}
	}

	return bestBundle
}
