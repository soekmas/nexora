package audit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

// Store defines the append-only audit persistence contract.
// Notice: There are strictly no Update or Delete operations defined.
type Store interface {
	// Record persists an audit log and optional field-level data changes inside an active database transaction.
	// Fails closed with ErrNoActiveTransaction if no transaction is active.
	Record(ctx context.Context, log *Log, changes []*DataChange) error

	// RecordStandalone persists an audit log outside a business transaction by executing within
	// a managed transaction using TxManager and establishing tenant RLS context.
	RecordStandalone(ctx context.Context, log *Log) error

	// RecordSecurityEvent persists a security event into audit.security_event.
	RecordSecurityEvent(ctx context.Context, evt *SecurityEvent) error

	// RecordLoginHistory persists an authentication event into audit.login_history.
	RecordLoginHistory(ctx context.Context, entry *LoginHistory) error

	// FindAuditLogs queries audit logs for the authenticated tenant with bounded pagination.
	FindAuditLogs(ctx context.Context, filter Filter) ([]*Log, int, error)

	// GetDataChanges retrieves data changes for a specific audit log ID, strictly scoped to the tenant.
	GetDataChanges(ctx context.Context, auditLogID int64) ([]*DataChange, error)
}

// PgxStore implements Store using PostgreSQL pgxpool and transaction manager.
type PgxStore struct {
	pool   *pgxpool.Pool
	tm     postgres.TxManager
	logger *slog.Logger
}

// NewPgxStore constructs a new PgxStore.
func NewPgxStore(pool *pgxpool.Pool, tm postgres.TxManager, logger *slog.Logger) *PgxStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &PgxStore{
		pool:   pool,
		tm:     tm,
		logger: logger,
	}
}

// Record persists an audit log and any associated field-level data changes inside an active database transaction.
func (s *PgxStore) Record(ctx context.Context, log *Log, changes []*DataChange) error {
	tx, ok := postgres.TxFromContext(ctx)
	if !ok || tx == nil {
		return ErrNoActiveTransaction
	}

	if log == nil {
		return fmt.Errorf("audit: log cannot be nil")
	}

	// Resolve ambient security context
	pc, hasPC := postgres.FromContext(ctx)
	if hasPC && pc.TenantID != uuid.Nil {
		if log.TenantID == uuid.Nil {
			log.TenantID = pc.TenantID
		} else if log.TenantID != pc.TenantID {
			return fmt.Errorf("%w: record tenant %s != context tenant %s", ErrTenantMismatch, log.TenantID, pc.TenantID)
		}

		if log.CompanyID == nil && pc.CompanyID != uuid.Nil {
			log.CompanyID = &pc.CompanyID
		}
		if log.StoreID == nil && pc.StoreID != uuid.Nil {
			log.StoreID = &pc.StoreID
		}
		if log.TerminalID == nil && pc.TerminalID != uuid.Nil {
			log.TerminalID = &pc.TerminalID
		}
		if log.UserID == nil && pc.UserID != uuid.Nil {
			log.UserID = &pc.UserID
		}
	}

	if err := ValidateLog(log); err != nil {
		return err
	}

	const insertLogSQL = `
		INSERT INTO audit.audit_log (
			tenant_id, company_id, store_id, terminal_id, user_id,
			module, action, resource_type, resource_id, business_number,
			status, request_id, trace_id, ip_address, user_agent,
			reason, metadata
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17
		) RETURNING id, created_at
	`

	var metadataJSON any = log.Metadata
	if len(log.Metadata) == 0 {
		metadataJSON = []byte("{}")
	}

	err := tx.QueryRow(ctx, insertLogSQL,
		log.TenantID,
		log.CompanyID,
		log.StoreID,
		log.TerminalID,
		log.UserID,
		log.Module,
		log.Action,
		log.ResourceType,
		log.ResourceID,
		log.BusinessNumber,
		log.Status,
		log.RequestID,
		log.TraceID,
		log.IPAddress,
		log.UserAgent,
		log.Reason,
		metadataJSON,
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", postgres.MapError(err))
	}

	// Persist associated data changes
	if len(changes) > 0 {
		const insertChangeSQL = `
			INSERT INTO audit.data_change (
				audit_log_id, table_schema, table_name, row_id,
				field_name, old_value, new_value
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, created_at
		`

		for _, ch := range changes {
			if err := ValidateDataChange(ch); err != nil {
				return err
			}

			ch.AuditLogID = log.ID

			var oldValJSON any = ch.OldValue
			if len(ch.OldValue) == 0 {
				oldValJSON = nil
			}
			var newValJSON any = ch.NewValue
			if len(ch.NewValue) == 0 {
				newValJSON = nil
			}

			err := tx.QueryRow(ctx, insertChangeSQL,
				ch.AuditLogID,
				ch.TableSchema,
				ch.TableName,
				ch.RowID,
				ch.FieldName,
				oldValJSON,
				newValJSON,
			).Scan(&ch.ID, &ch.CreatedAt)
			if err != nil {
				return fmt.Errorf("insert data change: %w", postgres.MapError(err))
			}
		}
	}

	return nil
}

// RecordStandalone persists an audit log outside a business transaction by creating a managed transaction with RLS context.
func (s *PgxStore) RecordStandalone(ctx context.Context, log *Log) error {
	if log == nil {
		return fmt.Errorf("audit: log cannot be nil")
	}

	// If context does not have tenant ID, embed log's tenant ID
	targetTenantID := log.TenantID
	if pc, ok := postgres.FromContext(ctx); ok && pc.TenantID != uuid.Nil {
		targetTenantID = pc.TenantID
	}
	if targetTenantID == uuid.Nil {
		return ErrMissingTenantContext
	}

	txCtx := postgres.WithTenantID(ctx, targetTenantID)
	return s.tm.WithinTx(txCtx, func(execCtx context.Context) error {
		return s.Record(execCtx, log, nil)
	})
}

// RecordSecurityEvent persists a security event into audit.security_event.
func (s *PgxStore) RecordSecurityEvent(ctx context.Context, evt *SecurityEvent) error {
	if evt == nil {
		return fmt.Errorf("audit: security event cannot be nil")
	}

	// Populate tenant context if present
	if pc, ok := postgres.FromContext(ctx); ok && pc.TenantID != uuid.Nil {
		if evt.TenantID == nil {
			evt.TenantID = &pc.TenantID
		} else if *evt.TenantID != pc.TenantID {
			return fmt.Errorf("%w: event tenant %s != context tenant %s", ErrTenantMismatch, *evt.TenantID, pc.TenantID)
		}
		if evt.UserID == nil && pc.UserID != uuid.Nil {
			evt.UserID = &pc.UserID
		}
	}

	if err := ValidateSecurityEvent(evt); err != nil {
		return err
	}

	execFunc := func(c context.Context, tx postgres.DBTX) error {
		const insertSecSQL = `
			INSERT INTO audit.security_event (
				tenant_id, user_id, event_type, severity,
				ip_address, user_agent, request_id, trace_id, metadata
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id, occurred_at
		`

		var metadataJSON any = evt.Metadata
		if len(evt.Metadata) == 0 {
			metadataJSON = []byte("{}")
		}

		err := tx.QueryRow(c, insertSecSQL,
			evt.TenantID,
			evt.UserID,
			evt.EventType,
			evt.Severity,
			evt.IPAddress,
			evt.UserAgent,
			evt.RequestID,
			evt.TraceID,
			metadataJSON,
		).Scan(&evt.ID, &evt.OccurredAt)
		if err != nil {
			return fmt.Errorf("insert security event: %w", postgres.MapError(err))
		}
		return nil
	}

	if tx, ok := postgres.TxFromContext(ctx); ok && tx != nil {
		return execFunc(ctx, tx)
	}

	targetCtx := ctx
	if evt.TenantID != nil && *evt.TenantID != uuid.Nil {
		targetCtx = postgres.WithTenantID(ctx, *evt.TenantID)
	}
	return s.tm.WithinTx(targetCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		return execFunc(txCtx, tx)
	})
}

// RecordLoginHistory persists an authentication outcome into audit.login_history.
func (s *PgxStore) RecordLoginHistory(ctx context.Context, entry *LoginHistory) error {
	if entry == nil {
		return fmt.Errorf("audit: login history cannot be nil")
	}

	if pc, ok := postgres.FromContext(ctx); ok && pc.TenantID != uuid.Nil {
		if entry.TenantID == nil {
			entry.TenantID = &pc.TenantID
		}
		if entry.UserID == nil && pc.UserID != uuid.Nil {
			entry.UserID = &pc.UserID
		}
	}

	if err := ValidateLoginHistory(entry); err != nil {
		return err
	}

	execFunc := func(c context.Context, tx postgres.DBTX) error {
		const insertLoginSQL = `
			INSERT INTO audit.login_history (
				tenant_id, user_id, username, success,
				failure_reason, ip_address, user_agent
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, occurred_at
		`

		err := tx.QueryRow(c, insertLoginSQL,
			entry.TenantID,
			entry.UserID,
			entry.Username,
			entry.Success,
			entry.FailureReason,
			entry.IPAddress,
			entry.UserAgent,
		).Scan(&entry.ID, &entry.OccurredAt)
		if err != nil {
			return fmt.Errorf("insert login history: %w", postgres.MapError(err))
		}
		return nil
	}

	if tx, ok := postgres.TxFromContext(ctx); ok && tx != nil {
		return execFunc(ctx, tx)
	}

	targetCtx := ctx
	if entry.TenantID != nil && *entry.TenantID != uuid.Nil {
		targetCtx = postgres.WithTenantID(ctx, *entry.TenantID)
	}
	return s.tm.WithinTx(targetCtx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)
		return execFunc(txCtx, tx)
	})
}

// FindAuditLogs queries audit logs with deterministic ordering and bounded pagination.
func (s *PgxStore) FindAuditLogs(ctx context.Context, filter Filter) ([]*Log, int, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, 0, ErrMissingTenantContext
	}

	if filter.TenantID == uuid.Nil {
		filter.TenantID = pc.TenantID
	} else if filter.TenantID != pc.TenantID {
		return nil, 0, ErrTenantMismatch
	}

	// Enforce bounded pagination
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	whereClauses := []string{"tenant_id = $1"}
	args := []any{filter.TenantID}
	argIdx := 2

	if filter.UserID != nil && *filter.UserID != uuid.Nil {
		whereClauses = append(whereClauses, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, *filter.UserID)
		argIdx++
	}
	if filter.Module != nil && strings.TrimSpace(*filter.Module) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("module = $%d", argIdx))
		args = append(args, strings.TrimSpace(*filter.Module))
		argIdx++
	}
	if filter.Action != nil && strings.TrimSpace(*filter.Action) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, strings.TrimSpace(*filter.Action))
		argIdx++
	}
	if filter.ResourceType != nil && strings.TrimSpace(*filter.ResourceType) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("resource_type = $%d", argIdx))
		args = append(args, strings.TrimSpace(*filter.ResourceType))
		argIdx++
	}
	if filter.ResourceID != nil && *filter.ResourceID != uuid.Nil {
		whereClauses = append(whereClauses, fmt.Sprintf("resource_id = $%d", argIdx))
		args = append(args, *filter.ResourceID)
		argIdx++
	}
	if filter.Status != nil && strings.TrimSpace(*filter.Status) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, strings.TrimSpace(*filter.Status))
		argIdx++
	}
	if filter.From != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filter.To)
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	dbtx := postgres.GetDBTX(ctx, s.pool)

	// Count query
	countSQL := fmt.Sprintf("SELECT count(*) FROM audit.audit_log WHERE %s", whereSQL)
	var totalCount int
	if err := dbtx.QueryRow(ctx, countSQL, args...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", postgres.MapError(err))
	}

	// Select query with deterministic ordering
	selectSQL := fmt.Sprintf(`
		SELECT id, tenant_id, company_id, store_id, terminal_id, user_id,
		       module, action, resource_type, resource_id, business_number,
		       status, request_id, trace_id, ip_address::text, user_agent,
		       reason, metadata, created_at
		FROM audit.audit_log
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)
	rows, err := dbtx.Query(ctx, selectSQL, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("select audit logs: %w", postgres.MapError(err))
	}
	defer rows.Close()

	var logs []*Log
	for rows.Next() {
		var (
			al        Log
			ipStr     *string
			createdAt time.Time
		)

		err := rows.Scan(
			&al.ID,
			&al.TenantID,
			&al.CompanyID,
			&al.StoreID,
			&al.TerminalID,
			&al.UserID,
			&al.Module,
			&al.Action,
			&al.ResourceType,
			&al.ResourceID,
			&al.BusinessNumber,
			&al.Status,
			&al.RequestID,
			&al.TraceID,
			&ipStr,
			&al.UserAgent,
			&al.Reason,
			&al.Metadata,
			&createdAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", postgres.MapError(err))
		}
		al.IPAddress = ipStr
		al.CreatedAt = createdAt
		logs = append(logs, &al)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", postgres.MapError(err))
	}

	return logs, totalCount, nil
}

// GetDataChanges retrieves data changes for a specific audit log ID, strictly scoped to the authenticated tenant.
func (s *PgxStore) GetDataChanges(ctx context.Context, auditLogID int64) ([]*DataChange, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, ErrMissingTenantContext
	}

	dbtx := postgres.GetDBTX(ctx, s.pool)

	const querySQL = `
		SELECT dc.id, dc.audit_log_id, dc.table_schema, dc.table_name, dc.row_id,
		       dc.field_name, dc.old_value, dc.new_value, dc.created_at
		FROM audit.data_change dc
		JOIN audit.audit_log al ON dc.audit_log_id = al.id
		WHERE dc.audit_log_id = $1 AND al.tenant_id = $2
		ORDER BY dc.id ASC
	`

	rows, err := dbtx.Query(ctx, querySQL, auditLogID, pc.TenantID)
	if err != nil {
		return nil, fmt.Errorf("query data changes: %w", postgres.MapError(err))
	}
	defer rows.Close()

	var changes []*DataChange
	for rows.Next() {
		var dc DataChange
		if err := rows.Scan(
			&dc.ID,
			&dc.AuditLogID,
			&dc.TableSchema,
			&dc.TableName,
			&dc.RowID,
			&dc.FieldName,
			&dc.OldValue,
			&dc.NewValue,
			&dc.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan data change: %w", postgres.MapError(err))
		}
		changes = append(changes, &dc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate data changes: %w", postgres.MapError(err))
	}

	return changes, nil
}
