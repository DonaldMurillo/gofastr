// Package db holds the shared database seams: Executor, the transaction on the context, CommitQueue and InSavepoint.
package db

import (
	"context"
	"database/sql"
	"sync"
)

// Executor is satisfied by *sql.DB, *sql.Tx and wrappers that delegate to one.
type Executor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type txKey struct{}

// TxFromContext returns the ambient transaction, so a hook's queries commit or roll back with the write.
func TxFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	return tx, ok
}

// WithTx returns ctx carrying tx and no commit queue.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// WithoutTx hides the ambient transaction and its commit queue, for a goroutine that runs beside its owner.
func WithoutTx(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, txKey{}, nil)
	return context.WithValue(ctx, commitQueueKey{}, nil)
}

// Beginner is satisfied by *sql.DB and not by *sql.Tx.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// CommitQueue holds work to run only after its transaction commits.
type CommitQueue struct {
	mu      sync.Mutex
	fns     []func()
	drained bool
}

// Add queues fn, or runs it at once when the queue has already drained; safe for concurrent use.
func (q *CommitQueue) Add(fn func()) {
	q.mu.Lock()
	if q.drained {
		q.mu.Unlock()
		fn()
		return
	}
	q.fns = append(q.fns, fn)
	q.mu.Unlock()
}

// RunAfterCommit runs the queued work once, in Add order; call it only after Commit succeeds.
func (q *CommitQueue) RunAfterCommit() {
	q.mu.Lock()
	fns := q.fns
	q.fns = nil
	q.drained = true
	q.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

type commitQueueKey struct{}

// WithTxQueue returns ctx carrying tx and a fresh CommitQueue, and the queue for the owner to drain.
func WithTxQueue(ctx context.Context, tx *sql.Tx) (context.Context, *CommitQueue) {
	q := &CommitQueue{}
	return context.WithValue(WithTx(ctx, tx), commitQueueKey{}, q), q
}

// CommitQueueFromContext returns the ambient transaction's queue; a tx set by bare WithTx has none.
func CommitQueueFromContext(ctx context.Context) (*CommitQueue, bool) {
	q, ok := ctx.Value(commitQueueKey{}).(*CommitQueue)
	return q, ok
}
