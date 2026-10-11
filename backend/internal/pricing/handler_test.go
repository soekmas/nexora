package pricing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/postgres"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
	"example.com/nexora/backend/internal/pricing"
)

// mockPricingService implements pricing.Service for handler tests.
type mockPricingService struct {
	calculateQuoteFn func(ctx context.Context, req pricing.PriceQuoteRequest) (*pricing.PriceQuote, error)
	createPriceListFn func(ctx context.Context, req pricing.CreatePriceListRequest) (*pricing.PriceList, error)
	getPriceListFn    func(ctx context.Context, id uuid.UUID) (*pricing.PriceList, error)
	updatePriceListFn func(ctx context.Context, id uuid.UUID, req pricing.UpdatePriceListRequest) (*pricing.PriceList, error)
	listPriceListsFn  func(ctx context.Context, filter pricing.PriceListFilter) ([]*pricing.PriceList, int, error)
	createPriceRuleFn func(ctx context.Context, req pricing.CreatePriceRuleRequest) (*pricing.PriceRule, error)
	getPriceRuleFn    func(ctx context.Context, id uuid.UUID) (*pricing.PriceRule, error)
	updatePriceRuleFn func(ctx context.Context, id uuid.UUID, req pricing.UpdatePriceRuleRequest) (*pricing.PriceRule, error)
	listPriceRulesFn  func(ctx context.Context, filter pricing.PriceRuleFilter) ([]*pricing.PriceRule, int, error)
	submitPriceRuleFn func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error)
	approvePriceRuleFn func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error)
	rejectPriceRuleFn  func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error)
	activatePriceRuleFn func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error)
	deactivatePriceRuleFn func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error)
}

func (m *mockPricingService) CalculateQuote(ctx context.Context, req pricing.PriceQuoteRequest) (*pricing.PriceQuote, error) {
	if m.calculateQuoteFn != nil {
		return m.calculateQuoteFn(ctx, req)
	}
	return nil, nil
}

func (m *mockPricingService) CreatePriceList(ctx context.Context, req pricing.CreatePriceListRequest) (*pricing.PriceList, error) {
	if m.createPriceListFn != nil {
		return m.createPriceListFn(ctx, req)
	}
	return nil, nil
}

func (m *mockPricingService) GetPriceList(ctx context.Context, id uuid.UUID) (*pricing.PriceList, error) {
	if m.getPriceListFn != nil {
		return m.getPriceListFn(ctx, id)
	}
	return nil, nil
}

func (m *mockPricingService) UpdatePriceList(ctx context.Context, id uuid.UUID, req pricing.UpdatePriceListRequest) (*pricing.PriceList, error) {
	if m.updatePriceListFn != nil {
		return m.updatePriceListFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPricingService) ListPriceLists(ctx context.Context, filter pricing.PriceListFilter) ([]*pricing.PriceList, int, error) {
	if m.listPriceListsFn != nil {
		return m.listPriceListsFn(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockPricingService) CreatePriceRule(ctx context.Context, req pricing.CreatePriceRuleRequest) (*pricing.PriceRule, error) {
	if m.createPriceRuleFn != nil {
		return m.createPriceRuleFn(ctx, req)
	}
	return nil, nil
}

func (m *mockPricingService) GetPriceRule(ctx context.Context, id uuid.UUID) (*pricing.PriceRule, error) {
	if m.getPriceRuleFn != nil {
		return m.getPriceRuleFn(ctx, id)
	}
	return nil, nil
}

func (m *mockPricingService) UpdatePriceRule(ctx context.Context, id uuid.UUID, req pricing.UpdatePriceRuleRequest) (*pricing.PriceRule, error) {
	if m.updatePriceRuleFn != nil {
		return m.updatePriceRuleFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPricingService) ListPriceRules(ctx context.Context, filter pricing.PriceRuleFilter) ([]*pricing.PriceRule, int, error) {
	if m.listPriceRulesFn != nil {
		return m.listPriceRulesFn(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockPricingService) SubmitPriceRule(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
	if m.submitPriceRuleFn != nil {
		return m.submitPriceRuleFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPricingService) ApprovePriceRule(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
	if m.approvePriceRuleFn != nil {
		return m.approvePriceRuleFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPricingService) RejectPriceRule(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
	if m.rejectPriceRuleFn != nil {
		return m.rejectPriceRuleFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPricingService) ActivatePriceRule(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
	if m.activatePriceRuleFn != nil {
		return m.activatePriceRuleFn(ctx, id, req)
	}
	return nil, nil
}

func (m *mockPricingService) DeactivatePriceRule(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
	if m.deactivatePriceRuleFn != nil {
		return m.deactivatePriceRuleFn(ctx, id, req)
	}
	return nil, nil
}

func setupTestRouter(handler *pricing.Handler) chi.Router {
	r := chi.NewRouter()
	r.Use(platformmw.RequestID)
	r.Mount("/api/v1/pricing", handler.Routes())
	r.Mount("/pricing", handler.CompatibilityRoutes())
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

func TestPricingHandler_AuthenticationAndPermissions(t *testing.T) {
	mockSvc := &mockPricingService{}
	handler := pricing.NewHandler(mockSvc, nil)
	router := setupTestRouter(handler)

	tenantID := uuid.New()
	userID := uuid.New()

	t.Run("Unauthenticated request returns 401 with canonical envelope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pricing/quote", bytes.NewBufferString("{}"))
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

	t.Run("Forbidden request lacking price.read returns 403", func(t *testing.T) {
		principal := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{"unrelated.perm"},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/quote", pricing.PriceQuoteRequest{}, principal, "req_forbid_456")

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
		if errResp.RequestID != "req_forbid_456" {
			t.Errorf("expected req_forbid_456, got %s", errResp.RequestID)
		}
	})
}

func TestPricingHandler_QuoteEndpoints(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	storeID := uuid.New()
	variantID := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		UserID:      userID,
		TenantID:    tenantID,
		Permissions: []string{pricing.PermPriceRead},
	}

	t.Run("Successful quote on both /api/v1/pricing/quote and /pricing/quote", func(t *testing.T) {
		mockSvc := &mockPricingService{
			calculateQuoteFn: func(ctx context.Context, req pricing.PriceQuoteRequest) (*pricing.PriceQuote, error) {
				return &pricing.PriceQuote{
					Items: []pricing.PriceQuoteItem{
						{
							VariantID: variantID,
							Quantity:  2,
							UnitPrice: 50000.0,
							Discount:  0.0000,
							NetAmount: 100000.0,
						},
					},
					Subtotal:      100000.0,
					DiscountTotal: 0.0000,
					TaxTotal:      0.0000,
					GrandTotal:    100000.0,
					Currency:      "IDR",
				}, nil
			},
		}
		handler := pricing.NewHandler(mockSvc, nil)
		router := setupTestRouter(handler)

		quoteReq := pricing.PriceQuoteRequest{
			StoreID:  storeID,
			Currency: "IDR",
			Channel:  "POS",
			Items: []pricing.CartItem{
				{VariantID: variantID, Quantity: 2},
			},
		}

		// Test canonical /api/v1/pricing/quote
		reqV1 := makeAuthRequest(http.MethodPost, "/api/v1/pricing/quote", quoteReq, principal, "req_v1")
		recV1 := httptest.NewRecorder()
		router.ServeHTTP(recV1, reqV1)

		if recV1.Code != http.StatusOK {
			t.Fatalf("expected 200 on /api/v1/pricing/quote, got %d: %s", recV1.Code, recV1.Body.String())
		}

		var quoteV1 pricing.PriceQuote
		_ = json.Unmarshal(recV1.Body.Bytes(), &quoteV1)
		if quoteV1.GrandTotal != 100000.0 {
			t.Errorf("expected 100000.0 grand total, got %f", quoteV1.GrandTotal)
		}

		// Test compatibility alias /pricing/quote
		reqCompat := makeAuthRequest(http.MethodPost, "/pricing/quote", quoteReq, principal, "req_compat")
		recCompat := httptest.NewRecorder()
		router.ServeHTTP(recCompat, reqCompat)

		if recCompat.Code != http.StatusOK {
			t.Fatalf("expected 200 on /pricing/quote, got %d: %s", recCompat.Code, recCompat.Body.String())
		}
	})

	t.Run("Missing price returns 422 with unpriced_items details", func(t *testing.T) {
		mockSvc := &mockPricingService{
			calculateQuoteFn: func(ctx context.Context, req pricing.PriceQuoteRequest) (*pricing.PriceQuote, error) {
				return nil, &pricing.MissingPriceError{
					UnpricedItems: []pricing.UnpricedItemDetail{
						{Index: 0, VariantID: variantID.String()},
					},
				}
			},
		}
		handler := pricing.NewHandler(mockSvc, nil)
		router := setupTestRouter(handler)

		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/quote", pricing.PriceQuoteRequest{}, principal, "req_missing_price")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp apperr.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if errResp.Error.Code != "PRICE_NOT_FOUND" {
			t.Errorf("expected PRICE_NOT_FOUND, got %s", errResp.Error.Code)
		}
		if errResp.Error.Field != "items" {
			t.Errorf("expected field 'items', got %s", errResp.Error.Field)
		}
		unpriced, ok := errResp.Error.Details["unpriced_items"]
		if !ok || unpriced == nil {
			t.Fatalf("expected details.unpriced_items to be populated")
		}
		if errResp.RequestID != "req_missing_price" {
			t.Errorf("expected req_missing_price, got %s", errResp.RequestID)
		}
	})

	t.Run("Currency mismatch returns 400 CURRENCY_MISMATCH", func(t *testing.T) {
		mockSvc := &mockPricingService{
			calculateQuoteFn: func(ctx context.Context, req pricing.PriceQuoteRequest) (*pricing.PriceQuote, error) {
				return nil, pricing.ErrCurrencyMismatch
			},
		}
		handler := pricing.NewHandler(mockSvc, nil)
		router := setupTestRouter(handler)

		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/quote", pricing.PriceQuoteRequest{}, principal, "req_cur_err")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "CURRENCY_MISMATCH" {
			t.Errorf("expected CURRENCY_MISMATCH, got %s", errResp.Error.Code)
		}
	})

	t.Run("Missing default price list returns 422 PRICE_LIST_NOT_FOUND", func(t *testing.T) {
		mockSvc := &mockPricingService{
			calculateQuoteFn: func(ctx context.Context, req pricing.PriceQuoteRequest) (*pricing.PriceQuote, error) {
				return nil, pricing.ErrPriceListNotFound
			},
		}
		handler := pricing.NewHandler(mockSvc, nil)
		router := setupTestRouter(handler)

		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/quote", pricing.PriceQuoteRequest{}, principal, "req_pl_not_found")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d", rec.Code)
		}
		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "PRICE_LIST_NOT_FOUND" {
			t.Errorf("expected PRICE_LIST_NOT_FOUND, got %s", errResp.Error.Code)
		}
	})
}

func TestPricingHandler_AdminPriceListAndRuleRoutes(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	priceListID := uuid.New()
	ruleID := uuid.New()

	adminPrincipal := &auth.AuthenticatedPrincipal{
		UserID:   userID,
		TenantID: tenantID,
		Permissions: []string{
			pricing.PermPriceRead,
			pricing.PermPriceListCreate,
			pricing.PermPriceListUpdate,
			pricing.PermPriceRuleRead,
			pricing.PermPriceRuleCreate,
			pricing.PermPriceRuleUpdate,
			pricing.PermPriceRuleSubmit,
			pricing.PermPriceRuleApprove,
			pricing.PermPriceRuleActivate,
			pricing.PermPriceRuleDeactivate,
		},
	}

	mockSvc := &mockPricingService{
		createPriceListFn: func(ctx context.Context, req pricing.CreatePriceListRequest) (*pricing.PriceList, error) {
			return &pricing.PriceList{
				ID:        priceListID,
				TenantID:  tenantID,
				Code:      req.Code,
				Name:      req.Name,
				Currency:  req.Currency,
				Channel:   req.Channel,
				Status:    pricing.PriceListStatusActive,
				IsDefault: req.IsDefault,
				Version:   1,
			}, nil
		},
		getPriceListFn: func(ctx context.Context, id uuid.UUID) (*pricing.PriceList, error) {
			return &pricing.PriceList{
				ID:       id,
				TenantID: tenantID,
				Code:     "RETAIL-IDR",
				Status:   pricing.PriceListStatusActive,
			}, nil
		},
		createPriceRuleFn: func(ctx context.Context, req pricing.CreatePriceRuleRequest) (*pricing.PriceRule, error) {
			return &pricing.PriceRule{
				ID:          ruleID,
				TenantID:    tenantID,
				PriceListID: req.PriceListID,
				ProductID:   req.ProductID,
				MinQty:      req.MinQty,
				UnitPrice:   req.UnitPrice,
				Status:      pricing.PriceRuleStatusDraft,
				Version:     1,
			}, nil
		},
		approvePriceRuleFn: func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
			return &pricing.PriceRule{
				ID:      id,
				Status:  pricing.PriceRuleStatusApproved,
				Version: req.Version + 1,
			}, nil
		},
		activatePriceRuleFn: func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
			return &pricing.PriceRule{
				ID:      id,
				Status:  pricing.PriceRuleStatusActive,
				Version: req.Version + 1,
			}, nil
		},
		deactivatePriceRuleFn: func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
			return &pricing.PriceRule{
				ID:      id,
				Status:  pricing.PriceRuleStatusInactive,
				Version: req.Version + 1,
			}, nil
		},
		rejectPriceRuleFn: func(ctx context.Context, id uuid.UUID, req pricing.RuleTransitionRequest) (*pricing.PriceRule, error) {
			return &pricing.PriceRule{
				ID:      id,
				Status:  pricing.PriceRuleStatusDraft,
				Version: req.Version,
			}, nil
		},
	}

	handler := pricing.NewHandler(mockSvc, nil)
	router := setupTestRouter(handler)

	t.Run("Create price list returns 201", func(t *testing.T) {
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/price-lists", pricing.CreatePriceListRequest{
			Code:      "RETAIL-IDR",
			Name:      "Retail IDR Price List",
			Currency:  "IDR",
			Channel:   "ALL",
			IsDefault: true,
		}, adminPrincipal, "req_create_pl")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Also verify compatibility mount /pricing/price-lists
		reqCompat := makeAuthRequest(http.MethodPost, "/pricing/price-lists", pricing.CreatePriceListRequest{
			Code:      "RETAIL-IDR-2",
			Name:      "Retail IDR Price List 2",
			Currency:  "IDR",
			Channel:   "ALL",
		}, adminPrincipal, "req_create_pl_compat")
		recCompat := httptest.NewRecorder()
		router.ServeHTTP(recCompat, reqCompat)
		if recCompat.Code != http.StatusCreated {
			t.Fatalf("expected 201 on /pricing/price-lists, got %d: %s", recCompat.Code, recCompat.Body.String())
		}
	})

	t.Run("Get price list returns 200", func(t *testing.T) {
		req := makeAuthRequest(http.MethodGet, "/api/v1/pricing/price-lists/"+priceListID.String(), nil, adminPrincipal, "req_get_pl")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("Create rule returns 201", func(t *testing.T) {
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules", pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     uuid.New(),
			MinQty:        1,
			UnitPrice:     10000.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		}, adminPrincipal, "req_create_rule")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Approve rule returns 200", func(t *testing.T) {
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules/"+ruleID.String()+"/approve", pricing.RuleTransitionRequest{
			Version: 1,
		}, adminPrincipal, "req_approve")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("Reject rule returns 200 with PermPriceRuleApprove", func(t *testing.T) {
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules/"+ruleID.String()+"/reject", pricing.RuleTransitionRequest{
			Version: 1,
			Reason:  "Margin insufficient",
		}, adminPrincipal, "req_reject")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Approve rule lacking PermPriceRuleApprove returns 403", func(t *testing.T) {
		unprivileged := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{pricing.PermPriceRuleRead},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules/"+ruleID.String()+"/approve", pricing.RuleTransitionRequest{
			Version: 1,
		}, unprivileged, "req_approve_denied")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("Reject rule lacking PermPriceRuleApprove returns 403", func(t *testing.T) {
		unprivileged := &auth.AuthenticatedPrincipal{
			UserID:      userID,
			TenantID:    tenantID,
			Permissions: []string{pricing.PermPriceRuleRead},
		}
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules/"+ruleID.String()+"/reject", pricing.RuleTransitionRequest{
			Version: 1,
			Reason:  "Forbidden attempt",
		}, unprivileged, "req_reject_denied")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("Activate rule returns 200", func(t *testing.T) {
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules/"+ruleID.String()+"/activate", pricing.RuleTransitionRequest{
			Version: 2,
		}, adminPrincipal, "req_activate")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("Deactivate rule returns 200", func(t *testing.T) {
		req := makeAuthRequest(http.MethodPost, "/api/v1/pricing/rules/"+ruleID.String()+"/deactivate", pricing.RuleTransitionRequest{
			Version: 3,
		}, adminPrincipal, "req_deactivate")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}
