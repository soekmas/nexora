package promotion_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/idempotency"
	"example.com/nexora/backend/internal/platform/postgres"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
	"example.com/nexora/backend/internal/promotion"
)

// mockPromotionService implements promotion.Service for handler unit tests.
type mockPromotionService struct {
	evaluatePromotionsFn  func(ctx context.Context, req promotion.EvaluateRequest) (*promotion.EvaluationOutput, error)
	createPromotionFn     func(ctx context.Context, req promotion.CreatePromotionRequest) (*promotion.Promotion, error)
	getPromotionFn        func(ctx context.Context, id uuid.UUID) (*promotion.Promotion, error)
	getPromotionByCodeFn  func(ctx context.Context, code string) (*promotion.Promotion, error)
	updateDraftPromotionFn func(ctx context.Context, id uuid.UUID, req promotion.UpdatePromotionRequest) (*promotion.Promotion, error)
	listPromotionsFn      func(ctx context.Context, filter promotion.PromotionFilter) ([]*promotion.Promotion, int, error)
	createDraftVersionFn  func(ctx context.Context, id uuid.UUID, req promotion.CreatePromotionRequest) (*promotion.Promotion, error)
	submitPromotionFn     func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error)
	approvePromotionFn    func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error)
	rejectPromotionFn     func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error)
	activatePromotionFn   func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error)
	pausePromotionFn      func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error)
}

func (m *mockPromotionService) EvaluatePromotions(ctx context.Context, req promotion.EvaluateRequest) (*promotion.EvaluationOutput, error) {
	if m.evaluatePromotionsFn != nil {
		return m.evaluatePromotionsFn(ctx, req)
	}
	return nil, nil
}

func (m *mockPromotionService) CreatePromotion(ctx context.Context, req promotion.CreatePromotionRequest) (*promotion.Promotion, error) {
	if m.createPromotionFn != nil {
		return m.createPromotionFn(ctx, req)
	}
	return nil, nil
}

func (m *mockPromotionService) GetPromotion(ctx context.Context, id uuid.UUID) (*promotion.Promotion, error) {
	if m.getPromotionFn != nil {
		return m.getPromotionFn(ctx, id)
	}
	return nil, nil
}

func (m *mockPromotionService) GetPromotionByCode(ctx context.Context, code string) (*promotion.Promotion, error) {
	if m.getPromotionByCodeFn != nil {
		return m.getPromotionByCodeFn(ctx, code)
	}
	return nil, nil
}

func (m *mockPromotionService) UpdateDraftPromotion(ctx context.Context, id uuid.UUID, req promotion.UpdatePromotionRequest) (*promotion.Promotion, error) {
	if m.updateDraftPromotionFn != nil {
		return m.updateDraftPromotionFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPromotionService) ListPromotions(ctx context.Context, filter promotion.PromotionFilter) ([]*promotion.Promotion, int, error) {
	if m.listPromotionsFn != nil {
		return m.listPromotionsFn(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockPromotionService) CreateDraftVersion(ctx context.Context, id uuid.UUID, req promotion.CreatePromotionRequest) (*promotion.Promotion, error) {
	if m.createDraftVersionFn != nil {
		return m.createDraftVersionFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPromotionService) SubmitPromotion(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
	if m.submitPromotionFn != nil {
		return m.submitPromotionFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPromotionService) ApprovePromotion(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
	if m.approvePromotionFn != nil {
		return m.approvePromotionFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPromotionService) RejectPromotion(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
	if m.rejectPromotionFn != nil {
		return m.rejectPromotionFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPromotionService) ActivatePromotion(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
	if m.activatePromotionFn != nil {
		return m.activatePromotionFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPromotionService) PausePromotion(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
	if m.pausePromotionFn != nil {
		return m.pausePromotionFn(ctx, id, req)
	}
	return nil, nil
}

func setupTestRouter(handler *promotion.Handler) chi.Router {
	r := chi.NewRouter()
	r.Use(platformmw.RequestID)
	r.Mount("/api/v1/promotions", handler.Routes())
	r.Mount("/promotions", handler.CompatibilityRoutes())
	return r
}

func makeAuthRequest(method, url string, body any, principal *auth.AuthenticatedPrincipal, reqID string) *http.Request {
	var bodyBuf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&bodyBuf).Encode(body)
	}

	req := httptest.NewRequest(method, url, &bodyBuf)
	req.Header.Set("Content-Type", "application/json")
	if reqID != "" {
		req.Header.Set("X-Request-ID", reqID)
	}

	ctx := req.Context()
	if principal != nil {
		ctx = auth.WithPrincipal(ctx, principal)
		ctx = postgres.WithPrincipalContext(ctx, postgres.PrincipalContext{
			TenantID: principal.TenantID,
			UserID:   principal.UserID,
		})
	}
	return req.WithContext(ctx)
}

func TestPromotionHandler_AuthenticationAndPermissions(t *testing.T) {
	mockSvc := &mockPromotionService{}
	handler := promotion.NewHandler(mockSvc, nil, nil)
	router := setupTestRouter(handler)

	tenantID := uuid.New()
	userID := uuid.New()
	promoID := uuid.New()

	t.Run("Unauthenticated request returns 401 with canonical envelope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/promotions/evaluate", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "req_unauth_123")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errResp.Error.Code != "UNAUTHORIZED" {
			t.Errorf("expected UNAUTHORIZED, got %s", errResp.Error.Code)
		}
		if errResp.RequestID != "req_unauth_123" {
			t.Errorf("expected request_id req_unauth_123, got %s", errResp.RequestID)
		}
	})

	t.Run("Forbidden request lacking promotion.evaluate returns 403", func(t *testing.T) {
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{"unrelated.perm"},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/evaluate", promotion.EvaluateRequest{}, principal, "req_forbid_eval")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "FORBIDDEN" {
			t.Errorf("expected FORBIDDEN, got %s", errResp.Error.Code)
		}
	})

	t.Run("Forbidden request lacking promotion.create returns 403", func(t *testing.T) {
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionRead},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions", promotion.CreatePromotionRequest{
			Code: "P1", Name: "Promo 1",
		}, principal, "req_forbid_create")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("Invalid path UUID returns 422 validation error", func(t *testing.T) {
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionRead},
		}
		req := makeAuthRequest(http.MethodGet, "/api/v1/promotions/not-a-valid-uuid", nil, principal, "req_bad_uuid")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("expected VALIDATION_ERROR, got %s", errResp.Error.Code)
		}
	})

	t.Run("Malformed JSON payload returns 400", func(t *testing.T) {
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionCreate},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/promotions", bytes.NewBufferString("{invalid-json"))
		req.Header.Set("Content-Type", "application/json")
		ctx := auth.WithPrincipal(req.Context(), principal)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Compatibility routes mounted at /promotions operate identically", func(t *testing.T) {
		mockSvc.getPromotionFn = func(ctx context.Context, id uuid.UUID) (*promotion.Promotion, error) {
			return &promotion.Promotion{
				ID:   id,
				Code: "COMPAT-1",
				Name: "Compat Promo",
			}, nil
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionRead},
		}
		req := makeAuthRequest(http.MethodGet, "/promotions/"+promoID.String(), nil, principal, "req_compat")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on /promotions/{id}, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestPromotionHandler_LifecycleAndSoD(t *testing.T) {
	mockSvc := &mockPromotionService{}
	handler := promotion.NewHandler(mockSvc, nil, nil)
	router := setupTestRouter(handler)

	tenantID := uuid.New()
	creatorID := uuid.New()
	approverID := uuid.New()
	promoID := uuid.New()

	t.Run("Submit promotion returns 200 OK", func(t *testing.T) {
		mockSvc.submitPromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return &promotion.Promotion{
				ID:     id,
				Status: promotion.StatusPendingApproval,
			}, nil
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      creatorID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionSubmit},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/submit", promotion.TransitionRequest{Version: 1}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Approve promotion with SoD violation returns 403 SOD_VIOLATION", func(t *testing.T) {
		mockSvc.approvePromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return nil, promotion.ErrSoDViolation
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      creatorID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionApprove},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/approve", promotion.TransitionRequest{Version: 2}, principal, "req_sod")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "SOD_VIOLATION" {
			t.Errorf("expected SOD_VIOLATION, got %s", errResp.Error.Code)
		}
	})

	t.Run("Approve promotion with distinct approver returns 200 OK", func(t *testing.T) {
		mockSvc.approvePromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return &promotion.Promotion{
				ID:         id,
				Status:     promotion.StatusActive,
				ApprovedBy: &approverID,
			}, nil
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      approverID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionApprove},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/approve", promotion.TransitionRequest{Version: 2, Comment: "Looks good"}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Reject promotion returns 200 OK", func(t *testing.T) {
		mockSvc.rejectPromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return &promotion.Promotion{
				ID:     id,
				Status: promotion.StatusRejected,
			}, nil
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      approverID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionApprove},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/reject", promotion.TransitionRequest{Version: 2, Reason: "Budget exceeded"}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("Pause and Activate promotion return 200 OK", func(t *testing.T) {
		mockSvc.pausePromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return &promotion.Promotion{ID: id, Status: promotion.StatusPaused}, nil
		}
		mockSvc.activatePromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return &promotion.Promotion{ID: id, Status: promotion.StatusActive}, nil
		}

		principalPause := &auth.AuthenticatedPrincipal{
			UserID:      approverID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionPause},
		}
		reqPause := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/pause", promotion.TransitionRequest{Version: 3}, principalPause, "")
		recPause := httptest.NewRecorder()
		router.ServeHTTP(recPause, reqPause)
		if recPause.Code != http.StatusOK {
			t.Fatalf("expected 200 on pause, got %d", recPause.Code)
		}

		principalAct := &auth.AuthenticatedPrincipal{
			UserID:      approverID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionActivate},
		}
		reqAct := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/activate", promotion.TransitionRequest{Version: 4}, principalAct, "")
		recAct := httptest.NewRecorder()
		router.ServeHTTP(recAct, reqAct)
		if recAct.Code != http.StatusOK {
			t.Fatalf("expected 200 on activate, got %d", recAct.Code)
		}

		// OpenAPI compatibility: /deactivate endpoint
		reqDeact := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/deactivate", promotion.TransitionRequest{Version: 5}, principalPause, "")
		recDeact := httptest.NewRecorder()
		router.ServeHTTP(recDeact, reqDeact)
		if recDeact.Code != http.StatusOK {
			t.Fatalf("expected 200 on deactivate, got %d", recDeact.Code)
		}
	})

	t.Run("Invalid state transition returns 409 INVALID_STATE_TRANSITION", func(t *testing.T) {
		mockSvc.approvePromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.TransitionRequest) (*promotion.Promotion, error) {
			return nil, &promotion.StateTransitionError{From: promotion.StatusDraft, To: promotion.StatusApproved}
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      approverID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionApprove},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/"+promoID.String()+"/approve", promotion.TransitionRequest{Version: 1}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "INVALID_STATE_TRANSITION" {
			t.Errorf("expected INVALID_STATE_TRANSITION, got %s", errResp.Error.Code)
		}
	})
}

func TestPromotionHandler_CRUDAndConcurrency(t *testing.T) {
	mockSvc := &mockPromotionService{}
	handler := promotion.NewHandler(mockSvc, nil, nil)
	router := setupTestRouter(handler)

	tenantID := uuid.New()
	userID := uuid.New()
	promoID := uuid.New()

	t.Run("Create promotion returns 201 Created", func(t *testing.T) {
		mockSvc.createPromotionFn = func(ctx context.Context, req promotion.CreatePromotionRequest) (*promotion.Promotion, error) {
			return &promotion.Promotion{
				ID:      promoID,
				Code:    req.Code,
				Name:    req.Name,
				Status:  promotion.StatusDraft,
				Version: 1,
			}, nil
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionCreate},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions", promotion.CreatePromotionRequest{
			Code: "PROMO-NEW", Name: "New Promo", Priority: 10,
		}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Duplicate code returns 409 PROMOTION_CODE_EXISTS", func(t *testing.T) {
		mockSvc.createPromotionFn = func(ctx context.Context, req promotion.CreatePromotionRequest) (*promotion.Promotion, error) {
			return nil, promotion.ErrPromotionCodeExists
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionCreate},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/promotions", promotion.CreatePromotionRequest{
			Code: "DUPLICATE", Name: "Duplicate",
		}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "PROMOTION_CODE_EXISTS" {
			t.Errorf("expected PROMOTION_CODE_EXISTS, got %s", errResp.Error.Code)
		}
	})

	t.Run("Update approved promotion returns 409 PROMOTION_NOT_EDITABLE", func(t *testing.T) {
		mockSvc.updateDraftPromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.UpdatePromotionRequest) (*promotion.Promotion, error) {
			return nil, promotion.ErrPromotionNotEditable
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionUpdate},
		}
		req := makeAuthRequest(http.MethodPut, "/api/v1/promotions/"+promoID.String(), promotion.UpdatePromotionRequest{
			Version: 1,
		}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "PROMOTION_NOT_EDITABLE" {
			t.Errorf("expected PROMOTION_NOT_EDITABLE, got %s", errResp.Error.Code)
		}
	})

	t.Run("Optimistic locking conflict returns 409 VERSION_CONFLICT", func(t *testing.T) {
		mockSvc.updateDraftPromotionFn = func(ctx context.Context, id uuid.UUID, req promotion.UpdatePromotionRequest) (*promotion.Promotion, error) {
			return nil, promotion.ErrVersionConflict
		}
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{promotion.PermPromotionUpdate},
		}
		req := makeAuthRequest(http.MethodPut, "/api/v1/promotions/"+promoID.String(), promotion.UpdatePromotionRequest{
			Version: 1,
		}, principal, "")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}
		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "VERSION_CONFLICT" {
			t.Errorf("expected VERSION_CONFLICT, got %s", errResp.Error.Code)
		}
	})
}

// inMemoryIdemStore implements idempotency.Store for HTTP handler tests.
type inMemoryIdemStore struct {
	records map[string]*idempotency.Record
}

func newInMemoryIdemStore() *inMemoryIdemStore {
	return &inMemoryIdemStore{records: make(map[string]*idempotency.Record)}
}

func (s *inMemoryIdemStore) compositeKey(tenantID uuid.UUID, key, operation string) string {
	return tenantID.String() + ":" + key + ":" + operation
}

func (s *inMemoryIdemStore) AcquireOrGet(
	_ context.Context,
	tenantID uuid.UUID,
	key, operation, requestHash string,
	ttl time.Duration,
) (*idempotency.AcquireResult, error) {
	cKey := s.compositeKey(tenantID, key, operation)
	existing, ok := s.records[cKey]
	if !ok {
		exp := time.Now().Add(ttl)
		rec := &idempotency.Record{
			ID:          uuid.New(),
			TenantID:    tenantID,
			Key:         key,
			Operation:   operation,
			RequestHash: requestHash,
			CreatedAt:   time.Now(),
			ExpiresAt:   &exp,
		}
		s.records[cKey] = rec
		return &idempotency.AcquireResult{
			Acquired: true,
			Record:   rec,
		}, nil
	}

	if existing.RequestHash != requestHash {
		return &idempotency.AcquireResult{
			Acquired:     false,
			Record:       existing,
			HashMismatch: true,
		}, nil
	}

	return &idempotency.AcquireResult{
		Acquired:     false,
		Record:       existing,
		HashMismatch: false,
	}, nil
}

func (s *inMemoryIdemStore) Complete(
	_ context.Context,
	recordID, tenantID uuid.UUID,
	status int,
	responseBody []byte,
) error {
	for _, rec := range s.records {
		if rec.ID == recordID && rec.TenantID == tenantID {
			if rec.ResponseStatus != nil {
				return idempotency.ErrReservationLost
			}
			rec.ResponseStatus = &status
			rec.ResponseBody = responseBody
			return nil
		}
	}
	return idempotency.ErrReservationLost
}

func (s *inMemoryIdemStore) Release(_ context.Context, recordID, tenantID uuid.UUID) error {
	for k, rec := range s.records {
		if rec.ID == recordID && rec.TenantID == tenantID && rec.ResponseStatus == nil {
			delete(s.records, k)
			return nil
		}
	}
	return nil
}

func (s *inMemoryIdemStore) Get(_ context.Context, tenantID uuid.UUID, key, operation string) (*idempotency.Record, error) {
	cKey := s.compositeKey(tenantID, key, operation)
	rec, ok := s.records[cKey]
	if !ok {
		return nil, idempotency.ErrRecordNotFound
	}
	return rec, nil
}

func (s *inMemoryIdemStore) Delete(_ context.Context, tenantID uuid.UUID, key, operation string) error {
	cKey := s.compositeKey(tenantID, key, operation)
	delete(s.records, cKey)
	return nil
}

func (s *inMemoryIdemStore) CleanupExpired(_ context.Context, batchSize int) (int64, error) {
	return 0, nil
}

func (s *inMemoryIdemStore) FindStuckReservations(_ context.Context, tenantID uuid.UUID, staleThreshold time.Duration, limit int) ([]*idempotency.Record, error) {
	return nil, nil
}

func (s *inMemoryIdemStore) MarkStuckAsFailed(_ context.Context, tenantID, recordID uuid.UUID, staleThreshold time.Duration, failStatus int, failBody []byte) error {
	return nil
}

func TestPromotionHandler_IdempotencyCommandEndpoints(t *testing.T) {
	mockSvc := &mockPromotionService{}
	idemStore := newInMemoryIdemStore()
	handler := promotion.NewHandler(mockSvc, idemStore, nil)
	router := setupTestRouter(handler)

	tenantID := uuid.New()
	userID := uuid.New()
	promoID := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		UserID:      userID,
		TenantID:    tenantID,
		Permissions: []string{promotion.PermPromotionCreate},
	}

	callCount := 0
	mockSvc.createPromotionFn = func(ctx context.Context, req promotion.CreatePromotionRequest) (*promotion.Promotion, error) {
		callCount++
		return &promotion.Promotion{
			ID:      promoID,
			Code:    req.Code,
			Name:    req.Name,
			Status:  promotion.StatusDraft,
			Version: 1,
		}, nil
	}

	idemKey := "idem-promo-key-" + uuid.New().String()
	payload1 := promotion.CreatePromotionRequest{Code: "IDEM-1", Name: "Idem Promo"}
	body1, _ := json.Marshal(payload1)

	// 1. First execution -> succeeds with 201 Created
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/promotions", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", idemKey)
	ctx1 := auth.WithPrincipal(req1.Context(), principal)
	ctx1 = postgres.WithPrincipalContext(ctx1, postgres.PrincipalContext{TenantID: tenantID, UserID: userID})
	req1 = req1.WithContext(ctx1)

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first execution failed: code %d, body: %s", rec1.Code, rec1.Body.String())
	}
	if callCount != 1 {
		t.Fatalf("expected service called 1 time, got %d", callCount)
	}

	// 2. Replay with identical key and identical payload -> must replay cached 201 without invoking service
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/promotions", bytes.NewReader(body1))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", idemKey)
	ctx2 := auth.WithPrincipal(req2.Context(), principal)
	ctx2 = postgres.WithPrincipalContext(ctx2, postgres.PrincipalContext{TenantID: tenantID, UserID: userID})
	req2 = req2.WithContext(ctx2)

	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("replay failed: code %d, body: %s", rec2.Code, rec2.Body.String())
	}
	if callCount != 1 {
		t.Errorf("service was called on replay! expected callCount 1, got %d", callCount)
	}

	// 3. Replay with identical key but DIFFERENT payload -> 409 IDEMPOTENCY_KEY_REUSED
	payloadDiff := promotion.CreatePromotionRequest{Code: "IDEM-DIFFERENT", Name: "Different Payload"}
	bodyDiff, _ := json.Marshal(payloadDiff)
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/promotions", bytes.NewReader(bodyDiff))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Idempotency-Key", idemKey)
	ctx3 := auth.WithPrincipal(req3.Context(), principal)
	ctx3 = postgres.WithPrincipalContext(ctx3, postgres.PrincipalContext{TenantID: tenantID, UserID: userID})
	req3 = req3.WithContext(ctx3)

	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on key reuse, got %d: %s", rec3.Code, rec3.Body.String())
	}
	var errResp apperr.ErrorResponse
	_ = json.Unmarshal(rec3.Body.Bytes(), &errResp)
	if errResp.Error.Code != "IDEMPOTENCY_KEY_REUSED" {
		t.Errorf("expected IDEMPOTENCY_KEY_REUSED, got %s", errResp.Error.Code)
	}
}

func TestPromotionHandler_EvaluatePromotions_EndToEnd(t *testing.T) {
	mockSvc := &mockPromotionService{}
	handler := promotion.NewHandler(mockSvc, nil, nil)
	router := setupTestRouter(handler)

	tenantID := uuid.New()
	userID := uuid.New()
	prodCoffee := uuid.New()
	prodCookie := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		UserID:      userID,
		TenantID:    tenantID,
		Permissions: []string{promotion.PermPromotionEvaluate},
	}

	evalOutput := &promotion.EvaluationOutput{
		Subtotal:      70.00,
		DiscountTotal: 17.00,
		NetTotal:      53.00,
		AppliedPromotions: []promotion.AppliedPromotion{
			{
				PromotionID:    uuid.New(),
				PromotionCode:  "BUY2-GET1",
				PromotionName:  "Buy 2 Coffee Get 1 Free Cookie",
				DiscountAmount: 10.00,
				RewardType:     promotion.RewardTypeBuyXGetY,
			},
			{
				PromotionID:    uuid.New(),
				PromotionCode:  "10PCT-OFF",
				PromotionName:  "10% Off",
				DiscountAmount: 7.00,
				RewardType:     promotion.RewardTypePercentageDiscount,
			},
		},
		BonusItems: []promotion.BonusItem{
			{
				ProductID:      prodCookie,
				ItemName:       "Choco Cookie",
				FreeQuantity:   2.0,
				DiscountAmount: 10.00,
			},
		},
	}

	mockSvc.evaluatePromotionsFn = func(ctx context.Context, req promotion.EvaluateRequest) (*promotion.EvaluationOutput, error) {
		if len(req.Items) != 2 {
			return nil, errors.New("expected 2 items")
		}
		return evalOutput, nil
	}

	evalReq := promotion.EvaluateRequest{
		Channel: "POS",
		Items: []promotion.CartItem{
			{ProductID: prodCoffee, Name: "Coffee", Quantity: 4, UnitPrice: 15.00},
			{ProductID: prodCookie, Name: "Cookie", Quantity: 2, UnitPrice: 5.00},
		},
	}

	// 1. Evaluate on canonical /api/v1/promotions/evaluate
	req := makeAuthRequest(http.MethodPost, "/api/v1/promotions/evaluate", evalReq, principal, "req_eval_success")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var res promotion.EvaluationOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal evaluation output: %v", err)
	}

	if res.Subtotal != 70.00 || res.DiscountTotal != 17.00 || res.NetTotal != 53.00 {
		t.Errorf("amounts mismatch: subtotal=%v, discount=%v, net=%v", res.Subtotal, res.DiscountTotal, res.NetTotal)
	}
	if len(res.AppliedPromotions) != 2 {
		t.Errorf("expected 2 applied promotions, got %d", len(res.AppliedPromotions))
	}
	if len(res.BonusItems) != 1 || res.BonusItems[0].FreeQuantity != 2 {
		t.Errorf("expected 2 bonus cookies, got %+v", res.BonusItems)
	}

	// 2. Evaluate on compatibility /promotions/evaluate
	reqCompat := makeAuthRequest(http.MethodPost, "/promotions/evaluate", evalReq, principal, "req_eval_compat")
	recCompat := httptest.NewRecorder()
	router.ServeHTTP(recCompat, reqCompat)

	if recCompat.Code != http.StatusOK {
		t.Fatalf("expected 200 on /promotions/evaluate, got %d: %s", recCompat.Code, recCompat.Body.String())
	}
}
