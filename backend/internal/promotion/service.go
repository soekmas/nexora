package promotion

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// Service defines the application service operations for promotions.
type Service interface {
	// Evaluation
	EvaluatePromotions(ctx context.Context, req EvaluateRequest) (*EvaluationOutput, error)

	// CRUD
	CreatePromotion(ctx context.Context, req CreatePromotionRequest) (*Promotion, error)
	GetPromotion(ctx context.Context, id uuid.UUID) (*Promotion, error)
	GetPromotionByCode(ctx context.Context, code string) (*Promotion, error)
	UpdateDraftPromotion(ctx context.Context, id uuid.UUID, req UpdatePromotionRequest) (*Promotion, error)
	ListPromotions(ctx context.Context, filter PromotionFilter) ([]*Promotion, int, error)

	// Versioning
	CreateDraftVersion(ctx context.Context, id uuid.UUID, req CreatePromotionRequest) (*Promotion, error)

	// Lifecycle Transitions
	SubmitPromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error)
	ApprovePromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error)
	RejectPromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error)
	ActivatePromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error)
	PausePromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error)
}

type promotionService struct {
	store        Store
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	logger       *slog.Logger
	clock        func() time.Time
}

// NewService constructs a new Promotion Service.
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
	return &promotionService{
		store:        store,
		txManager:    txManager,
		auditStore:   auditStore,
		outboxWriter: outboxWriter,
		logger:       logger,
		clock:        time.Now,
	}
}

func (s *promotionService) checkPermission(ctx context.Context, permission string) error {
	principal, _ := auth.PrincipalFromContext(ctx)
	if principal != nil && !principal.HasPermission(permission) {
		return apperr.NewForbiddenError(fmt.Sprintf("Missing required permission %q", permission))
	}
	return nil
}

func getActorID(ctx context.Context) uuid.UUID {
	principal, _ := auth.PrincipalFromContext(ctx)
	if principal != nil && principal.UserID != uuid.Nil {
		return principal.UserID
	}
	if pc, ok := postgres.FromContext(ctx); ok && pc.UserID != uuid.Nil {
		return pc.UserID
	}
	return uuid.Nil
}

// ============================================================================
// Evaluation Application Service
// ============================================================================

func (s *promotionService) EvaluatePromotions(ctx context.Context, req EvaluateRequest) (*EvaluationOutput, error) {
	if err := s.checkPermission(ctx, PermPromotionEvaluate); err != nil {
		return nil, err
	}

	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	channel := req.Channel
	if channel == "" {
		channel = "POS"
	}

	evalTime := s.clock().UTC()
	if req.EvaluationTime != nil && !req.EvaluationTime.IsZero() {
		evalTime = req.EvaluationTime.UTC()
	}

	// 1. Query active candidate promotions from PostgreSQL within RLS context
	var activePromos []Promotion
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		activePromos, err = s.store.GetActivePromotions(txCtx, channel, evalTime)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load active promotions: %w", err)
	}

	// 2. Execute Pure Deterministic Evaluator from Stage 2
	evalInput := EvaluationInput{
		TenantID:        pc.TenantID,
		StoreID:         req.StoreID,
		Channel:         channel,
		CustomerTier:    req.CustomerTier,
		CustomerGroupID: req.CustomerGroupID,
		EvaluationTime:  evalTime,
		Items:           req.Items,
		Promotions:      activePromos,
	}

	return Evaluate(evalInput)
}

// ============================================================================
// Promotion CRUD & Versioning
// ============================================================================

func (s *promotionService) CreatePromotion(ctx context.Context, req CreatePromotionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionCreate); err != nil {
		return nil, err
	}

	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()

	promo := &Promotion{
		ID:            uuid.New(),
		TenantID:      pc.TenantID,
		Code:          strings.TrimSpace(req.Code),
		Name:          strings.TrimSpace(req.Name),
		Description:   req.Description,
		Channel:       req.Channel,
		StackingMode:  req.StackingMode,
		Priority:      req.Priority,
		EffectiveFrom: req.EffectiveFrom.UTC(),
		EffectiveTo:   nil,
		Status:        StatusDraft,
		UsageLimit:    req.UsageLimit,
		UsageCount:    0,
		CreatedBy:     &actorID,
		UpdatedBy:     &actorID,
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if promo.EffectiveFrom.IsZero() && req.ValidFrom != nil {
		promo.EffectiveFrom = req.ValidFrom.UTC()
	}
	if req.EffectiveTo != nil {
		t := req.EffectiveTo.UTC()
		promo.EffectiveTo = &t
	} else if req.ValidTo != nil {
		t := req.ValidTo.UTC()
		promo.EffectiveTo = &t
	}
	if promo.Channel == "" {
		promo.Channel = "ALL"
	}
	if promo.StackingMode == "" {
		promo.StackingMode = StackingModeStackable
	}

	for i, c := range req.Conditions {
		promo.Conditions = append(promo.Conditions, Condition{
			ID:          uuid.New(),
			PromotionID: promo.ID,
			Type:        c.Type,
			Config:      c.Config,
			SequenceNo:  i + 1,
		})
	}

	for i, r := range req.Rewards {
		promo.Rewards = append(promo.Rewards, Reward{
			ID:          uuid.New(),
			PromotionID: promo.ID,
			Type:        r.Type,
			Config:      r.Config,
			SequenceNo:  i + 1,
		})
	}

	if err := promo.Validate(); err != nil {
		return nil, err
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreatePromotion(txCtx, promo); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			meta, _ := json.Marshal(map[string]any{
				"code":          promo.Code,
				"name":          promo.Name,
				"priority":      promo.Priority,
				"stacking_mode": promo.StackingMode,
			})
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     promo.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionCreated,
				ResourceType: &resType,
				ResourceID:   &promo.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    now,
			}, nil)
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"promotion_id":   promo.ID,
				"code":           promo.Code,
				"status":         promo.Status,
				"effective_from": promo.EffectiveFrom,
			})
			_ = s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      promo.TenantID,
				AggregateType: "promotion",
				AggregateID:   promo.ID,
				EventType:     EventPromotionCreated,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return promo, nil
}

func (s *promotionService) GetPromotion(ctx context.Context, id uuid.UUID) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionRead); err != nil {
		return nil, err
	}
	var promo *Promotion
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		promo, err = s.store.GetPromotionByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return promo, nil
}

func (s *promotionService) GetPromotionByCode(ctx context.Context, code string) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionRead); err != nil {
		return nil, err
	}
	var promo *Promotion
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		promo, err = s.store.GetPromotionByCode(txCtx, code)
		return err
	})
	if err != nil {
		return nil, err
	}
	return promo, nil
}

func (s *promotionService) UpdateDraftPromotion(ctx context.Context, id uuid.UUID, req UpdatePromotionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionUpdate); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var updated *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		existing, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		// Only promotions in DRAFT can be modified in-place
		if existing.Status != StatusDraft {
			return ErrPromotionNotEditable
		}

		if req.Name != nil {
			existing.Name = strings.TrimSpace(*req.Name)
		}
		if req.Description != nil {
			existing.Description = *req.Description
		}
		if req.Channel != nil {
			existing.Channel = *req.Channel
		}
		if req.StackingMode != nil {
			existing.StackingMode = *req.StackingMode
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
		if req.UsageLimit != nil {
			existing.UsageLimit = req.UsageLimit
		}

		if req.Conditions != nil {
			existing.Conditions = nil
			for i, c := range req.Conditions {
				existing.Conditions = append(existing.Conditions, Condition{
					ID:          uuid.New(),
					PromotionID: existing.ID,
					Type:        c.Type,
					Config:      c.Config,
					SequenceNo:  i + 1,
				})
			}
		}

		if req.Rewards != nil {
			existing.Rewards = nil
			for i, r := range req.Rewards {
				existing.Rewards = append(existing.Rewards, Reward{
					ID:          uuid.New(),
					PromotionID: existing.ID,
					Type:        r.Type,
					Config:      r.Config,
					SequenceNo:  i + 1,
				})
			}
		}

		existing.UpdatedBy = &actorID
		if err := existing.Validate(); err != nil {
			return err
		}

		if err := s.store.UpdatePromotion(txCtx, existing, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     existing.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionUpdated,
				ResourceType: &resType,
				ResourceID:   &existing.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    now,
			}, nil)
		}

		updated = existing
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *promotionService) ListPromotions(ctx context.Context, filter PromotionFilter) ([]*Promotion, int, error) {
	if err := s.checkPermission(ctx, PermPromotionRead); err != nil {
		return nil, 0, err
	}
	var list []*Promotion
	var total int
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		list, total, err = s.store.ListPromotions(txCtx, filter)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// CreateDraftVersion creates a new DRAFT version snapshot when editing an already approved/active promotion.
func (s *promotionService) CreateDraftVersion(ctx context.Context, id uuid.UUID, req CreatePromotionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionUpdate); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var updatedPromo *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		existing, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		// Snapshot current approved state into promotion_version
		snapshotBytes, _ := json.Marshal(existing)
		versionNo := int(existing.Version)
		if versionNo <= 0 {
			versionNo = 1
		}
		ver := &PromotionVersion{
			ID:           uuid.New(),
			TenantID:     existing.TenantID,
			PromotionID:  existing.ID,
			VersionNo:    versionNo,
			RuleSnapshot: snapshotBytes,
			Status:       existing.Status,
			CreatedAt:    now,
			CreatedBy:    existing.CreatedBy,
			ApprovedAt:   existing.ApprovedAt,
			ApprovedBy:   existing.ApprovedBy,
		}
		if err := s.store.CreatePromotionVersion(txCtx, ver); err != nil {
			return fmt.Errorf("save version snapshot: %w", err)
		}

		// Transition promotion to DRAFT for new version
		existing.Status = StatusDraft
		existing.ApprovedBy = nil
		existing.ApprovedAt = nil
		if req.Name != "" {
			existing.Name = req.Name
		}
		if req.Description != "" {
			existing.Description = req.Description
		}
		if req.Priority > 0 {
			existing.Priority = req.Priority
		}
		if !req.EffectiveFrom.IsZero() {
			existing.EffectiveFrom = req.EffectiveFrom.UTC()
		}
		if req.EffectiveTo != nil {
			t := req.EffectiveTo.UTC()
			existing.EffectiveTo = &t
		}
		if req.UsageLimit != nil {
			existing.UsageLimit = req.UsageLimit
		}

		if len(req.Conditions) > 0 {
			existing.Conditions = nil
			for i, c := range req.Conditions {
				existing.Conditions = append(existing.Conditions, Condition{
					ID:          uuid.New(),
					PromotionID: existing.ID,
					Type:        c.Type,
					Config:      c.Config,
					SequenceNo:  i + 1,
				})
			}
		}
		if len(req.Rewards) > 0 {
			existing.Rewards = nil
			for i, r := range req.Rewards {
				existing.Rewards = append(existing.Rewards, Reward{
					ID:          uuid.New(),
					PromotionID: existing.ID,
					Type:        r.Type,
					Config:      r.Config,
					SequenceNo:  i + 1,
				})
			}
		}

		existing.UpdatedBy = &actorID
		if err := existing.Validate(); err != nil {
			return err
		}

		if err := s.store.UpdatePromotion(txCtx, existing, existing.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     existing.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionVersionCreated,
				ResourceType: &resType,
				ResourceID:   &existing.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    now,
			}, nil)
		}

		updatedPromo = existing
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updatedPromo, nil
}

// ============================================================================
// Lifecycle Transitions
// ============================================================================

func (s *promotionService) SubmitPromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionSubmit); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var resPromo *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status != StatusDraft {
			return &StateTransitionError{From: p.Status, To: StatusPendingApproval}
		}

		p.Status = StatusPendingApproval
		p.UpdatedBy = &actorID
		if err := s.store.UpdatePromotion(txCtx, p, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionSubmitted,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    now,
			}, nil)
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"promotion_id": p.ID,
				"code":         p.Code,
				"status":       p.Status,
			})
			_ = s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      p.TenantID,
				AggregateType: "promotion",
				AggregateID:   p.ID,
				EventType:     EventPromotionSubmitted,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			})
		}

		resPromo = p
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resPromo, nil
}

func (s *promotionService) ApprovePromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionApprove); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var resPromo *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status != StatusPendingApproval {
			return &StateTransitionError{From: p.Status, To: StatusApproved}
		}

		// Enforce Segregation of Duties (SoD): Creator cannot approve own promotion!
		if p.CreatedBy != nil && actorID != uuid.Nil && *p.CreatedBy == actorID {
			return ErrSoDViolation
		}

		// Determine target state based on effective window
		if p.IsEffectiveAt(now) {
			p.Status = StatusActive
		} else if p.EffectiveFrom.After(now) {
			p.Status = StatusScheduled
		} else {
			p.Status = StatusApproved
		}

		p.ApprovedBy = &actorID
		p.ApprovedAt = &now
		p.UpdatedBy = &actorID

		if err := s.store.UpdatePromotion(txCtx, p, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionApproved,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil)
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"promotion_id": p.ID,
				"code":         p.Code,
				"status":       p.Status,
				"approved_by":  p.ApprovedBy,
			})
			_ = s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      p.TenantID,
				AggregateType: "promotion",
				AggregateID:   p.ID,
				EventType:     EventPromotionApproved,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			})
		}

		resPromo = p
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resPromo, nil
}

func (s *promotionService) RejectPromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionApprove); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var resPromo *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status != StatusPendingApproval {
			return &StateTransitionError{From: p.Status, To: StatusRejected}
		}

		p.Status = StatusRejected
		p.UpdatedBy = &actorID

		if err := s.store.UpdatePromotion(txCtx, p, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionRejected,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil)
		}

		resPromo = p
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resPromo, nil
}

func (s *promotionService) ActivatePromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionActivate); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var resPromo *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status != StatusApproved && p.Status != StatusScheduled && p.Status != StatusPaused {
			return &StateTransitionError{From: p.Status, To: StatusActive}
		}

		p.Status = StatusActive
		p.UpdatedBy = &actorID

		if err := s.store.UpdatePromotion(txCtx, p, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionActivated,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil)
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"promotion_id": p.ID,
				"code":         p.Code,
				"status":       p.Status,
			})
			_ = s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      p.TenantID,
				AggregateType: "promotion",
				AggregateID:   p.ID,
				EventType:     EventPromotionActivated,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			})
		}

		resPromo = p
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resPromo, nil
}

func (s *promotionService) PausePromotion(ctx context.Context, id uuid.UUID, req TransitionRequest) (*Promotion, error) {
	if err := s.checkPermission(ctx, PermPromotionPause); err != nil {
		return nil, err
	}

	actorID := getActorID(ctx)
	now := s.clock().UTC()
	var resPromo *Promotion

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetPromotionByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status != StatusActive {
			return &StateTransitionError{From: p.Status, To: StatusPaused}
		}

		p.Status = StatusPaused
		p.UpdatedBy = &actorID

		if err := s.store.UpdatePromotion(txCtx, p, req.Version); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "promotion"
			var reasonPtr *string
			if req.Reason != "" {
				reasonPtr = &req.Reason
			}
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       &actorID,
				Module:       "PROMOTION",
				Action:       AuditActionPromotionPaused,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Reason:       reasonPtr,
				CreatedAt:    now,
			}, nil)
		}

		if s.outboxWriter != nil {
			payload, _ := json.Marshal(map[string]any{
				"promotion_id": p.ID,
				"code":         p.Code,
				"status":       p.Status,
			})
			_ = s.outboxWriter.Write(txCtx, &outbox.Event{
				ID:            uuid.New(),
				TenantID:      p.TenantID,
				AggregateType: "promotion",
				AggregateID:   p.ID,
				EventType:     EventPromotionPaused,
				EventVersion:  1,
				Payload:       payload,
				Status:        outbox.StatusPending,
				OccurredAt:    now,
			})
		}

		resPromo = p
		return nil
	})

	if err != nil {
		return nil, err
	}
	return resPromo, nil
}
