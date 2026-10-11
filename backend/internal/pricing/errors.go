// Package pricing defines domain error types for pricing operations.
package pricing

import (
	"errors"
	"fmt"
)

var (
	ErrPriceNotFound          = errors.New("one or more cart items have no valid active price rule")
	ErrPriceListNotFound      = errors.New("no eligible default price list found")
	ErrCurrencyMismatch       = errors.New("requested currency is incompatible with the price list")
	ErrInvalidStateTransition = errors.New("invalid price rule state transition")
	ErrVersionConflict        = errors.New("resource has been modified concurrently (version conflict)")
	ErrPriceListArchived      = errors.New("price list is archived and cannot be updated")
	ErrReasonRequired         = errors.New("audit reason is required for this operation")
	ErrVariantNotFound        = errors.New("variant not found")
	ErrVariantProductMismatch = errors.New("variant does not belong to specified product")
	ErrStoreNotFound          = errors.New("store not found")
	ErrStoreInactive          = errors.New("store is inactive")
	ErrPriceListNotFound404   = errors.New("price list not found")
	ErrPriceRuleNotFound404   = errors.New("price rule not found")
	ErrDuplicateDefaultList   = errors.New("an active default price list already exists for this currency and channel")
	ErrDefaultCompanyNotNull  = errors.New("default price list cannot have a company_id")
	ErrRuleNotEditable        = errors.New("only rules in DRAFT status can be edited")
	ErrUnauthorizedPriceList   = errors.New("not authorized to access price list")
)

// StateTransitionError represents an illegal transition.
type StateTransitionError struct {
	From string
	To   string
}

func (e *StateTransitionError) Error() string {
	return fmt.Sprintf("cannot transition price rule from status %s to %s", e.From, e.To)
}

func (e *StateTransitionError) Is(target error) bool {
	return target == ErrInvalidStateTransition
}

// MissingPriceError carries the unpriced items details for 422 error serialization.
type MissingPriceError struct {
	UnpricedItems []UnpricedItemDetail
}

func (e *MissingPriceError) Error() string {
	return "one or more cart items have no valid active price rule"
}

func (e *MissingPriceError) Is(target error) bool {
	return target == ErrPriceNotFound
}

