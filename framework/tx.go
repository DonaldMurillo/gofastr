package framework

import (
	"context"
	"database/sql"
	"fmt"
)

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

// contextWithTx returns a derived context carrying tx for hook consumption.
func contextWithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// txBeginner is satisfied by *sql.DB. *sql.Tx does not satisfy it, which lets
// inTx skip nested begin attempts.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// inTx executes fn within a database transaction. On error, the transaction
// is rolled back; otherwise it commits. If the handler's DB does not support
// BeginTx (e.g., it is already a *sql.Tx from a parent operation, or a mock
// without tx support), fn runs against the existing executor and the caller
// owns the transaction lifecycle.
//
// fn receives a derived context carrying the *sql.Tx (accessible via
// TxFromContext) and a tx-bound copy of the handler — its DB field points at
// the transaction so all queries within fn participate.
func (ch *CrudHandler) inTx(ctx context.Context, fn func(ctx context.Context, ch *CrudHandler) error) error {
	starter, ok := ch.DB.(txBeginner)
	if !ok {
		return fn(ctx, ch)
	}
	tx, err := starter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txCh := *ch
	txCh.DB = tx
	txCtx := contextWithTx(ctx, tx)
	if err := fn(txCtx, &txCh); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
