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

// TxFromContext returns the transaction on ctx, if any.
func TxFromContext(ctx context.Context) (*sql.Tx, bool) {
	return db.TxFromContext(ctx)
}

// TxOption configures the transaction App.InTx opens.
type TxOption func(*sql.TxOptions)

// TxIsolation sets the isolation level of the transaction App.InTx opens.
// SQLite runs every transaction serializable whatever level is asked.
func TxIsolation(level sql.IsolationLevel) TxOption {
	return func(o *sql.TxOptions) { o.Isolation = level }
}

// TxReadOnly opens a read-only transaction: a write inside it fails.
func TxReadOnly() TxOption {
	return func(o *sql.TxOptions) { o.ReadOnly = true }
}

// ErrTxOptionsNested is returned by a nested App.InTx given options.
var ErrTxOptionsNested = errors.New("app.InTx: transaction options cannot change a transaction that is already open")

// InTx runs fn in a transaction on the App's DB, with the transaction on
// fn's context. An error or panic from fn rolls back, and the error is
// returned unchanged. With a transaction already on ctx, fn runs under a
// savepoint of it instead, and options are refused with
// ErrTxOptionsNested.
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

func runTx(ctx context.Context, tx *sql.Tx, fn func(ctx context.Context, tx *sql.Tx) error) error {
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
	queue.RunAfterCommit()
	return nil
}

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
