// Package pricing implements persistence using pgx and PostgreSQL under forced RLS.
package pricing

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

// mapPgError maps PostgreSQL constraint and trigger failures to domain errors.
func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			if strings.Contains(pgErr.ConstraintName, "ux_price_list_active_default") {
				return ErrDuplicateDefaultList
			}
			return fmt.Errorf("duplicate record violates constraint %s", pgErr.ConstraintName)
		case "23514": // check_violation
			if strings.Contains(pgErr.ConstraintName, "ck_price_list_default_tenant_wide") {
				return ErrDefaultCompanyNotNull
			}
			if strings.Contains(pgErr.ConstraintName, "price_rule_status_check") {
				return ErrInvalidStateTransition
			}
		case "P0001": // raise_exception from triggers
			if strings.Contains(pgErr.Message, "does not belong to product") {
				return ErrVariantProductMismatch
			}
			if strings.Contains(pgErr.Message, "does not exist or does not belong to tenant") {
				return ErrVariantNotFound
			}
		}
	}
	return err
}

// ============================================================================
// Price List Persistence
// ============================================================================

// CreatePriceList inserts a new price list into pricing.price_list.
func (s *PgxStore) CreatePriceList(ctx context.Context, pl *PriceList) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	pl.TenantID = pc.TenantID
	if pl.ID == uuid.Nil {
		pl.ID = uuid.New()
	}
	if pl.Version == 0 {
		pl.Version = 1
	}
	now := time.Now().UTC()
	if pl.CreatedAt.IsZero() {
		pl.CreatedAt = now
	}
	if pl.UpdatedAt.IsZero() {
		pl.UpdatedAt = now
	}
	if pl.Status == "" {
		pl.Status = PriceListStatusActive
	}

	query := `
		INSERT INTO pricing.price_list (
			id, tenant_id, company_id, code, name, currency, channel, status, is_default, version, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
	`

	_, err := s.exec(ctx).Exec(ctx, query,
		pl.ID,
		pl.TenantID,
		pl.CompanyID,
		pl.Code,
		pl.Name,
		pl.Currency,
		pl.Channel,
		pl.Status,
		pl.IsDefault,
		pl.Version,
		pl.CreatedAt,
		pl.UpdatedAt,
	)
	return mapPgError(err)
}

// GetPriceListByID retrieves a price list by ID within tenant context.
func (s *PgxStore) GetPriceListByID(ctx context.Context, id uuid.UUID) (*PriceList, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT id, tenant_id, company_id, code, name, currency, channel, status, is_default, version, created_at, updated_at
		FROM pricing.price_list
		WHERE id = $1 AND tenant_id = $2
	`

	var pl PriceList
	err := s.exec(ctx).QueryRow(ctx, query, id, pc.TenantID).Scan(
		&pl.ID,
		&pl.TenantID,
		&pl.CompanyID,
		&pl.Code,
		&pl.Name,
		&pl.Currency,
		&pl.Channel,
		&pl.Status,
		&pl.IsDefault,
		&pl.Version,
		&pl.CreatedAt,
		&pl.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPriceListNotFound404
	}
	if err != nil {
		return nil, err
	}
	return &pl, nil
}

// GetPriceListByCode retrieves a price list by code within tenant context.
func (s *PgxStore) GetPriceListByCode(ctx context.Context, code string) (*PriceList, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT id, tenant_id, company_id, code, name, currency, channel, status, is_default, version, created_at, updated_at
		FROM pricing.price_list
		WHERE code = $1 AND tenant_id = $2
	`

	var pl PriceList
	err := s.exec(ctx).QueryRow(ctx, query, code, pc.TenantID).Scan(
		&pl.ID,
		&pl.TenantID,
		&pl.CompanyID,
		&pl.Code,
		&pl.Name,
		&pl.Currency,
		&pl.Channel,
		&pl.Status,
		&pl.IsDefault,
		&pl.Version,
		&pl.CreatedAt,
		&pl.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPriceListNotFound404
	}
	if err != nil {
		return nil, err
	}
	return &pl, nil
}

// UpdatePriceList performs an optimistic concurrency safe update.
func (s *PgxStore) UpdatePriceList(ctx context.Context, pl *PriceList, expectedVersion int64) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	pl.UpdatedAt = time.Now().UTC()
	query := `
		UPDATE pricing.price_list
		SET name = $1, status = $2, is_default = $3, version = version + 1, updated_at = $4
		WHERE id = $5 AND tenant_id = $6 AND version = $7
	`

	tag, err := s.exec(ctx).Exec(ctx, query,
		pl.Name,
		pl.Status,
		pl.IsDefault,
		pl.UpdatedAt,
		pl.ID,
		pc.TenantID,
		expectedVersion,
	)
	if err != nil {
		return mapPgError(err)
	}

	if tag.RowsAffected() == 0 {
		var exists bool
		_ = s.exec(ctx).QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing.price_list WHERE id = $1 AND tenant_id = $2)", pl.ID, pc.TenantID).Scan(&exists)
		if exists {
			return ErrVersionConflict
		}
		return ErrPriceListNotFound404
	}

	pl.Version = expectedVersion + 1
	return nil
}

// ListPriceLists returns price lists matching filter criteria with total count.
func (s *PgxStore) ListPriceLists(ctx context.Context, filter PriceListFilter) ([]*PriceList, int, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, 0, postgres.ErrMissingTenantContext
	}

	var conditions []string
	var args []any
	args = append(args, pc.TenantID)
	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", len(args)))

	if filter.Currency != nil && *filter.Currency != "" {
		args = append(args, *filter.Currency)
		conditions = append(conditions, fmt.Sprintf("currency = $%d", len(args)))
	}
	if filter.Channel != nil && *filter.Channel != "" {
		args = append(args, *filter.Channel)
		conditions = append(conditions, fmt.Sprintf("channel = $%d", len(args)))
	}
	if filter.Status != nil && *filter.Status != "" {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	whereClause := strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM pricing.price_list WHERE %s", whereClause)
	var total int
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	args = append(args, pageSize, offset)
	listQuery := fmt.Sprintf(`
		SELECT id, tenant_id, company_id, code, name, currency, channel, status, is_default, version, created_at, updated_at
		FROM pricing.price_list
		WHERE %s
		ORDER BY code ASC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args))

	rows, err := s.exec(ctx).Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []*PriceList
	for rows.Next() {
		var pl PriceList
		if err := rows.Scan(
			&pl.ID,
			&pl.TenantID,
			&pl.CompanyID,
			&pl.Code,
			&pl.Name,
			&pl.Currency,
			&pl.Channel,
			&pl.Status,
			&pl.IsDefault,
			&pl.Version,
			&pl.CreatedAt,
			&pl.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		result = append(result, &pl)
	}
	return result, total, rows.Err()
}

// GetDefaultPriceList resolves the eligible active default price list for the tenant/currency/channel.
func (s *PgxStore) GetDefaultPriceList(ctx context.Context, currency, channel string) (*PriceList, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	// Automatic default selection per DEC-P2-017 & Q-P2-001 (no implicit channel fallback):
	// is_default = true AND company_id IS NULL AND status = 'ACTIVE' AND currency = $2 AND channel = $3
	query := `
		SELECT id, tenant_id, company_id, code, name, currency, channel, status, is_default, version, created_at, updated_at
		FROM pricing.price_list
		WHERE tenant_id = $1
		  AND is_default = true
		  AND company_id IS NULL
		  AND status = 'ACTIVE'
		  AND currency = $2
		  AND channel = $3
		LIMIT 1
	`

	var pl PriceList
	err := s.exec(ctx).QueryRow(ctx, query, pc.TenantID, currency, channel).Scan(
		&pl.ID,
		&pl.TenantID,
		&pl.CompanyID,
		&pl.Code,
		&pl.Name,
		&pl.Currency,
		&pl.Channel,
		&pl.Status,
		&pl.IsDefault,
		&pl.Version,
		&pl.CreatedAt,
		&pl.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPriceListNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pl, nil
}

// ============================================================================
// Price Rule Persistence
// ============================================================================

// CreatePriceRule inserts a new price rule into pricing.price_rule.
func (s *PgxStore) CreatePriceRule(ctx context.Context, pr *PriceRule) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	pr.TenantID = pc.TenantID
	if pr.ID == uuid.Nil {
		pr.ID = uuid.New()
	}
	if pr.Version == 0 {
		pr.Version = 1
	}
	if pr.Status == "" {
		pr.Status = PriceRuleStatusDraft
	}

	query := `
		INSERT INTO pricing.price_rule (
			id, tenant_id, price_list_id, product_id, variant_id, customer_group_id, store_id,
			min_qty, unit_price, priority, effective_from, effective_to, status, version
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
	`

	_, err := s.exec(ctx).Exec(ctx, query,
		pr.ID,
		pr.TenantID,
		pr.PriceListID,
		pr.ProductID,
		pr.VariantID,
		pr.CustomerGroupID,
		pr.StoreID,
		pr.GetMinQtyRat().FloatString(6),
		pr.GetUnitPriceRat().FloatString(4),
		pr.Priority,
		pr.EffectiveFrom,
		pr.EffectiveTo,
		pr.Status,
		pr.Version,
	)
	return mapPgError(err)
}

// GetPriceRuleByID retrieves a rule by ID within tenant context.
func (s *PgxStore) GetPriceRuleByID(ctx context.Context, id uuid.UUID) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT id, tenant_id, price_list_id, product_id, variant_id, customer_group_id, store_id,
		       min_qty, unit_price, priority, effective_from, effective_to, status, version
		FROM pricing.price_rule
		WHERE id = $1 AND tenant_id = $2
	`

	var pr PriceRule
	var minQtyStr, unitPriceStr string
	err := s.exec(ctx).QueryRow(ctx, query, id, pc.TenantID).Scan(
		&pr.ID,
		&pr.TenantID,
		&pr.PriceListID,
		&pr.ProductID,
		&pr.VariantID,
		&pr.CustomerGroupID,
		&pr.StoreID,
		&minQtyStr,
		&unitPriceStr,
		&pr.Priority,
		&pr.EffectiveFrom,
		&pr.EffectiveTo,
		&pr.Status,
		&pr.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPriceRuleNotFound404
	}
	if err != nil {
		return nil, err
	}
	pr.SetScannedDecimals(minQtyStr, unitPriceStr)
	return &pr, nil
}

// GetPriceRuleByIDForUpdate locks and retrieves a rule by ID.
func (s *PgxStore) GetPriceRuleByIDForUpdate(ctx context.Context, id uuid.UUID) (*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT id, tenant_id, price_list_id, product_id, variant_id, customer_group_id, store_id,
		       min_qty, unit_price, priority, effective_from, effective_to, status, version
		FROM pricing.price_rule
		WHERE id = $1 AND tenant_id = $2
		FOR UPDATE
	`

	var pr PriceRule
	var minQtyStrUpdate, unitPriceStrUpdate string
	err := s.exec(ctx).QueryRow(ctx, query, id, pc.TenantID).Scan(
		&pr.ID,
		&pr.TenantID,
		&pr.PriceListID,
		&pr.ProductID,
		&pr.VariantID,
		&pr.CustomerGroupID,
		&pr.StoreID,
		&minQtyStrUpdate,
		&unitPriceStrUpdate,
		&pr.Priority,
		&pr.EffectiveFrom,
		&pr.EffectiveTo,
		&pr.Status,
		&pr.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPriceRuleNotFound404
	}
	if err != nil {
		return nil, err
	}
	pr.SetScannedDecimals(minQtyStrUpdate, unitPriceStrUpdate)
	return &pr, nil
}

// UpdatePriceRule updates a price rule with optimistic version check.
func (s *PgxStore) UpdatePriceRule(ctx context.Context, pr *PriceRule, expectedVersion int64) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	query := `
		UPDATE pricing.price_rule
		SET customer_group_id = $1, store_id = $2, min_qty = $3, unit_price = $4,
		    priority = $5, effective_from = $6, effective_to = $7, status = $8, version = version + 1
		WHERE id = $9 AND tenant_id = $10 AND version = $11
	`

	tag, err := s.exec(ctx).Exec(ctx, query,
		pr.CustomerGroupID,
		pr.StoreID,
		pr.GetMinQtyRat().FloatString(6),
		pr.GetUnitPriceRat().FloatString(4),
		pr.Priority,
		pr.EffectiveFrom,
		pr.EffectiveTo,
		pr.Status,
		pr.ID,
		pc.TenantID,
		expectedVersion,
	)
	if err != nil {
		return mapPgError(err)
	}

	if tag.RowsAffected() == 0 {
		var exists bool
		_ = s.exec(ctx).QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing.price_rule WHERE id = $1 AND tenant_id = $2)", pr.ID, pc.TenantID).Scan(&exists)
		if exists {
			return ErrVersionConflict
		}
		return ErrPriceRuleNotFound404
	}

	pr.Version = expectedVersion + 1
	return nil
}

// ListPriceRules returns rules matching filter criteria.
func (s *PgxStore) ListPriceRules(ctx context.Context, filter PriceRuleFilter) ([]*PriceRule, int, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, 0, postgres.ErrMissingTenantContext
	}

	var conditions []string
	var args []any
	args = append(args, pc.TenantID)
	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", len(args)))

	if filter.PriceListID != nil {
		args = append(args, *filter.PriceListID)
		conditions = append(conditions, fmt.Sprintf("price_list_id = $%d", len(args)))
	}
	if filter.ProductID != nil {
		args = append(args, *filter.ProductID)
		conditions = append(conditions, fmt.Sprintf("product_id = $%d", len(args)))
	}
	if filter.VariantID != nil {
		args = append(args, *filter.VariantID)
		conditions = append(conditions, fmt.Sprintf("variant_id = $%d", len(args)))
	}
	if filter.StoreID != nil {
		args = append(args, *filter.StoreID)
		conditions = append(conditions, fmt.Sprintf("store_id = $%d", len(args)))
	}
	if filter.CustomerGroupID != nil {
		args = append(args, *filter.CustomerGroupID)
		conditions = append(conditions, fmt.Sprintf("customer_group_id = $%d", len(args)))
	}
	if filter.Status != nil && *filter.Status != "" {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	whereClause := strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM pricing.price_rule WHERE %s", whereClause)
	var total int
	if err := s.exec(ctx).QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	args = append(args, pageSize, offset)
	listQuery := fmt.Sprintf(`
		SELECT id, tenant_id, price_list_id, product_id, variant_id, customer_group_id, store_id,
		       min_qty, unit_price, priority, effective_from, effective_to, status, version
		FROM pricing.price_rule
		WHERE %s
		ORDER BY effective_from DESC, id ASC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args))

	rows, err := s.exec(ctx).Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []*PriceRule
	for rows.Next() {
		var pr PriceRule
		var minQtyStr, unitPriceStr string
		if err := rows.Scan(
			&pr.ID,
			&pr.TenantID,
			&pr.PriceListID,
			&pr.ProductID,
			&pr.VariantID,
			&pr.CustomerGroupID,
			&pr.StoreID,
			&minQtyStr,
			&unitPriceStr,
			&pr.Priority,
			&pr.EffectiveFrom,
			&pr.EffectiveTo,
			&pr.Status,
			&pr.Version,
		); err != nil {
			return nil, 0, err
		}
		pr.SetScannedDecimals(minQtyStr, unitPriceStr)
		result = append(result, &pr)
	}
	return result, total, rows.Err()
}

// GetCandidateRules retrieves active rules within effective dates for given products and variants.
func (s *PgxStore) GetCandidateRules(ctx context.Context, priceListID uuid.UUID, productIDs []uuid.UUID, variantIDs []uuid.UUID, refTime time.Time) ([]*PriceRule, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	if len(productIDs) == 0 && len(variantIDs) == 0 {
		return nil, nil
	}

	query := `
		SELECT id, tenant_id, price_list_id, product_id, variant_id, customer_group_id, store_id,
		       min_qty, unit_price, priority, effective_from, effective_to, status, version
		FROM pricing.price_rule
		WHERE tenant_id = $1
		  AND price_list_id = $2
		  AND status = 'ACTIVE'
		  AND effective_from <= $3
		  AND (effective_to IS NULL OR effective_to > $3)
		  AND (product_id = ANY($4) OR variant_id = ANY($5))
		ORDER BY effective_from DESC, id ASC
	`

	rows, err := s.exec(ctx).Query(ctx, query, pc.TenantID, priceListID, refTime, productIDs, variantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*PriceRule
	for rows.Next() {
		var pr PriceRule
		var minQtyStr, unitPriceStr string
		if err := rows.Scan(
			&pr.ID,
			&pr.TenantID,
			&pr.PriceListID,
			&pr.ProductID,
			&pr.VariantID,
			&pr.CustomerGroupID,
			&pr.StoreID,
			&minQtyStr,
			&unitPriceStr,
			&pr.Priority,
			&pr.EffectiveFrom,
			&pr.EffectiveTo,
			&pr.Status,
			&pr.Version,
		); err != nil {
			return nil, err
		}
		pr.SetScannedDecimals(minQtyStr, unitPriceStr)
		rules = append(rules, &pr)
	}
	return rules, rows.Err()
}

// ============================================================================
// Context Helper Queries
// ============================================================================

// GetStore retrieves store details and tenant scope.
func (s *PgxStore) GetStore(ctx context.Context, storeID uuid.UUID) (companyID uuid.UUID, status string, err error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return uuid.Nil, "", postgres.ErrMissingTenantContext
	}

	query := `
		SELECT company_id, status
		FROM organization.store
		WHERE id = $1 AND tenant_id = $2
	`
	err = s.exec(ctx).QueryRow(ctx, query, storeID, pc.TenantID).Scan(&companyID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrStoreNotFound
	}
	return companyID, status, err
}

// GetCustomerGroup retrieves customer group if customer exists.
func (s *PgxStore) GetCustomerGroup(ctx context.Context, customerID uuid.UUID) (customerGroupID *uuid.UUID, status string, err error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, "", postgres.ErrMissingTenantContext
	}

	query := `
		SELECT customer_group_id, status
		FROM crm.customer
		WHERE id = $1 AND tenant_id = $2
	`
	err = s.exec(ctx).QueryRow(ctx, query, customerID, pc.TenantID).Scan(&customerGroupID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil // Not an error if optional customer not found or guest
	}
	return customerGroupID, status, err
}

// GetVariants resolves a set of variant IDs to their parent product IDs.
func (s *PgxStore) GetVariants(ctx context.Context, variantIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	if len(variantIDs) == 0 {
		return map[uuid.UUID]uuid.UUID{}, nil
	}

	query := `
		SELECT id, product_id
		FROM master.product_variant
		WHERE tenant_id = $1 AND id = ANY($2)
	`

	rows, err := s.exec(ctx).Query(ctx, query, pc.TenantID, variantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]uuid.UUID)
	for rows.Next() {
		var vID, pID uuid.UUID
		if err := rows.Scan(&vID, &pID); err != nil {
			return nil, err
		}
		result[vID] = pID
	}
	return result, rows.Err()
}
