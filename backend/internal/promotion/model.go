// Package promotion implements the NEXORA Promotion Engine domain models,
// pure deterministic rule evaluators, stacking resolution, and promotion lifecycle.
package promotion

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Canonical Promotion Statuses (docs/architecture/10-state-machine-catalog.md Section 36)
const (
	StatusDraft           = "DRAFT"
	StatusPendingApproval = "PENDING_APPROVAL"
	StatusApproved        = "APPROVED"
	StatusScheduled       = "SCHEDULED"
	StatusActive          = "ACTIVE"
	StatusPaused          = "PAUSED"
	StatusExpired         = "EXPIRED"
	StatusRejected        = "REJECTED"
	StatusCancelled       = "CANCELLED"
)

// Canonical Stacking Modes (docs/architecture/00-supplemental-requirements.md Section 12, DEC-B2-006)
const (
	StackingModeStackable = "STACKABLE"
	StackingModeExclusive = "EXCLUSIVE"
	StackingModeBestOnly  = "BEST_ONLY"
)

// Canonical Condition Types (DEC-B2-005)
const (
	ConditionTypeMinOrderAmount = "MIN_ORDER_AMOUNT"
	ConditionTypeMinItemQty     = "MIN_ITEM_QTY"
	ConditionTypeProductInList  = "PRODUCT_IN_LIST"
	ConditionTypeCategoryInList = "CATEGORY_IN_LIST"
)

// Canonical Reward Types (DEC-B2-005, DEC-B2-009)
const (
	RewardTypePercentageDiscount  = "PERCENTAGE_DISCOUNT"
	RewardTypeFixedAmountDiscount = "FIXED_AMOUNT_DISCOUNT"
	RewardTypeBuyXGetY           = "BUY_X_GET_Y"
)

// Canonical Audit Actions (docs/architecture/13-audit-action-catalog.md Section 16)
const (
	AuditActionPromotionCreated        = "PROMOTION_CREATED"
	AuditActionPromotionUpdated        = "PROMOTION_UPDATED"
	AuditActionPromotionSubmitted      = "PROMOTION_SUBMITTED"
	AuditActionPromotionApproved       = "PROMOTION_APPROVED"
	AuditActionPromotionRejected       = "PROMOTION_REJECTED"
	AuditActionPromotionActivated      = "PROMOTION_ACTIVATED"
	AuditActionPromotionPaused         = "PROMOTION_PAUSED"
	AuditActionPromotionExpired        = "PROMOTION_EXPIRED"
	AuditActionPromotionVersionCreated = "PROMOTION_VERSION_CREATED"
	AuditActionPromotionRuleChanged    = "PROMOTION_RULE_CHANGED"
	AuditActionPromotionApplied        = "PROMOTION_APPLIED"
	AuditActionPromotionDeactivated    = "PROMOTION_DEACTIVATED"
)

// Canonical Permissions (docs/architecture/11-permission-matrix.md Section 14)
const (
	PermPromotionRead     = "promotion.read"
	PermPromotionCreate   = "promotion.create"
	PermPromotionUpdate   = "promotion.update"
	PermPromotionSubmit   = "promotion.submit"
	PermPromotionApprove  = "promotion.approve"
	PermPromotionActivate = "promotion.activate"
	PermPromotionPause    = "promotion.pause"
	PermPromotionExpire   = "promotion.expire"
	PermPromotionEvaluate = "promotion.evaluate"
	PermPromotionSchedule = "promotion.schedule"
)

// Canonical Outbox Events (docs/architecture/12-event-catalog.md Section 13)
const (
	EventPromotionCreated   = "promotion.promotion.created.v1"
	EventPromotionSubmitted = "promotion.promotion.submitted.v1"
	EventPromotionApproved  = "promotion.promotion.approved.v1"
	EventPromotionActivated = "promotion.promotion.activated.v1"
	EventPromotionPaused    = "promotion.promotion.paused.v1"
	EventPromotionExpired   = "promotion.promotion.expired.v1"
	EventPromotionApplied   = "promotion.promotion.applied.v1"
)

// MinOrderAmountConfig models parameters for MIN_ORDER_AMOUNT condition.
type MinOrderAmountConfig struct {
	MinAmount float64 `json:"min_amount"`
}

// MinItemQtyConfig models parameters for MIN_ITEM_QTY condition.
type MinItemQtyConfig struct {
	MinQty      float64     `json:"min_qty"`
	ProductIDs  []uuid.UUID `json:"product_ids,omitempty"`
	CategoryIDs []string    `json:"category_ids,omitempty"`
}

// ProductInListConfig models parameters for PRODUCT_IN_LIST condition.
type ProductInListConfig struct {
	ProductIDs []uuid.UUID `json:"product_ids"`
	VariantIDs []uuid.UUID `json:"variant_ids,omitempty"`
}

// CategoryInListConfig models parameters for CATEGORY_IN_LIST condition.
type CategoryInListConfig struct {
	CategoryIDs []string `json:"category_ids"`
}

// PercentageDiscountConfig models parameters for PERCENTAGE_DISCOUNT reward.
type PercentageDiscountConfig struct {
	Percentage        float64  `json:"percentage"`                    // Percentage discount, e.g. 15.0 for 15%
	MaxDiscountAmount *float64 `json:"max_discount_amount,omitempty"` // Optional monetary cap
}

// FixedAmountDiscountConfig models parameters for FIXED_AMOUNT_DISCOUNT reward.
type FixedAmountDiscountConfig struct {
	Amount float64 `json:"amount"` // Fixed nominal discount amount
}

// BuyXGetYConfig models parameters for BUY_X_GET_Y reward.
type BuyXGetYConfig struct {
	BuyQty             float64    `json:"buy_qty"`                        // Quantity of qualifying item required (X)
	GetQty             float64    `json:"get_qty"`                        // Quantity of bonus item rewarded (Y)
	RewardProductID    *uuid.UUID `json:"reward_product_id,omitempty"`    // If nil, same product as bought
	RewardVariantID    *uuid.UUID `json:"reward_variant_id,omitempty"`    // Optional variant of bonus product
	RewardItemName     string     `json:"reward_item_name,omitempty"`     // Display name of bonus item
	DiscountPercentage float64    `json:"discount_percentage"`            // Percentage off the bonus item (e.g. 100 for free)
	MaxApplications    *int       `json:"max_applications,omitempty"`     // Maximum application sets permitted per cart
}

// Condition represents a single rule condition for promotion eligibility.
type Condition struct {
	ID          uuid.UUID       `json:"id"`
	PromotionID uuid.UUID       `json:"promotion_id"`
	Type        string          `json:"condition_type"`
	Config      json.RawMessage `json:"condition_config"`
	SequenceNo  int             `json:"sequence_no"`
}

// Reward represents a single promotion benefit/reward formula.
type Reward struct {
	ID          uuid.UUID       `json:"id"`
	PromotionID uuid.UUID       `json:"promotion_id"`
	Type        string          `json:"reward_type"`
	Config      json.RawMessage `json:"reward_config"`
	SequenceNo  int             `json:"sequence_no"`
}

// Promotion represents a tenant-isolated promotion master aggregate.
type Promotion struct {
	ID            uuid.UUID   `json:"id"`
	TenantID      uuid.UUID   `json:"tenant_id"`
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Description   string      `json:"description,omitempty"`
	Channel       string      `json:"channel"`
	StackingMode  string      `json:"stacking_mode"`
	Priority      int         `json:"priority"`
	EffectiveFrom time.Time   `json:"effective_from"`
	EffectiveTo   *time.Time  `json:"effective_to,omitempty"`
	Status        string      `json:"status"`
	UsageLimit    *int        `json:"usage_limit,omitempty"`
	UsageCount    int         `json:"usage_count"`
	CreatedBy     *uuid.UUID  `json:"created_by,omitempty"`
	UpdatedBy     *uuid.UUID  `json:"updated_by,omitempty"`
	ApprovedBy    *uuid.UUID  `json:"approved_by,omitempty"`
	ApprovedAt    *time.Time  `json:"approved_at,omitempty"`
	Version       int64       `json:"version"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	Conditions    []Condition `json:"conditions,omitempty"`
	Rewards       []Reward    `json:"rewards,omitempty"`
}

// Validate validates core Promotion attributes.
func (p *Promotion) Validate() error {
	p.Code = strings.TrimSpace(p.Code)
	if p.Code == "" || len(p.Code) > 50 {
		return errors.New("promotion code is required and must not exceed 50 characters")
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 200 {
		return errors.New("promotion name is required and must not exceed 200 characters")
	}
	if p.Priority < 0 {
		return errors.New("priority must be non-negative")
	}
	switch p.StackingMode {
	case StackingModeStackable, StackingModeExclusive, StackingModeBestOnly:
	default:
		return fmt.Errorf("invalid stacking mode %q: must be STACKABLE, EXCLUSIVE, or BEST_ONLY", p.StackingMode)
	}
	switch p.Status {
	case StatusDraft, StatusPendingApproval, StatusApproved, StatusScheduled,
		StatusActive, StatusPaused, StatusExpired, StatusRejected, StatusCancelled:
	default:
		return fmt.Errorf("invalid promotion status %q", p.Status)
	}
	if p.EffectiveFrom.IsZero() {
		return errors.New("effective_from timestamp is required")
	}
	if p.EffectiveTo != nil && !p.EffectiveTo.After(p.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	if p.UsageLimit != nil && *p.UsageLimit <= 0 {
		return errors.New("usage_limit must be greater than zero when set")
	}
	if p.UsageCount < 0 {
		return errors.New("usage_count must not be negative")
	}
	return nil
}

// IsEffectiveAt checks whether the promotion is active for the given reference time.
func (p *Promotion) IsEffectiveAt(t time.Time) bool {
	if t.Before(p.EffectiveFrom) {
		return false
	}
	if p.EffectiveTo != nil && !t.Before(*p.EffectiveTo) {
		return false
	}
	return true
}

// HasUsageAvailable checks whether promotion quota has not been exceeded.
func (p *Promotion) HasUsageAvailable() bool {
	if p.UsageLimit == nil {
		return true
	}
	return p.UsageCount < *p.UsageLimit
}

// PromotionVersion represents an immutable historical version snapshot of a promotion.
type PromotionVersion struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	PromotionID  uuid.UUID       `json:"promotion_id"`
	VersionNo    int             `json:"version_no"`
	RuleSnapshot json.RawMessage `json:"rule_snapshot"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	CreatedBy    *uuid.UUID      `json:"created_by,omitempty"`
	ApprovedAt   *time.Time      `json:"approved_at,omitempty"`
	ApprovedBy   *uuid.UUID      `json:"approved_by,omitempty"`
}

// CreateConditionRequest models input to define a condition.
type CreateConditionRequest struct {
	Type       string          `json:"condition_type"`
	Config     json.RawMessage `json:"condition_config"`
	SequenceNo int             `json:"sequence_no"`
}

// CreateRewardRequest models input to define a reward.
type CreateRewardRequest struct {
	Type       string          `json:"reward_type"`
	Config     json.RawMessage `json:"reward_config"`
	SequenceNo int             `json:"sequence_no"`
}

// CreatePromotionRequest models payload for creating a new draft promotion.
type CreatePromotionRequest struct {
	Code          string                   `json:"code"`
	Name          string                   `json:"name"`
	Description   string                   `json:"description,omitempty"`
	Channel       string                   `json:"channel,omitempty"`
	StackingMode  string                   `json:"stacking_mode,omitempty"`
	Priority      int                      `json:"priority"`
	EffectiveFrom time.Time                `json:"effective_from"`
	EffectiveTo   *time.Time               `json:"effective_to,omitempty"`
	ValidFrom     *time.Time               `json:"valid_from,omitempty"`
	ValidTo       *time.Time               `json:"valid_to,omitempty"`
	UsageLimit    *int                     `json:"usage_limit,omitempty"`
	Conditions    []CreateConditionRequest `json:"conditions,omitempty"`
	Rewards       []CreateRewardRequest    `json:"rewards,omitempty"`
}

// UpdatePromotionRequest models payload for updating an existing DRAFT promotion in-place.
type UpdatePromotionRequest struct {
	Name          *string                  `json:"name,omitempty"`
	Description   *string                  `json:"description,omitempty"`
	Channel       *string                  `json:"channel,omitempty"`
	StackingMode  *string                  `json:"stacking_mode,omitempty"`
	Priority      *int                     `json:"priority,omitempty"`
	EffectiveFrom *time.Time               `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time               `json:"effective_to,omitempty"`
	ValidFrom     *time.Time               `json:"valid_from,omitempty"`
	ValidTo       *time.Time               `json:"valid_to,omitempty"`
	UsageLimit    *int                     `json:"usage_limit,omitempty"`
	Conditions    []CreateConditionRequest `json:"conditions,omitempty"`
	Rewards       []CreateRewardRequest    `json:"rewards,omitempty"`
	Version       int64                    `json:"version"`
}

// TransitionRequest models action transition requests (submit, approve, reject, activate, pause).
type TransitionRequest struct {
	Reason  string `json:"reason,omitempty"`
	Comment string `json:"comment,omitempty"`
	Version int64  `json:"version"`
}

// EvaluateRequest models an evaluation request via Application Service.
type EvaluateRequest struct {
	StoreID         *uuid.UUID `json:"store_id,omitempty"`
	Channel         string     `json:"channel,omitempty"`
	CustomerTier    string     `json:"customer_tier,omitempty"`
	CustomerGroupID *uuid.UUID `json:"customer_group_id,omitempty"`
	EvaluationTime  *time.Time `json:"evaluation_time,omitempty"`
	Items           []CartItem `json:"items"`
}
