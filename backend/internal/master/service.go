package master

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// Service defines business operations for the Master Data & Catalog domain.
type Service interface {
	// Brand
	CreateBrand(ctx context.Context, actorID *uuid.UUID, req CreateBrandRequest) (*Brand, error)
	GetBrand(ctx context.Context, id uuid.UUID) (*Brand, error)
	UpdateBrand(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateBrandRequest) (*Brand, error)
	ListBrands(ctx context.Context, limit, offset int) ([]Brand, int64, error)

	// Category
	CreateCategory(ctx context.Context, actorID *uuid.UUID, req CreateCategoryRequest) (*Category, error)
	GetCategory(ctx context.Context, id uuid.UUID) (*Category, error)
	UpdateCategory(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCategoryRequest) (*Category, error)
	ListCategories(ctx context.Context, limit, offset int) ([]Category, int64, error)

	// UOM
	CreateUOM(ctx context.Context, actorID *uuid.UUID, req CreateUOMRequest) (*UOM, error)
	GetUOM(ctx context.Context, id uuid.UUID) (*UOM, error)
	ListUOMs(ctx context.Context, limit, offset int) ([]UOM, int64, error)

	// Product
	CreateProduct(ctx context.Context, actorID *uuid.UUID, req CreateProductRequest) (*Product, error)
	GetProduct(ctx context.Context, id uuid.UUID) (*ProductDetails, error)
	UpdateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateProductRequest) (*Product, error)
	ActivateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID) (*Product, error)
	DeactivateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, reason string) (*Product, error)
	ListProducts(ctx context.Context, limit, offset int) ([]Product, int64, error)

	// Product Variant
	CreateVariant(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req CreateVariantRequest) (*ProductVariant, error)
	ListVariants(ctx context.Context, productID uuid.UUID) ([]ProductVariant, error)

	// Barcode
	CreateBarcode(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req CreateBarcodeRequest) (*ProductBarcode, error)
	ListBarcodes(ctx context.Context, productID uuid.UUID, limit, offset int) ([]ProductBarcode, int64, error)
	LookupBarcode(ctx context.Context, barcode string) (*ProductDetails, error)

	// Serial Number (DEC-B1-011 catalog definition and lookup only)
	CreateSerialNumber(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, serialNo string, variantID *uuid.UUID) (*SerialNumber, error)
	GetSerialNumber(ctx context.Context, serialNo string) (*SerialNumber, error)
	ListSerialNumbers(ctx context.Context, productID uuid.UUID, limit, offset int) ([]SerialNumber, int64, error)
}

// DomainService implements Service.
type DomainService struct {
	store        Store
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	logger       *slog.Logger
}

// NewService constructs a new Master Data DomainService.
func NewService(
	store Store,
	txManager postgres.TxManager,
	auditStore audit.Store,
	outboxWriter outbox.Writer,
	logger *slog.Logger,
) *DomainService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DomainService{
		store:        store,
		txManager:    txManager,
		auditStore:   auditStore,
		outboxWriter: outboxWriter,
		logger:       logger,
	}
}

// ============================================================================
// Brand
// ============================================================================

// CreateBrand creates a new product brand.
func (s *DomainService) CreateBrand(ctx context.Context, actorID *uuid.UUID, req CreateBrandRequest) (*Brand, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, ErrBrandCodeRequired
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrBrandNameRequired
	}

	b := &Brand{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		Code:      req.Code,
		Name:      req.Name,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreateBrand(txCtx, b); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code": b.Code,
				"name": b.Name,
			})
			resType := "brand"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     b.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionBrandCreated,
				ResourceType: &resType,
				ResourceID:   &b.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return b, nil
}

// GetBrand retrieves a brand by ID.
func (s *DomainService) GetBrand(ctx context.Context, id uuid.UUID) (*Brand, error) {
	var b *Brand
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		b, err = s.store.GetBrandByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// UpdateBrand updates a brand.
func (s *DomainService) UpdateBrand(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateBrandRequest) (*Brand, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	var b *Brand
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		b, err = s.store.GetBrandByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return ErrBrandNameRequired
			}
			b.Name = name
		}
		if req.Status != nil {
			st := strings.ToUpper(strings.TrimSpace(*req.Status))
			if st != StatusActive && st != StatusInactive {
				return ErrInvalidStatus
			}
			b.Status = st
		}

		if err := s.store.UpdateBrand(txCtx, b); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"name":   b.Name,
				"status": b.Status,
			})
			resType := "brand"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     b.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionBrandUpdated,
				ResourceType: &resType,
				ResourceID:   &b.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListBrands lists brands.
func (s *DomainService) ListBrands(ctx context.Context, limit, offset int) ([]Brand, int64, error) {
	var items []Brand
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, total, err = s.store.ListBrands(txCtx, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ============================================================================
// Category
// ============================================================================

// CreateCategory creates a new product category.
func (s *DomainService) CreateCategory(ctx context.Context, actorID *uuid.UUID, req CreateCategoryRequest) (*Category, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, ErrCategoryCodeRequired
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrCategoryNameRequired
	}

	c := &Category{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		ParentID:  req.ParentID,
		Code:      req.Code,
		Name:      req.Name,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if c.ParentID != nil {
			if _, err := s.store.GetCategoryByID(txCtx, *c.ParentID); err != nil {
				return err
			}
		}

		if err := s.store.CreateCategory(txCtx, c); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":      c.Code,
				"name":      c.Name,
				"parent_id": c.ParentID,
			})
			resType := "category"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionCategoryCreated,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCategory retrieves a category by ID.
func (s *DomainService) GetCategory(ctx context.Context, id uuid.UUID) (*Category, error) {
	var c *Category
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		c, err = s.store.GetCategoryByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateCategory updates a category, validating parent hierarchy to prevent cycles.
func (s *DomainService) UpdateCategory(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCategoryRequest) (*Category, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	var c *Category
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		c, err = s.store.GetCategoryByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return ErrCategoryNameRequired
			}
			c.Name = name
		}
		if req.Status != nil {
			st := strings.ToUpper(strings.TrimSpace(*req.Status))
			if st != StatusActive && st != StatusInactive {
				return ErrInvalidStatus
			}
			c.Status = st
		}
		if req.ParentID != nil {
			newParentID := *req.ParentID
			if newParentID == id {
				return ErrCategorySelfParent
			}

			// Validate cycle by traversing ancestors
			curr := newParentID
			for {
				if curr == id {
					return ErrCategoryCycleDetected
				}
				parentCat, err := s.store.GetCategoryByID(txCtx, curr)
				if err != nil {
					return err
				}
				if parentCat.ParentID == nil {
					break
				}
				curr = *parentCat.ParentID
			}
			c.ParentID = &newParentID
		}

		if err := s.store.UpdateCategory(txCtx, c); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"name":      c.Name,
				"parent_id": c.ParentID,
				"status":    c.Status,
			})
			resType := "category"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionCategoryUpdated,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListCategories lists categories.
func (s *DomainService) ListCategories(ctx context.Context, limit, offset int) ([]Category, int64, error) {
	var items []Category
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, total, err = s.store.ListCategories(txCtx, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ============================================================================
// UOM
// ============================================================================

// CreateUOM creates a unit of measure.
func (s *DomainService) CreateUOM(ctx context.Context, actorID *uuid.UUID, req CreateUOMRequest) (*UOM, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, ErrUOMCodeRequired
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrUOMNameRequired
	}

	var scale int16
	if req.PrecisionScale != nil {
		scale = *req.PrecisionScale
		if scale < 0 || scale > 6 {
			return nil, ErrInvalidUOMPrecision
		}
	}

	u := &UOM{
		ID:             uuid.New(),
		TenantID:       pc.TenantID,
		Code:           req.Code,
		Name:           req.Name,
		PrecisionScale: scale,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreateUOM(txCtx, u); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":            u.Code,
				"name":            u.Name,
				"precision_scale": u.PrecisionScale,
			})
			resType := "uom"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     u.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionUOMCreated,
				ResourceType: &resType,
				ResourceID:   &u.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUOM retrieves a UOM by ID.
func (s *DomainService) GetUOM(ctx context.Context, id uuid.UUID) (*UOM, error) {
	var u *UOM
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		u, err = s.store.GetUOMByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUOMs lists UOMs.
func (s *DomainService) ListUOMs(ctx context.Context, limit, offset int) ([]UOM, int64, error) {
	var items []UOM
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, total, err = s.store.ListUOMs(txCtx, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ============================================================================
// Product
// ============================================================================

// CreateProduct creates a new product aggregate.
func (s *DomainService) CreateProduct(ctx context.Context, actorID *uuid.UUID, req CreateProductRequest) (*Product, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.SKU = strings.TrimSpace(req.SKU)
	if req.SKU == "" {
		return nil, ErrProductSKURequired
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrProductNameRequired
	}
	if req.BaseUOMID == uuid.Nil && req.UOMID != nil {
		req.BaseUOMID = *req.UOMID
	}
	if req.BaseUOMID == uuid.Nil {
		return nil, ErrProductBaseUOMRequired
	}

	pType := strings.ToUpper(strings.TrimSpace(req.ProductType))
	if pType == "" {
		pType = ProductTypeStockable
	}
	switch pType {
	case ProductTypeStockable, ProductTypeService, ProductTypeConsumable, ProductTypeBundle:
	default:
		return nil, ErrInvalidProductType
	}

	p := &Product{
		ID:          uuid.New(),
		TenantID:    pc.TenantID,
		CompanyID:   req.CompanyID,
		SKU:         req.SKU,
		Name:        req.Name,
		BrandID:     req.BrandID,
		CategoryID:  req.CategoryID,
		BaseUOMID:   req.BaseUOMID,
		ProductType: pType,
		TrackSerial: req.TrackSerial,
		TrackLot:    req.TrackLot,
		TaxCategory: req.TaxCategory,
		Status:      StatusActive,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		CreatedBy:   actorID,
		UpdatedBy:   actorID,
		Version:     1,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// DEC-B1-010: Validate company if specified
		if p.CompanyID != nil {
			exists, err := s.store.ValidateCompany(txCtx, *p.CompanyID)
			if err != nil {
				return err
			}
			if !exists {
				return ErrCompanyCrossTenant
			}
		}

		// Validate references
		if p.BrandID != nil {
			if _, err := s.store.GetBrandByID(txCtx, *p.BrandID); err != nil {
				return err
			}
		}
		if p.CategoryID != nil {
			if _, err := s.store.GetCategoryByID(txCtx, *p.CategoryID); err != nil {
				return err
			}
		}
		if _, err := s.store.GetUOMByID(txCtx, p.BaseUOMID); err != nil {
			return err
		}

		if err := s.store.CreateProduct(txCtx, p); err != nil {
			return err
		}

		for _, vr := range req.Variants {
			vSKU := strings.TrimSpace(vr.SKU)
			vName := strings.TrimSpace(vr.Name)
			if vSKU == "" || vName == "" {
				continue
			}
			v := &ProductVariant{
				ID:         uuid.New(),
				TenantID:   p.TenantID,
				ProductID:  p.ID,
				SKU:        vSKU,
				Name:       vName,
				Attributes: vr.Attributes,
				Status:     StatusActive,
				CreatedAt:  time.Now().UTC(),
				UpdatedAt:  time.Now().UTC(),
			}
			if err := s.store.CreateVariant(txCtx, v); err != nil {
				return err
			}
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"sku":          p.SKU,
				"name":         p.Name,
				"product_type": p.ProductType,
				"company_id":   p.CompanyID,
			})
			resType := "product"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionProductCreated,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"product_id":   p.ID,
				"sku":          p.SKU,
				"name":         p.Name,
				"product_type": p.ProductType,
				"company_id":   p.CompanyID,
			}
			if evt, err := outbox.NewEvent(p.TenantID, "product", p.ID, EventProductCreated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProduct retrieves a product and its associated variants and barcodes.
func (s *DomainService) GetProduct(ctx context.Context, id uuid.UUID) (*ProductDetails, error) {
	var details ProductDetails
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetProductByID(txCtx, id)
		if err != nil {
			return err
		}
		details.Product = *p

		variants, err := s.store.ListVariantsByProductID(txCtx, id)
		if err != nil {
			return err
		}
		details.Variants = variants

		barcodes, _, err := s.store.ListBarcodesByProductID(txCtx, id, 100, 0)
		if err != nil {
			return err
		}
		details.Barcodes = barcodes

		return nil
	})
	if err != nil {
		return nil, err
	}
	return &details, nil
}

// UpdateProduct updates product metadata.
func (s *DomainService) UpdateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateProductRequest) (*Product, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	var p *Product
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		p, err = s.store.GetProductByID(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status == StatusDiscontinued {
			return ErrProductDiscontinued
		}

		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return ErrProductNameRequired
			}
			p.Name = name
		}
		if req.BrandID != nil {
			if _, err := s.store.GetBrandByID(txCtx, *req.BrandID); err != nil {
				return err
			}
			p.BrandID = req.BrandID
		}
		if req.CategoryID != nil {
			if _, err := s.store.GetCategoryByID(txCtx, *req.CategoryID); err != nil {
				return err
			}
			p.CategoryID = req.CategoryID
		}
		if req.BaseUOMID == nil && req.UOMID != nil {
			req.BaseUOMID = req.UOMID
		}
		if req.BaseUOMID != nil {
			if _, err := s.store.GetUOMByID(txCtx, *req.BaseUOMID); err != nil {
				return err
			}
			p.BaseUOMID = *req.BaseUOMID
		}
		if req.ProductType != nil {
			pType := strings.ToUpper(strings.TrimSpace(*req.ProductType))
			switch pType {
			case ProductTypeStockable, ProductTypeService, ProductTypeConsumable, ProductTypeBundle:
				p.ProductType = pType
			default:
				return ErrInvalidProductType
			}
		}
		if req.TaxCategory != nil {
			p.TaxCategory = req.TaxCategory
		}

		p.UpdatedBy = actorID

		if err := s.store.UpdateProduct(txCtx, p); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"sku":          p.SKU,
				"name":         p.Name,
				"product_type": p.ProductType,
				"version":      p.Version,
			})
			resType := "product"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionProductUpdated,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"product_id": p.ID,
				"sku":        p.SKU,
				"name":       p.Name,
				"version":    p.Version,
			}
			if evt, err := outbox.NewEvent(p.TenantID, "product", p.ID, EventProductUpdated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return p, nil
}

// ActivateProduct transitions a product from INACTIVE to ACTIVE.
func (s *DomainService) ActivateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID) (*Product, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	var p *Product
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		p, err = s.store.GetProductByID(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status == StatusDiscontinued {
			return ErrProductDiscontinued
		}
		if p.Status == StatusActive {
			return nil
		}

		p.Status = StatusActive
		p.UpdatedBy = actorID

		if err := s.store.UpdateProduct(txCtx, p); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"action": "activate",
				"sku":    p.SKU,
			})
			resType := "product"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionProductActivated,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"product_id": p.ID,
				"sku":        p.SKU,
				"status":     p.Status,
			}
			if evt, err := outbox.NewEvent(p.TenantID, "product", p.ID, EventProductActivated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return p, nil
}

// DeactivateProduct transitions a product from ACTIVE to INACTIVE.
func (s *DomainService) DeactivateProduct(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, reason string) (*Product, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	var p *Product
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		p, err = s.store.GetProductByID(txCtx, id)
		if err != nil {
			return err
		}

		if p.Status == StatusDiscontinued {
			return ErrProductDiscontinued
		}
		if p.Status == StatusInactive {
			return nil
		}

		p.Status = StatusInactive
		p.UpdatedBy = actorID

		if err := s.store.UpdateProduct(txCtx, p); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"action": "deactivate",
				"sku":    p.SKU,
				"reason": reason,
			})
			resType := "product"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     p.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionProductDeactivated,
				ResourceType: &resType,
				ResourceID:   &p.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"product_id": p.ID,
				"sku":        p.SKU,
				"status":     p.Status,
				"reason":     reason,
			}
			if evt, err := outbox.NewEvent(p.TenantID, "product", p.ID, EventProductDeactivated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListProducts lists products.
func (s *DomainService) ListProducts(ctx context.Context, limit, offset int) ([]Product, int64, error) {
	var items []Product
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, total, err = s.store.ListProducts(txCtx, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ============================================================================
// Product Variant
// ============================================================================

// CreateVariant creates a product variant.
func (s *DomainService) CreateVariant(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req CreateVariantRequest) (*ProductVariant, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.SKU = strings.TrimSpace(req.SKU)
	if req.SKU == "" {
		return nil, ErrVariantSKURequired
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrVariantNameRequired
	}

	v := &ProductVariant{
		ID:         uuid.New(),
		TenantID:   pc.TenantID,
		ProductID:  productID,
		SKU:        req.SKU,
		Name:       req.Name,
		Attributes: req.Attributes,
		Status:     StatusActive,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetProductByID(txCtx, productID)
		if err != nil {
			return err
		}
		if p.Status == StatusDiscontinued {
			return ErrProductDiscontinued
		}

		if err := s.store.CreateVariant(txCtx, v); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"product_id": productID,
				"sku":        v.SKU,
				"name":       v.Name,
			})
			resType := "product_variant"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     v.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionVariantCreated,
				ResourceType: &resType,
				ResourceID:   &v.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"variant_id": v.ID,
				"product_id": productID,
				"sku":        v.SKU,
			}
			if evt, err := outbox.NewEvent(v.TenantID, "product_variant", v.ID, EventVariantCreated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return v, nil
}

// ListVariants lists variants for a product.
func (s *DomainService) ListVariants(ctx context.Context, productID uuid.UUID) ([]ProductVariant, error) {
	var items []ProductVariant
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, err = s.store.ListVariantsByProductID(txCtx, productID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ============================================================================
// Barcode
// ============================================================================

// CreateBarcode creates a barcode mapping.
func (s *DomainService) CreateBarcode(ctx context.Context, actorID *uuid.UUID, productID uuid.UUID, req CreateBarcodeRequest) (*ProductBarcode, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Barcode = strings.TrimSpace(req.Barcode)
	if req.Barcode == "" {
		return nil, ErrBarcodeRequired
	}
	bType := strings.ToUpper(strings.TrimSpace(req.BarcodeType))
	if bType == "" {
		bType = "EAN"
	}

	b := &ProductBarcode{
		ID:          uuid.New(),
		TenantID:    pc.TenantID,
		ProductID:   productID,
		VariantID:   req.VariantID,
		Barcode:     req.Barcode,
		BarcodeType: bType,
		IsPrimary:   req.IsPrimary,
		Status:      StatusActive,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetProductByID(txCtx, productID)
		if err != nil {
			return err
		}
		if p.Status == StatusDiscontinued {
			return ErrProductDiscontinued
		}

		if req.VariantID != nil {
			v, err := s.store.GetVariantByID(txCtx, *req.VariantID)
			if err != nil {
				return err
			}
			if v.ProductID != productID {
				return ErrVariantNotFound
			}
		}

		if b.IsPrimary {
			if err := s.store.ClearPrimaryBarcode(txCtx, productID, req.VariantID); err != nil {
				return err
			}
		}

		if err := s.store.CreateBarcode(txCtx, b); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"product_id":   productID,
				"variant_id":   req.VariantID,
				"barcode":      b.Barcode,
				"barcode_type": b.BarcodeType,
				"is_primary":   b.IsPrimary,
			})
			resType := "product_barcode"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     b.TenantID,
				UserID:       actorID,
				Module:       "master",
				Action:       AuditActionBarcodeCreated,
				ResourceType: &resType,
				ResourceID:   &b.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"barcode_id": b.ID,
				"product_id": productID,
				"variant_id": req.VariantID,
				"barcode":    b.Barcode,
			}
			if evt, err := outbox.NewEvent(b.TenantID, "product_barcode", b.ID, EventBarcodeAssigned, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListBarcodes lists barcodes for a product.
func (s *DomainService) ListBarcodes(ctx context.Context, productID uuid.UUID, limit, offset int) ([]ProductBarcode, int64, error) {
	var items []ProductBarcode
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, total, err = s.store.ListBarcodesByProductID(txCtx, productID, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// LookupBarcode looks up a product by scanning its barcode.
func (s *DomainService) LookupBarcode(ctx context.Context, barcode string) (*ProductDetails, error) {
	barcode = strings.TrimSpace(barcode)
	if barcode == "" {
		return nil, ErrBarcodeRequired
	}

	var details ProductDetails
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		b, err := s.store.GetBarcodeByValue(txCtx, barcode)
		if err != nil {
			return err
		}

		p, err := s.store.GetProductByID(txCtx, b.ProductID)
		if err != nil {
			return err
		}
		details.Product = *p

		variants, err := s.store.ListVariantsByProductID(txCtx, p.ID)
		if err != nil {
			return err
		}
		details.Variants = variants

		barcodes, _, err := s.store.ListBarcodesByProductID(txCtx, p.ID, 100, 0)
		if err != nil {
			return err
		}
		details.Barcodes = barcodes

		return nil
	})

	if err != nil {
		return nil, err
	}
	return &details, nil
}

// ============================================================================
// Serial Number (DEC-B1-011)
// ============================================================================

// CreateSerialNumber defines an individual serial number unit in catalog.
func (s *DomainService) CreateSerialNumber(ctx context.Context, _ *uuid.UUID, productID uuid.UUID, serialNo string, variantID *uuid.UUID) (*SerialNumber, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	serialNo = strings.TrimSpace(serialNo)
	if serialNo == "" {
		return nil, ErrSerialNumberRequired
	}

	sn := &SerialNumber{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		ProductID: productID,
		VariantID: variantID,
		SerialNo:  serialNo,
		Status:    "RECEIVED",
		CreatedAt: time.Now().UTC(),
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		p, err := s.store.GetProductByID(txCtx, productID)
		if err != nil {
			return err
		}
		if !p.TrackSerial {
			return ErrSerialTrackingNotEnabled
		}

		if variantID != nil {
			v, err := s.store.GetVariantByID(txCtx, *variantID)
			if err != nil {
				return err
			}
			if v.ProductID != productID {
				return ErrVariantNotFound
			}
		}

		return s.store.CreateSerialNumber(txCtx, sn)
	})

	if err != nil {
		return nil, err
	}
	return sn, nil
}

// GetSerialNumber fetches a serial number by its serial number string.
func (s *DomainService) GetSerialNumber(ctx context.Context, serialNo string) (*SerialNumber, error) {
	serialNo = strings.TrimSpace(serialNo)
	if serialNo == "" {
		return nil, ErrSerialNumberRequired
	}

	var sn *SerialNumber
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		sn, err = s.store.GetSerialNumberByNo(txCtx, serialNo)
		return err
	})
	if err != nil {
		return nil, err
	}
	return sn, nil
}

// ListSerialNumbers lists serial numbers for a product.
func (s *DomainService) ListSerialNumbers(ctx context.Context, productID uuid.UUID, limit, offset int) ([]SerialNumber, int64, error) {
	var items []SerialNumber
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		items, total, err = s.store.ListSerialNumbersByProductID(txCtx, productID, limit, offset)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
