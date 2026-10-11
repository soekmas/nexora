package promotion_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/promotion"
)

func TestPgxStore_CreateAndGetPromotion(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}

	store := promotion.NewPgxStore(migrationPool)
	tenantID := uuid.New()
	userID := uuid.New()
	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{
		TenantID: tenantID,
		UserID:   userID,
	})

	now := time.Now().UTC()
	promo := &promotion.Promotion{
		Code:          "TEST-PROMO-1",
		Name:          "Test Promo 1",
		Description:   "Description for Promo 1",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      150,
		EffectiveFrom: now.Add(-time.Hour),
		EffectiveTo:   nil,
		Status:        promotion.StatusDraft,
		Conditions: []promotion.Condition{
			{
				Type:       promotion.ConditionTypeMinOrderAmount,
				Config:     mustJSON(promotion.MinOrderAmountConfig{MinAmount: 50.00}),
				SequenceNo: 1,
			},
		},
		Rewards: []promotion.Reward{
			{
				Type:       promotion.RewardTypePercentageDiscount,
				Config:     mustJSON(promotion.PercentageDiscountConfig{Percentage: 10.0}),
				SequenceNo: 1,
			},
		},
	}

	err := store.CreatePromotion(ctx, promo)
	if err != nil {
		t.Fatalf("CreatePromotion failed: %v", err)
	}

	if promo.ID == uuid.Nil {
		t.Fatal("Expected promo.ID to be populated")
	}

	// Fetch by ID
	fetched, err := store.GetPromotionByID(ctx, promo.ID)
	if err != nil {
		t.Fatalf("GetPromotionByID failed: %v", err)
	}
	if fetched.Code != promo.Code || fetched.Name != promo.Name || fetched.Priority != 150 {
		t.Errorf("Fetched promo mismatch: %+v", fetched)
	}
	if len(fetched.Conditions) != 1 || fetched.Conditions[0].Type != promotion.ConditionTypeMinOrderAmount {
		t.Errorf("Conditions not loaded properly: %+v", fetched.Conditions)
	}
	if len(fetched.Rewards) != 1 || fetched.Rewards[0].Type != promotion.RewardTypePercentageDiscount {
		t.Errorf("Rewards not loaded properly: %+v", fetched.Rewards)
	}

	// Fetch by Code
	fetchedByCode, err := store.GetPromotionByCode(ctx, promo.Code)
	if err != nil {
		t.Fatalf("GetPromotionByCode failed: %v", err)
	}
	if fetchedByCode.ID != promo.ID {
		t.Errorf("Expected ID %s, got %s", promo.ID, fetchedByCode.ID)
	}
}

func TestPgxStore_OptimisticLocking(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}

	store := promotion.NewPgxStore(migrationPool)
	tenantID := uuid.New()
	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{
		TenantID: tenantID,
	})

	now := time.Now().UTC()
	promo := &promotion.Promotion{
		Code:          "LOCK-PROMO",
		Name:          "Lock Promo",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now,
		Status:        promotion.StatusDraft,
	}

	if err := store.CreatePromotion(ctx, promo); err != nil {
		t.Fatalf("CreatePromotion failed: %v", err)
	}

	// Update with wrong expected version (e.g. 99 instead of 1) -> ErrVersionConflict
	promo.Name = "Concurrent Edit"
	err := store.UpdatePromotion(ctx, promo, 99)
	if err != promotion.ErrVersionConflict {
		t.Fatalf("Expected ErrVersionConflict, got %v", err)
	}

	// Update with correct version 1 -> succeeds, version becomes 2
	err = store.UpdatePromotion(ctx, promo, 1)
	if err != nil {
		t.Fatalf("Update with expected version 1 failed: %v", err)
	}
	if promo.Version != 2 {
		t.Errorf("Expected version 2, got %d", promo.Version)
	}
}

func TestPgxStore_TenantIsolation(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}

	store := promotion.NewPgxStore(migrationPool)
	tenantA := uuid.New()
	tenantB := uuid.New()

	ctxA := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{TenantID: tenantA})
	ctxB := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{TenantID: tenantB})

	promoA := &promotion.Promotion{
		Code:          "TENANT-A-PROMO",
		Name:          "Tenant A Promo",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: time.Now().UTC(),
		Status:        promotion.StatusDraft,
	}

	if err := store.CreatePromotion(ctxA, promoA); err != nil {
		t.Fatalf("Create promotion in Tenant A failed: %v", err)
	}

	// Tenant B cannot get promoA by ID
	_, err := store.GetPromotionByID(ctxB, promoA.ID)
	if err != promotion.ErrPromotionNotFound {
		t.Fatalf("Tenant B should receive ErrPromotionNotFound, got %v", err)
	}

	// Tenant B cannot get promoA by Code
	_, err = store.GetPromotionByCode(ctxB, promoA.Code)
	if err != promotion.ErrPromotionNotFound {
		t.Fatalf("Tenant B should receive ErrPromotionNotFound by code, got %v", err)
	}

	// Tenant B can create promo with the same code (tenant-isolated uniqueness)
	promoB := &promotion.Promotion{
		Code:          "TENANT-A-PROMO", // Same code
		Name:          "Tenant B Promo",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: time.Now().UTC(),
		Status:        promotion.StatusDraft,
	}
	if err := store.CreatePromotion(ctxB, promoB); err != nil {
		t.Fatalf("Tenant B creating same code in separate tenant should succeed: %v", err)
	}
}

func TestPgxStore_GetActivePromotions(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}

	store := promotion.NewPgxStore(migrationPool)
	tenantID := uuid.New()
	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{TenantID: tenantID})

	now := time.Now().UTC()

	// Active promo
	activePromo := &promotion.Promotion{
		Code:          "ACTIVE-PROMO",
		Name:          "Active",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusActive,
		Rewards: []promotion.Reward{
			{
				Type:       promotion.RewardTypePercentageDiscount,
				Config:     mustJSON(promotion.PercentageDiscountConfig{Percentage: 10.0}),
				SequenceNo: 1,
			},
		},
	}
	if err := store.CreatePromotion(ctx, activePromo); err != nil {
		t.Fatalf("Create active promo: %v", err)
	}

	// Draft promo (should NOT be returned)
	draftPromo := &promotion.Promotion{
		Code:          "DRAFT-PROMO",
		Name:          "Draft",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Status:        promotion.StatusDraft,
	}
	if err := store.CreatePromotion(ctx, draftPromo); err != nil {
		t.Fatalf("Create draft promo: %v", err)
	}

	// Query active promotions
	activeList, err := store.GetActivePromotions(ctx, "POS", now)
	if err != nil {
		t.Fatalf("GetActivePromotions error: %v", err)
	}

	if len(activeList) != 1 || activeList[0].Code != "ACTIVE-PROMO" {
		t.Fatalf("Expected only ACTIVE-PROMO, got %+v", activeList)
	}
	if len(activeList[0].Rewards) != 1 {
		t.Errorf("Active promo rewards not loaded: %+v", activeList[0].Rewards)
	}
}

func TestPgxStore_PromotionVersions(t *testing.T) {
	migrationPool, _ := setupDisposableDatabase(t, 12)
	if migrationPool == nil {
		return
	}

	store := promotion.NewPgxStore(migrationPool)
	tenantID := uuid.New()
	userID := uuid.New()
	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{
		TenantID: tenantID,
		UserID:   userID,
	})

	now := time.Now().UTC()
	promo := &promotion.Promotion{
		Code:          "VERSION-PROMO",
		Name:          "Versioned Promo",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now,
		Status:        promotion.StatusDraft,
	}
	if err := store.CreatePromotion(ctx, promo); err != nil {
		t.Fatalf("Create promotion: %v", err)
	}

	// Create version snapshot 1
	ver1 := &promotion.PromotionVersion{
		PromotionID:  promo.ID,
		VersionNo:    1,
		RuleSnapshot: mustJSON(map[string]any{"v": 1}),
		Status:       "APPROVED",
		CreatedBy:    &userID,
	}
	if err := store.CreatePromotionVersion(ctx, ver1); err != nil {
		t.Fatalf("CreatePromotionVersion 1 failed: %v", err)
	}

	// Create version snapshot 2
	ver2 := &promotion.PromotionVersion{
		PromotionID:  promo.ID,
		VersionNo:    2,
		RuleSnapshot: mustJSON(map[string]any{"v": 2}),
		Status:       "APPROVED",
		CreatedBy:    &userID,
	}
	if err := store.CreatePromotionVersion(ctx, ver2); err != nil {
		t.Fatalf("CreatePromotionVersion 2 failed: %v", err)
	}

	// Get latest version
	latest, err := store.GetLatestPromotionVersion(ctx, promo.ID)
	if err != nil {
		t.Fatalf("GetLatestPromotionVersion failed: %v", err)
	}
	if latest == nil || latest.VersionNo != 2 {
		t.Fatalf("Expected latest version 2, got %+v", latest)
	}
}
