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

// txKey is the context key for the active CRUD transaction.
type txKey struct{}

// TxFromContext returns the active *sql.Tx from context when a CRUD handler
// has wrapped the operation in a transaction. Lifecycle hooks may use it to
// perform additional database work that is atomic with the parent operation —
// queries the hook runs through the tx see (and only commit with) the parent
// write.
func TxFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	return tx, ok
}

// WithTx returns a derived context carrying tx for hook consumption.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// Beginner is satisfied by *sql.DB. *sql.Tx does not satisfy it, which lets
// inTx skip nested begin attempts.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}
