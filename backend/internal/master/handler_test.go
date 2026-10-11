package master_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/master"
	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
)

type mockMasterService struct {
	// Brand
	createBrandFn func(ctx context.Context, actorID *uuid.UUID, req master.CreateBrandRequest) (*master.Brand, error)
	getBrandFn    func(ctx context.Context, id uuid.UUID) (*master.Brand, error)
	updateBrandFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req master.UpdateBrandRequest) (*master.Brand, error)
	listBrandsFn  func(ctx context.Context, limit, offset int) ([]master.Brand, int64, error)

	// Category
	createCategoryFn func(ctx context.Context, actorID *uuid.UUID, req master.CreateCategoryRequest) (*master.Category, error)
	getCategoryFn    func(ctx context.Context, id uuid.UUID) (*master.Category, error)
	updateCategoryFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req master.UpdateCategoryRequest) (*master.Category, error)
	listCategoriesFn func(ctx context.Context, limit, offset int) ([]master.Category, int64, error)

	// UOM
	createUOMFn func(ctx context.Context, actorID *uuid.UUID, req master.CreateUOMRequest) (*master.UOM, error)
	getUOMFn    func(ctx context.Context, id uuid.UUID) (*master.UOM, error)
	listUOMsFn  func(ctx context.Context, limit, offset int) ([]master.UOM, int64, error)

	// Product
	createProductFn     func(ctx context.Context, actorID *uuid.UUID, req master.CreateProductRequest) (*master.Product, error)
	getProductFn        func(ctx context.Context, id uuid.UUID) (*master.ProductDetails, error)
	updateProductFn     func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req master.UpdateProductRequest) (*master.Product, error)
	activateProductFn   func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID) (*master.Product, error)
	deactivateProductFn func(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, reason string) (*master.Product, error)
	listProductsFn      func(ctx context.Context, limit, offset int) ([]master.Product, int64, error)

	// Variant
	createVariantFn func(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req master.CreateVariantRequest) (*master.ProductVariant, error)
	listVariantsFn  func(ctx context.Context, productID uuid.UUID) ([]master.ProductVariant, error)

	// Barcode
	createBarcodeFn func(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req master.CreateBarcodeRequest) (*master.ProductBarcode, error)
	listBarcodesFn  func(ctx context.Context, productID uuid.UUID, limit, offset int) ([]master.ProductBarcode, int64, error)
	lookupBarcodeFn func(ctx context.Context, barcode string) (*master.ProductDetails, error)

	// Serial Number
	createSerialNumberFn func(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, serialNo string, variantID *uuid.UUID) (*master.SerialNumber, error)
	getSerialNumberFn    func(ctx context.Context, serialNo string) (*master.SerialNumber, error)
	listSerialNumbersFn  func(ctx context.Context, productID uuid.UUID, limit, offset int) ([]master.SerialNumber, int64, error)
}

func (m *mockMasterService) CreateBrand(ctx context.Context, actorID *uuid.UUID, req master.CreateBrandRequest) (*master.Brand, error) {
	if m.createBrandFn != nil {
		return m.createBrandFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockMasterService) GetBrand(ctx context.Context, id uuid.UUID) (*master.Brand, error) {
	if m.getBrandFn != nil {
		return m.getBrandFn(ctx, id)
	}
	return nil, nil
}
func (m *mockMasterService) UpdateBrand(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req master.UpdateBrandRequest) (*master.Brand, error) {
	if m.updateBrandFn != nil {
		return m.updateBrandFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockMasterService) ListBrands(ctx context.Context, limit, offset int) ([]master.Brand, int64, error) {
	if m.listBrandsFn != nil {
		return m.listBrandsFn(ctx, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockMasterService) CreateCategory(ctx context.Context, actorID *uuid.UUID, req master.CreateCategoryRequest) (*master.Category, error) {
	if m.createCategoryFn != nil {
		return m.createCategoryFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockMasterService) GetCategory(ctx context.Context, id uuid.UUID) (*master.Category, error) {
	if m.getCategoryFn != nil {
		return m.getCategoryFn(ctx, id)
	}
	return nil, nil
}
func (m *mockMasterService) UpdateCategory(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req master.UpdateCategoryRequest) (*master.Category, error) {
	if m.updateCategoryFn != nil {
		return m.updateCategoryFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockMasterService) ListCategories(ctx context.Context, limit, offset int) ([]master.Category, int64, error) {
	if m.listCategoriesFn != nil {
		return m.listCategoriesFn(ctx, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockMasterService) CreateUOM(ctx context.Context, actorID *uuid.UUID, req master.CreateUOMRequest) (*master.UOM, error) {
	if m.createUOMFn != nil {
		return m.createUOMFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockMasterService) GetUOM(ctx context.Context, id uuid.UUID) (*master.UOM, error) {
	if m.getUOMFn != nil {
		return m.getUOMFn(ctx, id)
	}
	return nil, nil
}
func (m *mockMasterService) ListUOMs(ctx context.Context, limit, offset int) ([]master.UOM, int64, error) {
	if m.listUOMsFn != nil {
		return m.listUOMsFn(ctx, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockMasterService) CreateProduct(ctx context.Context, actorID *uuid.UUID, req master.CreateProductRequest) (*master.Product, error) {
	if m.createProductFn != nil {
		return m.createProductFn(ctx, actorID, req)
	}
	return nil, nil
}
func (m *mockMasterService) GetProduct(ctx context.Context, id uuid.UUID) (*master.ProductDetails, error) {
	if m.getProductFn != nil {
		return m.getProductFn(ctx, id)
	}
	return nil, nil
}
func (m *mockMasterService) UpdateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req master.UpdateProductRequest) (*master.Product, error) {
	if m.updateProductFn != nil {
		return m.updateProductFn(ctx, actorID, id, req)
	}
	return nil, nil
}
func (m *mockMasterService) ActivateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID) (*master.Product, error) {
	if m.activateProductFn != nil {
		return m.activateProductFn(ctx, actorID, id)
	}
	return nil, nil
}
func (m *mockMasterService) DeactivateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, reason string) (*master.Product, error) {
	if m.deactivateProductFn != nil {
		return m.deactivateProductFn(ctx, actorID, id, reason)
	}
	return nil, nil
}
func (m *mockMasterService) ListProducts(ctx context.Context, limit, offset int) ([]master.Product, int64, error) {
	if m.listProductsFn != nil {
		return m.listProductsFn(ctx, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockMasterService) CreateVariant(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req master.CreateVariantRequest) (*master.ProductVariant, error) {
	if m.createVariantFn != nil {
		return m.createVariantFn(ctx, actorID, productID, req)
	}
	return nil, nil
}
func (m *mockMasterService) ListVariants(ctx context.Context, productID uuid.UUID) ([]master.ProductVariant, error) {
	if m.listVariantsFn != nil {
		return m.listVariantsFn(ctx, productID)
	}
	return nil, nil
}

func (m *mockMasterService) CreateBarcode(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req master.CreateBarcodeRequest) (*master.ProductBarcode, error) {
	if m.createBarcodeFn != nil {
		return m.createBarcodeFn(ctx, actorID, productID, req)
	}
	return nil, nil
}
func (m *mockMasterService) ListBarcodes(ctx context.Context, productID uuid.UUID, limit, offset int) ([]master.ProductBarcode, int64, error) {
	if m.listBarcodesFn != nil {
		return m.listBarcodesFn(ctx, productID, limit, offset)
	}
	return nil, 0, nil
}
func (m *mockMasterService) LookupBarcode(ctx context.Context, barcode string) (*master.ProductDetails, error) {
	if m.lookupBarcodeFn != nil {
		return m.lookupBarcodeFn(ctx, barcode)
	}
	return nil, nil
}

func (m *mockMasterService) CreateSerialNumber(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, serialNo string, variantID *uuid.UUID) (*master.SerialNumber, error) {
	if m.createSerialNumberFn != nil {
		return m.createSerialNumberFn(ctx, actorID, productID, serialNo, variantID)
	}
	return nil, nil
}
func (m *mockMasterService) GetSerialNumber(ctx context.Context, serialNo string) (*master.SerialNumber, error) {
	if m.getSerialNumberFn != nil {
		return m.getSerialNumberFn(ctx, serialNo)
	}
	return nil, nil
}
func (m *mockMasterService) ListSerialNumbers(ctx context.Context, productID uuid.UUID, limit, offset int) ([]master.SerialNumber, int64, error) {
	if m.listSerialNumbersFn != nil {
		return m.listSerialNumbersFn(ctx, productID, limit, offset)
	}
	return nil, 0, nil
}

// ============================================================================
// Tests
// ============================================================================

func TestHandler_AuthenticationAndAuthorization(t *testing.T) {
	mockSvc := &mockMasterService{}
	h := master.NewHandler(mockSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := h.Routes()

	// 1. Missing principal -> 401
	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got: %d", rr.Code)
	}

	var errResp apperr.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error envelope: %v", err)
	}
	if errResp.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("expected error code UNAUTHORIZED, got: %s", errResp.Error.Code)
	}

	// 2. Principal without product.create -> 403
	tenantID := uuid.New()
	userID := uuid.New()
	principalNoPerm := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Permissions: []string{"unrelated.perm"},
	}

	req2 := httptest.NewRequest(http.MethodPost, "/products", bytes.NewBufferString(`{"sku":"SKU1","name":"Name 1","base_uom_id":"`+uuid.New().String()+`"}`))
	req2 = req2.WithContext(auth.WithPrincipal(req2.Context(), principalNoPerm))
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got: %d", rr2.Code)
	}

	// 3. Principal with product.create -> 200 OK
	principalWithPerm := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Permissions: []string{"product.create"},
	}

	called := false
	mockSvc.createProductFn = func(_ context.Context, _ *uuid.UUID, _ master.CreateProductRequest) (*master.Product, error) {
		called = true
		return &master.Product{
			ID:        uuid.New(),
			TenantID:  tenantID,
			SKU:       "SKU1",
			Name:      "Name 1",
			Status:    master.StatusActive,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}, nil
	}

	req3 := httptest.NewRequest(http.MethodPost, "/products", bytes.NewBufferString(`{"sku":"SKU1","name":"Name 1","base_uom_id":"`+uuid.New().String()+`"}`))
	req3 = req3.WithContext(auth.WithPrincipal(req3.Context(), principalWithPerm))
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d (body: %s)", rr3.Code, rr3.Body.String())
	}
	if !called {
		t.Fatalf("service was not invoked")
	}
}

func TestHandler_DualMountCompatibilityAliasParity(t *testing.T) {
	mockSvc := &mockMasterService{}
	h := master.NewHandler(mockSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	canonicalRouter := h.Routes()
	aliasRouter := h.ProductAliasRoutes()

	tenantID := uuid.New()
	userID := uuid.New()
	principal := &auth.AuthenticatedPrincipal{
		TenantID: tenantID,
		UserID:   userID,
		Permissions: []string{
			"product.read",
			"product.create",
			"product.update",
			"product.activate",
			"product.deactivate",
			"product.barcode.read",
			"product.barcode.manage",
		},
	}

	testProdID := uuid.New()
	testProd := &master.Product{
		ID:        testProdID,
		TenantID:  tenantID,
		SKU:       "PARITY-001",
		Name:      "Parity Test Product",
		Status:    master.StatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	mockSvc.getProductFn = func(_ context.Context, _ uuid.UUID) (*master.ProductDetails, error) {
		return &master.ProductDetails{
			Product:  *testProd,
			Variants: []master.ProductVariant{},
			Barcodes: []master.ProductBarcode{},
		}, nil
	}

	// 1. Canonical route: GET /products/{id}
	reqCanon := httptest.NewRequest(http.MethodGet, "/products/"+testProdID.String(), nil)
	reqCanon = reqCanon.WithContext(auth.WithPrincipal(reqCanon.Context(), principal))
	rrCanon := httptest.NewRecorder()
	canonicalRouter.ServeHTTP(rrCanon, reqCanon)

	if rrCanon.Code != http.StatusOK {
		t.Fatalf("canonical route failed: status %d", rrCanon.Code)
	}

	// 2. Compatibility alias route: GET /{id} (mounted at /api/v1/products/{id})
	reqAlias := httptest.NewRequest(http.MethodGet, "/"+testProdID.String(), nil)
	reqAlias = reqAlias.WithContext(auth.WithPrincipal(reqAlias.Context(), principal))
	rrAlias := httptest.NewRecorder()
	aliasRouter.ServeHTTP(rrAlias, reqAlias)

	if rrAlias.Code != http.StatusOK {
		t.Fatalf("alias route failed: status %d", rrAlias.Code)
	}

	// 3. Response parity verification
	var canonResp, aliasResp master.ProductResponse
	if err := json.Unmarshal(rrCanon.Body.Bytes(), &canonResp); err != nil {
		t.Fatalf("unmarshal canon: %v", err)
	}
	if err := json.Unmarshal(rrAlias.Body.Bytes(), &aliasResp); err != nil {
		t.Fatalf("unmarshal alias: %v", err)
	}

	if canonResp.ID != aliasResp.ID || canonResp.SKU != aliasResp.SKU || canonResp.Active != aliasResp.Active {
		t.Fatalf("parity mismatch: %+v vs %+v", canonResp, aliasResp)
	}
}

func TestHandler_ErrorMappingEnvelopes(t *testing.T) {
	mockSvc := &mockMasterService{}
	h := master.NewHandler(mockSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := h.Routes()

	principal := &auth.AuthenticatedPrincipal{
		TenantID:    uuid.New(),
		UserID:      uuid.New(),
		Permissions: []string{"product.read", "product.update", "brand.create"},
	}

	// 1. Not Found mapping -> 404
	mockSvc.getProductFn = func(_ context.Context, _ uuid.UUID) (*master.ProductDetails, error) {
		return nil, master.ErrProductNotFound
	}
	reqNotFound := httptest.NewRequest(http.MethodGet, "/products/"+uuid.New().String(), nil)
	reqNotFound = reqNotFound.WithContext(auth.WithPrincipal(reqNotFound.Context(), principal))
	rrNotFound := httptest.NewRecorder()
	router.ServeHTTP(rrNotFound, reqNotFound)

	if rrNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rrNotFound.Code)
	}
	var errEnvelope apperr.ErrorResponse
	_ = json.Unmarshal(rrNotFound.Body.Bytes(), &errEnvelope)
	if errEnvelope.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", errEnvelope.Error.Code)
	}

	// 2. Conflict mapping -> 409
	mockSvc.createBrandFn = func(_ context.Context, _ *uuid.UUID, _ master.CreateBrandRequest) (*master.Brand, error) {
		return nil, master.ErrBrandCodeExists
	}
	reqConflict := httptest.NewRequest(http.MethodPost, "/brands", bytes.NewBufferString(`{"code":"EXISTS","name":"Brand"}`))
	reqConflict = reqConflict.WithContext(auth.WithPrincipal(reqConflict.Context(), principal))
	rrConflict := httptest.NewRecorder()
	router.ServeHTTP(rrConflict, reqConflict)

	if rrConflict.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rrConflict.Code)
	}
	_ = json.Unmarshal(rrConflict.Body.Bytes(), &errEnvelope)
	if errEnvelope.Error.Code != "BRAND_CODE_EXISTS" {
		t.Fatalf("expected BRAND_CODE_EXISTS, got %s", errEnvelope.Error.Code)
	}

	// 3. Optimistic Lock Conflict -> 409
	mockSvc.updateProductFn = func(_ context.Context, _ *uuid.UUID, _ uuid.UUID, _ master.UpdateProductRequest) (*master.Product, error) {
		return nil, master.ErrOptimisticLockConflict
	}
	reqLock := httptest.NewRequest(http.MethodPatch, "/products/"+uuid.New().String(), bytes.NewBufferString(`{"name":"New Name"}`))
	reqLock = reqLock.WithContext(auth.WithPrincipal(reqLock.Context(), principal))
	rrLock := httptest.NewRecorder()
	router.ServeHTTP(rrLock, reqLock)

	if rrLock.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rrLock.Code)
	}
	_ = json.Unmarshal(rrLock.Body.Bytes(), &errEnvelope)
	if errEnvelope.Error.Code != "OPTIMISTIC_LOCK_CONFLICT" {
		t.Fatalf("expected OPTIMISTIC_LOCK_CONFLICT, got %s", errEnvelope.Error.Code)
	}
}
