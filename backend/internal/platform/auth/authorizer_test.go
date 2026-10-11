package auth_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/auth"
)

func TestValidateSegregationOfDuties(t *testing.T) {
	actorID := uuid.New()
	creatorID := uuid.New()

	t.Run("same actor and creator violates SoD", func(t *testing.T) {
		err := auth.ValidateSegregationOfDuties(actorID, actorID)
		if err == nil {
			t.Fatal("expected SoD violation error when actor == creator")
		}
		if err != auth.ErrSegregationOfDutiesViolation {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("distinct actor and creator passes SoD", func(t *testing.T) {
		err := auth.ValidateSegregationOfDuties(actorID, creatorID)
		if err != nil {
			t.Fatalf("unexpected SoD violation error when actor != creator: %v", err)
		}
	})

	t.Run("nil uuid passes SoD without false positives", func(t *testing.T) {
		if err := auth.ValidateSegregationOfDuties(uuid.Nil, creatorID); err != nil {
			t.Errorf("unexpected error for nil actor: %v", err)
		}
	})
}

func TestEvaluatePermission(t *testing.T) {
	principal := &auth.AuthenticatedPrincipal{
		UserID:      uuid.New(),
		TenantID:    uuid.New(),
		Permissions: []string{"pos:order:create", "pos:order:read"},
	}

	t.Run("grants access when permission present", func(t *testing.T) {
		err := auth.EvaluatePermission(principal, "pos:order:create")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("denies access with 403 when permission absent", func(t *testing.T) {
		err := auth.EvaluatePermission(principal, "pos:order:void")
		if err == nil {
			t.Fatal("expected error for unassigned permission")
		}
		if err.HTTPStatus != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", err.HTTPStatus)
		}
		if err.Detail.Code != "FORBIDDEN" {
			t.Errorf("expected FORBIDDEN code, got %s", err.Detail.Code)
		}
	})

	t.Run("returns 401 when principal is nil", func(t *testing.T) {
		err := auth.EvaluatePermission(nil, "pos:order:create")
		if err == nil {
			t.Fatal("expected error for nil principal")
		}
		if err.HTTPStatus != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", err.HTTPStatus)
		}
	})
}

func TestEvaluateScope(t *testing.T) {
	authorizedStore := uuid.New()
	foreignStore := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		UserID:   uuid.New(),
		TenantID: uuid.New(),
		Scopes: []auth.UserScope{
			{
				Type:    "STORE",
				StoreID: &authorizedStore,
			},
		},
	}

	t.Run("grants access to authorized scope", func(t *testing.T) {
		err := auth.EvaluateScope(principal, "STORE", authorizedStore)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("denies access to foreign scope with 403 SCOPE_ACCESS_DENIED", func(t *testing.T) {
		err := auth.EvaluateScope(principal, "STORE", foreignStore)
		if err == nil {
			t.Fatal("expected error for foreign scope")
		}
		if err.HTTPStatus != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", err.HTTPStatus)
		}
		if err.Detail.Code != "SCOPE_ACCESS_DENIED" {
			t.Errorf("expected SCOPE_ACCESS_DENIED, got %s", err.Detail.Code)
		}
	})

	t.Run("returns 400 when target id is nil", func(t *testing.T) {
		err := auth.EvaluateScope(principal, "STORE", uuid.Nil)
		if err == nil {
			t.Fatal("expected error for nil target id")
		}
		if err.HTTPStatus != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", err.HTTPStatus)
		}
	})
}

func TestEnforceSegregationOfDuties(t *testing.T) {
	actorID := uuid.New()
	creatorID := uuid.New()

	t.Run("same actor and creator returns canonical 403 AppError with structured details", func(t *testing.T) {
		appErr := auth.EnforceSegregationOfDuties(actorID, actorID, "PROMOTION_APPROVE")
		if appErr == nil {
			t.Fatal("expected SoD violation error when actor == creator")
		}
		if appErr.HTTPStatus != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", appErr.HTTPStatus)
		}
		if appErr.Detail.Code != "FORBIDDEN" {
			t.Errorf("expected code FORBIDDEN, got %s", appErr.Detail.Code)
		}
		if appErr.Detail.Details["code"] != "SEGREGATION_OF_DUTIES_VIOLATION" {
			t.Errorf("expected detail code SEGREGATION_OF_DUTIES_VIOLATION, got %v", appErr.Detail.Details["code"])
		}
		if appErr.Detail.Details["action"] != "PROMOTION_APPROVE" {
			t.Errorf("expected detail action PROMOTION_APPROVE, got %v", appErr.Detail.Details["action"])
		}
	})

	t.Run("distinct actor and creator passes enforcement", func(t *testing.T) {
		appErr := auth.EnforceSegregationOfDuties(actorID, creatorID, "PROMOTION_APPROVE")
		if appErr != nil {
			t.Fatalf("expected nil error when actor != creator, got %v", appErr)
		}
	})
}
