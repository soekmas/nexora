// Package pricing provides business service orchestration for the pricing module.
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// Service defines the business operations for pricing catalog and quotes.
type Service interface {
	// Quote calculation
	CalculateQuote(ctx context.Context, req PriceQuoteRequest) (*PriceQuote, error)

	// Price List operations
	CreatePriceList(ctx context.Context, req CreatePriceListRequest) (*PriceList, error)
	GetPriceList(ctx context.Context, id uuid.UUID) (*PriceList, error)
	UpdatePriceList(ctx context.Context, id uuid.UUID, req UpdatePriceListRequest) (*PriceList, error)
	ListPriceLists(ctx context.Context, filter PriceListFilter) ([]*PriceList, int, error)

	// Price Rule operations
	CreatePriceRule(ctx context.Context, req CreatePriceRuleRequest) (*PriceRule, error)
	GetPriceRule(ctx context.Context, id uuid.UUID) (*PriceRule, error)
	UpdatePriceRule(ctx context.Context, id uuid.UUID, req UpdatePriceRuleRequest) (*PriceRule, error)
	ListPriceRules(ctx context.Context, filter PriceRuleFilter) ([]*PriceRule, int, error)

	// Rule Transitions
	SubmitPriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error)
	ApprovePriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error)
	RejectPriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error)
	ActivatePriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error)
	DeactivatePriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error)
}

type pricingService struct {
	store        Store
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	engine       *Engine
	logger       *slog.Logger
	clock        func() time.Time
}

// NewService constructs a new pricing Service.
func NewService(
	store Store,
	txManager postgres.TxManager,
	auditStore audit.Store,
	outboxWriter outbox.Writer,
	logger *slog.Logger,
) Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &pricingService{
		store:        store,
		txManager:    txManager,
		auditStore:   auditStore,
		outboxWriter: outboxWriter,
		engine:       NewEngine(),
		logger:       logger,
		clock:        time.Now,
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// ============================================================================
// Quote Application Service
// ============================================================================

// CalculateQuote orchestrates deterministic quote resolution.
func (s *pricingService) CalculateQuote(ctx context.Context, req PriceQuoteRequest) (*PriceQuote, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	now := s.clock().UTC()

	// 1. Validate Store and Tenant context
	storeCompanyID, storeStatus, err := s.store.GetStore(ctx, req.StoreID)
	if err != nil {
		return nil, err
	}
	if storeStatus != "ACTIVE" {
		return nil, ErrStoreInactive
	}

	// 2. Validate Customer context if present
	var customerGroupID *uuid.UUID
	if req.CustomerID != nil && *req.CustomerID != uuid.Nil {
		cgID, custStatus, err := s.store.GetCustomerGroup(ctx, *req.CustomerID)
		if err != nil {
			return nil, err
		}
		if custStatus == "ACTIVE" {
			customerGroupID = cgID
		}
	}

	// 3. Resolve Price List (explicit or automatic default)
	var priceList *PriceList
	if req.PriceListID != nil && *req.PriceListID != uuid.Nil {
		pl, err := s.store.GetPriceListByID(ctx, *req.PriceListID)
		if err != nil {
			return nil, err
		}
		if pl.Status != PriceListStatusActive {
			return nil, ErrPriceNotFound
		}
		// DEC-P2-021: Currency mismatch check
		if !strings.EqualFold(pl.Currency, req.Currency) {
			return nil, ErrCurrencyMismatch
		}
		// Channel compatibility check
		if pl.Channel != "ALL" && !strings.EqualFold(pl.Channel, req.Channel) {
			return nil, ErrPriceNotFound
		}
		// Company scope check
		if pl.CompanyID != nil && *pl.CompanyID != storeCompanyID {
			return nil, ErrUnauthorizedPriceList
		}
		priceList = pl
	} else {
		// Automatic default resolution (DEC-P2-017 & Q-P2-001)
		pl, err := s.store.GetDefaultPriceList(ctx, req.Currency, req.Channel)
		if err != nil {
			return nil, err
		}
		priceList = pl
	}

	// 4. Resolve Product and Variant mappings
	variantIDs := make([]uuid.UUID, 0, len(req.Items))
	for _, it := range req.Items {
		variantIDs = append(variantIDs, it.VariantID)
	}

	variantMap, err := s.store.GetVariants(ctx, variantIDs)
	if err != nil {
		return nil, err
	}

	productIDs := make([]uuid.UUID, 0, len(variantMap))
	productSet := make(map[uuid.UUID]struct{})
	for _, pID := range variantMap {
		if _, exists := productSet[pID]; !exists {
			productSet[pID] = struct{}{}
			productIDs = append(productIDs, pID)
		}
	}

	// 5. Load candidate pricing rules
	candidates, err := s.store.GetCandidateRules(ctx, priceList.ID, productIDs, variantIDs, now)
	if err != nil {
		return nil, err
	}

	// 6. Build PricingContext
	pricingCtx := PricingContext{
		TenantID:        pc.TenantID,
		StoreID:         req.StoreID,
		CompanyID:       &storeCompanyID,
		CustomerGroupID: customerGroupID,
		Currency:        req.Currency,
		Channel:         req.Channel,
		ReferenceTime:   now,
	}

	// 7. Resolve each line item deterministically
	var unpricedItems []UnpricedItemDetail
	quoteItems := make([]PriceQuoteItem, len(req.Items))
	subtotalRat := new(big.Rat)

	for i, item := range req.Items {
		productID, found := variantMap[item.VariantID]
		if !found {
			unpricedItems = append(unpricedItems, UnpricedItemDetail{
				Index:     i,
				VariantID: item.VariantID.String(),
			})
			continue
		}

		res := s.engine.ResolveLine(pricingCtx, ResolveItemInput{
			VariantID:   item.VariantID,
			ProductID:   productID,
			Quantity:    item.Quantity,
			QuantityRat: item.GetQuantityRat(),
		}, candidates, now)

		if res == nil {
			unpricedItems = append(unpricedItems, UnpricedItemDetail{
				Index:     i,
				VariantID: item.VariantID.String(),
			})
			continue
		}

		quoteItems[i] = PriceQuoteItem{
			VariantID: item.VariantID,
			Quantity:  item.Quantity,
			UnitPrice: res.UnitPrice,
			Discount:  0.0000,
			NetAmount: res.LineTotal,
		}

		if res.LineTotalRat != nil {
			subtotalRat.Add(subtotalRat, res.LineTotalRat)
		} else {
			rLine, ok := new(big.Rat).SetString(strconv.FormatFloat(res.LineTotal, 'f', -1, 64))
			if ok {
				subtotalRat.Add(subtotalRat, rLine)
			} else {
				subtotalRat.Add(subtotalRat, new(big.Rat).SetFloat64(res.LineTotal))
			}
		}
	}

	// 8. Atomic all-or-nothing quote evaluation (DEC-P2-019 & Q-P2-003)
	if len(unpricedItems) > 0 {
		return nil, &MissingPriceError{
			UnpricedItems: unpricedItems,
		}
	}

	subtotalFloat, _ := subtotalRat.Float64()
	subtotal := RoundTo4(subtotalFloat)
	quote := &PriceQuote{
		Items:         quoteItems,
		Subtotal:      subtotal,
		DiscountTotal: 0.0000,
		TaxTotal:      0.0000,
		GrandTotal:    subtotal,
		Currency:      req.Currency,
	}

	return quote, nil
}

// ============================================================================
// Price List Management
// ============================================================================

func (s *pricingService) CreatePriceList(ctx context.Context, req CreatePriceListRequest) (*PriceList, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	status := req.Status
	if status == "" {
		status = PriceListStatusActive
	}

	pl := &PriceList{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		CompanyID: req.CompanyID,
		Code:      strings.TrimSpace(req.Code),
		Name:      strings.TrimSpace(req.Name),
		Currency:  strings.ToUpper(strings.TrimSpace(req.Currency)),
		Channel:   strings.ToUpper(strings.TrimSpace(req.Channel)),
		Status:    status,
		IsDefault: req.IsDefault,
		Version:   1,
	}

	if err := pl.Validate(); err != nil {
		return nil, err
	}

	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreatePriceList(txCtx, pl); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":       pl.Code,
				"name":       pl.Name,
				"currency":   pl.Currency,
				"channel":    pl.Channel,
				"is_default": pl.IsDefault,
			})
			resType := "price_list"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     pl.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceCreated,
				ResourceType: &resType,
				ResourceID:   &pl.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return pl, nil
}

func (s *pricingService) GetPriceList(ctx context.Context, id uuid.UUID) (*PriceList, error) {
	return s.store.GetPriceListByID(ctx, id)
}

func (s *pricingService) UpdatePriceList(ctx context.Context, id uuid.UUID, req UpdatePriceListRequest) (*PriceList, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	var updated *PriceList
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		existing, err := s.store.GetPriceListByID(txCtx, id)
		if err != nil {
			return err
		}

		if existing.Status == PriceListStatusArchived {
			return ErrPriceListArchived
		}

		if req.Name != nil {
			existing.Name = strings.TrimSpace(*req.Name)
		}
		if req.Status != nil {
			existing.Status = strings.ToUpper(strings.TrimSpace(*req.Status))
		}
		if req.IsDefault != nil {
			existing.IsDefault = *req.IsDefault
		}

		if err := existing.Validate(); err != nil {
			return err
		}

		if err := s.store.UpdatePriceList(txCtx, existing, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":       existing.Code,
				"name":       existing.Name,
				"status":     existing.Status,
				"is_default": existing.IsDefault,
				"version":    existing.Version,
			})
			resType := "price_list"
			reason := "Price list updated"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     existing.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceUpdated,
				ResourceType: &resType,
				ResourceID:   &existing.ID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		updated = existing
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *pricingService) ListPriceLists(ctx context.Context, filter PriceListFilter) ([]*PriceList, int, error) {
	return s.store.ListPriceLists(ctx, filter)
}

// ============================================================================
// Price Rule Management
// ============================================================================

func (s *pricingService) CreatePriceRule(ctx context.Context, req CreatePriceRuleRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	pr := &PriceRule{
		ID:              uuid.New(),
		TenantID:        pc.TenantID,
		PriceListID:     req.PriceListID,
		ProductID:       req.ProductID,
		VariantID:       req.VariantID,
		CustomerGroupID: req.CustomerGroupID,
		StoreID:         req.StoreID,
		MinQty:          req.MinQty,
		UnitPrice:       RoundTo4(req.UnitPrice),
		Priority:        req.Priority,
		EffectiveFrom:   req.EffectiveFrom.UTC(),
		Status:          PriceRuleStatusDraft,
		Version:         1,
	}
	if req.EffectiveTo != nil {
		t := req.EffectiveTo.UTC()
		pr.EffectiveTo = &t
	}
	if pr.Priority == 0 {
		pr.Priority = 100
	}
	if pr.MinQty == 0 {
		pr.MinQty = 1
	}

	if err := pr.Validate(); err != nil {
		return nil, err
	}

	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreatePriceRule(txCtx, pr); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"price_list_id":  pr.PriceListID,
				"product_id":     pr.ProductID,
				"variant_id":     pr.VariantID,
				"unit_price":     pr.UnitPrice,
				"min_qty":        pr.MinQty,
				"effective_from": pr.EffectiveFrom,
			})
			resType := "price_rule"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     pr.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceCreated,
				ResourceType: &resType,
				ResourceID:   &pr.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"rule_id":        pr.ID,
				"price_list_id":  pr.PriceListID,
				"product_id":     pr.ProductID,
				"variant_id":     pr.VariantID,
				"unit_price":     pr.UnitPrice,
				"status":         pr.Status,
				"effective_from": pr.EffectiveFrom,
			})
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      pr.TenantID,
				AggregateType: "price_rule",
				AggregateID:   pr.ID,
				EventType:     EventPriceCreated,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (s *pricingService) GetPriceRule(ctx context.Context, id uuid.UUID) (*PriceRule, error) {
	return s.store.GetPriceRuleByID(ctx, id)
}

func (s *pricingService) UpdatePriceRule(ctx context.Context, id uuid.UUID, req UpdatePriceRuleRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, ErrReasonRequired
	}

	var updated *PriceRule
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		existing, err := s.store.GetPriceRuleByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if existing.Status != PriceRuleStatusDraft {
			return ErrRuleNotEditable
		}

		if req.CustomerGroupID != nil {
			existing.CustomerGroupID = req.CustomerGroupID
		}
		if req.StoreID != nil {
			existing.StoreID = req.StoreID
		}
		if req.MinQty != nil {
			existing.MinQty = *req.MinQty
		}
		if req.UnitPrice != nil {
			existing.UnitPrice = RoundTo4(*req.UnitPrice)
		}
		if req.Priority != nil {
			existing.Priority = *req.Priority
		}
		if req.EffectiveFrom != nil {
			existing.EffectiveFrom = req.EffectiveFrom.UTC()
		}
		if req.EffectiveTo != nil {
			t := req.EffectiveTo.UTC()
			existing.EffectiveTo = &t
		}

		if err := existing.Validate(); err != nil {
			return err
		}

		if err := s.store.UpdatePriceRule(txCtx, existing, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"unit_price":     existing.UnitPrice,
				"min_qty":        existing.MinQty,
				"priority":       existing.Priority,
				"effective_from": existing.EffectiveFrom,
				"version":        existing.Version,
			})
			resType := "price_rule"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     existing.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceUpdated,
				ResourceType: &resType,
				ResourceID:   &existing.ID,
				Status:       audit.StatusSuccess,
				Reason:       &reason,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		updated = existing
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *pricingService) ListPriceRules(ctx context.Context, filter PriceRuleFilter) ([]*PriceRule, int, error) {
	return s.store.ListPriceRules(ctx, filter)
}

// ============================================================================
// State Machine Transitions
// ============================================================================

// SubmitPriceRule submits a draft rule for review. Rule remains in DRAFT; emits PRICE_SUBMITTED audit log.
func (s *pricingService) SubmitPriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	var resRule *PriceRule
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		rule, err := s.store.GetPriceRuleByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if rule.Status != PriceRuleStatusDraft {
			return &StateTransitionError{From: rule.Status, To: "SUBMITTED"}
		}

		if s.auditStore != nil {
			resType := "price_rule"
			reason := req.Reason
			var reasonPtr *string
			if reason != "" {
				reasonPtr = &reason
			}
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     rule.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceSubmitted,
				ResourceType: &resType,
				ResourceID:   &rule.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		resRule = rule
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resRule, nil
}

// ApprovePriceRule approves a draft rule: DRAFT -> APPROVED.
func (s *pricingService) ApprovePriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	var resRule *PriceRule
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		rule, err := s.store.GetPriceRuleByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if rule.Status != PriceRuleStatusDraft {
			return &StateTransitionError{From: rule.Status, To: PriceRuleStatusApproved}
		}

		rule.Status = PriceRuleStatusApproved
		if err := s.store.UpdatePriceRule(txCtx, rule, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "price_rule"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     rule.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceApproved,
				ResourceType: &resType,
				ResourceID:   &rule.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"rule_id":       rule.ID,
				"price_list_id": rule.PriceListID,
				"status":        rule.Status,
				"approved_at":   now.Format(time.RFC3339),
			})
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      rule.TenantID,
				AggregateType: "price_rule",
				AggregateID:   rule.ID,
				EventType:     EventPriceApproved,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		resRule = rule
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resRule, nil
}

// RejectPriceRule rejects a draft rule. Rule remains in DRAFT; emits PRICE_REJECTED audit log.
func (s *pricingService) RejectPriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	var resRule *PriceRule
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		rule, err := s.store.GetPriceRuleByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if rule.Status != PriceRuleStatusDraft {
			return &StateTransitionError{From: rule.Status, To: "REJECTED"}
		}

		if s.auditStore != nil {
			resType := "price_rule"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     rule.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceRejected,
				ResourceType: &resType,
				ResourceID:   &rule.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		resRule = rule
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resRule, nil
}

// ActivatePriceRule activates an approved or inactive rule: APPROVED/INACTIVE -> ACTIVE.
func (s *pricingService) ActivatePriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	var resRule *PriceRule
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		rule, err := s.store.GetPriceRuleByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if rule.Status != PriceRuleStatusApproved {
			return &StateTransitionError{From: rule.Status, To: PriceRuleStatusActive}
		}

		// Ensure rule is not expired before activating
		if rule.EffectiveTo != nil && !now.Before(*rule.EffectiveTo) {
			return errors.New("cannot activate an expired price rule")
		}

		rule.Status = PriceRuleStatusActive
		if err := s.store.UpdatePriceRule(txCtx, rule, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "price_rule"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     rule.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceActivated,
				ResourceType: &resType,
				ResourceID:   &rule.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"rule_id":        rule.ID,
				"price_list_id":  rule.PriceListID,
				"product_id":     rule.ProductID,
				"variant_id":     rule.VariantID,
				"unit_price":     rule.UnitPrice,
				"status":         rule.Status,
				"effective_from": rule.EffectiveFrom,
				"activated_at":   now.Format(time.RFC3339),
			})
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      rule.TenantID,
				AggregateType: "price_rule",
				AggregateID:   rule.ID,
				EventType:     EventPriceActivated,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		resRule = rule
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resRule, nil
}

// DeactivatePriceRule deactivates an active rule: ACTIVE -> INACTIVE.
func (s *pricingService) DeactivatePriceRule(ctx context.Context, id uuid.UUID, req RuleTransitionRequest) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	principal, _ := auth.PrincipalFromContext(ctx)
	actorID := pc.UserID
	if principal != nil && principal.UserID != uuid.Nil {
		actorID = principal.UserID
	}

	var resRule *PriceRule
	now := s.clock().UTC()

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		rule, err := s.store.GetPriceRuleByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if rule.Status != PriceRuleStatusActive {
			return &StateTransitionError{From: rule.Status, To: PriceRuleStatusInactive}
		}

		rule.Status = PriceRuleStatusInactive
		if err := s.store.UpdatePriceRule(txCtx, rule, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "price_rule"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     rule.TenantID,
				UserID:       uuidPtr(actorID),
				Module:       "PRICING",
				Action:       AuditActionPriceDeactivated,
				ResourceType: &resType,
				ResourceID:   &rule.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"rule_id":        rule.ID,
				"price_list_id":  rule.PriceListID,
				"status":         rule.Status,
				"deactivated_at": now.Format(time.RFC3339),
			})
			if err := s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      rule.TenantID,
				AggregateType: "price_rule",
				AggregateID:   rule.ID,
				EventType:     EventPriceDeactivated,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			}); err != nil {
				return fmt.Errorf("write outbox: %w", err)
			}
		}

		resRule = rule
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resRule, nil
}
