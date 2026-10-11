package promotion_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/promotion"
)

func setupTestService(t *testing.T) (promotion.Service, *promotion.PgxStore, *audit.PgxStore, *outbox.PgxWriter, postgres.TxManager, uuid.UUID) {
	_, appPool := setupDisposableDatabase(t, 12)
	if appPool == nil {
		t.Skip("PostgreSQL unavailable")
	}

	logger := newDiscardLogger()
	store := promotion.NewPgxStore(appPool)
	txManager := postgres.NewTxManager(appPool, logger)
	auditStore := audit.NewPgxStore(appPool, txManager, logger)
	outboxWriter := outbox.NewPgxWriter(logger)

	svc := promotion.NewService(store, txManager, auditStore, outboxWriter, logger)
	tenantID := uuid.New()
	return svc, store, auditStore, outboxWriter, txManager, tenantID
}

func contextWithUser(tenantID, userID uuid.UUID, permissions ...string) context.Context {
	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{
		TenantID: tenantID,
		UserID:   userID,
	})
	principal := &auth.AuthenticatedPrincipal{
		UserID:      userID,
		TenantID:    tenantID,
		Username:    "testuser",
		Permissions: permissions,
	}
	return auth.WithPrincipal(ctx, principal)
}

func TestService_CreateAndSubmitPromotion(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	creatorID := uuid.New()
	ctx := contextWithUser(tenantID, creatorID, promotion.PermPromotionCreate, promotion.PermPromotionSubmit)

	now := time.Now().UTC()
	req := promotion.CreatePromotionRequest{
		Code:          "SVC-PROMO-1",
		Name:          "Service Promo 1",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Rewards: []promotion.CreateRewardRequest{
			{
				Type:   promotion.RewardTypePercentageDiscount,
				Config: mustJSON(promotion.PercentageDiscountConfig{Percentage: 15.0}),
			},
		},
	}

	// 1. Create Promotion -> DRAFT
	p, err := svc.CreatePromotion(ctx, req)
	if err != nil {
		t.Fatalf("CreatePromotion failed: %v", err)
	}
	if p.Status != promotion.StatusDraft {
		t.Errorf("Expected status DRAFT, got %s", p.Status)
	}
	if p.CreatedBy == nil || *p.CreatedBy != creatorID {
		t.Errorf("Expected CreatedBy %s, got %v", creatorID, p.CreatedBy)
	}

	// 2. Submit Promotion -> PENDING_APPROVAL
	subReq := promotion.TransitionRequest{Version: p.Version}
	submitted, err := svc.SubmitPromotion(ctx, p.ID, subReq)
	if err != nil {
		t.Fatalf("SubmitPromotion failed: %v", err)
	}
	if submitted.Status != promotion.StatusPendingApproval {
		t.Errorf("Expected status PENDING_APPROVAL, got %s", submitted.Status)
	}

	// 3. Submitting again from PENDING_APPROVAL must fail
	_, err = svc.SubmitPromotion(ctx, p.ID, promotion.TransitionRequest{Version: submitted.Version})
	if err == nil {
		t.Fatal("Expected error transitioning already submitted promo, got nil")
	}
	var transErr *promotion.StateTransitionError
	if !errors.As(err, &transErr) {
		t.Errorf("Expected StateTransitionError, got %v", err)
	}
}

func TestService_ApprovePromotion_SegregationOfDuties(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	creatorID := uuid.New()
	approverID := uuid.New()

	ctxCreator := contextWithUser(tenantID, creatorID,
		promotion.PermPromotionCreate,
		promotion.PermPromotionSubmit,
		promotion.PermPromotionApprove, // Even if creator has approve permission!
	)
	ctxApprover := contextWithUser(tenantID, approverID,
		promotion.PermPromotionApprove,
	)

	now := time.Now().UTC()
	p, err := svc.CreatePromotion(ctxCreator, promotion.CreatePromotionRequest{
		Code:          "SOD-PROMO",
		Name:          "SoD Test Promo",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Rewards: []promotion.CreateRewardRequest{
			{
				Type:   promotion.RewardTypeFixedAmountDiscount,
				Config: mustJSON(promotion.FixedAmountDiscountConfig{Amount: 10.0}),
			},
		},
	})
	if err != nil {
		t.Fatalf("CreatePromotion failed: %v", err)
	}

	p, err = svc.SubmitPromotion(ctxCreator, p.ID, promotion.TransitionRequest{Version: p.Version})
	if err != nil {
		t.Fatalf("SubmitPromotion failed: %v", err)
	}

	// Case A: Creator attempts to approve own promotion -> MUST FAIL with ErrSoDViolation
	_, err = svc.ApprovePromotion(ctxCreator, p.ID, promotion.TransitionRequest{Version: p.Version})
	if !errors.Is(err, promotion.ErrSoDViolation) {
		t.Fatalf("Expected ErrSoDViolation on self-approval, got %v", err)
	}

	// Case B: Distinct approver approves -> SUCCEEDS
	approved, err := svc.ApprovePromotion(ctxApprover, p.ID, promotion.TransitionRequest{
		Reason:  "Approved by supervisor",
		Version: p.Version,
	})
	if err != nil {
		t.Fatalf("Distinct approver approval failed: %v", err)
	}

	// Since effective_from was in the past and no effective_to, should transition directly to ACTIVE
	if approved.Status != promotion.StatusActive {
		t.Errorf("Expected status ACTIVE, got %s", approved.Status)
	}
	if approved.ApprovedBy == nil || *approved.ApprovedBy != approverID {
		t.Errorf("Expected ApprovedBy %s, got %v", approverID, approved.ApprovedBy)
	}
}

func TestService_RejectPromotion(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	creatorID := uuid.New()
	approverID := uuid.New()

	ctxCreator := contextWithUser(tenantID, creatorID, promotion.PermPromotionCreate, promotion.PermPromotionSubmit)
	ctxApprover := contextWithUser(tenantID, approverID, promotion.PermPromotionApprove)

	p, err := svc.CreatePromotion(ctxCreator, promotion.CreatePromotionRequest{
		Code:          "REJECT-PROMO",
		Name:          "Reject Promo",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	p, err = svc.SubmitPromotion(ctxCreator, p.ID, promotion.TransitionRequest{Version: p.Version})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Reject by approver
	rejected, err := svc.RejectPromotion(ctxApprover, p.ID, promotion.TransitionRequest{
		Reason:  "Does not meet promotional campaign criteria",
		Version: p.Version,
	})
	if err != nil {
		t.Fatalf("RejectPromotion failed: %v", err)
	}
	if rejected.Status != promotion.StatusRejected {
		t.Errorf("Expected status REJECTED, got %s", rejected.Status)
	}
}

func TestService_PauseAndActivatePromotion(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	creatorID := uuid.New()
	approverID := uuid.New()

	ctxCreator := contextWithUser(tenantID, creatorID, promotion.PermPromotionCreate, promotion.PermPromotionSubmit)
	ctxAdmin := contextWithUser(tenantID, approverID,
		promotion.PermPromotionApprove,
		promotion.PermPromotionPause,
		promotion.PermPromotionActivate,
	)

	now := time.Now().UTC()
	p, _ := svc.CreatePromotion(ctxCreator, promotion.CreatePromotionRequest{
		Code:          "LIFECYCLE-PAUSE-TEST",
		Name:          "Pause/Activate Test",
		Channel:       "ALL",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
	})
	p, _ = svc.SubmitPromotion(ctxCreator, p.ID, promotion.TransitionRequest{Version: p.Version})
	p, _ = svc.ApprovePromotion(ctxAdmin, p.ID, promotion.TransitionRequest{Version: p.Version})

	if p.Status != promotion.StatusActive {
		t.Fatalf("Expected ACTIVE, got %s", p.Status)
	}

	// Pause: ACTIVE -> PAUSED
	paused, err := svc.PausePromotion(ctxAdmin, p.ID, promotion.TransitionRequest{Version: p.Version})
	if err != nil {
		t.Fatalf("PausePromotion failed: %v", err)
	}
	if paused.Status != promotion.StatusPaused {
		t.Errorf("Expected PAUSED, got %s", paused.Status)
	}

	// Reactivate: PAUSED -> ACTIVE
	reactivated, err := svc.ActivatePromotion(ctxAdmin, p.ID, promotion.TransitionRequest{Version: paused.Version})
	if err != nil {
		t.Fatalf("ActivatePromotion failed: %v", err)
	}
	if reactivated.Status != promotion.StatusActive {
		t.Errorf("Expected ACTIVE, got %s", reactivated.Status)
	}
}

func TestService_UpdateDraft_And_ImmutabilityAfterApproval(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	userID := uuid.New()
	approverID := uuid.New()
	ctx := contextWithUser(tenantID, userID,
		promotion.PermPromotionCreate,
		promotion.PermPromotionUpdate,
		promotion.PermPromotionSubmit,
	)
	ctxApprover := contextWithUser(tenantID, approverID, promotion.PermPromotionApprove)

	p, err := svc.CreatePromotion(ctx, promotion.CreatePromotionRequest{
		Code:          "EDIT-TEST",
		Name:          "Original Name",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update while DRAFT succeeds
	newName := "Updated Name in Draft"
	updated, err := svc.UpdateDraftPromotion(ctx, p.ID, promotion.UpdatePromotionRequest{
		Name:    &newName,
		Version: p.Version,
	})
	if err != nil {
		t.Fatalf("UpdateDraftPromotion failed: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Name not updated: got %s", updated.Name)
	}

	// Submit and Approve
	sub, _ := svc.SubmitPromotion(ctx, p.ID, promotion.TransitionRequest{Version: updated.Version})
	app, _ := svc.ApprovePromotion(ctxApprover, p.ID, promotion.TransitionRequest{Version: sub.Version})

	// Once APPROVED/ACTIVE, in-place update MUST FAIL with ErrPromotionNotEditable
	badName := "Illegal Edit After Approval"
	_, err = svc.UpdateDraftPromotion(ctx, p.ID, promotion.UpdatePromotionRequest{
		Name:    &badName,
		Version: app.Version,
	})
	if !errors.Is(err, promotion.ErrPromotionNotEditable) {
		t.Fatalf("Expected ErrPromotionNotEditable, got %v", err)
	}

	// Instead, creating a new draft version succeeds
	newVerReq := promotion.CreatePromotionRequest{
		Name: "New Draft Version Name",
	}
	newDraft, err := svc.CreateDraftVersion(ctx, p.ID, newVerReq)
	if err != nil {
		t.Fatalf("CreateDraftVersion failed: %v", err)
	}
	if newDraft.Status != promotion.StatusDraft {
		t.Errorf("Expected new version status to reset to DRAFT, got %s", newDraft.Status)
	}
	if newDraft.Name != "New Draft Version Name" {
		t.Errorf("Expected new draft version name, got %s", newDraft.Name)
	}
}

func TestService_EvaluatePromotions_EndToEnd(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	creatorID := uuid.New()
	approverID := uuid.New()
	ctxCreator := contextWithUser(tenantID, creatorID, promotion.PermPromotionCreate, promotion.PermPromotionSubmit)
	ctxAdmin := contextWithUser(tenantID, approverID,
		promotion.PermPromotionApprove,
		promotion.PermPromotionEvaluate,
		promotion.PermPromotionRead,
	)

	now := time.Now().UTC()
	prodCoffee := uuid.New()
	prodCookie := uuid.New()

	// 1. Create and Approve Buy 2 Coffee Get 1 Free Cookie (BUY_X_GET_Y)
	pBOGO, err := svc.CreatePromotion(ctxCreator, promotion.CreatePromotionRequest{
		Code:          "EVAL-BUY2-GET1",
		Name:          "Buy 2 Coffee Get 1 Free Cookie",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      100,
		EffectiveFrom: now.Add(-time.Hour),
		Conditions: []promotion.CreateConditionRequest{
			{
				Type: promotion.ConditionTypeProductInList,
				Config: mustJSON(promotion.ProductInListConfig{
					ProductIDs: []uuid.UUID{prodCoffee},
				}),
			},
		},
		Rewards: []promotion.CreateRewardRequest{
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
	})
	if err != nil {
		t.Fatalf("Create BOGO: %v", err)
	}
	pBOGO, _ = svc.SubmitPromotion(ctxCreator, pBOGO.ID, promotion.TransitionRequest{Version: pBOGO.Version})
	pBOGO, err = svc.ApprovePromotion(ctxAdmin, pBOGO.ID, promotion.TransitionRequest{Version: pBOGO.Version})
	if err != nil {
		t.Fatalf("Approve BOGO: %v", err)
	}

	// 2. Create and Approve 10% Off Orders over $50 (PERCENTAGE_DISCOUNT)
	pPct, err := svc.CreatePromotion(ctxCreator, promotion.CreatePromotionRequest{
		Code:          "EVAL-10PCT",
		Name:          "10% Off on Spend > $50",
		Channel:       "POS",
		StackingMode:  promotion.StackingModeStackable,
		Priority:      90,
		EffectiveFrom: now.Add(-time.Hour),
		Conditions: []promotion.CreateConditionRequest{
			{
				Type:   promotion.ConditionTypeMinOrderAmount,
				Config: mustJSON(promotion.MinOrderAmountConfig{MinAmount: 50.00}),
			},
		},
		Rewards: []promotion.CreateRewardRequest{
			{
				Type:   promotion.RewardTypePercentageDiscount,
				Config: mustJSON(promotion.PercentageDiscountConfig{Percentage: 10.0}),
			},
		},
	})
	if err != nil {
		t.Fatalf("Create PCT: %v", err)
	}
	pPct, _ = svc.SubmitPromotion(ctxCreator, pPct.ID, promotion.TransitionRequest{Version: pPct.Version})
	pPct, err = svc.ApprovePromotion(ctxAdmin, pPct.ID, promotion.TransitionRequest{Version: pPct.Version})
	if err != nil {
		t.Fatalf("Approve PCT: %v", err)
	}

	// Cart: 4 Coffees @ $15.00 = $60.00, 2 Cookies @ $5.00 = $10.00 -> Subtotal = $70.00
	// BOGO triggers on Coffee: floor(4/2) = 2 sets -> 2 Cookies Free = $10.00 discount
	// PCT triggers: Subtotal $70 >= $50 -> 10% of $70 = $7.00 discount
	// Total Discount = $10 + $7 = $17.00. Net Total = $70 - $17 = $53.00
	evalRes, err := svc.EvaluatePromotions(ctxAdmin, promotion.EvaluateRequest{
		Channel: "POS",
		Items: []promotion.CartItem{
			{ProductID: prodCoffee, Name: "Coffee", Quantity: 4, UnitPrice: 15.00},
			{ProductID: prodCookie, Name: "Cookie", Quantity: 2, UnitPrice: 5.00},
		},
	})
	if err != nil {
		t.Fatalf("EvaluatePromotions failed: %v", err)
	}

	if evalRes.Subtotal != 70.00 {
		t.Errorf("Subtotal mismatch: got %v, want 70.00", evalRes.Subtotal)
	}
	if evalRes.DiscountTotal != 17.00 {
		t.Errorf("DiscountTotal mismatch: got %v, want 17.00", evalRes.DiscountTotal)
	}
	if evalRes.NetTotal != 53.00 {
		t.Errorf("NetTotal mismatch: got %v, want 53.00", evalRes.NetTotal)
	}
	if len(evalRes.AppliedPromotions) != 2 {
		t.Errorf("Expected 2 applied promotions, got %d", len(evalRes.AppliedPromotions))
	}
	if len(evalRes.BonusItems) != 1 || evalRes.BonusItems[0].FreeQuantity != 2 {
		t.Errorf("Expected 2 bonus cookies, got %+v", evalRes.BonusItems)
	}

	// Verify database usage_count was NOT mutated during evaluate (pure read-only)
	pBOGOFetched, err := svc.GetPromotion(ctxAdmin, pBOGO.ID)
	if err != nil {
		t.Fatalf("Get promo: %v", err)
	}
	if pBOGOFetched.UsageCount != 0 {
		t.Errorf("UsageCount must remain 0 after evaluation! Got %d", pBOGOFetched.UsageCount)
	}
}

func TestService_PermissionDenied(t *testing.T) {
	svc, _, _, _, _, tenantID := setupTestService(t)
	unauthUserID := uuid.New()
	// Context with NO permissions
	ctxUnauth := contextWithUser(tenantID, unauthUserID)

	_, err := svc.CreatePromotion(ctxUnauth, promotion.CreatePromotionRequest{
		Code: "UNAUTH-PROMO",
		Name: "Unauth",
	})
	if err == nil {
		t.Fatal("Expected forbidden error when missing permission, got nil")
	}

	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 403 {
		t.Errorf("Expected HTTP 403 Forbidden AppError, got %v", err)
	}
}
