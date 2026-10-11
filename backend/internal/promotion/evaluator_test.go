package promotion_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/promotion"
)

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// 1. Percentage Discount with and without cap
func TestEvaluator_PercentageDiscount(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()

	// Promo A: 10% discount, no cap
	promoA := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-PCT-NOCAP",
		Name:          "10% Off All",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{
				Type: promotion.RewardTypePercentageDiscount,
				Config: mustJSON(promotion.PercentageDiscountConfig{
					Percentage: 10.0,
				}),
			},
		},
	}

	// Cart: 2 items @ $100.00 = $200.00
	cart := []promotion.CartItem{
		{
			ProductID: prodID,
			Name:      "Premium Coffee",
			Quantity:  2,
			UnitPrice: 100.00,
		},
	}

	out, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		Channel:        "POS",
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoA},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}

	if out.Subtotal != 200.00 {
		t.Errorf("Subtotal mismatch: got %v, want 200.00", out.Subtotal)
	}
	if out.DiscountTotal != 20.00 {
		t.Errorf("DiscountTotal mismatch: got %v, want 20.00", out.DiscountTotal)
	}
	if out.NetTotal != 180.00 {
		t.Errorf("NetTotal mismatch: got %v, want 180.00", out.NetTotal)
	}

	// Promo B: 20% discount with cap of $15.00
	maxCap := 15.00
	promoB := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-PCT-CAP",
		Name:          "20% Off Capped at $15",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{
				Type: promotion.RewardTypePercentageDiscount,
				Config: mustJSON(promotion.PercentageDiscountConfig{
					Percentage:        20.0, // 20% of 200 = 40, but capped at 15
					MaxDiscountAmount: &maxCap,
				}),
			},
		},
	}

	outB, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		Channel:        "POS",
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoB},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outB.DiscountTotal != 15.00 {
		t.Errorf("Capped DiscountTotal mismatch: got %v, want 15.00", outB.DiscountTotal)
	}
	if outB.NetTotal != 185.00 {
		t.Errorf("Capped NetTotal mismatch: got %v, want 185.00", outB.NetTotal)
	}
}

// 2. Fixed Amount Discount and subtotal ceiling
func TestEvaluator_FixedAmountDiscount(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()

	// Promo: $25.00 Fixed Discount
	promoFixed := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-FIXED-25",
		Name:          "$25 Off",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{
				Type: promotion.RewardTypeFixedAmountDiscount,
				Config: mustJSON(promotion.FixedAmountDiscountConfig{
					Amount: 25.00,
				}),
			},
		},
	}

	// Cart: $50.00 subtotal -> Discount $25.00, Net $25.00
	cartA := []promotion.CartItem{
		{
			ProductID: prodID,
			Name:      "Item",
			Quantity:  1,
			UnitPrice: 50.00,
		},
	}
	outA, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cartA,
		Promotions:     []promotion.Promotion{promoFixed},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outA.DiscountTotal != 25.00 || outA.NetTotal != 25.00 {
		t.Errorf("Expected disc=25 net=25, got disc=%v net=%v", outA.DiscountTotal, outA.NetTotal)
	}

	// Cart: $10.00 subtotal -> Fixed discount $25.00 capped at subtotal ($10.00), Net = 0.00
	cartB := []promotion.CartItem{
		{
			ProductID: prodID,
			Name:      "Small Item",
			Quantity:  1,
			UnitPrice: 10.00,
		},
	}
	outB, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cartB,
		Promotions:     []promotion.Promotion{promoFixed},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outB.DiscountTotal != 10.00 {
		t.Errorf("Expected disc=10 (capped), got %v", outB.DiscountTotal)
	}
	if outB.NetTotal != 0.00 {
		t.Errorf("Expected net=0.00, got %v", outB.NetTotal)
	}
}

// 3. Buy X Get Y deterministic evaluation
func TestEvaluator_BuyXGetY(t *testing.T) {
	tenantID := uuid.New()
	prodCoffee := uuid.New()
	prodCookie := uuid.New()
	now := time.Now().UTC()

	// 3.1 Buy 2 Get 1 Free on same product (Coffee)
	promoBOGO := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-BUY2-GET1",
		Name:          "Buy 2 Coffee Get 1 Free Coffee",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{
				Type: promotion.RewardTypeBuyXGetY,
				Config: mustJSON(promotion.BuyXGetYConfig{
					BuyQty:             2.0,
					GetQty:             1.0,
					DiscountPercentage: 100.0,
				}),
			},
		},
	}

	// Customer buys 5 Coffees @ $10.00 = $50.00
	// floor(5/2) = 2 sets -> 2 Free Coffees -> $20.00 discount
	cart := []promotion.CartItem{
		{
			ProductID: prodCoffee,
			Name:      "Arabica Coffee",
			Quantity:  5,
			UnitPrice: 10.00,
		},
	}

	out, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoBOGO},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}

	if out.DiscountTotal != 20.00 {
		t.Errorf("Expected disc=20.00, got %v", out.DiscountTotal)
	}
	if out.NetTotal != 30.00 {
		t.Errorf("Expected net=30.00, got %v", out.NetTotal)
	}
	if len(out.BonusItems) != 1 {
		t.Fatalf("Expected 1 bonus item entry, got %d", len(out.BonusItems))
	}
	bonus := out.BonusItems[0]
	if bonus.ProductID != prodCoffee || bonus.FreeQuantity != 2 || bonus.DiscountAmount != 20.00 {
		t.Errorf("Unexpected bonus item details: %+v", bonus)
	}

	// 3.2 Buy X Get Y with MaxApplications = 1
	maxApp := 1
	promoCappedBOGO := promoBOGO
	promoCappedBOGO.Rewards = []promotion.Reward{
		{
			Type: promotion.RewardTypeBuyXGetY,
			Config: mustJSON(promotion.BuyXGetYConfig{
				BuyQty:             2.0,
				GetQty:             1.0,
				DiscountPercentage: 100.0,
				MaxApplications:    &maxApp, // Max 1 set allowed
			}),
		},
	}

	outCapped, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoCappedBOGO},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outCapped.DiscountTotal != 10.00 {
		t.Errorf("Expected capped BOGO disc=10.00, got %v", outCapped.DiscountTotal)
	}
	if outCapped.BonusItems[0].FreeQuantity != 1 {
		t.Errorf("Expected 1 free quantity with cap, got %v", outCapped.BonusItems[0].FreeQuantity)
	}

	// 3.3 Buy X Get Y for different reward item (Buy 2 Coffee Get 1 Free Cookie)
	promoCross := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-COFFEE-COOKIE",
		Name:          "Buy 2 Coffee Get 1 Free Cookie",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{
				Type: promotion.RewardTypeBuyXGetY,
				Config: mustJSON(promotion.BuyXGetYConfig{
					BuyQty:             2.0,
					GetQty:             1.0,
					RewardProductID:    &prodCookie,
					RewardItemName:     "Choco Cookie",
					DiscountPercentage: 100.0,
				}),
			},
		},
	}

	// Cart contains 2 Coffees (@ $10) and 1 Cookie (@ $5)
	cartCross := []promotion.CartItem{
		{ProductID: prodCoffee, Name: "Coffee", Quantity: 2, UnitPrice: 10.00},
		{ProductID: prodCookie, Name: "Cookie", Quantity: 1, UnitPrice: 5.00},
	}
	outCross, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cartCross,
		Promotions:     []promotion.Promotion{promoCross},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outCross.DiscountTotal != 5.00 {
		t.Errorf("Expected cookie discount=5.00, got %v", outCross.DiscountTotal)
	}
	if len(outCross.BonusItems) != 1 || outCross.BonusItems[0].ProductID != prodCookie {
		t.Errorf("Expected Cookie as bonus item, got %+v", outCross.BonusItems)
	}

	// 3.4 Ineligible cart (Bought only 1 Coffee when BuyQty = 2)
	cartIneligible := []promotion.CartItem{
		{ProductID: prodCoffee, Name: "Coffee", Quantity: 1, UnitPrice: 10.00},
	}
	outIneligible, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cartIneligible,
		Promotions:     []promotion.Promotion{promoBOGO},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outIneligible.DiscountTotal != 0.00 || len(outIneligible.BonusItems) != 0 {
		t.Errorf("Expected 0 discount and no bonus items, got disc=%v bonus=%v",
			outIneligible.DiscountTotal, len(outIneligible.BonusItems))
	}
}

// 4. Conditions: MIN_ORDER_AMOUNT
func TestEvaluator_Condition_MinOrderAmount(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()

	promo := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-MIN-SPEND",
		Name:          "$10 Off on Min Spend $100",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Conditions: []promotion.Condition{
			{
				Type: promotion.ConditionTypeMinOrderAmount,
				Config: mustJSON(promotion.MinOrderAmountConfig{
					MinAmount: 100.00,
				}),
			},
		},
		Rewards: []promotion.Reward{
			{
				Type:   promotion.RewardTypeFixedAmountDiscount,
				Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 10.00}),
			},
		},
	}

	// Subtotal $80 -> Ineligible
	outBelow, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          []promotion.CartItem{{ProductID: prodID, Quantity: 1, UnitPrice: 80.00}},
		Promotions:     []promotion.Promotion{promo},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outBelow.DiscountTotal != 0.00 || len(outBelow.AppliedPromotions) != 0 {
		t.Errorf("Expected not applied for subtotal 80, got %v", outBelow.DiscountTotal)
	}

	// Subtotal $120 -> Eligible
	outAbove, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          []promotion.CartItem{{ProductID: prodID, Quantity: 1, UnitPrice: 120.00}},
		Promotions:     []promotion.Promotion{promo},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if outAbove.DiscountTotal != 10.00 || len(outAbove.AppliedPromotions) != 1 {
		t.Errorf("Expected applied discount 10 for subtotal 120, got %v", outAbove.DiscountTotal)
	}
}

// 5. Conditions: MIN_ITEM_QTY and PRODUCT_IN_LIST
func TestEvaluator_Condition_MinItemQty_And_ProductInList(t *testing.T) {
	tenantID := uuid.New()
	targetProd := uuid.New()
	otherProd := uuid.New()
	now := time.Now().UTC()

	promo := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-TARGET-QTY",
		Name:          "Discount when buying at least 3 Target items",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Conditions: []promotion.Condition{
			{
				Type: promotion.ConditionTypeProductInList,
				Config: mustJSON(promotion.ProductInListConfig{
					ProductIDs: []uuid.UUID{targetProd},
				}),
			},
			{
				Type: promotion.ConditionTypeMinItemQty,
				Config: mustJSON(promotion.MinItemQtyConfig{
					MinQty:     3.0,
					ProductIDs: []uuid.UUID{targetProd},
				}),
			},
		},
		Rewards: []promotion.Reward{
			{
				Type:   promotion.RewardTypeFixedAmountDiscount,
				Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 15.00}),
			},
		},
	}

	// Cart with 1 TargetProd and 5 OtherProd -> total qty 6, but TargetProd qty only 1 -> Ineligible
	outFail, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items: []promotion.CartItem{
			{ProductID: targetProd, Quantity: 1, UnitPrice: 20.00},
			{ProductID: otherProd, Quantity: 5, UnitPrice: 10.00},
		},
		Promotions: []promotion.Promotion{promo},
	})
	if outFail.DiscountTotal != 0.00 {
		t.Errorf("Expected 0 discount when target item qty < 3, got %v", outFail.DiscountTotal)
	}

	// Cart with 3 TargetProd -> Eligible
	outSuccess, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items: []promotion.CartItem{
			{ProductID: targetProd, Quantity: 3, UnitPrice: 20.00},
		},
		Promotions: []promotion.Promotion{promo},
	})
	if outSuccess.DiscountTotal != 15.00 {
		t.Errorf("Expected 15 discount when target item qty >= 3, got %v", outSuccess.DiscountTotal)
	}
}

// 6. Conditions: CATEGORY_IN_LIST
func TestEvaluator_Condition_CategoryInList(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()

	promo := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-CAT-BEVERAGE",
		Name:          "10% off Beverages",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Conditions: []promotion.Condition{
			{
				Type: promotion.ConditionTypeCategoryInList,
				Config: mustJSON(promotion.CategoryInListConfig{
					CategoryIDs: []string{"BEVERAGE"},
				}),
			},
		},
		Rewards: []promotion.Reward{
			{
				Type:   promotion.RewardTypePercentageDiscount,
				Config: mustJSON(promotion.PercentageDiscountConfig{Percentage: 10.0}),
			},
		},
	}

	// Cart with category FOOD only -> Ineligible
	outFood, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items: []promotion.CartItem{
			{ProductID: uuid.New(), CategoryID: "FOOD", Quantity: 2, UnitPrice: 50.00},
		},
		Promotions: []promotion.Promotion{promo},
	})
	if outFood.DiscountTotal != 0.00 {
		t.Errorf("Expected 0 discount for FOOD, got %v", outFood.DiscountTotal)
	}

	// Cart with category BEVERAGE -> Eligible
	outBev, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items: []promotion.CartItem{
			{ProductID: uuid.New(), CategoryID: "BEVERAGE", Quantity: 2, UnitPrice: 50.00},
		},
		Promotions: []promotion.Promotion{promo},
	})
	if outBev.DiscountTotal != 10.00 {
		t.Errorf("Expected 10.00 discount for BEVERAGE, got %v", outBev.DiscountTotal)
	}
}

// 7. Effective Period Boundaries
func TestEvaluator_EffectivePeriod_Boundaries(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	refTime := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	makePromo := func(code string, from, to time.Time) promotion.Promotion {
		return promotion.Promotion{
			ID:            uuid.New(),
			TenantID:      tenantID,
			Code:          code,
			Name:          code,
			Channel:       "ALL",
			StackingMode:  promotion.StackingModeStackable,
			Priority:      100,
			EffectiveFrom: from,
			EffectiveTo:   &to,
			Status:        promotion.StatusActive,
			Rewards: []promotion.Reward{
				{
					Type:   promotion.RewardTypeFixedAmountDiscount,
					Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 5.00}),
				},
			},
		}
	}

	cart := []promotion.CartItem{{ProductID: prodID, Quantity: 1, UnitPrice: 50.00}}

	// Future promo: starts 1 hour after refTime -> Ineligible
	futurePromo := makePromo("FUTURE", refTime.Add(time.Hour), refTime.Add(2*time.Hour))
	outFuture, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: refTime,
		Items:          cart,
		Promotions:     []promotion.Promotion{futurePromo},
	})
	if outFuture.DiscountTotal != 0.00 {
		t.Errorf("Future promo should not apply, got %v", outFuture.DiscountTotal)
	}

	// Expired promo: ended 1 hour before refTime -> Ineligible
	expiredPromo := makePromo("EXPIRED", refTime.Add(-2*time.Hour), refTime.Add(-time.Hour))
	outExpired, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: refTime,
		Items:          cart,
		Promotions:     []promotion.Promotion{expiredPromo},
	})
	if outExpired.DiscountTotal != 0.00 {
		t.Errorf("Expired promo should not apply, got %v", outExpired.DiscountTotal)
	}

	// Active promo: valid during refTime -> Eligible
	activePromo := makePromo("ACTIVE", refTime.Add(-time.Hour), refTime.Add(time.Hour))
	outActive, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: refTime,
		Items:          cart,
		Promotions:     []promotion.Promotion{activePromo},
	})
	if outActive.DiscountTotal != 5.00 {
		t.Errorf("Active promo should apply, got %v", outActive.DiscountTotal)
	}
}

// 8. Usage Limit Check (Pure read-only, no side effects)
func TestEvaluator_UsageLimit_ReadOnly(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()

	limit := 10
	promo := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-LIMITED",
		Name:          "Limited to 10 uses",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		UsageLimit:    &limit,
		UsageCount:    5, // 5 out of 10 used -> still available
		Rewards: []promotion.Reward{
			{
				Type:   promotion.RewardTypeFixedAmountDiscount,
				Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 10.00}),
			},
		},
	}

	cart := []promotion.CartItem{{ProductID: prodID, Quantity: 1, UnitPrice: 50.00}}

	// Evaluate when usage is 5/10 -> Applied
	out, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promo},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if out.DiscountTotal != 10.00 {
		t.Errorf("Expected promo to apply with usage 5/10, got %v", out.DiscountTotal)
	}
	// Verify promo.UsageCount was NOT mutated
	if promo.UsageCount != 5 {
		t.Errorf("UsageCount must not be modified by evaluator! Got %d", promo.UsageCount)
	}

	// When usage limit is reached (10/10) -> Ineligible
	promoSoldOut := promo
	promoSoldOut.UsageCount = 10
	outSoldOut, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoSoldOut},
	})
	if outSoldOut.DiscountTotal != 0.00 {
		t.Errorf("Sold out promo should not apply, got %v", outSoldOut.DiscountTotal)
	}
}

// 9. Priority and Tie-Breakers
func TestEvaluator_Priority_And_TieBreakers(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()

	// Two exclusive promos with different priorities
	promoLow := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-PRIO-50",
		Name:          "Priority 50 ($10 Off)",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeExclusive,
		Priority:      50,
		EffectiveFrom: now.Add(-2 * time.Hour),
		CreatedAt:     now.Add(-2 * time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 10.00})},
		},
	}

	promoHigh := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-PRIO-100",
		Name:          "Priority 100 ($8 Off)",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeExclusive,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		CreatedAt:     now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 8.00})},
		},
	}

	cart := []promotion.CartItem{{ProductID: prodID, Quantity: 1, UnitPrice: 100.00}}

	// Priority 100 EXCLUSIVE promo takes precedence over Priority 50 EXCLUSIVE promo
	out, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoLow, promoHigh},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if len(out.AppliedPromotions) != 1 || out.AppliedPromotions[0].PromotionCode != "PROMO-PRIO-100" {
		t.Fatalf("Expected PROMO-PRIO-100 to win on higher priority, got %+v", out.AppliedPromotions)
	}

	// Tie-breaker: Same priority (100), but PromoEarly has earlier EffectiveFrom
	promoEarly := promoHigh
	promoEarly.Code = "PROMO-EARLY"
	promoEarly.EffectiveFrom = now.Add(-5 * time.Hour)

	promoLate := promoHigh
	promoLate.Code = "PROMO-LATE"
	promoLate.EffectiveFrom = now.Add(-1 * time.Hour)

	outTie, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoLate, promoEarly},
	})
	if outTie.AppliedPromotions[0].PromotionCode != "PROMO-EARLY" {
		t.Errorf("Expected earlier start date to win tie-breaker, got %s", outTie.AppliedPromotions[0].PromotionCode)
	}
}

// 10. Stacking Policies (STACKABLE, EXCLUSIVE, BEST_ONLY)
func TestEvaluator_StackingPolicies(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()
	cart := []promotion.CartItem{{ProductID: prodID, Quantity: 1, UnitPrice: 100.00}}

	// 10.1 Multiple STACKABLE promotions combine
	promoStack1 := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "STACK-10",
		Name:          "$10 Off",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 10.00})},
		},
	}
	promoStack2 := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "STACK-15",
		Name:          "$15 Off",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      90,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 15.00})},
		},
	}

	outStack, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoStack1, promoStack2},
	})
	if outStack.DiscountTotal != 25.00 || len(outStack.AppliedPromotions) != 2 {
		t.Errorf("Expected 2 stacked promos totaling 25.00, got disc=%v count=%d",
			outStack.DiscountTotal, len(outStack.AppliedPromotions))
	}

	// 10.2 Highest Priority EXCLUSIVE promo overrides everything
	promoExclusiveTop := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "EXCLUSIVE-TOP",
		Name:          "Exclusive Top Priority",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeExclusive,
		Priority:      200, // Higher than stackable promos
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 20.00})},
		},
	}

	outExc, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoStack1, promoStack2, promoExclusiveTop},
	})
	if len(outExc.AppliedPromotions) != 1 || outExc.AppliedPromotions[0].PromotionCode != "EXCLUSIVE-TOP" {
		t.Errorf("Top priority EXCLUSIVE promo must apply alone, got %+v", outExc.AppliedPromotions)
	}

	// 10.3 BEST_ONLY resolves to single promo with highest customer benefit
	promoBestA := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "BEST-10",
		Name:          "$10 Off (Best Only)",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeBestOnly,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 10.00})},
		},
	}
	promoBestB := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "BEST-30",
		Name:          "$30 Off (Best Only)",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeBestOnly,
		Priority:      90,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 30.00})},
		},
	}

	outBest, _ := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoBestA, promoBestB},
	})
	if len(outBest.AppliedPromotions) != 1 || outBest.AppliedPromotions[0].PromotionCode != "BEST-30" {
		t.Errorf("BEST_ONLY must select single best discount (BEST-30), got %+v", outBest.AppliedPromotions)
	}
	if outBest.DiscountTotal != 30.00 {
		t.Errorf("Expected best discount 30.00, got %v", outBest.DiscountTotal)
	}
}

// 11. Non-negative net price and 4-decimal exact arithmetic
func TestEvaluator_NonNegativeNetPrice_And_Precision(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	now := time.Now().UTC()

	// Promo that offers $200 discount
	promoHuge := promotion.Promotion{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Code:          "PROMO-HUGE",
		Name:          "$200 Off",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{Type: promotion.RewardTypeFixedAmountDiscount, Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 200.00})},
		},
	}

	// Cart with fractional quantity and price: 3.3333 items @ 10.0001
	cart := []promotion.CartItem{
		{
			ProductID: prodID,
			Quantity:  3.3333,
			UnitPrice: 10.0001,
		},
	}

	out, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       tenantID,
		EvaluationTime: now,
		Items:          cart,
		Promotions:     []promotion.Promotion{promoHuge},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}

	// Net total must NEVER be negative
	if out.NetTotal < 0 {
		t.Fatalf("NetTotal must be non-negative! Got %v", out.NetTotal)
	}
	if out.NetTotal != 0.0000 {
		t.Errorf("NetTotal should be exactly 0.0000 when discount > subtotal, got %v", out.NetTotal)
	}
	if out.DiscountTotal != out.Subtotal {
		t.Errorf("DiscountTotal (%v) must equal Subtotal (%v)", out.DiscountTotal, out.Subtotal)
	}

	// Verify exact rational getters
	if out.GetNetTotalRat().Sign() < 0 {
		t.Errorf("GetNetTotalRat is negative: %v", out.GetNetTotalRat())
	}
	if out.GetDiscountTotalRat().Cmp(out.GetSubtotalRat()) != 0 {
		t.Errorf("Rational discount (%v) != subtotal (%v)", out.GetDiscountTotalRat(), out.GetSubtotalRat())
	}
}

// 12. Input Validation
func TestEvaluator_InputValidation(t *testing.T) {
	now := time.Now().UTC()

	// 12.1 Nil TenantID
	_, err := promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       uuid.Nil,
		EvaluationTime: now,
		Items:          []promotion.CartItem{{ProductID: uuid.New(), Quantity: 1, UnitPrice: 10.0}},
	})
	if err == nil {
		t.Error("Expected error on nil TenantID, got nil")
	}

	// 12.2 Empty Items
	_, err = promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       uuid.New(),
		EvaluationTime: now,
		Items:          []promotion.CartItem{},
	})
	if err == nil {
		t.Error("Expected error on empty cart items, got nil")
	}

	// 12.3 Non-positive Quantity
	_, err = promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       uuid.New(),
		EvaluationTime: now,
		Items:          []promotion.CartItem{{ProductID: uuid.New(), Quantity: 0, UnitPrice: 10.0}},
	})
	if err == nil {
		t.Error("Expected error on zero quantity, got nil")
	}

	// 12.4 Negative UnitPrice
	_, err = promotion.Evaluate(promotion.EvaluationInput{
		TenantID:       uuid.New(),
		EvaluationTime: now,
		Items:          []promotion.CartItem{{ProductID: uuid.New(), Quantity: 1, UnitPrice: -5.0}},
	})
	if err == nil {
		t.Error("Expected error on negative unit price, got nil")
	}
}
