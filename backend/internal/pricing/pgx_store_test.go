package pricing_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/pricing"
)

func TestPricingStore_Integration(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t, 11)
	if appPool == nil {
		return
	}

	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, nil)
	store := pricing.NewPgxStore(appPool)

	// Tenants
	tenantA := uuid.New()
	tenantB := uuid.New()

	ctxA := postgres.WithPrincipalContext(ctx, postgres.PrincipalContext{
		TenantID: tenantA,
		UserID:   uuid.New(),
	})
	ctxB := postgres.WithPrincipalContext(ctx, postgres.PrincipalContext{
		TenantID: tenantB,
		UserID:   uuid.New(),
	})

	t.Run("PriceList CRUD and Tenant Isolation", func(t *testing.T) {
		plA := &pricing.PriceList{
			ID:        uuid.New(),
			Code:      "RETAIL-IDR",
			Name:      "Retail IDR List",
			Currency:  "IDR",
			Channel:   "POS",
			Status:    pricing.PriceListStatusActive,
			IsDefault: false,
		}

		err := tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceList(txCtx, plA)
		})
		if err != nil {
			t.Fatalf("create price list tenant A: %v", err)
		}

		// Read back under Tenant A
		var retrieved *pricing.PriceList
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			var getErr error
			retrieved, getErr = store.GetPriceListByID(txCtx, plA.ID)
			return getErr
		})
		if err != nil {
			t.Fatalf("get price list tenant A: %v", err)
		}
		if retrieved.Code != "RETAIL-IDR" || retrieved.Version != 1 {
			t.Errorf("mismatch on retrieved price list: %+v", retrieved)
		}

		// Read under Tenant B must fail with 404 (isolated)
		err = tm.WithinTx(ctxB, func(txCtx context.Context) error {
			_, getErr := store.GetPriceListByID(txCtx, plA.ID)
			return getErr
		})
		if !errors.Is(err, pricing.ErrPriceListNotFound404) {
			t.Fatalf("expected 404 for cross-tenant access, got %v", err)
		}

		// Optimistic concurrency update
		retrieved.Name = "Updated Retail List"
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.UpdatePriceList(txCtx, retrieved, 1)
		})
		if err != nil {
			t.Fatalf("update price list: %v", err)
		}
		if retrieved.Version != 2 {
			t.Fatalf("expected version 2 after update, got %d", retrieved.Version)
		}

		// Concurrent update with stale version must fail with ErrVersionConflict
		staleUpdate := *retrieved
		staleUpdate.Name = "Stale Name"
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.UpdatePriceList(txCtx, &staleUpdate, 1)
		})
		if !errors.Is(err, pricing.ErrVersionConflict) {
			t.Fatalf("expected ErrVersionConflict, got %v", err)
		}
	})

	t.Run("Default Price List Resolution and Uniqueness", func(t *testing.T) {
		// 1. Create an exact channel default list (IDR / POS)
		defPOS := &pricing.PriceList{
			ID:        uuid.New(),
			Code:      "DEFAULT-IDR-POS",
			Name:      "Default POS List",
			Currency:  "IDR",
			Channel:   "POS",
			Status:    pricing.PriceListStatusActive,
			IsDefault: true,
		}

		err := tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceList(txCtx, defPOS)
		})
		if err != nil {
			t.Fatalf("create default POS price list: %v", err)
		}

		// Exact channel default exists: resolve default list for IDR / POS should find defPOS
		var found *pricing.PriceList
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			var getErr error
			found, getErr = store.GetDefaultPriceList(txCtx, "IDR", "POS")
			return getErr
		})
		if err != nil {
			t.Fatalf("resolve default list for IDR/POS: %v", err)
		}
		if found.ID != defPOS.ID {
			t.Errorf("expected default list ID %s, got %s", defPOS.ID, found.ID)
		}

		// 2. Create an ALL channel default list for USD
		defUSDAll := &pricing.PriceList{
			ID:        uuid.New(),
			Code:      "DEFAULT-USD-ALL",
			Name:      "Default USD All Channels",
			Currency:  "USD",
			Channel:   "ALL",
			Status:    pricing.PriceListStatusActive,
			IsDefault: true,
		}
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceList(txCtx, defUSDAll)
		})
		if err != nil {
			t.Fatalf("create USD ALL default: %v", err)
		}

		// Exact channel default is absent (POS), but an ALL default exists:
		// Must NOT fall back to ALL! Must return ErrPriceListNotFound.
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			_, getErr := store.GetDefaultPriceList(txCtx, "USD", "POS")
			return getErr
		})
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Errorf("expected ErrPriceListNotFound when POS default absent despite ALL existing, got %v", err)
		}

		// 3. Currency mismatch: no default for EUR / POS
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			_, getErr := store.GetDefaultPriceList(txCtx, "EUR", "POS")
			return getErr
		})
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Errorf("expected ErrPriceListNotFound for EUR/POS, got %v", err)
		}

		// 4. Company-specific list: never select it automatically
		companyID := uuid.New()
		_, err = migrationPool.Exec(ctx, `INSERT INTO organization.company (id, tenant_id, code, name) VALUES ($1, $2, 'COMP-1', 'Company 1')`, companyID, tenantA)
		if err != nil {
			t.Fatalf("seed company: %v", err)
		}
		compDef := &pricing.PriceList{
			ID:        uuid.New(),
			CompanyID: &companyID,
			Code:      "COMP-SGD-POS",
			Name:      "Company Specific SGD List",
			Currency:  "SGD",
			Channel:   "POS",
			Status:    pricing.PriceListStatusActive,
			IsDefault: false,
		}
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceList(txCtx, compDef)
		})
		if err != nil {
			t.Fatalf("create company-specific list: %v", err)
		}

		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			_, getErr := store.GetDefaultPriceList(txCtx, "SGD", "POS")
			return getErr
		})
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Errorf("expected ErrPriceListNotFound for company-specific list, got %v", err)
		}

		// 5. Inactive default: never select it
		inactDef := &pricing.PriceList{
			ID:        uuid.New(),
			Code:      "INACT-JPY-POS",
			Name:      "Inactive JPY List",
			Currency:  "JPY",
			Channel:   "POS",
			Status:    pricing.PriceListStatusInactive,
			IsDefault: true,
		}
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceList(txCtx, inactDef)
		})
		if err != nil {
			t.Fatalf("create inactive list: %v", err)
		}

		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			_, getErr := store.GetDefaultPriceList(txCtx, "JPY", "POS")
			return getErr
		})
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Errorf("expected ErrPriceListNotFound for inactive default, got %v", err)
		}

		// 6. Cross-tenant list: tenant B cannot resolve tenant A's default
		err = tm.WithinTx(ctxB, func(txCtx context.Context) error {
			_, getErr := store.GetDefaultPriceList(txCtx, "IDR", "POS")
			return getErr
		})
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Errorf("expected ErrPriceListNotFound under Tenant B, got %v", err)
		}

		// 7. Creating a second active default for same currency and channel must fail with ErrDuplicateDefaultList
		secondDef := &pricing.PriceList{
			ID:        uuid.New(),
			Code:      "DEFAULT-IDR-POS-2",
			Name:      "Second POS Default",
			Currency:  "IDR",
			Channel:   "POS",
			Status:    pricing.PriceListStatusActive,
			IsDefault: true,
		}
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceList(txCtx, secondDef)
		})
		if !errors.Is(err, pricing.ErrDuplicateDefaultList) {
			t.Fatalf("expected ErrDuplicateDefaultList on duplicate active default, got %v", err)
		}
	})

	t.Run("PriceRule CRUD, Variant Consistency, and Candidate Lookup", func(t *testing.T) {
		productID := uuid.New()
		variantID := uuid.New()
		otherProductID := uuid.New()
		otherVariantID := uuid.New()

		// Seed master data using migrationPool (superuser connection)
		statements := []string{
			`INSERT INTO master.category (id, tenant_id, code, name) VALUES (gen_random_uuid(), '` + tenantA.String() + `', 'CAT1', 'Category 1');`,
			`INSERT INTO master.uom (id, tenant_id, code, name) VALUES (gen_random_uuid(), '` + tenantA.String() + `', 'PCS', 'Pieces');`,
			`INSERT INTO master.product (id, tenant_id, sku, name, category_id, base_uom_id, status)
			 SELECT '` + productID.String() + `', '` + tenantA.String() + `', 'PROD-1', 'Product 1', c.id, u.id, 'ACTIVE'
			 FROM master.category c, master.uom u WHERE c.tenant_id = '` + tenantA.String() + `' LIMIT 1;`,
			`INSERT INTO master.product (id, tenant_id, sku, name, category_id, base_uom_id, status)
			 SELECT '` + otherProductID.String() + `', '` + tenantA.String() + `', 'PROD-2', 'Product 2', c.id, u.id, 'ACTIVE'
			 FROM master.category c, master.uom u WHERE c.tenant_id = '` + tenantA.String() + `' LIMIT 1;`,
			`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name, status)
			 VALUES ('` + variantID.String() + `', '` + tenantA.String() + `', '` + productID.String() + `', 'SKU-1', 'Variant 1', 'ACTIVE');`,
			`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name, status)
			 VALUES ('` + otherVariantID.String() + `', '` + tenantA.String() + `', '` + otherProductID.String() + `', 'SKU-2', 'Variant 2', 'ACTIVE');`,
		}
		for _, stmt := range statements {
			if _, err := migrationPool.Exec(ctx, stmt); err != nil {
				t.Fatalf("seed master data stmt: %v", err)
			}
		}

		// Retrieve default list under Tenant A
		var defList *pricing.PriceList
		err := tm.WithinTx(ctxA, func(txCtx context.Context) error {
			var getErr error
			defList, getErr = store.GetDefaultPriceList(txCtx, "IDR", "POS")
			return getErr
		})
		if err != nil {
			t.Fatalf("get default list: %v", err)
		}

		// Trigger check: Attempting to create rule with variant belonging to another product fails
		mismatchRule := &pricing.PriceRule{
			ID:            uuid.New(),
			PriceListID:   defList.ID,
			ProductID:     productID,      // PROD-1
			VariantID:     &otherVariantID, // Belongs to PROD-2!
			MinQty:        1,
			UnitPrice:     1000.0,
			Priority:      100,
			EffectiveFrom: time.Now().Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusDraft,
		}
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceRule(txCtx, mismatchRule)
		})
		if !errors.Is(err, pricing.ErrVariantProductMismatch) {
			t.Fatalf("expected ErrVariantProductMismatch from trigger, got %v", err)
		}

		// Valid rule creation
		validRule := &pricing.PriceRule{
			ID:            uuid.New(),
			PriceListID:   defList.ID,
			ProductID:     productID,
			VariantID:     &variantID,
			MinQty:        1,
			UnitPrice:     50000.0,
			Priority:      100,
			EffectiveFrom: time.Now().Add(-10 * time.Minute),
			Status:        pricing.PriceRuleStatusActive,
		}
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			return store.CreatePriceRule(txCtx, validRule)
		})
		if err != nil {
			t.Fatalf("create valid rule: %v", err)
		}

		// Candidate rule retrieval
		refTime := time.Now()
		var candidates []*pricing.PriceRule
		err = tm.WithinTx(ctxA, func(txCtx context.Context) error {
			var getErr error
			candidates, getErr = store.GetCandidateRules(txCtx, defList.ID, []uuid.UUID{productID}, []uuid.UUID{variantID}, refTime)
			return getErr
		})
		if err != nil {
			t.Fatalf("get candidate rules: %v", err)
		}
		if len(candidates) != 1 || candidates[0].ID != validRule.ID {
			t.Fatalf("expected 1 candidate rule matching ID %s, got %d", validRule.ID, len(candidates))
		}

		// Tenant B must not see Tenant A candidate rules
		var candidatesB []*pricing.PriceRule
		err = tm.WithinTx(ctxB, func(txCtx context.Context) error {
			var getErr error
			candidatesB, getErr = store.GetCandidateRules(txCtx, defList.ID, []uuid.UUID{productID}, []uuid.UUID{variantID}, refTime)
			return getErr
		})
		if err != nil {
			t.Fatalf("get candidate rules tenant B: %v", err)
		}
		if len(candidatesB) != 0 {
			t.Fatalf("tenant B must see 0 candidate rules, got %d", len(candidatesB))
		}
	})
}

func TestPricing_DatabaseDecimalRoundTrip(t *testing.T) {
	migrationPool, appPool := setupDisposableDatabase(t, 11)
	if appPool == nil {
		return
	}
	ctx := context.Background()
	tm := postgres.NewTxManager(appPool, nil)
	store := pricing.NewPgxStore(appPool)
	engine := pricing.NewEngine()

	tenantID := uuid.New()
	storeID := uuid.New()
	companyID := uuid.New()
	productID := uuid.New()
	variantID := uuid.New()
	priceListID := uuid.New()

	ctxTenant := postgres.WithPrincipalContext(ctx, postgres.PrincipalContext{
		TenantID: tenantID,
		UserID:   uuid.New(),
	})

	catID := uuid.New()
	uomID := uuid.New()

	// 1. Seed master and organization records via superuser migrationPool
	statements := []string{
		`INSERT INTO organization.company (id, tenant_id, code, name) VALUES ('` + companyID.String() + `', '` + tenantID.String() + `', 'CMP-DEC', 'Decimal Test Co');`,
		`INSERT INTO organization.store (id, tenant_id, company_id, code, name) VALUES ('` + storeID.String() + `', '` + tenantID.String() + `', '` + companyID.String() + `', 'STR-DEC', 'Decimal Test Store');`,
		`INSERT INTO master.category (id, tenant_id, code, name) VALUES ('` + catID.String() + `', '` + tenantID.String() + `', 'CAT-DEC', 'Decimal Category');`,
		`INSERT INTO master.uom (id, tenant_id, code, name) VALUES ('` + uomID.String() + `', '` + tenantID.String() + `', 'UOM-DEC', 'Pieces');`,
		`INSERT INTO master.product (id, tenant_id, sku, name, category_id, base_uom_id, status)
		 VALUES ('` + productID.String() + `', '` + tenantID.String() + `', 'PROD-DEC', 'Decimal Prod', '` + catID.String() + `', '` + uomID.String() + `', 'ACTIVE');`,
		`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name, status)
		 VALUES ('` + variantID.String() + `', '` + tenantID.String() + `', '` + productID.String() + `', 'SKU-DEC', 'Decimal Variant', 'ACTIVE');`,
	}
	for _, stmt := range statements {
		if _, err := migrationPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed master data stmt: %v", err)
		}
	}

	// 2. Create Active default price list
	pl := &pricing.PriceList{
		ID:        priceListID,
		Code:      "PL-DEC-TEST",
		Name:      "Decimal Price List",
		Currency:  "IDR",
		Channel:   "POS",
		Status:    pricing.PriceListStatusActive,
		IsDefault: true,
	}
	err := tm.WithinTx(ctxTenant, func(txCtx context.Context) error {
		return store.CreatePriceList(txCtx, pl)
	})
	if err != nil {
		t.Fatalf("create price list: %v", err)
	}

	// 3. Insert synthetic price rule directly with 4-decimal NUMERIC(19,4) unit_price = 19.9900
	ruleID := uuid.New()
	refTime := time.Now().UTC()
	err = tm.WithinTx(ctxTenant, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `
			INSERT INTO pricing.price_rule (
				id, tenant_id, price_list_id, product_id, variant_id,
				min_qty, unit_price, priority, effective_from, status, version
			) VALUES (
				$1, $2, $3, $4, $5,
				'1.000000'::numeric(18,6), '19.9900'::numeric(19,4), 100, $6, 'ACTIVE', 1
			)
		`, ruleID, tenantID, priceListID, productID, variantID, refTime.Add(-1*time.Hour))
		return execErr
	})
	if err != nil {
		t.Fatalf("insert synthetic price rule: %v", err)
	}

	// 4. Read the rule through the actual repository scan path (GetCandidateRules)
	var candidates []*pricing.PriceRule
	err = tm.WithinTx(ctxTenant, func(txCtx context.Context) error {
		var getErr error
		candidates, getErr = store.GetCandidateRules(txCtx, priceListID, []uuid.UUID{productID}, []uuid.UUID{variantID}, refTime)
		return getErr
	})
	if err != nil {
		t.Fatalf("get candidate rules: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate rule, got %d", len(candidates))
	}

	scannedRule := candidates[0]

	// Verify exact rational representation scanned directly from database NUMERIC text
	if scannedRule.GetUnitPriceRat().FloatString(4) != "19.9900" {
		t.Errorf("expected scanned unit_price rational 19.9900, got %s", scannedRule.GetUnitPriceRat().FloatString(4))
	}
	if scannedRule.GetMinQtyRat().FloatString(6) != "1.000000" {
		t.Errorf("expected scanned min_qty rational 1.000000, got %s", scannedRule.GetMinQtyRat().FloatString(6))
	}

	// 5. Construct 6-decimal quantity 1.250000 without float binary corruption
	qtyRat, ok := new(big.Rat).SetString("1.250000")
	if !ok {
		t.Fatalf("failed to construct exact rational quantity")
	}
	qtyFloat, _ := qtyRat.Float64()

	pricingCtx := pricing.PricingContext{
		TenantID:      tenantID,
		StoreID:       storeID,
		Currency:      "IDR",
		Channel:       "POS",
		ReferenceTime: refTime,
	}

	res := engine.ResolveLine(pricingCtx, pricing.ResolveItemInput{
		VariantID:   variantID,
		ProductID:   productID,
		Quantity:    qtyFloat,
		QuantityRat: qtyRat,
	}, candidates, refTime)

	if res == nil {
		t.Fatalf("expected line resolution result, got nil")
	}

	// 19.9900 * 1.250000 = 24.987500 -> exact 24.9875
	if res.LineTotalRat.FloatString(4) != "24.9875" {
		t.Errorf("expected LineTotalRat 24.9875, got %s", res.LineTotalRat.FloatString(4))
	}
	if res.LineTotal != 24.9875 {
		t.Errorf("expected LineTotal 24.9875, got %f", res.LineTotal)
	}

	// 6. Test a second synthetic rule with 12.3456 * 0.123456
	// 12.3456 * 0.123456 = 1.5241373936 -> rounds to 1.5241
	rule2ID := uuid.New()
	product2ID := uuid.New()
	variant2ID := uuid.New()
	statements2 := []string{
		`INSERT INTO master.product (id, tenant_id, sku, name, category_id, base_uom_id, status)
		 VALUES ('` + product2ID.String() + `', '` + tenantID.String() + `', 'PROD-DEC-2', 'Decimal Prod 2', '` + catID.String() + `', '` + uomID.String() + `', 'ACTIVE');`,
		`INSERT INTO master.product_variant (id, tenant_id, product_id, sku, name, status)
		 VALUES ('` + variant2ID.String() + `', '` + tenantID.String() + `', '` + product2ID.String() + `', 'SKU-DEC-2', 'Decimal Variant 2', 'ACTIVE');`,
	}
	for _, stmt := range statements2 {
		if _, err := migrationPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed prod 2: %v", err)
		}
	}
	err = tm.WithinTx(ctxTenant, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		_, execErr := tx.Exec(txCtx, `
			INSERT INTO pricing.price_rule (
				id, tenant_id, price_list_id, product_id, variant_id,
				min_qty, unit_price, priority, effective_from, status, version
			) VALUES (
				$1, $2, $3, $4, $5,
				'0.000001'::numeric(18,6), '12.3456'::numeric(19,4), 100, $6, 'ACTIVE', 1
			)
		`, rule2ID, tenantID, priceListID, product2ID, variant2ID, refTime.Add(-1*time.Hour))
		return execErr
	})
	if err != nil {
		t.Fatalf("insert synthetic price rule 2: %v", err)
	}

	var candidates2 []*pricing.PriceRule
	err = tm.WithinTx(ctxTenant, func(txCtx context.Context) error {
		var getErr error
		candidates2, getErr = store.GetCandidateRules(txCtx, priceListID, []uuid.UUID{product2ID}, []uuid.UUID{variant2ID}, refTime)
		return getErr
	})
	if err != nil {
		t.Fatalf("get candidate rules 2: %v", err)
	}

	qty2Rat, _ := new(big.Rat).SetString("0.123456")
	qty2Float, _ := qty2Rat.Float64()

	res2 := engine.ResolveLine(pricingCtx, pricing.ResolveItemInput{
		VariantID:   variant2ID,
		ProductID:   product2ID,
		Quantity:    qty2Float,
		QuantityRat: qty2Rat,
	}, candidates2, refTime)

	if res2 == nil {
		t.Fatalf("expected line resolution result 2, got nil")
	}

	if res2.LineTotalRat.FloatString(4) != "1.5241" {
		t.Errorf("expected LineTotalRat 1.5241, got %s", res2.LineTotalRat.FloatString(4))
	}
	if res2.LineTotal != 1.5241 {
		t.Errorf("expected LineTotal 1.5241, got %f", res2.LineTotal)
	}
}
