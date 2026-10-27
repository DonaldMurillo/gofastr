package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
)

// savepointSeq names savepoints. A process-wide counter keeps every name
// distinct, so nested savepoints on one transaction never shadow each other.
var savepointSeq atomic.Uint64

// InSavepoint runs fn under a savepoint on the ambient tx. Work fn stages
// for after the commit goes to a queue of its own: a rollback to the
// savepoint drops it, and a release hands it to the outer queue, which
// runs it only when the outer transaction commits. A tx wrapped with bare
// WithTx carries no queue; fn then stages nothing here, and CRUD falls
// back to confirming its rows after that tx resolves, which also sees a
// rolled-back savepoint's rows as absent.
func InSavepoint(ctx context.Context, tx *sql.Tx, fn func(ctx context.Context, tx *sql.Tx) error) error {
	name := "gofastr_sp_" + strconv.FormatUint(savepointSeq.Add(1), 10)
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return fmt.Errorf("savepoint: %w", err)
	}
	spCtx := ctx
	parent, hasParent := CommitQueueFromContext(ctx)
	var queue *CommitQueue
	if hasParent {
		spCtx, queue = WithTxQueue(ctx, tx)
	}
	// done gates the deferred rollback, which covers a panic in fn the same
	// way InTx's does: the savepoint's writes are undone before the panic
	// reaches the outer owner, which then rolls back the whole transaction.
	// Rollbacks run without ctx's cancellation so a cancelled request still
	// undoes its own writes.
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
	// done stays false until the release lands: a release that fails (a
	// cancelled ctx, say) must not leave fn's writes on the transaction
	// while the caller sees an error, or an outer owner that handles the
	// error and commits would keep them.
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}
	done = true
	if hasParent {
		parent.Add(queue.RunAfterCommit)
	}
	return nil
}

// rollbackToSavepoint undoes the savepoint's writes and then releases it,
// so the savepoint does not stay on the transaction's stack. Both Postgres
// and SQLite keep a savepoint after ROLLBACK TO.
func rollbackToSavepoint(ctx context.Context, tx *sql.Tx, name string) error {
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name); err != nil {
		return fmt.Errorf("rollback to savepoint: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}
	return nil
}
