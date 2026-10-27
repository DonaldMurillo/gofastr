package framework

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TxFromContext returns the active *sql.Tx from context when a CRUD handler
// has wrapped the operation in a transaction. Re-exports framework/db.TxFromContext
// for callers (typed hooks etc.) that import framework directly.
func TxFromContext(ctx context.Context) (*sql.Tx, bool) {
	return db.TxFromContext(ctx)
}

// TxOption configures the transaction App.InTx opens.
type TxOption func(*sql.TxOptions)

// TxIsolation sets the isolation level of the transaction App.InTx opens.
// Postgres takes the four ANSI levels and refuses the others with an
// error. SQLite runs every transaction serializable whatever level is
// asked, which is at least as strong as any of them.
func TxIsolation(level sql.IsolationLevel) TxOption {
	return func(o *sql.TxOptions) { o.Isolation = level }
}

// TxReadOnly opens a read-only transaction: a write inside it fails.
func TxReadOnly() TxOption {
	return func(o *sql.TxOptions) { o.ReadOnly = true }
}

// ErrTxOptionsNested is returned by a nested App.InTx given options. The
// transaction is already open, so its isolation level and read-only mode
// are fixed; running fn anyway would run it under settings it did not ask
// for.
var ErrTxOptionsNested = errors.New("app.InTx: transaction options cannot change a transaction that is already open")

// InTx runs fn inside a database transaction opened on the App's DB. The
// inner context carries the *sql.Tx so any code path that calls
// TxFromContext (typed hooks, the various do* helpers, generated repo
// methods invoked via WithTx) participates atomically.
//
// Convenience wrapper for callers that aren't already inside a CRUD hook,
// e.g. seeders, batch jobs, multi-entity write paths that need an explicit
// boundary. If fn returns an error, the tx rolls back and that error is
// returned unchanged.
//
// Called with a transaction already in ctx (from a CRUD hook, or inside
// another InTx), InTx runs fn under a savepoint on that transaction instead
// of opening a second one: an error from fn rolls back fn's own writes and
// leaves the outer transaction usable, and success releases the savepoint
// into it, so the writes commit or roll back with the outer owner. Options
// are refused there with ErrTxOptionsNested.
func (a *App) InTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error, opts ...TxOption) error {
	if tx, ok := db.TxFromContext(ctx); ok {
		if len(opts) > 0 {
			return ErrTxOptionsNested
		}
		return db.InSavepoint(ctx, tx, fn)
	}
	if a.DB == nil {
		return fmt.Errorf("app.InTx: no DB configured")
	}
	var txOpts *sql.TxOptions
	if len(opts) > 0 {
		txOpts = &sql.TxOptions{}
		for _, o := range opts {
			o(txOpts)
		}
	}
	if txOpts != nil && txOpts.ReadOnly {
		dialect, err := a.txDialect()
		if err != nil {
			return fmt.Errorf("app.InTx: %w", err)
		}
		if dialect == migrate.DialectSQLite {
			return a.inSQLiteReadOnlyTx(ctx, txOpts, fn)
		}
	}
	tx, err := a.DB.BeginTx(ctx, txOpts)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	return runTx(ctx, tx, fn)
}

// runTx runs fn on tx and commits, or rolls back on an error or a panic.
func runTx(ctx context.Context, tx *sql.Tx, fn func(ctx context.Context, tx *sql.Tx) error) error {
	// committed gates the deferred rollback. The defer guarantees the tx is
	// closed on EVERY exit path, including when fn panics, so the pooled
	// connection (and any rows it locked) is released. Without it a panic in
	// fn unwinds past both Rollback and Commit, leaking the connection until
	// the pool/finalizer reclaims it; repeated panics exhaust MaxOpenConns.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txCtx, queue := db.WithTxQueue(ctx, tx)
	if err := fn(txCtx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	// Work staged for after the commit (live-bus event emissions from CRUD
	// operations that joined this tx) runs only now, on the confirmed-commit
	// path. Error returns above drop the queue with the rollback.
	queue.RunAfterCommit()
	return nil
}

// txDialect probes the App's database once and remembers the answer. A
// failed probe is not remembered, so the next call asks again.
func (a *App) txDialect() (migrate.Dialect, error) {
	a.txDialectMu.Lock()
	defer a.txDialectMu.Unlock()
	if a.txDialectVal != "" {
		return a.txDialectVal, nil
	}
	d, err := migrate.DetectDialectStrict(a.DB)
	if err != nil {
		return "", err
	}
	a.txDialectVal = d
	return d, nil
}

// inSQLiteReadOnlyTx runs fn in a read-only transaction on SQLite. The
// SQLite driver accepts sql.TxOptions.ReadOnly and ignores it, so the
// transaction runs on a connection of its own with PRAGMA query_only set,
// and the pragma is cleared before the connection goes back to the pool. A
// connection whose pragma cannot be cleared is discarded instead, so a later
// caller never inherits a read-only connection.
func (a *App) inSQLiteReadOnlyTx(ctx context.Context, txOpts *sql.TxOptions, fn func(ctx context.Context, tx *sql.Tx) error) error {
	conn, err := a.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA query_only = OFF"); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	tx, err := conn.BeginTx(ctx, txOpts)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	return runTx(ctx, tx, fn)
}
