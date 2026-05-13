package framework

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/db"
)

// TxFromContext returns the active *sql.Tx from context when a CRUD handler
// has wrapped the operation in a transaction. Re-exports framework/db.TxFromContext
// for callers (typed hooks etc.) that import framework directly.
func TxFromContext(ctx context.Context) (*sql.Tx, bool) {
	return db.TxFromContext(ctx)
}

// InTx runs fn inside a database transaction opened on the App's DB. The
// inner context carries the *sql.Tx so any code path that calls
// TxFromContext (typed hooks, the various do* helpers, generated repo
// methods invoked via WithTx) participates atomically.
//
// Convenience wrapper for callers that aren't already inside a CRUD hook —
// e.g. seeders, batch jobs, multi-entity write paths that need an explicit
// boundary. If fn returns an error, the tx rolls back and that error is
// returned unchanged.
func (a *App) InTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	if a.DB == nil {
		return fmt.Errorf("app.InTx: no DB configured")
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txCtx := db.WithTx(ctx, tx)
	if err := fn(txCtx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
