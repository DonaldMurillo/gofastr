package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
)

var savepointSeq atomic.Uint64

// InSavepoint runs fn under a savepoint on tx, undoing its writes on error or panic; work fn
// stages after commit moves to the outer queue on release and is dropped on rollback.
func InSavepoint(ctx context.Context, tx *sql.Tx, fn func(ctx context.Context, tx *sql.Tx) error) error {
	name := "gofastr_sp_" + strconv.FormatUint(savepointSeq.Add(1), 10)
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return fmt.Errorf("savepoint: %w", err)
	}
	spCtx := WithTx(ctx, tx)
	parent, hasParent := CommitQueueFromContext(spCtx)
	var queue *CommitQueue
	if hasParent {
		spCtx, queue = WithTxQueue(ctx, tx)
	}
	done := false
	defer func() {
		if !done {
			_ = rollbackToSavepoint(context.WithoutCancel(ctx), tx, name)
		}
	}()
	if err := fn(spCtx, tx); err != nil {
		done = true
		if rbErr := rollbackToSavepoint(context.WithoutCancel(ctx), tx, name); rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}
	done = true
	if hasParent {
		parent.Add(queue.RunAfterCommit)
	}
	return nil
}

func rollbackToSavepoint(ctx context.Context, tx *sql.Tx, name string) error {
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name); err != nil {
		return fmt.Errorf("rollback to savepoint: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}
	return nil
}
