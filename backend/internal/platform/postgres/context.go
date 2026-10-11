package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type principalContextKey struct{}
type txContextKey struct{}

var (
	principalKey = principalContextKey{}
	txKey        = txContextKey{}
)

// PrincipalContext represents the security and organizational boundary context for a request.
type PrincipalContext struct {
	TenantID    uuid.UUID
	UserID      uuid.UUID
	CompanyID   uuid.UUID
	StoreID     uuid.UUID
	WarehouseID uuid.UUID
	TerminalID  uuid.UUID
}

// WithPrincipalContext embeds a PrincipalContext into the context.
func WithPrincipalContext(ctx context.Context, pc PrincipalContext) context.Context {
	return context.WithValue(ctx, principalKey, pc)
}

// FromContext extracts the PrincipalContext from context if present.
func FromContext(ctx context.Context) (PrincipalContext, bool) {
	pc, ok := ctx.Value(principalKey).(PrincipalContext)
	return pc, ok
}

// WithTenantID is a convenience helper to inject a tenant ID into context.
func WithTenantID(ctx context.Context, tenantID uuid.UUID) context.Context {
	pc, _ := FromContext(ctx)
	pc.TenantID = tenantID
	return WithPrincipalContext(ctx, pc)
}

// WithTx embeds an active pgx.Tx into the context.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey, tx)
}

// TxFromContext extracts an active pgx.Tx from the context if present.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey).(pgx.Tx)
	return tx, ok
}

// DBTX represents a generic database executor compatible with both *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GetDBTX returns the active transaction from context if present; otherwise, it returns the fallback pool.
func GetDBTX(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}
	return pool
}
