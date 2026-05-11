// Package db holds shared low-level database abstractions used across the
// GoFastr framework subpackages. Splitting this out lets slow_query, eager
// loading, and the CRUD handler all share the same Executor interface
// without depending on each other.
package db

import (
	"context"
	"database/sql"
)

// Executor is the interface for database operations. Both *sql.DB and *sql.Tx
// satisfy it; wrappers (e.g. SlowQueryLogger) implement it by delegating to
// an inner Executor.
type Executor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
