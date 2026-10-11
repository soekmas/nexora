// Package crm provides persistence operations for the CRM domain.
package crm

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

// Store defines persistence operations for CRM aggregate roots and entities.
type Store interface {
	// Customer Group operations
	CreateCustomerGroup(ctx context.Context, g *CustomerGroup) error
	GetCustomerGroupByID(ctx context.Context, id uuid.UUID) (*CustomerGroup, error)
	GetCustomerGroupByCode(ctx context.Context, code string) (*CustomerGroup, error)
	UpdateCustomerGroup(ctx context.Context, g *CustomerGroup) error
	ListCustomerGroups(ctx context.Context, status string, limit, offset int) ([]CustomerGroup, int64, error)

	// Customer operations
	CreateCustomer(ctx context.Context, c *Customer) error
	GetCustomerByID(ctx context.Context, id uuid.UUID) (*Customer, error)
	GetCustomerByNumber(ctx context.Context, no string) (*Customer, error)
	UpdateCustomer(ctx context.Context, c *Customer, expectedVersion int64) error
	ListCustomers(ctx context.Context, filter ListCustomersFilter) ([]Customer, int64, error)
	LockCustomerForUpdate(ctx context.Context, id uuid.UUID) (*Customer, error)

	// Customer Address operations
	CreateCustomerAddress(ctx context.Context, a *CustomerAddress) error
	GetCustomerAddressByID(ctx context.Context, id uuid.UUID) (*CustomerAddress, error)
	UpdateCustomerAddress(ctx context.Context, a *CustomerAddress) error
	DeleteCustomerAddress(ctx context.Context, id uuid.UUID) error
	ListAddressesByCustomerID(ctx context.Context, customerID uuid.UUID) ([]CustomerAddress, error)
	ClearDefaultAddresses(ctx context.Context, customerID uuid.UUID, excludeAddressID *uuid.UUID) error
}

// PgxStore implements Store using pgxpool and PostgreSQL under forced RLS.
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
// Customer Group
// ============================================================================

// CreateCustomerGroup inserts a new customer group.
func (s *PgxStore) CreateCustomerGroup(ctx context.Context, g *CustomerGroup) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}
	g.TenantID = pc.TenantID
	now := time.Now().UTC()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	if g.UpdatedAt.IsZero() {
		g.UpdatedAt = now
	}
	if g.Status == "" {
		g.Status = StatusActive
	}

	query := `
		INSERT INTO crm.customer_group (id, tenant_id, code, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := s.exec(ctx).Exec(ctx, query,
		g.ID,
		g.TenantID,
		g.Code,
		g.Name,
		g.Status,
		g.CreatedAt,
		g.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrCustomerGroupCodeConflict
		}
		return fmt.Errorf("create customer group: %w", err)
	}
	return nil
}

// GetCustomerGroupByID retrieves a customer group by UUID.
func (s *PgxStore) GetCustomerGroupByID(ctx context.Context, id uuid.UUID) (*CustomerGroup, error) {
	query := `
		SELECT id, tenant_id, code, name, status, created_at, updated_at
		FROM crm.customer_group
		WHERE id = $1
	`
	var g CustomerGroup
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&g.ID,
		&g.TenantID,
		&g.Code,
		&g.Name,
		&g.Status,
		&g.CreatedAt,
		&g.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerGroupNotFound
		}
		return nil, fmt.Errorf("get customer group by id: %w", err)
	}
	return &g, nil
}

// GetCustomerGroupByCode retrieves a customer group by its unique code.
func (s *PgxStore) GetCustomerGroupByCode(ctx context.Context, code string) (*CustomerGroup, error) {
	query := `
		SELECT id, tenant_id, code, name, status, created_at, updated_at
		FROM crm.customer_group
		WHERE code = $1
	`
	var g CustomerGroup
	err := s.exec(ctx).QueryRow(ctx, query, code).Scan(
		&g.ID,
		&g.TenantID,
		&g.Code,
		&g.Name,
		&g.Status,
		&g.CreatedAt,
		&g.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerGroupNotFound
		}
		return nil, fmt.Errorf("get customer group by code: %w", err)
	}
	return &g, nil
}

// UpdateCustomerGroup updates a customer group's attributes.
func (s *PgxStore) UpdateCustomerGroup(ctx context.Context, g *CustomerGroup) error {
	g.UpdatedAt = time.Now().UTC()
	query := `
		UPDATE crm.customer_group
		SET code = $1, name = $2, status = $3, updated_at = $4
		WHERE id = $5
	`
	tag, err := s.exec(ctx).Exec(ctx, query,
		g.Code,
		g.Name,
		g.Status,
		g.UpdatedAt,
		g.ID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrCustomerGroupCodeConflict
		}
		return fmt.Errorf("update customer group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCustomerGroupNotFound
	}
	return nil
}

// ListCustomerGroups retrieves a paginated list of customer groups with optional status filter.
func (s *PgxStore) ListCustomerGroups(ctx context.Context, status string, limit, offset int) ([]CustomerGroup, int64, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT count(*) FROM crm.customer_group %s", whereClause)
	var total int64
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customer groups: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, code, name, status, created_at, updated_at
		FROM crm.customer_group
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)

	args = append(args, limit, offset)
	rows, err := s.exec(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list customer groups: %w", err)
	}
	defer rows.Close()

	var groups []CustomerGroup
	for rows.Next() {
		var g CustomerGroup
		if err := rows.Scan(
			&g.ID,
			&g.TenantID,
			&g.Code,
			&g.Name,
			&g.Status,
			&g.CreatedAt,
			&g.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan customer group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, total, nil
}

// ============================================================================
// Customer
// ============================================================================

// CreateCustomer inserts a new customer profile.
func (s *PgxStore) CreateCustomer(ctx context.Context, c *Customer) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}
	c.TenantID = pc.TenantID
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	if c.Status == "" {
		c.Status = StatusActive
	}
	if c.Version == 0 {
		c.Version = 1
	}

	query := `
		INSERT INTO crm.customer (
			id, tenant_id, customer_no, customer_group_id, name,
			email, phone, tax_id, status, created_at, updated_at,
			created_by, updated_by, version
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11,
			$12, $13, $14
		)
	`
	_, err := s.exec(ctx).Exec(ctx, query,
		c.ID,
		c.TenantID,
		c.CustomerNo,
		c.CustomerGroupID,
		c.Name,
		c.Email,
		c.Phone,
		c.TaxID,
		c.Status,
		c.CreatedAt,
		c.UpdatedAt,
		c.CreatedBy,
		c.UpdatedBy,
		c.Version,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrCustomerNumberConflict
		}
		return fmt.Errorf("create customer: %w", err)
	}
	return nil
}

// GetCustomerByID retrieves a customer by ID.
func (s *PgxStore) GetCustomerByID(ctx context.Context, id uuid.UUID) (*Customer, error) {
	query := `
		SELECT id, tenant_id, customer_no, customer_group_id, name,
		       email, phone, tax_id, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM crm.customer
		WHERE id = $1
	`
	var c Customer
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&c.ID,
		&c.TenantID,
		&c.CustomerNo,
		&c.CustomerGroupID,
		&c.Name,
		&c.Email,
		&c.Phone,
		&c.TaxID,
		&c.Status,
		&c.CreatedAt,
		&c.UpdatedAt,
		&c.CreatedBy,
		&c.UpdatedBy,
		&c.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("get customer by id: %w", err)
	}
	return &c, nil
}

// GetCustomerByNumber retrieves a customer by customer_no.
func (s *PgxStore) GetCustomerByNumber(ctx context.Context, no string) (*Customer, error) {
	query := `
		SELECT id, tenant_id, customer_no, customer_group_id, name,
		       email, phone, tax_id, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM crm.customer
		WHERE customer_no = $1
	`
	var c Customer
	err := s.exec(ctx).QueryRow(ctx, query, no).Scan(
		&c.ID,
		&c.TenantID,
		&c.CustomerNo,
		&c.CustomerGroupID,
		&c.Name,
		&c.Email,
		&c.Phone,
		&c.TaxID,
		&c.Status,
		&c.CreatedAt,
		&c.UpdatedAt,
		&c.CreatedBy,
		&c.UpdatedBy,
		&c.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("get customer by number: %w", err)
	}
	return &c, nil
}

// LockCustomerForUpdate locks a customer row for exclusive transactional modification.
func (s *PgxStore) LockCustomerForUpdate(ctx context.Context, id uuid.UUID) (*Customer, error) {
	query := `
		SELECT id, tenant_id, customer_no, customer_group_id, name,
		       email, phone, tax_id, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM crm.customer
		WHERE id = $1
		FOR UPDATE
	`
	var c Customer
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&c.ID,
		&c.TenantID,
		&c.CustomerNo,
		&c.CustomerGroupID,
		&c.Name,
		&c.Email,
		&c.Phone,
		&c.TaxID,
		&c.Status,
		&c.CreatedAt,
		&c.UpdatedAt,
		&c.CreatedBy,
		&c.UpdatedBy,
		&c.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("lock customer: %w", err)
	}
	return &c, nil
}

// UpdateCustomer updates customer profile details enforcing optimistic concurrency.
func (s *PgxStore) UpdateCustomer(ctx context.Context, c *Customer, expectedVersion int64) error {
	c.UpdatedAt = time.Now().UTC()
	query := `
		UPDATE crm.customer
		SET customer_group_id = $1, name = $2, email = $3,
		    phone = $4, tax_id = $5, status = $6, updated_at = $7,
		    updated_by = $8, version = version + 1
		WHERE id = $9 AND version = $10
		RETURNING version
	`
	var newVersion int64
	err := s.exec(ctx).QueryRow(ctx, query,
		c.CustomerGroupID,
		c.Name,
		c.Email,
		c.Phone,
		c.TaxID,
		c.Status,
		c.UpdatedAt,
		c.UpdatedBy,
		c.ID,
		expectedVersion,
	).Scan(&newVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Determine whether it's NotFound or VersionConflict
			var currentVersion int64
			errCheck := s.exec(ctx).QueryRow(ctx, "SELECT version FROM crm.customer WHERE id = $1", c.ID).Scan(&currentVersion)
			if errCheck != nil {
				return ErrCustomerNotFound
			}
			return ErrCustomerVersionConflict
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrCustomerNumberConflict
		}
		return fmt.Errorf("update customer: %w", err)
	}
	c.Version = newVersion
	return nil
}

// ListCustomers retrieves a paginated and filtered list of customer profiles.
func (s *PgxStore) ListCustomers(ctx context.Context, filter ListCustomersFilter) ([]Customer, int64, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}

	if filter.GroupID != nil {
		conditions = append(conditions, fmt.Sprintf("customer_group_id = $%d", argIdx))
		args = append(args, *filter.GroupID)
		argIdx++
	}

	if filter.Email != "" {
		conditions = append(conditions, fmt.Sprintf("email = $%d", argIdx))
		args = append(args, filter.Email)
		argIdx++
	}

	if filter.Phone != "" {
		conditions = append(conditions, fmt.Sprintf("phone = $%d", argIdx))
		args = append(args, filter.Phone)
		argIdx++
	}

	if filter.Query != "" {
		queryPattern := "%" + strings.TrimSpace(filter.Query) + "%"
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR customer_no ILIKE $%d OR email ILIKE $%d OR phone ILIKE $%d)", argIdx, argIdx, argIdx, argIdx))
		args = append(args, queryPattern)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT count(*) FROM crm.customer %s", whereClause)
	var total int64
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customers: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, customer_no, customer_group_id, name,
		       email, phone, tax_id, status, created_at, updated_at,
		       created_by, updated_by, version
		FROM crm.customer
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)

	limit := filter.PageSize
	if limit <= 0 {
		limit = 50
	}
	offset := (filter.Page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	args = append(args, limit, offset)
	rows, err := s.exec(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()

	var customers []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(
			&c.ID,
			&c.TenantID,
			&c.CustomerNo,
			&c.CustomerGroupID,
			&c.Name,
			&c.Email,
			&c.Phone,
			&c.TaxID,
			&c.Status,
			&c.CreatedAt,
			&c.UpdatedAt,
			&c.CreatedBy,
			&c.UpdatedBy,
			&c.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan customer: %w", err)
		}
		customers = append(customers, c)
	}
	return customers, total, nil
}

// ============================================================================
// Customer Address
// ============================================================================

// CreateCustomerAddress inserts a new address into the customer's address book.
func (s *PgxStore) CreateCustomerAddress(ctx context.Context, a *CustomerAddress) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}
	a.TenantID = pc.TenantID
	if a.CountryCode == "" {
		a.CountryCode = "ID"
	}

	query := `
		INSERT INTO crm.customer_address (
			id, tenant_id, customer_id, address_type, recipient_name,
			phone, address_line, city, province, postal_code,
			country_code, is_default
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12
		)
	`
	_, err := s.exec(ctx).Exec(ctx, query,
		a.ID,
		a.TenantID,
		a.CustomerID,
		a.AddressType,
		a.RecipientName,
		a.Phone,
		a.AddressLine,
		a.City,
		a.Province,
		a.PostalCode,
		a.CountryCode,
		a.IsDefault,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errors.New("a default address already exists for this customer")
		}
		return fmt.Errorf("create customer address: %w", err)
	}
	return nil
}

// GetCustomerAddressByID retrieves a customer address by ID.
func (s *PgxStore) GetCustomerAddressByID(ctx context.Context, id uuid.UUID) (*CustomerAddress, error) {
	query := `
		SELECT id, tenant_id, customer_id, address_type, recipient_name,
		       phone, address_line, city, province, postal_code,
		       country_code, is_default
		FROM crm.customer_address
		WHERE id = $1
	`
	var a CustomerAddress
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&a.ID,
		&a.TenantID,
		&a.CustomerID,
		&a.AddressType,
		&a.RecipientName,
		&a.Phone,
		&a.AddressLine,
		&a.City,
		&a.Province,
		&a.PostalCode,
		&a.CountryCode,
		&a.IsDefault,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAddressNotFound
		}
		return nil, fmt.Errorf("get customer address by id: %w", err)
	}
	return &a, nil
}

// UpdateCustomerAddress updates an existing address.
func (s *PgxStore) UpdateCustomerAddress(ctx context.Context, a *CustomerAddress) error {
	query := `
		UPDATE crm.customer_address
		SET address_type = $1, recipient_name = $2, phone = $3,
		    address_line = $4, city = $5, province = $6,
		    postal_code = $7, country_code = $8, is_default = $9
		WHERE id = $10
	`
	tag, err := s.exec(ctx).Exec(ctx, query,
		a.AddressType,
		a.RecipientName,
		a.Phone,
		a.AddressLine,
		a.City,
		a.Province,
		a.PostalCode,
		a.CountryCode,
		a.IsDefault,
		a.ID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errors.New("a default address already exists for this customer")
		}
		return fmt.Errorf("update customer address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAddressNotFound
	}
	return nil
}

// DeleteCustomerAddress deletes an address by ID.
func (s *PgxStore) DeleteCustomerAddress(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM crm.customer_address WHERE id = $1`
	tag, err := s.exec(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete customer address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAddressNotFound
	}
	return nil
}

// ListAddressesByCustomerID retrieves all addresses for a specific customer.
func (s *PgxStore) ListAddressesByCustomerID(ctx context.Context, customerID uuid.UUID) ([]CustomerAddress, error) {
	query := `
		SELECT id, tenant_id, customer_id, address_type, recipient_name,
		       phone, address_line, city, province, postal_code,
		       country_code, is_default
		FROM crm.customer_address
		WHERE customer_id = $1
		ORDER BY is_default DESC, id ASC
	`
	rows, err := s.exec(ctx).Query(ctx, query, customerID)
	if err != nil {
		return nil, fmt.Errorf("list customer addresses: %w", err)
	}
	defer rows.Close()

	var addresses []CustomerAddress
	for rows.Next() {
		var a CustomerAddress
		if err := rows.Scan(
			&a.ID,
			&a.TenantID,
			&a.CustomerID,
			&a.AddressType,
			&a.RecipientName,
			&a.Phone,
			&a.AddressLine,
			&a.City,
			&a.Province,
			&a.PostalCode,
			&a.CountryCode,
			&a.IsDefault,
		); err != nil {
			return nil, fmt.Errorf("scan customer address: %w", err)
		}
		addresses = append(addresses, a)
	}
	return addresses, nil
}

// ClearDefaultAddresses resets the is_default flag on all addresses for the customer,
// optionally excluding a specific address that is about to become the new default.
func (s *PgxStore) ClearDefaultAddresses(ctx context.Context, customerID uuid.UUID, excludeAddressID *uuid.UUID) error {
	query := `
		UPDATE crm.customer_address
		SET is_default = false
		WHERE customer_id = $1 AND is_default = true
		  AND ($2::uuid IS NULL OR id != $2)
	`
	_, err := s.exec(ctx).Exec(ctx, query, customerID, excludeAddressID)
	if err != nil {
		return fmt.Errorf("clear default addresses: %w", err)
	}
	return nil
}
