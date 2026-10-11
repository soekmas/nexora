package auth

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
)

var (
	// ErrSegregationOfDutiesViolation is returned when an actor attempts to approve an entity they created.
	ErrSegregationOfDutiesViolation = errors.New("actor cannot approve entity created by the same actor")
)

// ValidateSegregationOfDuties enforces Segregation of Duties (SoD) by ensuring
// that the actor performing an approval/settlement is not the creator/initiator of the record.
func ValidateSegregationOfDuties(actorID, creatorID uuid.UUID) error {
	if actorID != uuid.Nil && creatorID != uuid.Nil && actorID == creatorID {
		return ErrSegregationOfDutiesViolation
	}
	return nil
}

// EnforceSegregationOfDuties enforces Segregation of Duties (SoD) by ensuring
// that the actor performing a sensitive operation (e.g. approval, settlement, void)
// is not the creator/initiator of the record. Returns a canonical HTTP 403 Forbidden
// error with structured violation details if the actor matches the creator.
func EnforceSegregationOfDuties(actorID, creatorID uuid.UUID, action string) *apperr.AppError {
	if err := ValidateSegregationOfDuties(actorID, creatorID); err != nil {
		appErr := apperr.NewForbiddenError(fmt.Sprintf("Segregation of duties violation: actor cannot perform %s on a record they created", action))
		appErr.Detail.Details = map[string]any{
			"code":       "SEGREGATION_OF_DUTIES_VIOLATION",
			"actor_id":   actorID.String(),
			"creator_id": creatorID.String(),
			"action":     action,
		}
		return appErr
	}
	return nil
}

// EvaluatePermission checks whether the principal possesses the requested permission,
// returning a canonical *apperr.AppError with HTTP 403 status if denied.
func EvaluatePermission(principal *AuthenticatedPrincipal, permission string) *apperr.AppError {
	if principal == nil {
		return apperr.NewUnauthorizedError("Authentication required")
	}

	if !principal.HasPermission(permission) {
		err := apperr.NewForbiddenError(fmt.Sprintf("Missing required permission %q", permission))
		err.Detail.Details = map[string]any{
			"required_permission": permission,
		}
		return err
	}

	return nil
}

// EvaluateScope checks whether the principal is authorized for the given organization scope,
// returning a canonical *apperr.AppError with HTTP 403 status if denied.
func EvaluateScope(principal *AuthenticatedPrincipal, scopeType string, targetID uuid.UUID) *apperr.AppError {
	if principal == nil {
		return apperr.NewUnauthorizedError("Authentication required")
	}

	if targetID == uuid.Nil {
		return apperr.NewBadRequestError("Target scope identifier is required")
	}

	if !principal.HasScope(scopeType, targetID) {
		return apperr.NewScopeAccessDeniedError(
			fmt.Sprintf("Access denied to %s %s", scopeType, targetID),
			map[string]any{
				"scope_type": scopeType,
				"target_id":  targetID.String(),
			},
		)
	}

	return nil
}
