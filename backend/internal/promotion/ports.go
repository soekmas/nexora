package promotion

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PromotionFilter defines query parameters for listing promotions.
type PromotionFilter struct {
	Channel  *string
	Status   *string
	Search   *string
	Page     int
	PageSize int
}

// Store defines persistence operations for promotion aggregates and active evaluations.
type Store interface {
	// Promotion master & aggregate operations
	CreatePromotion(ctx context.Context, p *Promotion) error
	GetPromotionByID(ctx context.Context, id uuid.UUID) (*Promotion, error)
	GetPromotionByIDForUpdate(ctx context.Context, id uuid.UUID) (*Promotion, error)
	GetPromotionByCode(ctx context.Context, code string) (*Promotion, error)
	UpdatePromotion(ctx context.Context, p *Promotion, expectedVersion int64) error
	ListPromotions(ctx context.Context, filter PromotionFilter) ([]*Promotion, int, error)

	// Active candidate promotions query for evaluation
	GetActivePromotions(ctx context.Context, channel string, refTime time.Time) ([]Promotion, error)

	// Versioning operations
	CreatePromotionVersion(ctx context.Context, v *PromotionVersion) error
	GetLatestPromotionVersion(ctx context.Context, promotionID uuid.UUID) (*PromotionVersion, error)
}
