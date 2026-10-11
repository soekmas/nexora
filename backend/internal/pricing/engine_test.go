package pricing_test

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/pricing"
)

func TestPricingEngine_SpecificityTiers(t *testing.T) {
	tenantID := uuid.New()
	storeID := uuid.New()
	customerGroupID := uuid.New()
	productID := uuid.New()
	variantID := uuid.New()
	priceListID := uuid.New()

	refTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	ctx := pricing.PricingContext{
		TenantID:        tenantID,
		StoreID:         storeID,
		CustomerGroupID: &customerGroupID,
		Currency:        "IDR",
		Channel:         "POS",
		ReferenceTime:   refTime,
	}

	item := pricing.ResolveItemInput{
		ProductID: productID,
		VariantID: variantID,
		Quantity:  2.0,
	}

	engine := pricing.NewEngine()

	// Helper to make rules
	makeRule := func(tier int, store *uuid.UUID, cg *uuid.UUID, variant *uuid.UUID, price float64) *pricing.PriceRule {
		return &pricing.PriceRule{
			ID:              uuid.New(),
			TenantID:        tenantID,
			PriceListID:     priceListID,
			ProductID:       productID,
			VariantID:       variant,
			CustomerGroupID: cg,
			StoreID:         store,
			MinQty:          1,
			UnitPrice:       price,
			Priority:        100,
			EffectiveFrom:   refTime.Add(-24 * time.Hour),
			EffectiveTo:     nil,
			Status:          pricing.PriceRuleStatusActive,
		}
	}

	t.Run("All 8 individual tiers match correctly", func(t *testing.T) {
		rulesByTier := map[int]*pricing.PriceRule{
			pricing.TierStoreCustomerGroupVariant: makeRule(1, &storeID, &customerGroupID, &variantID, 10000),
			pricing.TierStoreVariant:              makeRule(2, &storeID, nil, &variantID, 20000),
			pricing.TierCustomerGroupVariant:      makeRule(3, nil, &customerGroupID, &variantID, 30000),
			pricing.TierBaseListVariant:           makeRule(4, nil, nil, &variantID, 40000),
			pricing.TierStoreCustomerGroupProduct: makeRule(5, &storeID, &customerGroupID, nil, 50000),
			pricing.TierStoreProduct:              makeRule(6, &storeID, nil, nil, 60000),
			pricing.TierCustomerGroupProduct:      makeRule(7, nil, &customerGroupID, nil, 70000),
			pricing.TierBaseListProduct:           makeRule(8, nil, nil, nil, 80000),
		}

		for tier, rule := range rulesByTier {
			calculatedTier := pricing.DetermineTier(rule)
			if calculatedTier != tier {
				t.Fatalf("expected tier %d, got %d", tier, calculatedTier)
			}

			res := engine.ResolveLine(ctx, item, []*pricing.PriceRule{rule}, refTime)
			if res == nil {
				t.Fatalf("expected rule to resolve for tier %d", tier)
			}
			if res.Tier != tier {
				t.Fatalf("expected resolved tier %d, got %d", tier, res.Tier)
			}
			if res.WinningRule.ID != rule.ID {
				t.Fatalf("expected winning rule %s, got %s", rule.ID, res.WinningRule.ID)
			}
		}
	})

	t.Run("Adjacent tier precedence: Tier N beats Tier N+1 even with lower price", func(t *testing.T) {
		tierDefs := []struct {
			tier    int
			store   *uuid.UUID
			cg      *uuid.UUID
			variant *uuid.UUID
		}{
			{1, &storeID, &customerGroupID, &variantID},
			{2, &storeID, nil, &variantID},
			{3, nil, &customerGroupID, &variantID},
			{4, nil, nil, &variantID},
			{5, &storeID, &customerGroupID, nil},
			{6, &storeID, nil, nil},
			{7, nil, &customerGroupID, nil},
			{8, nil, nil, nil},
		}

		for i := 0; i < len(tierDefs)-1; i++ {
			highTierDef := tierDefs[i]
			lowTierDef := tierDefs[i+1]

			// More specific tier has LOWER price, less specific tier has HIGHER price
			ruleHigherTier := makeRule(highTierDef.tier, highTierDef.store, highTierDef.cg, highTierDef.variant, 100.0)
			ruleLowerTier := makeRule(lowTierDef.tier, lowTierDef.store, lowTierDef.cg, lowTierDef.variant, 500.0)

			// Present in both orders to test candidate-order independence
			candidates := []*pricing.PriceRule{ruleLowerTier, ruleHigherTier}
			res := engine.ResolveLine(ctx, item, candidates, refTime)
			if res == nil {
				t.Fatalf("expected winning rule between tier %d and %d", highTierDef.tier, lowTierDef.tier)
			}
			if res.Tier != highTierDef.tier {
				t.Fatalf("tier %d did not beat tier %d (got tier %d)", highTierDef.tier, lowTierDef.tier, res.Tier)
			}
			if res.WinningRule.ID != ruleHigherTier.ID {
				t.Fatalf("tier %d rule did not win against tier %d", highTierDef.tier, lowTierDef.tier)
			}

			// Reverse order
			candidatesRev := []*pricing.PriceRule{ruleHigherTier, ruleLowerTier}
			resRev := engine.ResolveLine(ctx, item, candidatesRev, refTime)
			if resRev == nil || resRev.WinningRule.ID != ruleHigherTier.ID {
				t.Fatalf("order independence failed for tier %d vs %d", highTierDef.tier, lowTierDef.tier)
			}
		}
	})

	t.Run("Specific tier wins over lower tier with higher priority or newer effective date", func(t *testing.T) {
		// Tier 2 (Store + Variant) with low priority & older date
		ruleTier2 := makeRule(2, &storeID, nil, &variantID, 100.0)
		ruleTier2.Priority = 10
		ruleTier2.EffectiveFrom = refTime.Add(-10 * 24 * time.Hour)

		// Tier 8 (Base List + Product) with high priority & newer date
		ruleTier8 := makeRule(8, nil, nil, nil, 50.0)
		ruleTier8.Priority = 999
		ruleTier8.EffectiveFrom = refTime.Add(-1 * time.Hour)

		res := engine.ResolveLine(ctx, item, []*pricing.PriceRule{ruleTier8, ruleTier2}, refTime)
		if res == nil || res.WinningRule.ID != ruleTier2.ID {
			t.Fatalf("tier 2 should win over tier 8 regardless of priority or effective date")
		}
	})
}

func TestPricingEngine_TieBreakers(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	variantID := uuid.New()
	priceListID := uuid.New()
	refTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	ctx := pricing.PricingContext{
		TenantID:      tenantID,
		StoreID:       uuid.New(),
		Currency:      "IDR",
		Channel:       "POS",
		ReferenceTime: refTime,
	}
	engine := pricing.NewEngine()

	t.Run("Quantity break selection (min_qty DESC) on whole line", func(t *testing.T) {
		// Tiers: min_qty = 1 (100.0), min_qty = 5 (90.0), min_qty = 10 (80.0)
		r1 := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}
		r5 := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        5,
			UnitPrice:     90.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}
		r10 := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        10,
			UnitPrice:     80.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}

		rules := []*pricing.PriceRule{r1, r10, r5}

		// Qty = 4 -> matches r1
		res4 := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 4}, rules, refTime)
		if res4 == nil || res4.WinningRule.ID != r1.ID || res4.UnitPrice != 100.0 || res4.LineTotal != 400.0 {
			t.Fatalf("expected r1 for qty 4, got %+v", res4)
		}

		// Qty = 5 -> matches r5
		res5 := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 5}, rules, refTime)
		if res5 == nil || res5.WinningRule.ID != r5.ID || res5.UnitPrice != 90.0 || res5.LineTotal != 450.0 {
			t.Fatalf("expected r5 for qty 5, got %+v", res5)
		}

		// Qty = 9 -> matches r5
		res9 := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 9}, rules, refTime)
		if res9 == nil || res9.WinningRule.ID != r5.ID || res9.UnitPrice != 90.0 || res9.LineTotal != 810.0 {
			t.Fatalf("expected r5 for qty 9, got %+v", res9)
		}

		// Qty = 10 -> matches r10
		res10 := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 10}, rules, refTime)
		if res10 == nil || res10.WinningRule.ID != r10.ID || res10.UnitPrice != 80.0 || res10.LineTotal != 800.0 {
			t.Fatalf("expected r10 for qty 10, got %+v", res10)
		}

		// Qty = 25 -> matches r10
		res25 := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 25}, rules, refTime)
		if res25 == nil || res25.WinningRule.ID != r10.ID || res25.UnitPrice != 80.0 || res25.LineTotal != 2000.0 {
			t.Fatalf("expected r10 for qty 25, got %+v", res25)
		}
	})

	t.Run("Priority tie-breaker", func(t *testing.T) {
		rLow := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      50,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}
		rHigh := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     95.0,
			Priority:      200,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}

		res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{rLow, rHigh}, refTime)
		if res == nil || res.WinningRule.ID != rHigh.ID {
			t.Fatalf("expected rHigh to win via priority tie-breaker")
		}
	})

	t.Run("Effective date tie-breaker", func(t *testing.T) {
		rOlder := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-48 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}
		rNewer := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     95.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-2 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}

		res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{rOlder, rNewer}, refTime)
		if res == nil || res.WinningRule.ID != rNewer.ID {
			t.Fatalf("expected rNewer to win via effective date tie-breaker")
		}
	})

	t.Run("ID ASC tie-breaker", func(t *testing.T) {
		idA := uuid.MustParse("00000000-0000-0000-0000-000000000001")
		idB := uuid.MustParse("00000000-0000-0000-0000-000000000002")

		rA := &pricing.PriceRule{
			ID:            idA,
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}
		rB := &pricing.PriceRule{
			ID:            idB,
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     95.0,
			Priority:      100,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
		}

		res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{rB, rA}, refTime)
		if res == nil || res.WinningRule.ID != idA {
			t.Fatalf("expected idA to win via ID ASC tie-breaker")
		}
	})
}

func TestPricingEngine_IneligibilityAndBoundaries(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	variantID := uuid.New()
	priceListID := uuid.New()
	refTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	ctx := pricing.PricingContext{
		TenantID:      tenantID,
		StoreID:       uuid.New(),
		Currency:      "IDR",
		Channel:       "POS",
		ReferenceTime: refTime,
	}
	engine := pricing.NewEngine()

	t.Run("Status ineligibility", func(t *testing.T) {
		statuses := []string{
			pricing.PriceRuleStatusDraft,
			pricing.PriceRuleStatusApproved,
			pricing.PriceRuleStatusInactive,
			pricing.PriceRuleStatusExpired,
		}

		for _, st := range statuses {
			rule := &pricing.PriceRule{
				ID:            uuid.New(),
				TenantID:      tenantID,
				PriceListID:   priceListID,
				ProductID:     productID,
				VariantID:     &variantID,
				MinQty:        1,
				UnitPrice:     100.0,
				Status:        st,
				EffectiveFrom: refTime.Add(-1 * time.Hour),
			}
			res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{rule}, refTime)
			if res != nil {
				t.Fatalf("rule with status %s should not be eligible", st)
			}
		}
	})

	t.Run("Effective time boundaries", func(t *testing.T) {
		// Future rule (not yet active)
		futureRule := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Status:        pricing.PriceRuleStatusActive,
			EffectiveFrom: refTime.Add(1 * time.Second),
		}
		if res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{futureRule}, refTime); res != nil {
			t.Fatalf("future rule should not be eligible")
		}

		// Expired rule (effective_to <= refTime)
		expiredTo := refTime.Add(-1 * time.Second)
		expiredRule := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Status:        pricing.PriceRuleStatusActive,
			EffectiveFrom: refTime.Add(-10 * time.Hour),
			EffectiveTo:   &expiredTo,
		}
		if res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{expiredRule}, refTime); res != nil {
			t.Fatalf("expired rule should not be eligible")
		}

		// Exact boundary: effective_to == refTime should NOT be eligible (effective_to is exclusive)
		boundaryTo := refTime
		boundaryRule := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Status:        pricing.PriceRuleStatusActive,
			EffectiveFrom: refTime.Add(-10 * time.Hour),
			EffectiveTo:   &boundaryTo,
		}
		if res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, []*pricing.PriceRule{boundaryRule}, refTime); res != nil {
			t.Fatalf("rule expiring exactly at refTime should not be eligible")
		}
	})

	t.Run("Candidate order independence under random shuffling", func(t *testing.T) {
		candidates := []*pricing.PriceRule{
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				PriceListID:   priceListID,
				ProductID:     productID,
				VariantID:     &variantID,
				MinQty:        1,
				UnitPrice:     150.0,
				Priority:      50,
				Status:        pricing.PriceRuleStatusActive,
				EffectiveFrom: refTime.Add(-10 * time.Hour),
			},
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				PriceListID:   priceListID,
				ProductID:     productID,
				VariantID:     &variantID,
				MinQty:        1,
				UnitPrice:     100.0,
				Priority:      200, // Winner
				Status:        pricing.PriceRuleStatusActive,
				EffectiveFrom: refTime.Add(-5 * time.Hour),
			},
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				PriceListID:   priceListID,
				ProductID:     productID,
				VariantID:     nil, // Product tier
				MinQty:        1,
				UnitPrice:     80.0,
				Priority:      999,
				Status:        pricing.PriceRuleStatusActive,
				EffectiveFrom: refTime.Add(-1 * time.Hour),
			},
		}

		expectedWinnerID := candidates[1].ID

		r := rand.New(rand.NewSource(42))
		for iter := 0; iter < 20; iter++ {
			shuffled := make([]*pricing.PriceRule, len(candidates))
			copy(shuffled, candidates)
			r.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, shuffled, refTime)
			if res == nil || res.WinningRule.ID != expectedWinnerID {
				t.Fatalf("order independence failed at iteration %d", iter)
			}
		}
	})

	t.Run("Zero and negative quantities", func(t *testing.T) {
		rule := &pricing.PriceRule{
			ID:            uuid.New(),
			TenantID:      tenantID,
			PriceListID:   priceListID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     100.0,
			Status:        pricing.PriceRuleStatusActive,
			EffectiveFrom: refTime.Add(-1 * time.Hour),
		}
		if res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 0}, []*pricing.PriceRule{rule}, refTime); res != nil {
			t.Fatalf("expected nil for zero quantity")
		}
		if res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: -5}, []*pricing.PriceRule{rule}, refTime); res != nil {
			t.Fatalf("expected nil for negative quantity")
		}
	})

	t.Run("Empty candidates returns nil", func(t *testing.T) {
		res := engine.ResolveLine(ctx, pricing.ResolveItemInput{ProductID: productID, VariantID: variantID, Quantity: 1}, nil, refTime)
		if res != nil {
			t.Fatalf("expected nil for empty candidate list")
		}
	})

	t.Run("Decimal precision rounding", func(t *testing.T) {
		val := pricing.RoundTo4(12.34567)
		if val != 12.3457 {
			t.Fatalf("expected 12.3457, got %f", val)
		}
		val2 := pricing.RoundTo4(12.34562)
		if val2 != 12.3456 {
			t.Fatalf("expected 12.3456, got %f", val2)
		}
	})
}

func TestPricing_DecimalArithmeticVerification(t *testing.T) {
	t.Run("Test A - Exact decimal multiplication without binary float intermediate", func(t *testing.T) {
		tests := []struct {
			name      string
			unitPrice string
			quantity  string
			expected  string // Exact four-decimal half-up result
		}{
			{
				name:      "0.1000 x 3",
				unitPrice: "0.1000",
				quantity:  "3.000000",
				expected:  "0.3000",
			},
			{
				name:      "19.9900 x 1.250000",
				unitPrice: "19.9900",
				quantity:  "1.250000",
				expected:  "24.9875",
			},
			{
				name:      "12.3456 x 0.123456 (5th fractional digit 3 < 5 rounds down)",
				unitPrice: "12.3456",
				quantity:  "0.123456",
				expected:  "1.5241", // 12.3456 * 0.123456 = 1.5241373936 -> 1.5241
			},
			{
				name:      "Product requiring half-up rounding (5th digit = 5)",
				unitPrice: "2.5000",
				quantity:  "0.500100",
				expected:  "1.2503", // 2.5000 * 0.500100 = 1.250250 -> 1.2503
			},
			{
				name:      "Product remaining below half-up threshold (5th digit = 4)",
				unitPrice: "2.5000",
				quantity:  "0.500099",
				expected:  "1.2502", // 2.5000 * 0.500099 = 1.2502475 -> 1.2502
			},
			{
				name:      "Large valid value within supported database precision",
				unitPrice: "9999999999.9999",
				quantity:  "1000.000000",
				expected:  "9999999999999.9000", // 9999999999.9999 * 1000 = 9999999999999.9000
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rPrice, ok1 := new(big.Rat).SetString(tc.unitPrice)
				if !ok1 {
					t.Fatalf("invalid test price rational: %s", tc.unitPrice)
				}
				rQty, ok2 := new(big.Rat).SetString(tc.quantity)
				if !ok2 {
					t.Fatalf("invalid test quantity rational: %s", tc.quantity)
				}

				ratResult, floatResult := pricing.MultiplyExactRat(rPrice, rQty)

				// Verify rational string matches expected exact string
				if ratResult.FloatString(4) != tc.expected {
					t.Errorf("rational result mismatch: expected %s, got %s", tc.expected, ratResult.FloatString(4))
				}

				// Verify floatResult matches when formatted to 4 decimals
				expectedRat, _ := new(big.Rat).SetString(tc.expected)
				expectedFloat, _ := expectedRat.Float64()
				if floatResult != expectedFloat {
					t.Errorf("float result mismatch: expected %f, got %f", expectedFloat, floatResult)
				}
			})
		}
	})

	t.Run("Test B - Subtotal precision without floating-point accumulation drift", func(t *testing.T) {
		// 10 lines of 0.1000 * 1 = 0.1000
		// In binary float: 0.1 + 0.1 + ... != 1.0 (drift occurs)
		// In rational arithmetic: exact 1.0000
		subtotalRat := new(big.Rat)
		lineRat, _ := new(big.Rat).SetString("0.1000")
		for i := 0; i < 10; i++ {
			subtotalRat.Add(subtotalRat, lineRat)
		}

		if subtotalRat.FloatString(4) != "1.0000" {
			t.Fatalf("expected exact subtotal 1.0000, got %s", subtotalRat.FloatString(4))
		}

		// Another case: 3 lines of 0.0700 * 1 = 0.0700 plus 0.1400 * 1 = 0.1400 -> total 0.3500
		subtotalRat2 := new(big.Rat)
		l1, _ := new(big.Rat).SetString("0.0700")
		l2, _ := new(big.Rat).SetString("0.1400")
		subtotalRat2.Add(subtotalRat2, l1)
		subtotalRat2.Add(subtotalRat2, l1)
		subtotalRat2.Add(subtotalRat2, l1)
		subtotalRat2.Add(subtotalRat2, l2)

		if subtotalRat2.FloatString(4) != "0.3500" {
			t.Fatalf("expected exact subtotal 0.3500, got %s", subtotalRat2.FloatString(4))
		}
	})

	t.Run("Test D - Rounding boundaries immediately below, at, and above 0.00005 half-up threshold", func(t *testing.T) {
		boundaryTests := []struct {
			name      string
			unitPrice string
			quantity  string
			expected  string
		}{
			{
				name:      "Immediately below threshold: 1.00000000 * 0.00004999",
				unitPrice: "1.0000",
				quantity:  "0.00004999",
				expected:  "0.0000", // product = 0.00004999 -> rounds down to 0.0000
			},
			{
				name:      "Exactly at threshold: 1.00000000 * 0.00005000",
				unitPrice: "1.0000",
				quantity:  "0.00005000",
				expected:  "0.0001", // product = 0.00005000 -> rounds up to 0.0001
			},
			{
				name:      "Immediately above threshold: 1.00000000 * 0.00005001",
				unitPrice: "1.0000",
				quantity:  "0.00005001",
				expected:  "0.0001", // product = 0.00005001 -> rounds up to 0.0001
			},
			{
				name:      "Base 10.0000 with offset 0.00004999",
				unitPrice: "10.00004999",
				quantity:  "1.000000",
				expected:  "10.0000",
			},
			{
				name:      "Base 10.0000 with offset 0.00005000",
				unitPrice: "10.00005000",
				quantity:  "1.000000",
				expected:  "10.0001",
			},
			{
				name:      "Base 10.0000 with offset 0.00005001",
				unitPrice: "10.00005001",
				quantity:  "1.000000",
				expected:  "10.0001",
			},
		}

		for _, tc := range boundaryTests {
			t.Run(tc.name, func(t *testing.T) {
				rPrice, _ := new(big.Rat).SetString(tc.unitPrice)
				rQty, _ := new(big.Rat).SetString(tc.quantity)
				ratResult, _ := pricing.MultiplyExactRat(rPrice, rQty)
				if ratResult.FloatString(4) != tc.expected {
					t.Errorf("boundary mismatch: expected %s, got %s", tc.expected, ratResult.FloatString(4))
				}
			})
		}
	})
}
