// Package db holds shared low-level database abstractions used across the
// GoFastr framework subpackages. Splitting this out lets slow_query, eager
// loading, and the CRUD handler all share the same Executor interface
// without depending on each other.
package db

import (
	"context"
	"database/sql"
	"sync"
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
// perform additional database work that is atomic with the parent operation,
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

// WithoutTx masks any ambient transaction (and its commit queue) in ctx,
// keeping everything else — tenant, owner, request identity. Use it when
// handing a context to a goroutine that outlives or runs beside the
// transaction's owner: a *sql.Tx shares one connection, and a statement
// from a second goroutine interleaves with the owner's on the wire (#353).
// TxFromContext and CommitQueueFromContext report absent on the result.
func WithoutTx(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, txKey{}, nil)
	return context.WithValue(ctx, commitQueueKey{}, nil)
}

// Beginner is satisfied by *sql.DB. *sql.Tx does not satisfy it, which lets
// inTx skip nested begin attempts.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// CommitQueue collects work that must run only after the transaction that
// owns it commits. CRUD handlers queue live-bus emissions here instead of
// probing the live *sql.Tx from a goroutine: a probe statement races the
// owner's own statements on the transaction's single connection, and the
// interleaved wire protocol hands each side the other's results — observed
// as sql.ErrNoRows from an INSERT…RETURNING and as pq "syntax error at end
// of input" from a well-formed query (#353). Draining after Commit also
// answers commit-vs-rollback exactly, where the probe had to re-derive it
// from row state.
type CommitQueue struct {
	mu      sync.Mutex
	fns     []func()
	drained bool
}

// Add queues fn to run after the owning transaction commits. Safe for
// concurrent use. An Add that arrives after the queue has drained runs fn
// immediately: the drain only ever happens once Commit has succeeded, so
// the work's precondition already holds, and appending to a list nothing
// will read again would lose it silently.
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

// RunAfterCommit runs and clears the queued work, in Add order. The tx
// owner calls it exactly once, after Commit returns nil; a rollback path
// simply drops the queue, and the queued work never runs. Calling it again
// is a no-op.
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

// commitQueueKey is the context key for the owning transaction's queue.
type commitQueueKey struct{}

// WithTxQueue derives a context carrying both tx and a fresh CommitQueue,
// returning the queue for the owner to drain. Framework-owned transactions
// (App.InTx and crud's inTx) use this instead of WithTx, so emissions
// staged inside the transaction fire only after it commits.
func WithTxQueue(ctx context.Context, tx *sql.Tx) (context.Context, *CommitQueue) {
	q := &CommitQueue{}
	return context.WithValue(WithTx(ctx, tx), commitQueueKey{}, q), q
}

// CommitQueueFromContext returns the commit queue attached by the ambient
// transaction's owner. A context carrying a tx but no queue means the
// caller wrapped its own transaction with WithTx directly; emissions for
// those fall back to POLLING the live *sql.Tx (a "SELECT 1" every few
// milliseconds until it reports sql.ErrTxDone) and then re-checking the
// row on the base connection. That probe statement can interleave with
// the owner's own statements on the transaction's single connection —
// the race #353 removed from every framework-owned path. There is no way
// to passively learn a foreign transaction's fate, so callers who own
// their tx should prefer WithTxQueue and drain the queue themselves after
// a successful Commit.
func CommitQueueFromContext(ctx context.Context) (*CommitQueue, bool) {
	q, ok := ctx.Value(commitQueueKey{}).(*CommitQueue)
	return q, ok
}
