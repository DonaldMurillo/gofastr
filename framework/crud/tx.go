package crud

import (
	"context"
	"fmt"

	"github.com/gofastr/gofastr/framework/db"
)

// inTx executes fn within a database transaction. On error, the transaction
// is rolled back; otherwise it commits. If the handler's DB does not support
// BeginTx (e.g., it is already a *sql.Tx from a parent operation, or a mock
// without tx support), fn runs against the existing executor and the caller
// owns the transaction lifecycle.
//
// fn receives a derived context carrying the *sql.Tx (accessible via
// db.TxFromContext) and a tx-bound copy of the handler — its DB field points
// at the transaction so all queries within fn participate.
func (ch *CrudHandler) inTx(ctx context.Context, fn func(ctx context.Context, ch *CrudHandler) error) error {
	starter, ok := ch.DB.(db.Beginner)
	if !ok {
		return fn(ctx, ch)
	}
	tx, err := starter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txCh := *ch
	txCh.DB = tx
	txCtx := db.WithTx(ctx, tx)
	if err := fn(txCtx, &txCh); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
