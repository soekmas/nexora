package master

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

// Store defines persistence operations for the Master Data & Catalog domain.
type Store interface {
	// Brand operations
	CreateBrand(ctx context.Context, b *Brand) error
	GetBrandByID(ctx context.Context, id uuid.UUID) (*Brand, error)
	GetBrandByCode(ctx context.Context, code string) (*Brand, error)
	UpdateBrand(ctx context.Context, b *Brand) error
	ListBrands(ctx context.Context, limit, offset int) ([]Brand, int64, error)

	// Category operations
	CreateCategory(ctx context.Context, c *Category) error
	GetCategoryByID(ctx context.Context, id uuid.UUID) (*Category, error)
	GetCategoryByCode(ctx context.Context, code string) (*Category, error)
	UpdateCategory(ctx context.Context, c *Category) error
	ListCategories(ctx context.Context, limit, offset int) ([]Category, int64, error)

	// UOM operations
	CreateUOM(ctx context.Context, u *UOM) error
	GetUOMByID(ctx context.Context, id uuid.UUID) (*UOM, error)
	GetUOMByCode(ctx context.Context, code string) (*UOM, error)
	ListUOMs(ctx context.Context, limit, offset int) ([]UOM, int64, error)

	// Product operations
	CreateProduct(ctx context.Context, p *Product) error
	GetProductByID(ctx context.Context, id uuid.UUID) (*Product, error)
	GetProductBySKU(ctx context.Context, sku string) (*Product, error)
	UpdateProduct(ctx context.Context, p *Product) error
	ListProducts(ctx context.Context, limit, offset int) ([]Product, int64, error)

	// Product Variant operations
	CreateVariant(ctx context.Context, v *ProductVariant) error
	GetVariantByID(ctx context.Context, id uuid.UUID) (*ProductVariant, error)
	GetVariantBySKU(ctx context.Context, sku string) (*ProductVariant, error)
	ListVariantsByProductID(ctx context.Context, productID uuid.UUID) ([]ProductVariant, error)

	// Barcode operations
	CreateBarcode(ctx context.Context, b *ProductBarcode) error
	GetBarcodeByID(ctx context.Context, id uuid.UUID) (*ProductBarcode, error)
	GetBarcodeByValue(ctx context.Context, barcode string) (*ProductBarcode, error)
	ListBarcodesByProductID(ctx context.Context, productID uuid.UUID, limit, offset int) ([]ProductBarcode, int64, error)
	ClearPrimaryBarcode(ctx context.Context, productID uuid.UUID, variantID *uuid.UUID) error

	// Serial Number operations
	CreateSerialNumber(ctx context.Context, s *SerialNumber) error
	GetSerialNumberByID(ctx context.Context, id uuid.UUID) (*SerialNumber, error)
	GetSerialNumberByNo(ctx context.Context, serialNo string) (*SerialNumber, error)
	ListSerialNumbersByProductID(ctx context.Context, productID uuid.UUID, limit, offset int) ([]SerialNumber, int64, error)

	// Company validation (DEC-B1-010)
	ValidateCompany(ctx context.Context, companyID uuid.UUID) (bool, error)
}

// PgxStore implements MasterStore using PostgreSQL and pgxpool.
type PgxStore struct {
	pool *pgxpool.Pool
}

// NewPgxStore constructs a new PgxStore.
func NewPgxStore(pool *pgxpool.Pool) *PgxStore {
	return &PgxStore{pool: pool}
}

func (s *PgxStore) exec(ctx context.Context) postgres.DBTX {
	return postgres.GetDBTX(ctx, s.pool)
}

// ============================================================================
// Brand
// ============================================================================

// CreateBrand inserts a brand record.
func (s *PgxStore) CreateBrand(ctx context.Context, b *Brand) error {
	if b == nil {
		return errors.New("master: brand cannot be nil")
	}
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	if b.Status == "" {
		b.Status = StatusActive
	}

	const query = `
		INSERT INTO master.brand (
			id, tenant_id, code, name, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		) RETURNING id, created_at, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		b.ID, b.TenantID, b.Code, b.Name, b.Status, b.CreatedAt, b.UpdatedAt,
	).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrBrandCodeExists
		}
		return fmt.Errorf("create brand: %w", err)
	}
	return nil
}

// GetBrandByID fetches a brand by ID within the current tenant.
func (s *PgxStore) GetBrandByID(ctx context.Context, id uuid.UUID) (*Brand, error) {
	const query = `
		SELECT id, tenant_id, code, name, status, created_at, updated_at
		FROM master.brand
		WHERE id = $1`

	var b Brand
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&b.ID, &b.TenantID, &b.Code, &b.Name, &b.Status, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBrandNotFound
		}
		return nil, fmt.Errorf("get brand by id: %w", err)
	}
	return &b, nil
}

// GetBrandByCode fetches a brand by code within the current tenant.
func (s *PgxStore) GetBrandByCode(ctx context.Context, code string) (*Brand, error) {
	const query = `
		SELECT id, tenant_id, code, name, status, created_at, updated_at
		FROM master.brand
		WHERE code = $1`

	var b Brand
	err := s.exec(ctx).QueryRow(ctx, query, code).Scan(
		&b.ID, &b.TenantID, &b.Code, &b.Name, &b.Status, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBrandNotFound
		}
		return nil, fmt.Errorf("get brand by code: %w", err)
	}
	return &b, nil
}

// UpdateBrand updates a brand record.
func (s *PgxStore) UpdateBrand(ctx context.Context, b *Brand) error {
	if b == nil {
		return errors.New("master: brand cannot be nil")
	}

	const query = `
		UPDATE master.brand
		SET name = $1, status = $2, updated_at = now()
		WHERE id = $3
		RETURNING updated_at`

	err := s.exec(ctx).QueryRow(ctx, query, b.Name, b.Status, b.ID).Scan(&b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBrandNotFound
		}
		return fmt.Errorf("update brand: %w", err)
	}
	return nil
}

// ListBrands returns a paginated list of brands.
func (s *PgxStore) ListBrands(ctx context.Context, limit, offset int) ([]Brand, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM master.brand`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count brands: %w", err)
	}

	const query = `
		SELECT id, tenant_id, code, name, status, created_at, updated_at
		FROM master.brand
		ORDER BY code ASC
		LIMIT $1 OFFSET $2`

	rows, err := s.exec(ctx).Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list brands: %w", err)
	}
	defer rows.Close()

	var items []Brand
	for rows.Next() {
		var b Brand
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.Code, &b.Name, &b.Status, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan brand: %w", err)
		}
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate brands: %w", err)
	}

	return items, total, nil
}

// ============================================================================
// Category
// ============================================================================

// CreateCategory inserts a category record.
func (s *PgxStore) CreateCategory(ctx context.Context, c *Category) error {
	if c == nil {
		return errors.New("master: category cannot be nil")
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = StatusActive
	}

	const query = `
		INSERT INTO master.category (
			id, tenant_id, parent_id, code, name, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		) RETURNING id, created_at, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		c.ID, c.TenantID, c.ParentID, c.Code, c.Name, c.Status, c.CreatedAt, c.UpdatedAt,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrCategoryCodeExists
			}
			if pgErr.Code == "23503" {
				return ErrCategoryNotFound
			}
		}
		return fmt.Errorf("create category: %w", err)
	}
	return nil
}

// GetCategoryByID fetches a category by ID within the current tenant.
func (s *PgxStore) GetCategoryByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	const query = `
		SELECT id, tenant_id, parent_id, code, name, status, created_at, updated_at
		FROM master.category
		WHERE id = $1`

	var c Category
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&c.ID, &c.TenantID, &c.ParentID, &c.Code, &c.Name, &c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCategoryNotFound
		}
		return nil, fmt.Errorf("get category by id: %w", err)
	}
	return &c, nil
}

// GetCategoryByCode fetches a category by code within the current tenant.
func (s *PgxStore) GetCategoryByCode(ctx context.Context, code string) (*Category, error) {
	const query = `
		SELECT id, tenant_id, parent_id, code, name, status, created_at, updated_at
		FROM master.category
		WHERE code = $1`

	var c Category
	err := s.exec(ctx).QueryRow(ctx, query, code).Scan(
		&c.ID, &c.TenantID, &c.ParentID, &c.Code, &c.Name, &c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCategoryNotFound
		}
		return nil, fmt.Errorf("get category by code: %w", err)
	}
	return &c, nil
}

// UpdateCategory updates a category record.
func (s *PgxStore) UpdateCategory(ctx context.Context, c *Category) error {
	if c == nil {
		return errors.New("master: category cannot be nil")
	}

	const query = `
		UPDATE master.category
		SET parent_id = $1, name = $2, status = $3, updated_at = now()
		WHERE id = $4
		RETURNING updated_at`

	err := s.exec(ctx).QueryRow(ctx, query, c.ParentID, c.Name, c.Status, c.ID).Scan(&c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCategoryNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrCategoryNotFound
		}
		return fmt.Errorf("update category: %w", err)
	}
	return nil
}

// ListCategories returns a paginated list of categories.
func (s *PgxStore) ListCategories(ctx context.Context, limit, offset int) ([]Category, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM master.category`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count categories: %w", err)
	}

	const query = `
		SELECT id, tenant_id, parent_id, code, name, status, created_at, updated_at
		FROM master.category
		ORDER BY code ASC
		LIMIT $1 OFFSET $2`

	rows, err := s.exec(ctx).Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var items []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.ParentID, &c.Code, &c.Name, &c.Status, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan category: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate categories: %w", err)
	}

	return items, total, nil
}

// ============================================================================
// UOM
// ============================================================================

// CreateUOM inserts a unit of measure record.
func (s *PgxStore) CreateUOM(ctx context.Context, u *UOM) error {
	if u == nil {
		return errors.New("master: uom cannot be nil")
	}
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}

	const query = `
		INSERT INTO master.uom (
			id, tenant_id, code, name, precision_scale
		) VALUES (
			$1, $2, $3, $4, $5
		) RETURNING id`

	err := s.exec(ctx).QueryRow(ctx, query,
		u.ID, u.TenantID, u.Code, u.Name, u.PrecisionScale,
	).Scan(&u.ID)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrUOMCodeExists
			}
			if pgErr.Code == "23514" {
				return ErrInvalidUOMPrecision
			}
		}
		return fmt.Errorf("create uom: %w", err)
	}
	return nil
}

// GetUOMByID fetches a UOM by ID within the current tenant.
func (s *PgxStore) GetUOMByID(ctx context.Context, id uuid.UUID) (*UOM, error) {
	const query = `
		SELECT id, tenant_id, code, name, precision_scale
		FROM master.uom
		WHERE id = $1`

	var u UOM
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&u.ID, &u.TenantID, &u.Code, &u.Name, &u.PrecisionScale,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUOMNotFound
		}
		return nil, fmt.Errorf("get uom by id: %w", err)
	}
	return &u, nil
}

// GetUOMByCode fetches a UOM by code within the current tenant.
func (s *PgxStore) GetUOMByCode(ctx context.Context, code string) (*UOM, error) {
	const query = `
		SELECT id, tenant_id, code, name, precision_scale
		FROM master.uom
		WHERE code = $1`

	var u UOM
	err := s.exec(ctx).QueryRow(ctx, query, code).Scan(
		&u.ID, &u.TenantID, &u.Code, &u.Name, &u.PrecisionScale,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUOMNotFound
		}
		return nil, fmt.Errorf("get uom by code: %w", err)
	}
	return &u, nil
}

// ListUOMs returns a paginated list of UOMs.
func (s *PgxStore) ListUOMs(ctx context.Context, limit, offset int) ([]UOM, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM master.uom`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count uoms: %w", err)
	}

	const query = `
		SELECT id, tenant_id, code, name, precision_scale
		FROM master.uom
		ORDER BY code ASC
		LIMIT $1 OFFSET $2`

	rows, err := s.exec(ctx).Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list uoms: %w", err)
	}
	defer rows.Close()

	var items []UOM
	for rows.Next() {
		var u UOM
		if err := rows.Scan(
			&u.ID, &u.TenantID, &u.Code, &u.Name, &u.PrecisionScale,
		); err != nil {
			return nil, 0, fmt.Errorf("scan uom: %w", err)
		}
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate uoms: %w", err)
	}

	return items, total, nil
}

// ============================================================================
// Product
// ============================================================================

// CreateProduct inserts a product record.
func (s *PgxStore) CreateProduct(ctx context.Context, p *Product) error {
	if p == nil {
		return errors.New("master: product cannot be nil")
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Version == 0 {
		p.Version = 1
	}
	if p.Status == "" {
		p.Status = StatusActive
	}
	if p.ProductType == "" {
		p.ProductType = ProductTypeStockable
	}

	const query = `
		INSERT INTO master.product (
			id, tenant_id, company_id, sku, name, brand_id, category_id,
			base_uom_id, product_type, track_serial, track_lot, tax_category,
			status, created_at, updated_at, created_by, updated_by, version
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		) RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		p.ID, p.TenantID, p.CompanyID, p.SKU, p.Name, p.BrandID, p.CategoryID,
		p.BaseUOMID, p.ProductType, p.TrackSerial, p.TrackLot, p.TaxCategory,
		p.Status, p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy, p.Version,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrProductSKUExists
			}
			if pgErr.Code == "23503" {
				switch pgErr.ConstraintName {
				case "product_brand_id_fkey":
					return ErrBrandNotFound
				case "product_category_id_fkey":
					return ErrCategoryNotFound
				case "product_base_uom_id_fkey":
					return ErrUOMNotFound
				case "product_company_id_fkey":
					return ErrCompanyCrossTenant
				}
			}
		}
		return fmt.Errorf("create product: %w", err)
	}
	return nil
}

// GetProductByID fetches a product by ID within the current tenant.
func (s *PgxStore) GetProductByID(ctx context.Context, id uuid.UUID) (*Product, error) {
	const query = `
		SELECT id, tenant_id, company_id, sku, name, brand_id, category_id,
		       base_uom_id, product_type, track_serial, track_lot, tax_category,
		       status, created_at, updated_at, created_by, updated_by, version
		FROM master.product
		WHERE id = $1`

	var p Product
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&p.ID, &p.TenantID, &p.CompanyID, &p.SKU, &p.Name, &p.BrandID, &p.CategoryID,
		&p.BaseUOMID, &p.ProductType, &p.TrackSerial, &p.TrackLot, &p.TaxCategory,
		&p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, fmt.Errorf("get product by id: %w", err)
	}
	return &p, nil
}

// GetProductBySKU fetches a product by SKU within the current tenant.
func (s *PgxStore) GetProductBySKU(ctx context.Context, sku string) (*Product, error) {
	const query = `
		SELECT id, tenant_id, company_id, sku, name, brand_id, category_id,
		       base_uom_id, product_type, track_serial, track_lot, tax_category,
		       status, created_at, updated_at, created_by, updated_by, version
		FROM master.product
		WHERE sku = $1`

	var p Product
	err := s.exec(ctx).QueryRow(ctx, query, sku).Scan(
		&p.ID, &p.TenantID, &p.CompanyID, &p.SKU, &p.Name, &p.BrandID, &p.CategoryID,
		&p.BaseUOMID, &p.ProductType, &p.TrackSerial, &p.TrackLot, &p.TaxCategory,
		&p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, fmt.Errorf("get product by sku: %w", err)
	}
	return &p, nil
}

// UpdateProduct updates a product with optimistic locking.
func (s *PgxStore) UpdateProduct(ctx context.Context, p *Product) error {
	if p == nil {
		return errors.New("master: product cannot be nil")
	}

	const query = `
		UPDATE master.product
		SET name = $1, brand_id = $2, category_id = $3,
		    base_uom_id = $4, product_type = $5, tax_category = $6,
		    status = $7, updated_by = $8, updated_at = now(), version = version + 1
		WHERE id = $9 AND version = $10
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		p.Name, p.BrandID, p.CategoryID, p.BaseUOMID, p.ProductType,
		p.TaxCategory, p.Status, p.UpdatedBy, p.ID, p.Version,
	).Scan(&p.Version, &p.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := s.GetProductByID(ctx, p.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != p.Version {
				return ErrOptimisticLockConflict
			}
			return ErrProductNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			switch pgErr.ConstraintName {
			case "product_brand_id_fkey":
				return ErrBrandNotFound
			case "product_category_id_fkey":
				return ErrCategoryNotFound
			case "product_base_uom_id_fkey":
				return ErrUOMNotFound
			}
		}
		return fmt.Errorf("update product: %w", err)
	}
	return nil
}

// ListProducts returns a paginated list of products.
func (s *PgxStore) ListProducts(ctx context.Context, limit, offset int) ([]Product, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM master.product`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	const query = `
		SELECT id, tenant_id, company_id, sku, name, brand_id, category_id,
		       base_uom_id, product_type, track_serial, track_lot, tax_category,
		       status, created_at, updated_at, created_by, updated_by, version
		FROM master.product
		ORDER BY sku ASC
		LIMIT $1 OFFSET $2`

	rows, err := s.exec(ctx).Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var items []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.CompanyID, &p.SKU, &p.Name, &p.BrandID, &p.CategoryID,
			&p.BaseUOMID, &p.ProductType, &p.TrackSerial, &p.TrackLot, &p.TaxCategory,
			&p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate products: %w", err)
	}

	return items, total, nil
}

// ============================================================================
// Product Variant
// ============================================================================

// CreateVariant inserts a product variant record.
func (s *PgxStore) CreateVariant(ctx context.Context, v *ProductVariant) error {
	if v == nil {
		return errors.New("master: variant cannot be nil")
	}
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	now := time.Now().UTC()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if v.Status == "" {
		v.Status = StatusActive
	}
	if v.Attributes == nil {
		v.Attributes = make(map[string]any)
	}

	attrsJSON, err := json.Marshal(v.Attributes)
	if err != nil {
		return fmt.Errorf("marshal variant attributes: %w", err)
	}

	const query = `
		INSERT INTO master.product_variant (
			id, tenant_id, product_id, sku, name, attributes, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		) RETURNING id, created_at, updated_at`

	err = s.exec(ctx).QueryRow(ctx, query,
		v.ID, v.TenantID, v.ProductID, v.SKU, v.Name, attrsJSON, v.Status, v.CreatedAt, v.UpdatedAt,
	).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrVariantSKUExists
			}
			if pgErr.Code == "23503" {
				return ErrProductNotFound
			}
		}
		return fmt.Errorf("create variant: %w", err)
	}
	return nil
}

// GetVariantByID fetches a variant by ID within the current tenant.
func (s *PgxStore) GetVariantByID(ctx context.Context, id uuid.UUID) (*ProductVariant, error) {
	const query = `
		SELECT id, tenant_id, product_id, sku, name, attributes, status, created_at, updated_at
		FROM master.product_variant
		WHERE id = $1`

	var v ProductVariant
	var attrsJSON []byte
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&v.ID, &v.TenantID, &v.ProductID, &v.SKU, &v.Name, &attrsJSON, &v.Status, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVariantNotFound
		}
		return nil, fmt.Errorf("get variant by id: %w", err)
	}
	if len(attrsJSON) > 0 {
		_ = json.Unmarshal(attrsJSON, &v.Attributes)
	}
	return &v, nil
}

// GetVariantBySKU fetches a variant by SKU within the current tenant.
func (s *PgxStore) GetVariantBySKU(ctx context.Context, sku string) (*ProductVariant, error) {
	const query = `
		SELECT id, tenant_id, product_id, sku, name, attributes, status, created_at, updated_at
		FROM master.product_variant
		WHERE sku = $1`

	var v ProductVariant
	var attrsJSON []byte
	err := s.exec(ctx).QueryRow(ctx, query, sku).Scan(
		&v.ID, &v.TenantID, &v.ProductID, &v.SKU, &v.Name, &attrsJSON, &v.Status, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVariantNotFound
		}
		return nil, fmt.Errorf("get variant by sku: %w", err)
	}
	if len(attrsJSON) > 0 {
		_ = json.Unmarshal(attrsJSON, &v.Attributes)
	}
	return &v, nil
}

// ListVariantsByProductID fetches all variants belonging to a product.
func (s *PgxStore) ListVariantsByProductID(ctx context.Context, productID uuid.UUID) ([]ProductVariant, error) {
	const query = `
		SELECT id, tenant_id, product_id, sku, name, attributes, status, created_at, updated_at
		FROM master.product_variant
		WHERE product_id = $1
		ORDER BY sku ASC`

	rows, err := s.exec(ctx).Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("list variants: %w", err)
	}
	defer rows.Close()

	var items []ProductVariant
	for rows.Next() {
		var v ProductVariant
		var attrsJSON []byte
		if err := rows.Scan(
			&v.ID, &v.TenantID, &v.ProductID, &v.SKU, &v.Name, &attrsJSON, &v.Status, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan variant: %w", err)
		}
		if len(attrsJSON) > 0 {
			_ = json.Unmarshal(attrsJSON, &v.Attributes)
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate variants: %w", err)
	}
	return items, nil
}

// ============================================================================
// Product Barcode
// ============================================================================

// CreateBarcode inserts a barcode record.
func (s *PgxStore) CreateBarcode(ctx context.Context, b *ProductBarcode) error {
	if b == nil {
		return errors.New("master: barcode cannot be nil")
	}
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.BarcodeType == "" {
		b.BarcodeType = "EAN"
	}
	if b.Status == "" {
		b.Status = StatusActive
	}

	const query = `
		INSERT INTO master.product_barcode (
			id, tenant_id, product_id, variant_id, barcode, barcode_type, is_primary, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		) RETURNING id`

	err := s.exec(ctx).QueryRow(ctx, query,
		b.ID, b.TenantID, b.ProductID, b.VariantID, b.Barcode, b.BarcodeType, b.IsPrimary, b.Status,
	).Scan(&b.ID)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrBarcodeExists
			}
			if pgErr.Code == "23503" {
				if pgErr.ConstraintName == "product_barcode_variant_id_fkey" {
					return ErrVariantNotFound
				}
				return ErrProductNotFound
			}
		}
		return fmt.Errorf("create barcode: %w", err)
	}
	return nil
}

// GetBarcodeByID fetches a barcode by ID within the current tenant.
func (s *PgxStore) GetBarcodeByID(ctx context.Context, id uuid.UUID) (*ProductBarcode, error) {
	const query = `
		SELECT id, tenant_id, product_id, variant_id, barcode, barcode_type, is_primary, status
		FROM master.product_barcode
		WHERE id = $1`

	var b ProductBarcode
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&b.ID, &b.TenantID, &b.ProductID, &b.VariantID, &b.Barcode, &b.BarcodeType, &b.IsPrimary, &b.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBarcodeNotFound
		}
		return nil, fmt.Errorf("get barcode by id: %w", err)
	}
	return &b, nil
}

// GetBarcodeByValue fetches a barcode by its barcode string within the current tenant.
func (s *PgxStore) GetBarcodeByValue(ctx context.Context, barcode string) (*ProductBarcode, error) {
	const query = `
		SELECT id, tenant_id, product_id, variant_id, barcode, barcode_type, is_primary, status
		FROM master.product_barcode
		WHERE barcode = $1`

	var b ProductBarcode
	err := s.exec(ctx).QueryRow(ctx, query, barcode).Scan(
		&b.ID, &b.TenantID, &b.ProductID, &b.VariantID, &b.Barcode, &b.BarcodeType, &b.IsPrimary, &b.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBarcodeNotFound
		}
		return nil, fmt.Errorf("get barcode by value: %w", err)
	}
	return &b, nil
}

// ListBarcodesByProductID lists barcodes for a given product.
func (s *PgxStore) ListBarcodesByProductID(ctx context.Context, productID uuid.UUID, limit, offset int) ([]ProductBarcode, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM master.product_barcode WHERE product_id = $1`, productID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count barcodes: %w", err)
	}

	const query = `
		SELECT id, tenant_id, product_id, variant_id, barcode, barcode_type, is_primary, status
		FROM master.product_barcode
		WHERE product_id = $1
		ORDER BY is_primary DESC, barcode ASC
		LIMIT $2 OFFSET $3`

	rows, err := s.exec(ctx).Query(ctx, query, productID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list barcodes: %w", err)
	}
	defer rows.Close()

	var items []ProductBarcode
	for rows.Next() {
		var b ProductBarcode
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ProductID, &b.VariantID, &b.Barcode, &b.BarcodeType, &b.IsPrimary, &b.Status,
		); err != nil {
			return nil, 0, fmt.Errorf("scan barcode: %w", err)
		}
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate barcodes: %w", err)
	}

	return items, total, nil
}

// ClearPrimaryBarcode resets is_primary to false for existing barcodes of a product or variant.
func (s *PgxStore) ClearPrimaryBarcode(ctx context.Context, productID uuid.UUID, variantID *uuid.UUID) error {
	var query string
	var args []any
	if variantID != nil {
		query = `UPDATE master.product_barcode SET is_primary = false WHERE product_id = $1 AND variant_id = $2 AND is_primary = true`
		args = []any{productID, *variantID}
	} else {
		query = `UPDATE master.product_barcode SET is_primary = false WHERE product_id = $1 AND variant_id IS NULL AND is_primary = true`
		args = []any{productID}
	}
	_, err := s.exec(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("clear primary barcode: %w", err)
	}
	return nil
}

// ============================================================================
// Serial Number
// ============================================================================

// CreateSerialNumber inserts a serial number record.
func (s *PgxStore) CreateSerialNumber(ctx context.Context, sn *SerialNumber) error {
	if sn == nil {
		return errors.New("master: serial number cannot be nil")
	}
	if sn.ID == uuid.Nil {
		sn.ID = uuid.New()
	}
	if sn.CreatedAt.IsZero() {
		sn.CreatedAt = time.Now().UTC()
	}
	if sn.Status == "" {
		sn.Status = "RECEIVED"
	}

	const query = `
		INSERT INTO master.serial_number (
			id, tenant_id, product_id, variant_id, serial_no, status, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		) RETURNING id, created_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		sn.ID, sn.TenantID, sn.ProductID, sn.VariantID, sn.SerialNo, sn.Status, sn.CreatedAt,
	).Scan(&sn.ID, &sn.CreatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrSerialNumberExists
			}
			if pgErr.Code == "23503" {
				return ErrProductNotFound
			}
		}
		return fmt.Errorf("create serial number: %w", err)
	}
	return nil
}

// GetSerialNumberByID fetches a serial number by ID within the current tenant.
func (s *PgxStore) GetSerialNumberByID(ctx context.Context, id uuid.UUID) (*SerialNumber, error) {
	const query = `
		SELECT id, tenant_id, product_id, variant_id, serial_no, status, created_at
		FROM master.serial_number
		WHERE id = $1`

	var sn SerialNumber
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&sn.ID, &sn.TenantID, &sn.ProductID, &sn.VariantID, &sn.SerialNo, &sn.Status, &sn.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSerialNumberNotFound
		}
		return nil, fmt.Errorf("get serial number by id: %w", err)
	}
	return &sn, nil
}

// GetSerialNumberByNo fetches a serial number by serial number string within the current tenant.
func (s *PgxStore) GetSerialNumberByNo(ctx context.Context, serialNo string) (*SerialNumber, error) {
	const query = `
		SELECT id, tenant_id, product_id, variant_id, serial_no, status, created_at
		FROM master.serial_number
		WHERE serial_no = $1`

	var sn SerialNumber
	err := s.exec(ctx).QueryRow(ctx, query, serialNo).Scan(
		&sn.ID, &sn.TenantID, &sn.ProductID, &sn.VariantID, &sn.SerialNo, &sn.Status, &sn.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSerialNumberNotFound
		}
		return nil, fmt.Errorf("get serial number by no: %w", err)
	}
	return &sn, nil
}

// ListSerialNumbersByProductID lists serial numbers for a given product.
func (s *PgxStore) ListSerialNumbersByProductID(ctx context.Context, productID uuid.UUID, limit, offset int) ([]SerialNumber, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM master.serial_number WHERE product_id = $1`, productID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count serial numbers: %w", err)
	}

	const query = `
		SELECT id, tenant_id, product_id, variant_id, serial_no, status, created_at
		FROM master.serial_number
		WHERE product_id = $1
		ORDER BY serial_no ASC
		LIMIT $2 OFFSET $3`

	rows, err := s.exec(ctx).Query(ctx, query, productID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list serial numbers: %w", err)
	}
	defer rows.Close()

	var items []SerialNumber
	for rows.Next() {
		var sn SerialNumber
		if err := rows.Scan(
			&sn.ID, &sn.TenantID, &sn.ProductID, &sn.VariantID, &sn.SerialNo, &sn.Status, &sn.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan serial number: %w", err)
		}
		items = append(items, sn)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate serial numbers: %w", err)
	}

	return items, total, nil
}

// ============================================================================
// Company Validation (DEC-B1-010)
// ============================================================================

// ValidateCompany checks whether a company exists within the tenant context.
func (s *PgxStore) ValidateCompany(ctx context.Context, companyID uuid.UUID) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM organization.company WHERE id = $1)`
	var exists bool
	err := s.exec(ctx).QueryRow(ctx, query, companyID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("validate company: %w", err)
	}
	return exists, nil
}
