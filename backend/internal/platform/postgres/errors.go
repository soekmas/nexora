// Package postgres provides PostgreSQL connection pooling, explicit transaction management,
// RLS context injection, and database error mapping for NEXORA.
package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrMissingTenantContext is returned when a tenant-scoped operation is invoked without a valid tenant ID.
	ErrMissingTenantContext = errors.New("missing tenant context: tenant_id is required")

	// ErrNoActiveTransaction is returned when attempting to access a transaction from context when none exists.
	ErrNoActiveTransaction = errors.New("no active transaction in context")

	// ErrTenantContextMismatch is returned when a nested transaction attempts to switch tenant ID.
	ErrTenantContextMismatch = errors.New("tenant context mismatch in nested transaction")

	// ErrUniqueViolation is returned when an insert or update violates a unique constraint (code 23505).
	ErrUniqueViolation = errors.New("unique constraint violation")

	// ErrForeignKeyViolation is returned when a query violates a foreign key constraint (code 23503).
	ErrForeignKeyViolation = errors.New("foreign key violation")

	// ErrCheckViolation is returned when a query violates a table check constraint (code 23514).
	ErrCheckViolation = errors.New("check constraint violation")

	// ErrNotNullViolation is returned when a required column is null (code 23502).
	ErrNotNullViolation = errors.New("not null constraint violation")

	// ErrRLSPolicyViolation is returned when a query violates a row-level security policy or permission (code 42501).
	ErrRLSPolicyViolation = errors.New("row level security policy violation")

	// ErrSerializationFailure is returned on concurrent transaction conflict (code 40001).
	ErrSerializationFailure = errors.New("serialization failure")

	// ErrDeadlockDetected is returned when PostgreSQL detects a transaction deadlock (code 40P01).
	ErrDeadlockDetected = errors.New("deadlock detected")

	// ErrStatementTimeout is returned when a statement exceeds execution deadline (code 57014).
	ErrStatementTimeout = errors.New("statement timeout")
)

// MapError inspects PostgreSQL errors and maps known SQL states to sentinel domain errors.
// If the error is nil or unrecognized, it is returned unchanged.
func MapError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s (table: %s, constraint: %s)", ErrUniqueViolation, pgErr.Message, pgErr.TableName, pgErr.ConstraintName)
		case "23503":
			return fmt.Errorf("%w: %s (table: %s, constraint: %s)", ErrForeignKeyViolation, pgErr.Message, pgErr.TableName, pgErr.ConstraintName)
		case "23514":
			return fmt.Errorf("%w: %s (table: %s, constraint: %s)", ErrCheckViolation, pgErr.Message, pgErr.TableName, pgErr.ConstraintName)
		case "23502":
			return fmt.Errorf("%w: %s (table: %s, column: %s)", ErrNotNullViolation, pgErr.Message, pgErr.TableName, pgErr.ColumnName)
		case "42501":
			return fmt.Errorf("%w: %s (table: %s)", ErrRLSPolicyViolation, pgErr.Message, pgErr.TableName)
		case "40001":
			return fmt.Errorf("%w: %s", ErrSerializationFailure, pgErr.Message)
		case "40P01":
			return fmt.Errorf("%w: %s", ErrDeadlockDetected, pgErr.Message)
		case "57014":
			return fmt.Errorf("%w: %s", ErrStatementTimeout, pgErr.Message)
		}
	}

	return err
}
