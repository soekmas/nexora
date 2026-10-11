package promotion

import (
	"errors"
	"fmt"
)

var (
	ErrPromotionNotFound      = errors.New("promotion not found")
	ErrPromotionCodeExists    = errors.New("promotion code already exists for this tenant")
	ErrInvalidStateTransition = errors.New("invalid promotion state transition")
	ErrVersionConflict        = errors.New("resource has been modified concurrently (version conflict)")
	ErrPromotionNotEditable   = errors.New("only promotions in DRAFT status can be modified in-place; approved promotions require a new draft version")
	ErrReasonRequired         = errors.New("audit reason is required for this operation")
	ErrSoDViolation           = errors.New("segregation of duties violation: creator cannot approve own promotion")
	ErrInvalidCondition       = errors.New("invalid promotion condition configuration")
	ErrInvalidReward          = errors.New("invalid promotion reward configuration")
	ErrUsageLimitExceeded     = errors.New("promotion usage limit has been reached")
)

// StateTransitionError represents an invalid promotion lifecycle state transition.
type StateTransitionError struct {
	From string
	To   string
}

func (e *StateTransitionError) Error() string {
	return fmt.Sprintf("cannot transition promotion from status %s to %s", e.From, e.To)
}

func (e *StateTransitionError) Is(target error) bool {
	return target == ErrInvalidStateTransition
}
