package promotion

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

func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			if strings.Contains(pgErr.ConstraintName, "promotion_tenant_id_code_key") ||
				strings.Contains(pgErr.ConstraintName, "code") {
				return ErrPromotionCodeExists
			}
			return fmt.Errorf("duplicate record violates constraint %s", pgErr.ConstraintName)
		case "23514": // check_violation
			if strings.Contains(pgErr.ConstraintName, "ck_promotion_status") {
				return ErrInvalidStateTransition
			}
			if strings.Contains(pgErr.ConstraintName, "ck_promotion_usage_limit") {
				return errors.New("usage_limit must be positive")
			}
			if strings.Contains(pgErr.ConstraintName, "ck_promotion_usage_count") {
				return errors.New("usage_count must be non-negative")
			}
		}
	}
	return err
}

// CreatePromotion inserts a new promotion aggregate along with its conditions and rewards.
func (s *PgxStore) CreatePromotion(ctx context.Context, p *Promotion) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	p.TenantID = pc.TenantID
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Version == 0 {
		p.Version = 1
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	if p.Status == "" {
		p.Status = StatusDraft
	}
	if p.Channel == "" {
		p.Channel = "ALL"
	}
	if p.StackingMode == "" {
		p.StackingMode = StackingModeStackable
	}

	tx := s.exec(ctx)

	query := `
		INSERT INTO promotion.promotion (
			id, tenant_id, code, name, description, channel, stacking_mode, priority,
			effective_from, effective_to, status, usage_limit, usage_count,
			created_by, updated_by, approved_by, approved_at, version, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20
		)
	`
	_, err := tx.Exec(ctx, query,
		p.ID, p.TenantID, p.Code, p.Name, p.Description, p.Channel, p.StackingMode, p.Priority,
		p.EffectiveFrom, p.EffectiveTo, p.Status, p.UsageLimit, p.UsageCount,
		p.CreatedBy, p.UpdatedBy, p.ApprovedBy, p.ApprovedAt, p.Version, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return mapPgError(err)
	}

	// Insert conditions
	for i := range p.Conditions {
		cond := &p.Conditions[i]
		if cond.ID == uuid.Nil {
			cond.ID = uuid.New()
		}
		cond.PromotionID = p.ID
		if cond.SequenceNo == 0 {
			cond.SequenceNo = i + 1
		}
		cQuery := `
			INSERT INTO promotion.promotion_condition (
				id, promotion_id, condition_type, condition_config, sequence_no
			) VALUES ($1, $2, $3, $4, $5)
		`
		if _, err := tx.Exec(ctx, cQuery, cond.ID, cond.PromotionID, cond.Type, cond.Config, cond.SequenceNo); err != nil {
			return mapPgError(err)
		}
	}

	// Insert rewards
	for i := range p.Rewards {
		rew := &p.Rewards[i]
		if rew.ID == uuid.Nil {
			rew.ID = uuid.New()
		}
		rew.PromotionID = p.ID
		if rew.SequenceNo == 0 {
			rew.SequenceNo = i + 1
		}
		rQuery := `
			INSERT INTO promotion.promotion_reward (
				id, promotion_id, reward_type, reward_config, sequence_no
			) VALUES ($1, $2, $3, $4, $5)
		`
		if _, err := tx.Exec(ctx, rQuery, rew.ID, rew.PromotionID, rew.Type, rew.Config, rew.SequenceNo); err != nil {
			return mapPgError(err)
		}
	}

	return nil
}

// GetPromotionByID loads a promotion by ID along with its conditions and rewards.
func (s *PgxStore) GetPromotionByID(ctx context.Context, id uuid.UUID) (*Promotion, error) {
	return s.getPromotion(ctx, id, false)
}

// GetPromotionByIDForUpdate loads a promotion by ID with row lock (FOR UPDATE).
func (s *PgxStore) GetPromotionByIDForUpdate(ctx context.Context, id uuid.UUID) (*Promotion, error) {
	return s.getPromotion(ctx, id, true)
}

func (s *PgxStore) getPromotion(ctx context.Context, id uuid.UUID, forUpdate bool) (*Promotion, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT id, tenant_id, code, name, description, channel, stacking_mode, priority,
		       effective_from, effective_to, status, usage_limit, usage_count,
		       created_by, updated_by, approved_by, approved_at, version, created_at, updated_at
		FROM promotion.promotion
		WHERE id = $1 AND tenant_id = $2
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	tx := s.exec(ctx)
	p := &Promotion{}
	err := tx.QueryRow(ctx, query, id, pc.TenantID).Scan(
		&p.ID, &p.TenantID, &p.Code, &p.Name, &p.Description, &p.Channel, &p.StackingMode, &p.Priority,
		&p.EffectiveFrom, &p.EffectiveTo, &p.Status, &p.UsageLimit, &p.UsageCount,
		&p.CreatedBy, &p.UpdatedBy, &p.ApprovedBy, &p.ApprovedAt, &p.Version, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPromotionNotFound
		}
		return nil, mapPgError(err)
	}

	if err := s.loadConditionsAndRewards(ctx, tx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// GetPromotionByCode loads a promotion by its unique code within tenant.
func (s *PgxStore) GetPromotionByCode(ctx context.Context, code string) (*Promotion, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	query := `
		SELECT id, tenant_id, code, name, description, channel, stacking_mode, priority,
		       effective_from, effective_to, status, usage_limit, usage_count,
		       created_by, updated_by, approved_by, approved_at, version, created_at, updated_at
		FROM promotion.promotion
		WHERE code = $1 AND tenant_id = $2
	`

	tx := s.exec(ctx)
	p := &Promotion{}
	err := tx.QueryRow(ctx, query, code, pc.TenantID).Scan(
		&p.ID, &p.TenantID, &p.Code, &p.Name, &p.Description, &p.Channel, &p.StackingMode, &p.Priority,
		&p.EffectiveFrom, &p.EffectiveTo, &p.Status, &p.UsageLimit, &p.UsageCount,
		&p.CreatedBy, &p.UpdatedBy, &p.ApprovedBy, &p.ApprovedAt, &p.Version, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPromotionNotFound
		}
		return nil, mapPgError(err)
	}

	if err := s.loadConditionsAndRewards(ctx, tx, p); err != nil {
		return nil, err
	}

	return p, nil
}

func (s *PgxStore) loadConditionsAndRewards(ctx context.Context, tx postgres.DBTX, p *Promotion) error {
	// Conditions
	cRows, err := tx.Query(ctx, `
		SELECT id, promotion_id, condition_type, condition_config, sequence_no
		FROM promotion.promotion_condition
		WHERE promotion_id = $1
		ORDER BY sequence_no ASC
	`, p.ID)
	if err != nil {
		return mapPgError(err)
	}
	defer cRows.Close()

	p.Conditions = nil
	for cRows.Next() {
		var cond Condition
		if err := cRows.Scan(&cond.ID, &cond.PromotionID, &cond.Type, &cond.Config, &cond.SequenceNo); err != nil {
			return mapPgError(err)
		}
		p.Conditions = append(p.Conditions, cond)
	}
	if err := cRows.Err(); err != nil {
		return mapPgError(err)
	}

	// Rewards
	rRows, err := tx.Query(ctx, `
		SELECT id, promotion_id, reward_type, reward_config, sequence_no
		FROM promotion.promotion_reward
		WHERE promotion_id = $1
		ORDER BY sequence_no ASC
	`, p.ID)
	if err != nil {
		return mapPgError(err)
	}
	defer rRows.Close()

	p.Rewards = nil
	for rRows.Next() {
		var rew Reward
		if err := rRows.Scan(&rew.ID, &rew.PromotionID, &rew.Type, &rew.Config, &rew.SequenceNo); err != nil {
			return mapPgError(err)
		}
		p.Rewards = append(p.Rewards, rew)
	}
	return mapPgError(rRows.Err())
}

// UpdatePromotion performs an optimistic concurrency check and updates promotion master and rules.
func (s *PgxStore) UpdatePromotion(ctx context.Context, p *Promotion, expectedVersion int64) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	tx := s.exec(ctx)

	query := `
		UPDATE promotion.promotion
		SET name = $1, description = $2, channel = $3, stacking_mode = $4, priority = $5,
		    effective_from = $6, effective_to = $7, status = $8, usage_limit = $9, usage_count = $10,
		    updated_by = $11, approved_by = $12, approved_at = $13, version = version + 1, updated_at = now()
		WHERE id = $14 AND tenant_id = $15 AND version = $16
		RETURNING version, updated_at
	`

	var newVersion int64
	var newUpdatedAt time.Time
	err := tx.QueryRow(ctx, query,
		p.Name, p.Description, p.Channel, p.StackingMode, p.Priority,
		p.EffectiveFrom, p.EffectiveTo, p.Status, p.UsageLimit, p.UsageCount,
		p.UpdatedBy, p.ApprovedBy, p.ApprovedAt,
		p.ID, pc.TenantID, expectedVersion,
	).Scan(&newVersion, &newUpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Check if row actually exists
			var exists bool
			_ = tx.QueryRow(ctx, `SELECT true FROM promotion.promotion WHERE id = $1 AND tenant_id = $2`, p.ID, pc.TenantID).Scan(&exists)
			if !exists {
				return ErrPromotionNotFound
			}
			return ErrVersionConflict
		}
		return mapPgError(err)
	}

	p.Version = newVersion
	p.UpdatedAt = newUpdatedAt

	// Update conditions if present
	if p.Conditions != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM promotion.promotion_condition WHERE promotion_id = $1`, p.ID); err != nil {
			return mapPgError(err)
		}
		for i := range p.Conditions {
			cond := &p.Conditions[i]
			if cond.ID == uuid.Nil {
				cond.ID = uuid.New()
			}
			cond.PromotionID = p.ID
			if cond.SequenceNo == 0 {
				cond.SequenceNo = i + 1
			}
			cQuery := `
				INSERT INTO promotion.promotion_condition (
					id, promotion_id, condition_type, condition_config, sequence_no
				) VALUES ($1, $2, $3, $4, $5)
			`
			if _, err := tx.Exec(ctx, cQuery, cond.ID, cond.PromotionID, cond.Type, cond.Config, cond.SequenceNo); err != nil {
				return mapPgError(err)
			}
		}
	}

	// Update rewards if present
	if p.Rewards != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM promotion.promotion_reward WHERE promotion_id = $1`, p.ID); err != nil {
			return mapPgError(err)
		}
		for i := range p.Rewards {
			rew := &p.Rewards[i]
			if rew.ID == uuid.Nil {
				rew.ID = uuid.New()
			}
			rew.PromotionID = p.ID
			if rew.SequenceNo == 0 {
				rew.SequenceNo = i + 1
			}
			rQuery := `
				INSERT INTO promotion.promotion_reward (
					id, promotion_id, reward_type, reward_config, sequence_no
				) VALUES ($1, $2, $3, $4, $5)
			`
			if _, err := tx.Exec(ctx, rQuery, rew.ID, rew.PromotionID, rew.Type, rew.Config, rew.SequenceNo); err != nil {
				return mapPgError(err)
			}
		}
	}

	return nil
}

// ListPromotions retrieves promotions matching filter criteria with pagination.
func (s *PgxStore) ListPromotions(ctx context.Context, filter PromotionFilter) ([]*Promotion, int, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, 0, postgres.ErrMissingTenantContext
	}

	tx := s.exec(ctx)

	var whereClauses []string
	var args []interface{}
	argIdx := 1

	whereClauses = append(whereClauses, fmt.Sprintf("tenant_id = $%d", argIdx))
	args = append(args, pc.TenantID)
	argIdx++

	if filter.Channel != nil && *filter.Channel != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(channel = 'ALL' OR channel = $%d)", argIdx))
		args = append(args, *filter.Channel)
		argIdx++
	}

	if filter.Status != nil && *filter.Status != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *filter.Status)
		argIdx++
	}

	if filter.Search != nil && *filter.Search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+*filter.Search+"%")
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	var total int
	countSQL := fmt.Sprintf("SELECT count(*) FROM promotion.promotion WHERE %s", whereSQL)
	if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, mapPgError(err)
	}

	limit := filter.PageSize
	if limit <= 0 {
		limit = 20
	}
	offset := (filter.Page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	selectSQL := fmt.Sprintf(`
		SELECT id, tenant_id, code, name, description, channel, stacking_mode, priority,
		       effective_from, effective_to, status, usage_limit, usage_count,
		       created_by, updated_by, approved_by, approved_at, version, created_at, updated_at
		FROM promotion.promotion
		WHERE %s
		ORDER BY priority DESC, effective_from ASC, created_at ASC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := tx.Query(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, mapPgError(err)
	}
	defer rows.Close()

	var promotions []*Promotion
	for rows.Next() {
		p := &Promotion{}
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.Code, &p.Name, &p.Description, &p.Channel, &p.StackingMode, &p.Priority,
			&p.EffectiveFrom, &p.EffectiveTo, &p.Status, &p.UsageLimit, &p.UsageCount,
			&p.CreatedBy, &p.UpdatedBy, &p.ApprovedBy, &p.ApprovedAt, &p.Version, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, 0, mapPgError(err)
		}
		promotions = append(promotions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, mapPgError(err)
	}

	// Load conditions & rewards for each promotion
	for _, p := range promotions {
		if err := s.loadConditionsAndRewards(ctx, tx, p); err != nil {
			return nil, 0, err
		}
	}

	return promotions, total, nil
}

// GetActivePromotions queries currently ACTIVE candidate promotions for evaluation.
func (s *PgxStore) GetActivePromotions(ctx context.Context, channel string, refTime time.Time) ([]Promotion, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	tx := s.exec(ctx)

	query := `
		SELECT id, tenant_id, code, name, description, channel, stacking_mode, priority,
		       effective_from, effective_to, status, usage_limit, usage_count,
		       created_by, updated_by, approved_by, approved_at, version, created_at, updated_at
		FROM promotion.promotion
		WHERE tenant_id = $1
		  AND status = 'ACTIVE'
		  AND (channel = 'ALL' OR channel = $2)
		  AND effective_from <= $3
		  AND (effective_to IS NULL OR effective_to > $3)
		ORDER BY priority DESC, effective_from ASC, created_at ASC
	`

	rows, err := tx.Query(ctx, query, pc.TenantID, channel, refTime)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	var list []Promotion
	var promoIDs []uuid.UUID
	for rows.Next() {
		var p Promotion
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.Code, &p.Name, &p.Description, &p.Channel, &p.StackingMode, &p.Priority,
			&p.EffectiveFrom, &p.EffectiveTo, &p.Status, &p.UsageLimit, &p.UsageCount,
			&p.CreatedBy, &p.UpdatedBy, &p.ApprovedBy, &p.ApprovedAt, &p.Version, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, mapPgError(err)
		}
		list = append(list, p)
		promoIDs = append(promoIDs, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	if len(promoIDs) == 0 {
		return list, nil
	}

	// Bulk load conditions
	cRows, err := tx.Query(ctx, `
		SELECT id, promotion_id, condition_type, condition_config, sequence_no
		FROM promotion.promotion_condition
		WHERE promotion_id = ANY($1)
		ORDER BY promotion_id, sequence_no ASC
	`, promoIDs)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer cRows.Close()

	conditionsByPromo := make(map[uuid.UUID][]Condition)
	for cRows.Next() {
		var c Condition
		if err := cRows.Scan(&c.ID, &c.PromotionID, &c.Type, &c.Config, &c.SequenceNo); err != nil {
			return nil, mapPgError(err)
		}
		conditionsByPromo[c.PromotionID] = append(conditionsByPromo[c.PromotionID], c)
	}
	if err := cRows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	// Bulk load rewards
	rRows, err := tx.Query(ctx, `
		SELECT id, promotion_id, reward_type, reward_config, sequence_no
		FROM promotion.promotion_reward
		WHERE promotion_id = ANY($1)
		ORDER BY promotion_id, sequence_no ASC
	`, promoIDs)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rRows.Close()

	rewardsByPromo := make(map[uuid.UUID][]Reward)
	for rRows.Next() {
		var r Reward
		if err := rRows.Scan(&r.ID, &r.PromotionID, &r.Type, &r.Config, &r.SequenceNo); err != nil {
			return nil, mapPgError(err)
		}
		rewardsByPromo[r.PromotionID] = append(rewardsByPromo[r.PromotionID], r)
	}
	if err := rRows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	for i := range list {
		list[i].Conditions = conditionsByPromo[list[i].ID]
		list[i].Rewards = rewardsByPromo[list[i].ID]
	}

	return list, nil
}

// CreatePromotionVersion saves an immutable version snapshot of a promotion.
func (s *PgxStore) CreatePromotionVersion(ctx context.Context, v *PromotionVersion) error {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}

	v.TenantID = pc.TenantID
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}

	tx := s.exec(ctx)

	query := `
		INSERT INTO promotion.promotion_version (
			id, tenant_id, promotion_id, version_no, rule_snapshot, status,
			created_at, created_by, approved_at, approved_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := tx.Exec(ctx, query,
		v.ID, v.TenantID, v.PromotionID, v.VersionNo, v.RuleSnapshot, v.Status,
		v.CreatedAt, v.CreatedBy, v.ApprovedAt, v.ApprovedBy,
	)
	return mapPgError(err)
}

// GetLatestPromotionVersion retrieves the highest version_no snapshot for a promotion.
func (s *PgxStore) GetLatestPromotionVersion(ctx context.Context, promotionID uuid.UUID) (*PromotionVersion, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	tx := s.exec(ctx)

	query := `
		SELECT id, tenant_id, promotion_id, version_no, rule_snapshot, status,
		       created_at, created_by, approved_at, approved_by
		FROM promotion.promotion_version
		WHERE promotion_id = $1 AND tenant_id = $2
		ORDER BY version_no DESC
		LIMIT 1
	`
	v := &PromotionVersion{}
	err := tx.QueryRow(ctx, query, promotionID, pc.TenantID).Scan(
		&v.ID, &v.TenantID, &v.PromotionID, &v.VersionNo, &v.RuleSnapshot, &v.Status,
		&v.CreatedAt, &v.CreatedBy, &v.ApprovedAt, &v.ApprovedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No version found yet
		}
		return nil, mapPgError(err)
	}
	return v, nil
}
