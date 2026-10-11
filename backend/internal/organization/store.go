package organization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

// OrgStore defines persistence operations for the Organization domain.
type OrgStore interface {
	// Company operations
	CreateCompany(ctx context.Context, c *Company) error
	GetCompanyByID(ctx context.Context, id uuid.UUID) (*Company, error)
	GetCompanyByCode(ctx context.Context, code string) (*Company, error)
	UpdateCompany(ctx context.Context, c *Company) error
	ListCompanies(ctx context.Context, limit, offset int) ([]Company, int64, error)

	// Region operations
	CreateRegion(ctx context.Context, r *Region) error
	GetRegionByID(ctx context.Context, id uuid.UUID) (*Region, error)
	GetRegionByCode(ctx context.Context, companyID uuid.UUID, code string) (*Region, error)
	UpdateRegion(ctx context.Context, r *Region) error
	ListRegions(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Region, int64, error)

	// Store operations
	CreateStore(ctx context.Context, s *Store) error
	GetStoreByID(ctx context.Context, id uuid.UUID) (*Store, error)
	GetStoreByCode(ctx context.Context, companyID uuid.UUID, code string) (*Store, error)
	UpdateStore(ctx context.Context, s *Store) error
	ListStores(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Store, int64, error)

	// Warehouse operations
	CreateWarehouse(ctx context.Context, w *Warehouse) error
	GetWarehouseByID(ctx context.Context, id uuid.UUID) (*Warehouse, error)
	GetWarehouseByCode(ctx context.Context, companyID uuid.UUID, code string) (*Warehouse, error)
	UpdateWarehouse(ctx context.Context, w *Warehouse) error
	ListWarehouses(ctx context.Context, companyID *uuid.UUID, storeID *uuid.UUID, limit, offset int) ([]Warehouse, int64, error)

	// Terminal operations
	CreateTerminal(ctx context.Context, t *Terminal) error
	GetTerminalByID(ctx context.Context, id uuid.UUID) (*Terminal, error)
	GetTerminalByCode(ctx context.Context, storeID uuid.UUID, code string) (*Terminal, error)
	GetTerminalByDeviceHash(ctx context.Context, deviceHash string) (*Terminal, error)
	UpdateTerminal(ctx context.Context, t *Terminal) error
	UpdateTerminalDeviceHash(ctx context.Context, terminalID uuid.UUID, deviceHash *string, updatedBy *uuid.UUID) error
	ListTerminals(ctx context.Context, storeID *uuid.UUID, limit, offset int) ([]Terminal, int64, error)

	// Cross-domain validation
	ValidateScopeEntity(ctx context.Context, scopeType string, entityID uuid.UUID) (bool, error)
}

// PgxStore implements Store using PostgreSQL and pgxpool.
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
// Company
// ============================================================================

// CreateCompany inserts a new company record.
func (s *PgxStore) CreateCompany(ctx context.Context, c *Company) error {
	if c == nil {
		return errors.New("organization: company cannot be nil")
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Status == "" {
		c.Status = StatusActive
	}

	const query = `
		INSERT INTO organization.company (
			id, tenant_id, code, name, legal_name, tax_id,
			base_currency, timezone, status, created_at, updated_at,
			created_by, updated_by, version
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		) RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		c.ID, c.TenantID, c.Code, c.Name, c.LegalName, c.TaxID,
		c.BaseCurrency, c.Timezone, c.Status, c.CreatedAt, c.UpdatedAt,
		c.CreatedBy, c.UpdatedBy, c.Version,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt, &c.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrCompanyCodeExists
		}
		return fmt.Errorf("create company: %w", err)
	}
	return nil
}

// GetCompanyByID fetches a company by ID within the current tenant.
func (s *PgxStore) GetCompanyByID(ctx context.Context, id uuid.UUID) (*Company, error) {
	const query = `
		SELECT id, tenant_id, code, name, legal_name, tax_id,
		       base_currency, timezone, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.company
		WHERE id = $1`

	var c Company
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&c.ID, &c.TenantID, &c.Code, &c.Name, &c.LegalName, &c.TaxID,
		&c.BaseCurrency, &c.Timezone, &c.Status, &c.CreatedAt, &c.UpdatedAt,
		&c.CreatedBy, &c.UpdatedBy, &c.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCompanyNotFound
		}
		return nil, fmt.Errorf("get company by id: %w", err)
	}
	return &c, nil
}

// GetCompanyByCode fetches a company by code within the current tenant.
func (s *PgxStore) GetCompanyByCode(ctx context.Context, code string) (*Company, error) {
	const query = `
		SELECT id, tenant_id, code, name, legal_name, tax_id,
		       base_currency, timezone, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.company
		WHERE code = $1`

	var c Company
	err := s.exec(ctx).QueryRow(ctx, query, code).Scan(
		&c.ID, &c.TenantID, &c.Code, &c.Name, &c.LegalName, &c.TaxID,
		&c.BaseCurrency, &c.Timezone, &c.Status, &c.CreatedAt, &c.UpdatedAt,
		&c.CreatedBy, &c.UpdatedBy, &c.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCompanyNotFound
		}
		return nil, fmt.Errorf("get company by code: %w", err)
	}
	return &c, nil
}

// UpdateCompany updates a company with optimistic locking.
func (s *PgxStore) UpdateCompany(ctx context.Context, c *Company) error {
	if c == nil {
		return errors.New("organization: company cannot be nil")
	}

	const query = `
		UPDATE organization.company
		SET name = $1, legal_name = $2, tax_id = $3,
		    base_currency = $4, timezone = $5, status = $6,
		    updated_by = $7, updated_at = now(), version = version + 1
		WHERE id = $8 AND version = $9
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		c.Name, c.LegalName, c.TaxID, c.BaseCurrency, c.Timezone, c.Status,
		c.UpdatedBy, c.ID, c.Version,
	).Scan(&c.Version, &c.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := s.GetCompanyByID(ctx, c.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != c.Version {
				return ErrOptimisticLockConflict
			}
			return ErrCompanyNotFound
		}
		return fmt.Errorf("update company: %w", err)
	}
	return nil
}

// ListCompanies returns a paginated list of companies.
func (s *PgxStore) ListCompanies(ctx context.Context, limit, offset int) ([]Company, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM organization.company`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count companies: %w", err)
	}

	const query = `
		SELECT id, tenant_id, code, name, legal_name, tax_id,
		       base_currency, timezone, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.company
		ORDER BY created_at ASC
		LIMIT $1 OFFSET $2`

	rows, err := s.exec(ctx).Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()

	var companies []Company
	for rows.Next() {
		var c Company
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.Code, &c.Name, &c.LegalName, &c.TaxID,
			&c.BaseCurrency, &c.Timezone, &c.Status, &c.CreatedAt, &c.UpdatedAt,
			&c.CreatedBy, &c.UpdatedBy, &c.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan company: %w", err)
		}
		companies = append(companies, c)
	}
	return companies, total, rows.Err()
}

// ============================================================================
// Region
// ============================================================================

// CreateRegion inserts a new region record.
func (s *PgxStore) CreateRegion(ctx context.Context, r *Region) error {
	if r == nil {
		return errors.New("organization: region cannot be nil")
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	if r.Version == 0 {
		r.Version = 1
	}
	if r.Status == "" {
		r.Status = StatusActive
	}

	const query = `
		INSERT INTO organization.region (
			id, tenant_id, company_id, code, name, status,
			created_at, updated_at, created_by, updated_by, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		r.ID, r.TenantID, r.CompanyID, r.Code, r.Name, r.Status,
		r.CreatedAt, r.UpdatedAt, r.CreatedBy, r.UpdatedBy, r.Version,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrRegionCodeExists
			}
			if pgErr.Code == "23503" {
				return ErrParentNotFound
			}
		}
		return fmt.Errorf("create region: %w", err)
	}
	return nil
}

// GetRegionByID fetches a region by ID within the current tenant.
func (s *PgxStore) GetRegionByID(ctx context.Context, id uuid.UUID) (*Region, error) {
	const query = `
		SELECT id, tenant_id, company_id, code, name, status,
		       created_at, updated_at, created_by, updated_by, version
		FROM organization.region
		WHERE id = $1`

	var r Region
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&r.ID, &r.TenantID, &r.CompanyID, &r.Code, &r.Name, &r.Status,
		&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRegionNotFound
		}
		return nil, fmt.Errorf("get region by id: %w", err)
	}
	return &r, nil
}

// GetRegionByCode fetches a region by code and company within the current tenant.
func (s *PgxStore) GetRegionByCode(ctx context.Context, companyID uuid.UUID, code string) (*Region, error) {
	const query = `
		SELECT id, tenant_id, company_id, code, name, status,
		       created_at, updated_at, created_by, updated_by, version
		FROM organization.region
		WHERE company_id = $1 AND code = $2`

	var r Region
	err := s.exec(ctx).QueryRow(ctx, query, companyID, code).Scan(
		&r.ID, &r.TenantID, &r.CompanyID, &r.Code, &r.Name, &r.Status,
		&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRegionNotFound
		}
		return nil, fmt.Errorf("get region by code: %w", err)
	}
	return &r, nil
}

// UpdateRegion updates a region with optimistic locking.
func (s *PgxStore) UpdateRegion(ctx context.Context, r *Region) error {
	if r == nil {
		return errors.New("organization: region cannot be nil")
	}

	const query = `
		UPDATE organization.region
		SET name = $1, status = $2, updated_by = $3,
		    updated_at = now(), version = version + 1
		WHERE id = $4 AND version = $5
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		r.Name, r.Status, r.UpdatedBy, r.ID, r.Version,
	).Scan(&r.Version, &r.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := s.GetRegionByID(ctx, r.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != r.Version {
				return ErrOptimisticLockConflict
			}
			return ErrRegionNotFound
		}
		return fmt.Errorf("update region: %w", err)
	}
	return nil
}

// ListRegions returns a paginated list of regions.
func (s *PgxStore) ListRegions(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Region, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause := ""
	var args []any
	if companyID != nil {
		whereClause = "WHERE company_id = $1"
		args = append(args, *companyID)
	}

	countQuery := "SELECT count(*) FROM organization.region " + whereClause
	var total int64
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count regions: %w", err)
	}

	query := `
		SELECT id, tenant_id, company_id, code, name, status,
		       created_at, updated_at, created_by, updated_by, version
		FROM organization.region ` + whereClause + `
		ORDER BY created_at ASC
		LIMIT ` + fmt.Sprintf("$%d OFFSET $%d", len(args)+1, len(args)+2)

	args = append(args, limit, offset)
	rows, err := s.exec(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()

	var regions []Region
	for rows.Next() {
		var r Region
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.CompanyID, &r.Code, &r.Name, &r.Status,
			&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan region: %w", err)
		}
		regions = append(regions, r)
	}
	return regions, total, rows.Err()
}

// ============================================================================
// Store
// ============================================================================

// CreateStore inserts a new store record.
func (s *PgxStore) CreateStore(ctx context.Context, st *Store) error {
	if st == nil {
		return errors.New("organization: store cannot be nil")
	}
	if st.ID == uuid.Nil {
		st.ID = uuid.New()
	}
	now := time.Now().UTC()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = now
	}
	st.UpdatedAt = now
	if st.Version == 0 {
		st.Version = 1
	}
	if st.Status == "" {
		st.Status = StatusActive
	}

	const query = `
		INSERT INTO organization.store (
			id, tenant_id, company_id, region_id, code, name,
			address, timezone, status, created_at, updated_at,
			created_by, updated_by, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		st.ID, st.TenantID, st.CompanyID, st.RegionID, st.Code, st.Name,
		st.Address, st.Timezone, st.Status, st.CreatedAt, st.UpdatedAt,
		st.CreatedBy, st.UpdatedBy, st.Version,
	).Scan(&st.ID, &st.CreatedAt, &st.UpdatedAt, &st.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrStoreCodeExists
			}
			if pgErr.Code == "23503" {
				return ErrParentNotFound
			}
		}
		return fmt.Errorf("create store: %w", err)
	}
	return nil
}

// GetStoreByID fetches a store by ID within the current tenant.
func (s *PgxStore) GetStoreByID(ctx context.Context, id uuid.UUID) (*Store, error) {
	const query = `
		SELECT id, tenant_id, company_id, region_id, code, name,
		       address, timezone, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.store
		WHERE id = $1`

	var st Store
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&st.ID, &st.TenantID, &st.CompanyID, &st.RegionID, &st.Code, &st.Name,
		&st.Address, &st.Timezone, &st.Status, &st.CreatedAt, &st.UpdatedAt,
		&st.CreatedBy, &st.UpdatedBy, &st.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStoreNotFound
		}
		return nil, fmt.Errorf("get store by id: %w", err)
	}
	return &st, nil
}

// GetStoreByCode fetches a store by code and company within the current tenant.
func (s *PgxStore) GetStoreByCode(ctx context.Context, companyID uuid.UUID, code string) (*Store, error) {
	const query = `
		SELECT id, tenant_id, company_id, region_id, code, name,
		       address, timezone, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.store
		WHERE company_id = $1 AND code = $2`

	var st Store
	err := s.exec(ctx).QueryRow(ctx, query, companyID, code).Scan(
		&st.ID, &st.TenantID, &st.CompanyID, &st.RegionID, &st.Code, &st.Name,
		&st.Address, &st.Timezone, &st.Status, &st.CreatedAt, &st.UpdatedAt,
		&st.CreatedBy, &st.UpdatedBy, &st.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStoreNotFound
		}
		return nil, fmt.Errorf("get store by code: %w", err)
	}
	return &st, nil
}

// UpdateStore updates a store with optimistic locking.
func (s *PgxStore) UpdateStore(ctx context.Context, st *Store) error {
	if st == nil {
		return errors.New("organization: store cannot be nil")
	}

	const query = `
		UPDATE organization.store
		SET region_id = $1, name = $2, address = $3, timezone = $4,
		    status = $5, updated_by = $6, updated_at = now(), version = version + 1
		WHERE id = $7 AND version = $8
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		st.RegionID, st.Name, st.Address, st.Timezone, st.Status,
		st.UpdatedBy, st.ID, st.Version,
	).Scan(&st.Version, &st.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := s.GetStoreByID(ctx, st.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != st.Version {
				return ErrOptimisticLockConflict
			}
			return ErrStoreNotFound
		}
		return fmt.Errorf("update store: %w", err)
	}
	return nil
}

// ListStores returns a paginated list of stores.
func (s *PgxStore) ListStores(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Store, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause := ""
	var args []any
	if companyID != nil {
		whereClause = "WHERE company_id = $1"
		args = append(args, *companyID)
	}

	countQuery := "SELECT count(*) FROM organization.store " + whereClause
	var total int64
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stores: %w", err)
	}

	query := `
		SELECT id, tenant_id, company_id, region_id, code, name,
		       address, timezone, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.store ` + whereClause + `
		ORDER BY created_at ASC
		LIMIT ` + fmt.Sprintf("$%d OFFSET $%d", len(args)+1, len(args)+2)

	args = append(args, limit, offset)
	rows, err := s.exec(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list stores: %w", err)
	}
	defer rows.Close()

	var stores []Store
	for rows.Next() {
		var st Store
		if err := rows.Scan(
			&st.ID, &st.TenantID, &st.CompanyID, &st.RegionID, &st.Code, &st.Name,
			&st.Address, &st.Timezone, &st.Status, &st.CreatedAt, &st.UpdatedAt,
			&st.CreatedBy, &st.UpdatedBy, &st.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan store: %w", err)
		}
		stores = append(stores, st)
	}
	return stores, total, rows.Err()
}

// ============================================================================
// Warehouse
// ============================================================================

// CreateWarehouse inserts a new warehouse record.
func (s *PgxStore) CreateWarehouse(ctx context.Context, w *Warehouse) error {
	if w == nil {
		return errors.New("organization: warehouse cannot be nil")
	}
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	w.UpdatedAt = now
	if w.Version == 0 {
		w.Version = 1
	}
	if w.Status == "" {
		w.Status = StatusActive
	}
	if w.WarehouseType == "" {
		w.WarehouseType = WarehouseTypeStore
	}

	const query = `
		INSERT INTO organization.warehouse (
			id, tenant_id, company_id, store_id, code, name,
			warehouse_type, status, created_at, updated_at,
			created_by, updated_by, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		w.ID, w.TenantID, w.CompanyID, w.StoreID, w.Code, w.Name,
		w.WarehouseType, w.Status, w.CreatedAt, w.UpdatedAt,
		w.CreatedBy, w.UpdatedBy, w.Version,
	).Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt, &w.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrWarehouseCodeExists
			}
			if pgErr.Code == "23503" {
				return ErrParentNotFound
			}
		}
		return fmt.Errorf("create warehouse: %w", err)
	}
	return nil
}

// GetWarehouseByID fetches a warehouse by ID within the current tenant.
func (s *PgxStore) GetWarehouseByID(ctx context.Context, id uuid.UUID) (*Warehouse, error) {
	const query = `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       warehouse_type, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.warehouse
		WHERE id = $1`

	var w Warehouse
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&w.ID, &w.TenantID, &w.CompanyID, &w.StoreID, &w.Code, &w.Name,
		&w.WarehouseType, &w.Status, &w.CreatedAt, &w.UpdatedAt,
		&w.CreatedBy, &w.UpdatedBy, &w.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWarehouseNotFound
		}
		return nil, fmt.Errorf("get warehouse by id: %w", err)
	}
	return &w, nil
}

// GetWarehouseByCode fetches a warehouse by code and company within the current tenant.
func (s *PgxStore) GetWarehouseByCode(ctx context.Context, companyID uuid.UUID, code string) (*Warehouse, error) {
	const query = `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       warehouse_type, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.warehouse
		WHERE company_id = $1 AND code = $2`

	var w Warehouse
	err := s.exec(ctx).QueryRow(ctx, query, companyID, code).Scan(
		&w.ID, &w.TenantID, &w.CompanyID, &w.StoreID, &w.Code, &w.Name,
		&w.WarehouseType, &w.Status, &w.CreatedAt, &w.UpdatedAt,
		&w.CreatedBy, &w.UpdatedBy, &w.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWarehouseNotFound
		}
		return nil, fmt.Errorf("get warehouse by code: %w", err)
	}
	return &w, nil
}

// UpdateWarehouse updates a warehouse with optimistic locking.
func (s *PgxStore) UpdateWarehouse(ctx context.Context, w *Warehouse) error {
	if w == nil {
		return errors.New("organization: warehouse cannot be nil")
	}

	const query = `
		UPDATE organization.warehouse
		SET store_id = $1, name = $2, warehouse_type = $3,
		    status = $4, updated_by = $5, updated_at = now(), version = version + 1
		WHERE id = $6 AND version = $7
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		w.StoreID, w.Name, w.WarehouseType, w.Status,
		w.UpdatedBy, w.ID, w.Version,
	).Scan(&w.Version, &w.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := s.GetWarehouseByID(ctx, w.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != w.Version {
				return ErrOptimisticLockConflict
			}
			return ErrWarehouseNotFound
		}
		return fmt.Errorf("update warehouse: %w", err)
	}
	return nil
}

// ListWarehouses returns a paginated list of warehouses.
func (s *PgxStore) ListWarehouses(ctx context.Context, companyID *uuid.UUID, storeID *uuid.UUID, limit, offset int) ([]Warehouse, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var conditions []string
	var args []any
	if companyID != nil {
		args = append(args, *companyID)
		conditions = append(conditions, fmt.Sprintf("company_id = $%d", len(args)))
	}
	if storeID != nil {
		args = append(args, *storeID)
		conditions = append(conditions, fmt.Sprintf("store_id = $%d", len(args)))
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := "SELECT count(*) FROM organization.warehouse " + whereClause
	var total int64
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count warehouses: %w", err)
	}

	query := `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       warehouse_type, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.warehouse ` + whereClause + `
		ORDER BY created_at ASC
		LIMIT ` + fmt.Sprintf("$%d OFFSET $%d", len(args)+1, len(args)+2)

	args = append(args, limit, offset)
	rows, err := s.exec(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list warehouses: %w", err)
	}
	defer rows.Close()

	var warehouses []Warehouse
	for rows.Next() {
		var w Warehouse
		if err := rows.Scan(
			&w.ID, &w.TenantID, &w.CompanyID, &w.StoreID, &w.Code, &w.Name,
			&w.WarehouseType, &w.Status, &w.CreatedAt, &w.UpdatedAt,
			&w.CreatedBy, &w.UpdatedBy, &w.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan warehouse: %w", err)
		}
		warehouses = append(warehouses, w)
	}
	return warehouses, total, rows.Err()
}

// ============================================================================
// Terminal
// ============================================================================

// CreateTerminal inserts a new terminal record.
func (s *PgxStore) CreateTerminal(ctx context.Context, t *Terminal) error {
	if t == nil {
		return errors.New("organization: terminal cannot be nil")
	}
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.Version == 0 {
		t.Version = 1
	}
	if t.Status == "" {
		t.Status = TerminalStatusActive
	}

	const query = `
		INSERT INTO organization.terminal (
			id, tenant_id, company_id, store_id, code, name,
			device_identifier, status, created_at, updated_at,
			created_by, updated_by, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		t.ID, t.TenantID, t.CompanyID, t.StoreID, t.Code, t.Name,
		t.DeviceIdentifier, t.Status, t.CreatedAt, t.UpdatedAt,
		t.CreatedBy, t.UpdatedBy, t.Version,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrTerminalCodeExists
			}
			if pgErr.Code == "23503" {
				return ErrParentNotFound
			}
		}
		return fmt.Errorf("create terminal: %w", err)
	}
	return nil
}

// GetTerminalByID fetches a terminal by ID within the current tenant.
func (s *PgxStore) GetTerminalByID(ctx context.Context, id uuid.UUID) (*Terminal, error) {
	const query = `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       device_identifier, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.terminal
		WHERE id = $1`

	var t Terminal
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&t.ID, &t.TenantID, &t.CompanyID, &t.StoreID, &t.Code, &t.Name,
		&t.DeviceIdentifier, &t.Status, &t.CreatedAt, &t.UpdatedAt,
		&t.CreatedBy, &t.UpdatedBy, &t.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTerminalNotFound
		}
		return nil, fmt.Errorf("get terminal by id: %w", err)
	}
	return &t, nil
}

// GetTerminalByCode fetches a terminal by code and store within the current tenant.
func (s *PgxStore) GetTerminalByCode(ctx context.Context, storeID uuid.UUID, code string) (*Terminal, error) {
	const query = `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       device_identifier, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.terminal
		WHERE store_id = $1 AND code = $2`

	var t Terminal
	err := s.exec(ctx).QueryRow(ctx, query, storeID, code).Scan(
		&t.ID, &t.TenantID, &t.CompanyID, &t.StoreID, &t.Code, &t.Name,
		&t.DeviceIdentifier, &t.Status, &t.CreatedAt, &t.UpdatedAt,
		&t.CreatedBy, &t.UpdatedBy, &t.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTerminalNotFound
		}
		return nil, fmt.Errorf("get terminal by code: %w", err)
	}
	return &t, nil
}

// GetTerminalByDeviceHash fetches a terminal by device token SHA-256 hash.
func (s *PgxStore) GetTerminalByDeviceHash(ctx context.Context, deviceHash string) (*Terminal, error) {
	const query = `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       device_identifier, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.terminal
		WHERE device_identifier = $1`

	var t Terminal
	err := s.exec(ctx).QueryRow(ctx, query, deviceHash).Scan(
		&t.ID, &t.TenantID, &t.CompanyID, &t.StoreID, &t.Code, &t.Name,
		&t.DeviceIdentifier, &t.Status, &t.CreatedAt, &t.UpdatedAt,
		&t.CreatedBy, &t.UpdatedBy, &t.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTerminalNotFound
		}
		return nil, fmt.Errorf("get terminal by device hash: %w", err)
	}
	return &t, nil
}

// UpdateTerminal updates a terminal with optimistic locking.
func (s *PgxStore) UpdateTerminal(ctx context.Context, t *Terminal) error {
	if t == nil {
		return errors.New("organization: terminal cannot be nil")
	}

	const query = `
		UPDATE organization.terminal
		SET name = $1, status = $2, updated_by = $3,
		    updated_at = now(), version = version + 1
		WHERE id = $4 AND version = $5
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		t.Name, t.Status, t.UpdatedBy, t.ID, t.Version,
	).Scan(&t.Version, &t.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := s.GetTerminalByID(ctx, t.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != t.Version {
				return ErrOptimisticLockConflict
			}
			return ErrTerminalNotFound
		}
		return fmt.Errorf("update terminal: %w", err)
	}
	return nil
}

// UpdateTerminalDeviceHash updates the device token hash for a terminal.
func (s *PgxStore) UpdateTerminalDeviceHash(ctx context.Context, terminalID uuid.UUID, deviceHash *string, updatedBy *uuid.UUID) error {
	const query = `
		UPDATE organization.terminal
		SET device_identifier = $1, updated_by = $2,
		    updated_at = now(), version = version + 1
		WHERE id = $3
		RETURNING version`

	var ver int64
	err := s.exec(ctx).QueryRow(ctx, query, deviceHash, updatedBy, terminalID).Scan(&ver)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTerminalNotFound
		}
		return fmt.Errorf("update terminal device hash: %w", err)
	}
	return nil
}

// ListTerminals returns a paginated list of terminals.
func (s *PgxStore) ListTerminals(ctx context.Context, storeID *uuid.UUID, limit, offset int) ([]Terminal, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	whereClause := ""
	var args []any
	if storeID != nil {
		whereClause = "WHERE store_id = $1"
		args = append(args, *storeID)
	}

	countQuery := "SELECT count(*) FROM organization.terminal " + whereClause
	var total int64
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count terminals: %w", err)
	}

	query := `
		SELECT id, tenant_id, company_id, store_id, code, name,
		       device_identifier, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM organization.terminal ` + whereClause + `
		ORDER BY created_at ASC
		LIMIT ` + fmt.Sprintf("$%d OFFSET $%d", len(args)+1, len(args)+2)

	args = append(args, limit, offset)
	rows, err := s.exec(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list terminals: %w", err)
	}
	defer rows.Close()

	var terminals []Terminal
	for rows.Next() {
		var t Terminal
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.CompanyID, &t.StoreID, &t.Code, &t.Name,
			&t.DeviceIdentifier, &t.Status, &t.CreatedAt, &t.UpdatedAt,
			&t.CreatedBy, &t.UpdatedBy, &t.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan terminal: %w", err)
		}
		terminals = append(terminals, t)
	}
	return terminals, total, rows.Err()
}

// ============================================================================
// Scope Entity Validation
// ============================================================================

// ValidateScopeEntity verifies whether an entity exists and is active for the given scope type.
func (s *PgxStore) ValidateScopeEntity(ctx context.Context, scopeType string, entityID uuid.UUID) (bool, error) {
	var table string
	switch strings.ToUpper(scopeType) {
	case "COMPANY":
		table = "organization.company"
	case "REGION":
		table = "organization.region"
	case "STORE":
		table = "organization.store"
	case "WAREHOUSE":
		table = "organization.warehouse"
	case "TERMINAL":
		table = "organization.terminal"
	default:
		return false, fmt.Errorf("unknown scope type: %s", scopeType)
	}

	query := fmt.Sprintf("SELECT 1 FROM %s WHERE id = $1 AND status = 'ACTIVE'", table)
	var dummy int
	err := s.exec(ctx).QueryRow(ctx, query, entityID).Scan(&dummy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("validate scope entity in %s: %w", table, err)
	}
	return true, nil
}
