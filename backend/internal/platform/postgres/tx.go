package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxManager manages transaction boundaries, panic recovery, rollback, and automatic RLS session variables.
type TxManager interface {
	// WithinTx executes fn within an explicit transaction with mandatory tenant context and RLS parameters.
	// If tenant context is missing, it fails closed immediately with ErrMissingTenantContext before executing SQL.
	// If the context already carries an active transaction with a matching tenant, the function is executed
	// directly on the existing transaction (flat nesting / pass-through).
	WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error

	// WithinUnscopedTx executes fn within an explicit transaction without requiring a tenant context.
	// Intended exclusively for platform maintenance, health checks, or system migration tasks.
	WithinUnscopedTx(ctx context.Context, fn func(txCtx context.Context) error) error

	// WithinSavepoint executes fn within a PostgreSQL SAVEPOINT inside an existing transaction.
	// If no active transaction exists in context, it returns ErrNoActiveTransaction.
	// On fn error or panic, the savepoint is rolled back; on success, it is released.
	// This enables partial rollback of nested operations without aborting the outer transaction.
	WithinSavepoint(ctx context.Context, name string, fn func(ctx context.Context) error) error
}

type pgxTxManager struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewTxManager returns a new TxManager backed by the provided pgxpool.Pool.
func NewTxManager(pool *pgxpool.Pool, logger *slog.Logger) TxManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &pgxTxManager{
		pool:   pool,
		logger: logger,
	}
}

type txTenantContextKey struct{}

var txTenantKey = txTenantContextKey{}

// WithinTx executes fn inside a transaction with mandatory tenant context.
func (tm *pgxTxManager) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	pc, ok := FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return ErrMissingTenantContext
	}

	// Support nested transaction propagation: reuse active transaction if tenant matches
	if _, hasTx := TxFromContext(ctx); hasTx {
		if activeTenantID, ok := ctx.Value(txTenantKey).(uuid.UUID); ok && activeTenantID != pc.TenantID {
			return ErrTenantContextMismatch
		}
		return fn(ctx)
	}

	tx, err := tm.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", MapError(err))
	}

	var committed bool
	defer func() {
		if !committed {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && rbErr != pgx.ErrTxClosed {
				tm.logger.Warn("Failed to rollback transaction", slog.String("error", rbErr.Error()))
			}
		}
	}()

	// Apply transaction-local RLS session parameters using parameterized set_config
	if err := tm.applyRLSContext(ctx, tx, pc); err != nil {
		return fmt.Errorf("apply RLS context: %w", MapError(err))
	}

	txCtx := WithTx(ctx, tx)
	txCtx = context.WithValue(txCtx, txTenantKey, pc.TenantID)

	fnErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				tm.logger.Error("Panic recovered inside transaction block", slog.Any("panic", r))
				err = fmt.Errorf("panic in transaction: %v", r)
			}
		}()
		return fn(txCtx)
	}()

	if fnErr != nil {
		return fnErr
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", MapError(err))
	}

	committed = true
	return nil
}

// WithinUnscopedTx executes fn inside an unscoped transaction without requiring tenant context.
func (tm *pgxTxManager) WithinUnscopedTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	if _, hasTx := TxFromContext(ctx); hasTx {
		return fn(ctx)
	}

	tx, err := tm.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unscoped transaction: %w", MapError(err))
	}

	var committed bool
	defer func() {
		if !committed {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && rbErr != pgx.ErrTxClosed {
				tm.logger.Warn("Failed to rollback unscoped transaction", slog.String("error", rbErr.Error()))
			}
		}
	}()

	txCtx := WithTx(ctx, tx)

	fnErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				tm.logger.Error("Panic recovered inside unscoped transaction block", slog.Any("panic", r))
				err = fmt.Errorf("panic in transaction: %v", r)
			}
		}()
		return fn(txCtx)
	}()

	if fnErr != nil {
		return fnErr
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit unscoped transaction: %w", MapError(err))
	}

	committed = true
	return nil
}

// WithinSavepoint executes fn within a PostgreSQL SAVEPOINT inside the active transaction.
func (tm *pgxTxManager) WithinSavepoint(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	tx, ok := TxFromContext(ctx)
	if !ok || tx == nil {
		return ErrNoActiveTransaction
	}

	if name == "" {
		name = "sp_" + uuid.New().String()[:8]
	}

	// Create savepoint
	if _, err := tx.Exec(ctx, fmt.Sprintf("SAVEPOINT %s", name)); err != nil {
		return fmt.Errorf("create savepoint %s: %w", name, MapError(err))
	}

	fnErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				tm.logger.Error("Panic recovered inside savepoint block",
					slog.String("savepoint", name),
					slog.Any("panic", r),
				)
				err = fmt.Errorf("panic in savepoint %s: %v", name, r)
			}
		}()
		return fn(ctx)
	}()

	if fnErr != nil {
		// Rollback to savepoint on error
		if _, rbErr := tx.Exec(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name)); rbErr != nil {
			tm.logger.Warn("Failed to rollback savepoint",
				slog.String("savepoint", name),
				slog.String("error", rbErr.Error()),
			)
		}
		return fnErr
	}

	// Release savepoint on success
	if _, err := tx.Exec(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", name)); err != nil {
		return fmt.Errorf("release savepoint %s: %w", name, MapError(err))
	}

	return nil
}

func (tm *pgxTxManager) applyRLSContext(ctx context.Context, tx pgx.Tx, pc PrincipalContext) error {
	// Mandatory tenant_id
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", pc.TenantID.String()); err != nil {
		return fmt.Errorf("set app.tenant_id: %w", err)
	}

	// Optional actor and scope boundaries
	if pc.UserID != uuid.Nil {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", pc.UserID.String()); err != nil {
			return fmt.Errorf("set app.user_id: %w", err)
		}
	}
	if pc.CompanyID != uuid.Nil {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.company_id', $1, true)", pc.CompanyID.String()); err != nil {
			return fmt.Errorf("set app.company_id: %w", err)
		}
	}
	if pc.StoreID != uuid.Nil {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.store_id', $1, true)", pc.StoreID.String()); err != nil {
			return fmt.Errorf("set app.store_id: %w", err)
		}
	}
	if pc.WarehouseID != uuid.Nil {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.warehouse_id', $1, true)", pc.WarehouseID.String()); err != nil {
			return fmt.Errorf("set app.warehouse_id: %w", err)
		}
	}
	if pc.TerminalID != uuid.Nil {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.terminal_id', $1, true)", pc.TerminalID.String()); err != nil {
			return fmt.Errorf("set app.terminal_id: %w", err)
		}
	}

	return nil
}
