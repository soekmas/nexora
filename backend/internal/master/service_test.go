package master_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/master"
	"example.com/nexora/backend/internal/platform/postgres"
)

type mockTxManager struct{}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (m *mockTxManager) WithinUnscopedTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (m *mockTxManager) WithinSavepoint(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

type inMemoryStore struct {
	brands    map[uuid.UUID]*master.Brand
	categories map[uuid.UUID]*master.Category
	uoms      map[uuid.UUID]*master.UOM
	products  map[uuid.UUID]*master.Product
	variants  map[uuid.UUID]*master.ProductVariant
	barcodes  map[uuid.UUID]*master.ProductBarcode
	serials   map[uuid.UUID]*master.SerialNumber
	companies map[uuid.UUID]bool
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		brands:     make(map[uuid.UUID]*master.Brand),
		categories: make(map[uuid.UUID]*master.Category),
		uoms:       make(map[uuid.UUID]*master.UOM),
		products:   make(map[uuid.UUID]*master.Product),
		variants:   make(map[uuid.UUID]*master.ProductVariant),
		barcodes:   make(map[uuid.UUID]*master.ProductBarcode),
		serials:    make(map[uuid.UUID]*master.SerialNumber),
		companies:  make(map[uuid.UUID]bool),
	}
}

// Brand
func (s *inMemoryStore) CreateBrand(_ context.Context, b *master.Brand) error {
	for _, existing := range s.brands {
		if existing.TenantID == b.TenantID && existing.Code == b.Code {
			return master.ErrBrandCodeExists
		}
	}
	s.brands[b.ID] = b
	return nil
}
func (s *inMemoryStore) GetBrandByID(_ context.Context, id uuid.UUID) (*master.Brand, error) {
	b, ok := s.brands[id]
	if !ok {
		return nil, master.ErrBrandNotFound
	}
	return b, nil
}
func (s *inMemoryStore) GetBrandByCode(_ context.Context, code string) (*master.Brand, error) {
	for _, b := range s.brands {
		if b.Code == code {
			return b, nil
		}
	}
	return nil, master.ErrBrandNotFound
}
func (s *inMemoryStore) UpdateBrand(_ context.Context, b *master.Brand) error {
	if _, ok := s.brands[b.ID]; !ok {
		return master.ErrBrandNotFound
	}
	s.brands[b.ID] = b
	return nil
}
func (s *inMemoryStore) ListBrands(_ context.Context, _, _ int) ([]master.Brand, int64, error) {
	var list []master.Brand
	for _, b := range s.brands {
		list = append(list, *b)
	}
	return list, int64(len(list)), nil
}

// Category
func (s *inMemoryStore) CreateCategory(_ context.Context, c *master.Category) error {
	for _, existing := range s.categories {
		if existing.TenantID == c.TenantID && existing.Code == c.Code {
			return master.ErrCategoryCodeExists
		}
	}
	s.categories[c.ID] = c
	return nil
}
func (s *inMemoryStore) GetCategoryByID(_ context.Context, id uuid.UUID) (*master.Category, error) {
	c, ok := s.categories[id]
	if !ok {
		return nil, master.ErrCategoryNotFound
	}
	return c, nil
}
func (s *inMemoryStore) GetCategoryByCode(_ context.Context, code string) (*master.Category, error) {
	for _, c := range s.categories {
		if c.Code == code {
			return c, nil
		}
	}
	return nil, master.ErrCategoryNotFound
}
func (s *inMemoryStore) UpdateCategory(_ context.Context, c *master.Category) error {
	if _, ok := s.categories[c.ID]; !ok {
		return master.ErrCategoryNotFound
	}
	s.categories[c.ID] = c
	return nil
}
func (s *inMemoryStore) ListCategories(_ context.Context, _, _ int) ([]master.Category, int64, error) {
	var list []master.Category
	for _, c := range s.categories {
		list = append(list, *c)
	}
	return list, int64(len(list)), nil
}

// UOM
func (s *inMemoryStore) CreateUOM(_ context.Context, u *master.UOM) error {
	for _, existing := range s.uoms {
		if existing.TenantID == u.TenantID && existing.Code == u.Code {
			return master.ErrUOMCodeExists
		}
	}
	s.uoms[u.ID] = u
	return nil
}
func (s *inMemoryStore) GetUOMByID(_ context.Context, id uuid.UUID) (*master.UOM, error) {
	u, ok := s.uoms[id]
	if !ok {
		return nil, master.ErrUOMNotFound
	}
	return u, nil
}
func (s *inMemoryStore) GetUOMByCode(_ context.Context, code string) (*master.UOM, error) {
	for _, u := range s.uoms {
		if u.Code == code {
			return u, nil
		}
	}
	return nil, master.ErrUOMNotFound
}
func (s *inMemoryStore) ListUOMs(_ context.Context, _, _ int) ([]master.UOM, int64, error) {
	var list []master.UOM
	for _, u := range s.uoms {
		list = append(list, *u)
	}
	return list, int64(len(list)), nil
}

// Product
func (s *inMemoryStore) CreateProduct(_ context.Context, p *master.Product) error {
	for _, existing := range s.products {
		if existing.TenantID == p.TenantID && existing.SKU == p.SKU {
			return master.ErrProductSKUExists
		}
	}
	s.products[p.ID] = p
	return nil
}
func (s *inMemoryStore) GetProductByID(_ context.Context, id uuid.UUID) (*master.Product, error) {
	p, ok := s.products[id]
	if !ok {
		return nil, master.ErrProductNotFound
	}
	return p, nil
}
func (s *inMemoryStore) GetProductBySKU(_ context.Context, sku string) (*master.Product, error) {
	for _, p := range s.products {
		if p.SKU == sku {
			return p, nil
		}
	}
	return nil, master.ErrProductNotFound
}
func (s *inMemoryStore) UpdateProduct(_ context.Context, p *master.Product) error {
	existing, ok := s.products[p.ID]
	if !ok {
		return master.ErrProductNotFound
	}
	if existing.Version != p.Version {
		return master.ErrOptimisticLockConflict
	}
	p.Version++
	s.products[p.ID] = p
	return nil
}
func (s *inMemoryStore) ListProducts(_ context.Context, _, _ int) ([]master.Product, int64, error) {
	var list []master.Product
	for _, p := range s.products {
		list = append(list, *p)
	}
	return list, int64(len(list)), nil
}

// Variant
func (s *inMemoryStore) CreateVariant(_ context.Context, v *master.ProductVariant) error {
	for _, existing := range s.variants {
		if existing.TenantID == v.TenantID && existing.SKU == v.SKU {
			return master.ErrVariantSKUExists
		}
	}
	s.variants[v.ID] = v
	return nil
}
func (s *inMemoryStore) GetVariantByID(_ context.Context, id uuid.UUID) (*master.ProductVariant, error) {
	v, ok := s.variants[id]
	if !ok {
		return nil, master.ErrVariantNotFound
	}
	return v, nil
}
func (s *inMemoryStore) GetVariantBySKU(_ context.Context, sku string) (*master.ProductVariant, error) {
	for _, v := range s.variants {
		if v.SKU == sku {
			return v, nil
		}
	}
	return nil, master.ErrVariantNotFound
}
func (s *inMemoryStore) ListVariantsByProductID(_ context.Context, productID uuid.UUID) ([]master.ProductVariant, error) {
	var list []master.ProductVariant
	for _, v := range s.variants {
		if v.ProductID == productID {
			list = append(list, *v)
		}
	}
	return list, nil
}

// Barcode
func (s *inMemoryStore) CreateBarcode(_ context.Context, b *master.ProductBarcode) error {
	for _, existing := range s.barcodes {
		if existing.TenantID == b.TenantID && existing.Barcode == b.Barcode {
			return master.ErrBarcodeExists
		}
	}
	s.barcodes[b.ID] = b
	return nil
}
func (s *inMemoryStore) GetBarcodeByID(_ context.Context, id uuid.UUID) (*master.ProductBarcode, error) {
	b, ok := s.barcodes[id]
	if !ok {
		return nil, master.ErrBarcodeNotFound
	}
	return b, nil
}
func (s *inMemoryStore) GetBarcodeByValue(_ context.Context, barcode string) (*master.ProductBarcode, error) {
	for _, b := range s.barcodes {
		if b.Barcode == barcode {
			return b, nil
		}
	}
	return nil, master.ErrBarcodeNotFound
}
func (s *inMemoryStore) ListBarcodesByProductID(_ context.Context, productID uuid.UUID, _, _ int) ([]master.ProductBarcode, int64, error) {
	var list []master.ProductBarcode
	for _, b := range s.barcodes {
		if b.ProductID == productID {
			list = append(list, *b)
		}
	}
	return list, int64(len(list)), nil
}
func (s *inMemoryStore) ClearPrimaryBarcode(_ context.Context, productID uuid.UUID, variantID *uuid.UUID) error {
	for _, b := range s.barcodes {
		if b.ProductID == productID {
			if variantID == nil && b.VariantID == nil {
				b.IsPrimary = false
			} else if variantID != nil && b.VariantID != nil && *b.VariantID == *variantID {
				b.IsPrimary = false
			}
		}
	}
	return nil
}

// SerialNumber
func (s *inMemoryStore) CreateSerialNumber(_ context.Context, sn *master.SerialNumber) error {
	for _, existing := range s.serials {
		if existing.TenantID == sn.TenantID && existing.SerialNo == sn.SerialNo {
			return master.ErrSerialNumberExists
		}
	}
	s.serials[sn.ID] = sn
	return nil
}
func (s *inMemoryStore) GetSerialNumberByID(_ context.Context, id uuid.UUID) (*master.SerialNumber, error) {
	sn, ok := s.serials[id]
	if !ok {
		return nil, master.ErrSerialNumberNotFound
	}
	return sn, nil
}
func (s *inMemoryStore) GetSerialNumberByNo(_ context.Context, serialNo string) (*master.SerialNumber, error) {
	for _, sn := range s.serials {
		if sn.SerialNo == serialNo {
			return sn, nil
		}
	}
	return nil, master.ErrSerialNumberNotFound
}
func (s *inMemoryStore) ListSerialNumbersByProductID(_ context.Context, productID uuid.UUID, _, _ int) ([]master.SerialNumber, int64, error) {
	var list []master.SerialNumber
	for _, sn := range s.serials {
		if sn.ProductID == productID {
			list = append(list, *sn)
		}
	}
	return list, int64(len(list)), nil
}

// Company validation
func (s *inMemoryStore) ValidateCompany(_ context.Context, companyID uuid.UUID) (bool, error) {
	return s.companies[companyID], nil
}

// Helper to create test service
func newTestService(store *inMemoryStore) master.Service {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return master.NewService(store, &mockTxManager{}, nil, nil, logger)
}

func contextWithTenant(tenantID uuid.UUID) context.Context {
	return postgres.WithTenantID(context.Background(), tenantID)
}

// ============================================================================
// Unit Tests
// ============================================================================

func TestMaster_BrandLifecycle(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	// 1. Create Brand Happy Path
	brand, err := svc.CreateBrand(ctx, &actorID, master.CreateBrandRequest{
		Code: "NIKE",
		Name: "Nike Athletic",
	})
	if err != nil {
		t.Fatalf("unexpected error creating brand: %v", err)
	}
	if brand.Code != "NIKE" || brand.Name != "Nike Athletic" || brand.Status != master.StatusActive {
		t.Fatalf("unexpected brand state: %+v", brand)
	}

	// 2. Duplicate Code Error
	_, err = svc.CreateBrand(ctx, &actorID, master.CreateBrandRequest{
		Code: "NIKE",
		Name: "Duplicate",
	})
	if !errors.Is(err, master.ErrBrandCodeExists) {
		t.Fatalf("expected ErrBrandCodeExists, got: %v", err)
	}

	// 3. Validation Errors
	_, err = svc.CreateBrand(ctx, &actorID, master.CreateBrandRequest{Code: "", Name: "Valid"})
	if !errors.Is(err, master.ErrBrandCodeRequired) {
		t.Fatalf("expected ErrBrandCodeRequired, got: %v", err)
	}
	_, err = svc.CreateBrand(ctx, &actorID, master.CreateBrandRequest{Code: "CODE", Name: ""})
	if !errors.Is(err, master.ErrBrandNameRequired) {
		t.Fatalf("expected ErrBrandNameRequired, got: %v", err)
	}

	// 4. Update Brand
	newName := "Nike Corporation"
	newStatus := master.StatusInactive
	updated, err := svc.UpdateBrand(ctx, &actorID, brand.ID, master.UpdateBrandRequest{
		Name:   &newName,
		Status: &newStatus,
	})
	if err != nil {
		t.Fatalf("unexpected error updating brand: %v", err)
	}
	if updated.Name != newName || updated.Status != newStatus {
		t.Fatalf("brand update not reflected: %+v", updated)
	}
}

func TestMaster_CategoryHierarchyAndCyclePrevention(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	// 1. Create Root Category
	root, err := svc.CreateCategory(ctx, &actorID, master.CreateCategoryRequest{
		Code: "ROOT",
		Name: "Root Department",
	})
	if err != nil {
		t.Fatalf("unexpected error creating root category: %v", err)
	}

	// 2. Create Child Category
	child, err := svc.CreateCategory(ctx, &actorID, master.CreateCategoryRequest{
		ParentID: &root.ID,
		Code:     "CHILD",
		Name:     "Child Category",
	})
	if err != nil {
		t.Fatalf("unexpected error creating child category: %v", err)
	}

	// 3. Create Grandchild Category
	grandchild, err := svc.CreateCategory(ctx, &actorID, master.CreateCategoryRequest{
		ParentID: &child.ID,
		Code:     "GRANDCHILD",
		Name:     "Grandchild Category",
	})
	if err != nil {
		t.Fatalf("unexpected error creating grandchild category: %v", err)
	}

	// 4. Test Self-Parenting Prevention
	_, err = svc.UpdateCategory(ctx, &actorID, root.ID, master.UpdateCategoryRequest{
		ParentID: &root.ID,
	})
	if !errors.Is(err, master.ErrCategorySelfParent) {
		t.Fatalf("expected ErrCategorySelfParent, got: %v", err)
	}

	// 5. Test Cycle Detection (Making root's parent grandchild)
	_, err = svc.UpdateCategory(ctx, &actorID, root.ID, master.UpdateCategoryRequest{
		ParentID: &grandchild.ID,
	})
	if !errors.Is(err, master.ErrCategoryCycleDetected) {
		t.Fatalf("expected ErrCategoryCycleDetected, got: %v", err)
	}
}

func TestMaster_UOMPrecisionScale(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	// 1. Valid Precision (0..6)
	scale2 := int16(2)
	uom, err := svc.CreateUOM(ctx, &actorID, master.CreateUOMRequest{
		Code:           "KG",
		Name:           "Kilogram",
		PrecisionScale: &scale2,
	})
	if err != nil {
		t.Fatalf("unexpected error creating UOM: %v", err)
	}
	if uom.PrecisionScale != 2 {
		t.Fatalf("expected precision scale 2, got: %d", uom.PrecisionScale)
	}

	// 2. Invalid Precision (> 6)
	scale7 := int16(7)
	_, err = svc.CreateUOM(ctx, &actorID, master.CreateUOMRequest{
		Code:           "BAD",
		Name:           "Bad Scale",
		PrecisionScale: &scale7,
	})
	if !errors.Is(err, master.ErrInvalidUOMPrecision) {
		t.Fatalf("expected ErrInvalidUOMPrecision, got: %v", err)
	}

	// 3. Invalid Precision (< 0)
	scaleNeg := int16(-1)
	_, err = svc.CreateUOM(ctx, &actorID, master.CreateUOMRequest{
		Code:           "NEG",
		Name:           "Neg Scale",
		PrecisionScale: &scaleNeg,
	})
	if !errors.Is(err, master.ErrInvalidUOMPrecision) {
		t.Fatalf("expected ErrInvalidUOMPrecision, got: %v", err)
	}
}

func TestMaster_HybridCompanyOwnership(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	// Create Base UOM
	uomID := uuid.New()
	store.uoms[uomID] = &master.UOM{ID: uomID, TenantID: tenantID, Code: "PCS", Name: "Pieces"}

	validCompanyID := uuid.New()
	store.companies[validCompanyID] = true
	invalidCompanyID := uuid.New() // not in store.companies

	// 1. Tenant-wide product (company_id is nil) - Valid
	p1, err := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:       "PROD-GLOBAL-001",
		Name:      "Tenant Wide Product",
		BaseUOMID: uomID,
	})
	if err != nil {
		t.Fatalf("unexpected error creating tenant-wide product: %v", err)
	}
	if p1.CompanyID != nil {
		t.Fatalf("expected nil company_id, got: %v", p1.CompanyID)
	}

	// 2. Company-specific product (company_id exists in tenant) - Valid
	p2, err := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		CompanyID: &validCompanyID,
		SKU:       "PROD-COMP-001",
		Name:      "Company Specific Product",
		BaseUOMID: uomID,
	})
	if err != nil {
		t.Fatalf("unexpected error creating company-specific product: %v", err)
	}
	if p2.CompanyID == nil || *p2.CompanyID != validCompanyID {
		t.Fatalf("expected company_id %v, got: %v", validCompanyID, p2.CompanyID)
	}

	// 3. Cross-tenant or non-existent company - Fails Closed
	_, err = svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		CompanyID: &invalidCompanyID,
		SKU:       "PROD-FAIL-001",
		Name:      "Invalid Company Product",
		BaseUOMID: uomID,
	})
	if !errors.Is(err, master.ErrCompanyCrossTenant) {
		t.Fatalf("expected ErrCompanyCrossTenant, got: %v", err)
	}
}

func TestMaster_ProductStateTransitions(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	uomID := uuid.New()
	store.uoms[uomID] = &master.UOM{ID: uomID, TenantID: tenantID, Code: "EA", Name: "Each"}

	p, err := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:       "SKU-TRANS-001",
		Name:      "Transition Test Product",
		BaseUOMID: uomID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initial state is ACTIVE
	if p.Status != master.StatusActive {
		t.Fatalf("expected ACTIVE, got %s", p.Status)
	}

	// 1. Deactivate ACTIVE -> INACTIVE
	deactivated, err := svc.DeactivateProduct(ctx, &actorID, p.ID, "End of season")
	if err != nil {
		t.Fatalf("unexpected error deactivating: %v", err)
	}
	if deactivated.Status != master.StatusInactive {
		t.Fatalf("expected INACTIVE, got %s", deactivated.Status)
	}

	// 2. Reactivate INACTIVE -> ACTIVE
	activated, err := svc.ActivateProduct(ctx, &actorID, p.ID)
	if err != nil {
		t.Fatalf("unexpected error activating: %v", err)
	}
	if activated.Status != master.StatusActive {
		t.Fatalf("expected ACTIVE, got %s", activated.Status)
	}

	// 3. Mark DISCONTINUED
	discStatus := master.StatusDiscontinued
	store.products[p.ID].Status = discStatus

	// 4. Try activating DISCONTINUED -> FORBIDDEN
	_, err = svc.ActivateProduct(ctx, &actorID, p.ID)
	if !errors.Is(err, master.ErrProductDiscontinued) {
		t.Fatalf("expected ErrProductDiscontinued, got: %v", err)
	}

	// 5. Try updating DISCONTINUED -> FORBIDDEN
	newName := "New Name"
	_, err = svc.UpdateProduct(ctx, &actorID, p.ID, master.UpdateProductRequest{Name: &newName})
	if !errors.Is(err, master.ErrProductDiscontinued) {
		t.Fatalf("expected ErrProductDiscontinued, got: %v", err)
	}
}

func TestMaster_BarcodePrimaryRulesAndLookup(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	uomID := uuid.New()
	store.uoms[uomID] = &master.UOM{ID: uomID, TenantID: tenantID, Code: "EA", Name: "Each"}

	p, err := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:       "SKU-BC-001",
		Name:      "Barcode Product",
		BaseUOMID: uomID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Add primary barcode
	bc1, err := svc.CreateBarcode(ctx, &actorID, p.ID, master.CreateBarcodeRequest{
		Barcode:   "111111111111",
		IsPrimary: true,
	})
	if err != nil {
		t.Fatalf("unexpected error adding barcode 1: %v", err)
	}
	if !bc1.IsPrimary {
		t.Fatalf("expected bc1 to be primary")
	}

	// 2. Add second primary barcode -> bc1 must lose primary
	bc2, err := svc.CreateBarcode(ctx, &actorID, p.ID, master.CreateBarcodeRequest{
		Barcode:   "222222222222",
		IsPrimary: true,
	})
	if err != nil {
		t.Fatalf("unexpected error adding barcode 2: %v", err)
	}
	if !bc2.IsPrimary {
		t.Fatalf("expected bc2 to be primary")
	}
	if store.barcodes[bc1.ID].IsPrimary {
		t.Fatalf("expected bc1 to no longer be primary")
	}

	// 3. Lookup barcode
	lookup, err := svc.LookupBarcode(ctx, "222222222222")
	if err != nil {
		t.Fatalf("unexpected error looking up barcode: %v", err)
	}
	if lookup.Product.ID != p.ID || lookup.Product.SKU != p.SKU {
		t.Fatalf("barcode lookup returned wrong product: %+v", lookup.Product)
	}
}

func TestMaster_SerialDefinitionAndLookup(t *testing.T) {
	store := newInMemoryStore()
	svc := newTestService(store)
	tenantID := uuid.New()
	ctx := contextWithTenant(tenantID)
	actorID := uuid.New()

	uomID := uuid.New()
	store.uoms[uomID] = &master.UOM{ID: uomID, TenantID: tenantID, Code: "EA", Name: "Each"}

	// Product with TrackSerial = false
	pNoSerial, _ := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:         "NO-SERIAL",
		Name:        "No Serial Product",
		BaseUOMID:   uomID,
		TrackSerial: false,
	})

	// Product with TrackSerial = true
	pSerial, _ := svc.CreateProduct(ctx, &actorID, master.CreateProductRequest{
		SKU:         "WITH-SERIAL",
		Name:        "Serial Tracked Product",
		BaseUOMID:   uomID,
		TrackSerial: true,
	})

	// 1. Defining serial for untracked product fails
	_, err := svc.CreateSerialNumber(ctx, &actorID, pNoSerial.ID, "SN-001", nil)
	if !errors.Is(err, master.ErrSerialTrackingNotEnabled) {
		t.Fatalf("expected ErrSerialTrackingNotEnabled, got: %v", err)
	}

	// 2. Defining serial for tracked product succeeds
	sn, err := svc.CreateSerialNumber(ctx, &actorID, pSerial.ID, "SN-001", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sn.SerialNo != "SN-001" || sn.Status != "RECEIVED" {
		t.Fatalf("unexpected serial state: %+v", sn)
	}

	// 3. Lookup serial by number
	found, err := svc.GetSerialNumber(ctx, "SN-001")
	if err != nil {
		t.Fatalf("unexpected error fetching serial: %v", err)
	}
	if found.ID != sn.ID {
		t.Fatalf("serial lookup mismatch: %+v vs %+v", found, sn)
	}
}
