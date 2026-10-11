package organization_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/organization"
	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
)

type mockOrgService struct {
	createCompanyFn func(ctx context.Context, actorID *uuid.UUID, req organization.CreateCompanyRequest) (*organization.Company, error)
	getCompanyFn    func(ctx context.Context, id uuid.UUID) (*organization.Company, error)
	updateCompanyFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateCompanyRequest) (*organization.Company, error)
	listCompaniesFn func(ctx context.Context, limit, offset int) ([]organization.Company, int64, error)

	createRegionFn func(ctx context.Context, actorID *uuid.UUID, req organization.CreateRegionRequest) (*organization.Region, error)
	getRegionFn    func(ctx context.Context, id uuid.UUID) (*organization.Region, error)
	updateRegionFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateRegionRequest) (*organization.Region, error)
	listRegionsFn func(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]organization.Region, int64, error)

	createStoreFn func(ctx context.Context, actorID *uuid.UUID, req organization.CreateStoreRequest) (*organization.Store, error)
	getStoreFn    func(ctx context.Context, id uuid.UUID) (*organization.Store, error)
	updateStoreFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateStoreRequest) (*organization.Store, error)
	listStoresFn func(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]organization.Store, int64, error)

	createWarehouseFn func(ctx context.Context, actorID *uuid.UUID, req organization.CreateWarehouseRequest) (*organization.Warehouse, error)
	getWarehouseFn    func(ctx context.Context, id uuid.UUID) (*organization.Warehouse, error)
	updateWarehouseFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateWarehouseRequest) (*organization.Warehouse, error)
	listWarehousesFn func(ctx context.Context, companyID *uuid.UUID, storeID *uuid.UUID, limit, offset int) ([]organization.Warehouse, int64, error)

	registerTerminalFn func(ctx context.Context, actorID *uuid.UUID, req organization.RegisterTerminalRequest) (*organization.TerminalRegistrationResult, error)
	getTerminalFn      func(ctx context.Context, id uuid.UUID) (*organization.Terminal, error)
	updateTerminalFn   func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateTerminalRequest) (*organization.Terminal, error)
	listTerminalsFn    func(ctx context.Context, storeID *uuid.UUID, limit, offset int) ([]organization.Terminal, int64, error)

	rotateDeviceTokenFn func(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) (*organization.TerminalRegistrationResult, error)
	revokeDeviceTokenFn func(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) error
	verifyDeviceTokenFn func(ctx context.Context, terminalID uuid.UUID, token string) (*organization.Terminal, error)

	validateScopeEntityFn func(ctx context.Context, scopeType string, entityID uuid.UUID) (bool, error)
}

func (m *mockOrgService) CreateCompany(ctx context.Context, actorID *uuid.UUID, req organization.CreateCompanyRequest) (*organization.Company, error) {
	if m.createCompanyFn != nil {
		return m.createCompanyFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockOrgService) GetCompany(ctx context.Context, id uuid.UUID) (*organization.Company, error) {
	if m.getCompanyFn != nil {
		return m.getCompanyFn(ctx, id)
	}
	return nil, nil
}
func (m *mockOrgService) UpdateCompany(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateCompanyRequest) (*organization.Company, error) {
	if m.updateCompanyFn != nil {
		return m.updateCompanyFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockOrgService) ListCompanies(ctx context.Context, limit, offset int) ([]organization.Company, int64, error) {
	if m.listCompaniesFn != nil {
		return m.listCompaniesFn(ctx, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockOrgService) CreateRegion(ctx context.Context, actorID *uuid.UUID, req organization.CreateRegionRequest) (*organization.Region, error) {
	if m.createRegionFn != nil {
		return m.createRegionFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockOrgService) GetRegion(ctx context.Context, id uuid.UUID) (*organization.Region, error) {
	if m.getRegionFn != nil {
		return m.getRegionFn(ctx, id)
	}
	return nil, nil
}
func (m *mockOrgService) UpdateRegion(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateRegionRequest) (*organization.Region, error) {
	if m.updateRegionFn != nil {
		return m.updateRegionFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockOrgService) ListRegions(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]organization.Region, int64, error) {
	if m.listRegionsFn != nil {
		return m.listRegionsFn(ctx, companyID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockOrgService) CreateStore(ctx context.Context, actorID *uuid.UUID, req organization.CreateStoreRequest) (*organization.Store, error) {
	if m.createStoreFn != nil {
		return m.createStoreFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockOrgService) GetStore(ctx context.Context, id uuid.UUID) (*organization.Store, error) {
	if m.getStoreFn != nil {
		return m.getStoreFn(ctx, id)
	}
	return nil, nil
}
func (m *mockOrgService) UpdateStore(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateStoreRequest) (*organization.Store, error) {
	if m.updateStoreFn != nil {
		return m.updateStoreFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockOrgService) ListStores(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]organization.Store, int64, error) {
	if m.listStoresFn != nil {
		return m.listStoresFn(ctx, companyID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockOrgService) CreateWarehouse(ctx context.Context, actorID *uuid.UUID, req organization.CreateWarehouseRequest) (*organization.Warehouse, error) {
	if m.createWarehouseFn != nil {
		return m.createWarehouseFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockOrgService) GetWarehouse(ctx context.Context, id uuid.UUID) (*organization.Warehouse, error) {
	if m.getWarehouseFn != nil {
		return m.getWarehouseFn(ctx, id)
	}
	return nil, nil
}
func (m *mockOrgService) UpdateWarehouse(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateWarehouseRequest) (*organization.Warehouse, error) {
	if m.updateWarehouseFn != nil {
		return m.updateWarehouseFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockOrgService) ListWarehouses(ctx context.Context, companyID *uuid.UUID, storeID *uuid.UUID, limit, offset int) ([]organization.Warehouse, int64, error) {
	if m.listWarehousesFn != nil {
		return m.listWarehousesFn(ctx, companyID, storeID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockOrgService) RegisterTerminal(ctx context.Context, actorID *uuid.UUID, req organization.RegisterTerminalRequest) (*organization.TerminalRegistrationResult, error) {
	if m.registerTerminalFn != nil {
		return m.registerTerminalFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockOrgService) GetTerminal(ctx context.Context, id uuid.UUID) (*organization.Terminal, error) {
	if m.getTerminalFn != nil {
		return m.getTerminalFn(ctx, id)
	}
	return nil, nil
}
func (m *mockOrgService) UpdateTerminal(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req organization.UpdateTerminalRequest) (*organization.Terminal, error) {
	if m.updateTerminalFn != nil {
		return m.updateTerminalFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockOrgService) ListTerminals(ctx context.Context, storeID *uuid.UUID, limit, offset int) ([]organization.Terminal, int64, error) {
	if m.listTerminalsFn != nil {
		return m.listTerminalsFn(ctx, storeID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockOrgService) RotateDeviceToken(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) (*organization.TerminalRegistrationResult, error) {
	if m.rotateDeviceTokenFn != nil {
		return m.rotateDeviceTokenFn(ctx, actorID, terminalID)
	}
	return nil, nil
}
func (m *mockOrgService) RevokeDeviceToken(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) error {
	if m.revokeDeviceTokenFn != nil {
		return m.revokeDeviceTokenFn(ctx, actorID, terminalID)
	}
	return nil
}
func (m *mockOrgService) VerifyDeviceToken(ctx context.Context, terminalID uuid.UUID, token string) (*organization.Terminal, error) {
	if m.verifyDeviceTokenFn != nil {
		return m.verifyDeviceTokenFn(ctx, terminalID, token)
	}
	return nil, nil
}
func (m *mockOrgService) ValidateScopeEntity(ctx context.Context, scopeType string, entityID uuid.UUID) (bool, error) {
	if m.validateScopeEntityFn != nil {
		return m.validateScopeEntityFn(ctx, scopeType, entityID)
	}
	return false, nil
}

func TestHandler_AuthenticationAndAuthorization(t *testing.T) {
	mockSvc := &mockOrgService{}
	h := organization.NewHandler(mockSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := h.Routes()

	// 1. Unauthenticated request -> 401
	req := httptest.NewRequest(http.MethodGet, "/companies", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing principal, got %d", rr.Code)
	}

	var errResp apperr.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error envelope: %v", err)
	}
	if errResp.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("expected error code UNAUTHORIZED, got %s", errResp.Error.Code)
	}

	// 2. Authenticated but missing permission -> 403
	tenantID := uuid.New()
	userID := uuid.New()
	principalNoPerm := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Permissions: []string{"unrelated.perm"},
	}

	req2 := httptest.NewRequest(http.MethodPost, "/companies", bytes.NewBufferString(`{"code":"C1","name":"Company 1"}`))
	req2 = req2.WithContext(auth.WithPrincipal(req2.Context(), principalNoPerm))
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for missing permission, got %d", rr2.Code)
	}

	// 3. Authorized request -> calls service
	principalWithPerm := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Permissions: []string{"company.create"},
	}

	called := false
	mockSvc.createCompanyFn = func(_ context.Context, _ *uuid.UUID, req organization.CreateCompanyRequest) (*organization.Company, error) {
		called = true
		return &organization.Company{
			ID:       uuid.New(),
			TenantID: tenantID,
			Code:     req.Code,
			Name:     req.Name,
			Status:   organization.StatusActive,
		}, nil
	}

	req3 := httptest.NewRequest(http.MethodPost, "/companies", bytes.NewBufferString(`{"code":"C1","name":"Company 1"}`))
	req3 = req3.WithContext(auth.WithPrincipal(req3.Context(), principalWithPerm))
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rr3.Code, rr3.Body.String())
	}
	if !called {
		t.Fatalf("expected service.CreateCompany to be invoked")
	}
}

func TestHandler_RegisterTerminal_And_TokenVerification(t *testing.T) {
	mockSvc := &mockOrgService{}
	h := organization.NewHandler(mockSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := h.Routes()

	tenantID := uuid.New()
	userID := uuid.New()
	principal := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Permissions: []string{"terminal.register", "terminal.read"},
	}

	termID := uuid.New()
	mockSvc.registerTerminalFn = func(_ context.Context, _ *uuid.UUID, req organization.RegisterTerminalRequest) (*organization.TerminalRegistrationResult, error) {
		return &organization.TerminalRegistrationResult{
			Terminal: organization.Terminal{
				ID:       termID,
				TenantID: tenantID,
				Code:     req.Code,
				Name:     req.Name,
				Status:   organization.TerminalStatusActive,
			},
			DeviceToken: "dev_tok_testsecret1234567890abcdef",
		}, nil
	}

	bodyJSON, _ := json.Marshal(organization.RegisterTerminalRequest{
		CompanyID: uuid.New(),
		StoreID:   uuid.New(),
		Code:      "TERM-01",
		Name:      "Checkout Counter 1",
	})

	req := httptest.NewRequest(http.MethodPost, "/terminals", bytes.NewReader(bodyJSON))
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rr.Code, rr.Body.String())
	}

	var res organization.TerminalRegistrationResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.DeviceToken != "dev_tok_testsecret1234567890abcdef" {
		t.Fatalf("unexpected device token: %s", res.DeviceToken)
	}
	if res.Terminal.ID != termID {
		t.Fatalf("unexpected terminal id: %s", res.Terminal.ID)
	}
}
