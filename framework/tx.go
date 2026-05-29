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
	// committed gates the deferred rollback. The defer guarantees the tx is
	// closed on EVERY exit path — including when fn panics — so the pooled
	// connection (and any rows it locked) is released. Without it a panic in
	// fn unwinds past both Rollback and Commit, leaking the connection until
	// the pool/finalizer reclaims it; repeated panics exhaust MaxOpenConns.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txCtx := db.WithTx(ctx, tx)
	if err := fn(txCtx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
