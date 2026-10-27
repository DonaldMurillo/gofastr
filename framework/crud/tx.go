package crud

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/db"
)

// conn is what a statement runs on: the transaction ctx carries
// (App.InTx, a hook's), else ch.DB. Writes join it through inTx; a read
// on the pool inside App.InTx missed the transaction's own uncommitted
// rows, and on a one-connection pool waited on the transaction forever.
func (ch *CrudHandler) conn(ctx context.Context) DBExecutor {
	if tx, ok := db.TxFromContext(ctx); ok {
		return tx
	}
	return ch.DB
}

// inTx executes fn within a database transaction. On error, the transaction
// is rolled back; otherwise it commits. If the handler's DB does not support
// BeginTx (e.g., it is already a *sql.Tx from a parent operation, or a mock
// without tx support), fn runs against the existing executor and the caller
// owns the transaction lifecycle.
//
// fn receives a derived context carrying the *sql.Tx (accessible via
// db.TxFromContext) and a tx-bound copy of the handler, its DB field points
// at the transaction so all queries within fn participate.
//
// Every write enters here, so this is where an audit operation override
// stops: a hook that writes during a restore or purge calls back in
// through inTx, and its write records its own operation even when it
// names the same record.
func (ch *CrudHandler) inTx(ctx context.Context, fn func(ctx context.Context, ch *CrudHandler) error) error {
	ctx = withoutAuditOperation(ctx)
	// Reuse an ambient transaction already in the context (e.g. one opened by
	// App.InTx). This is what lets several CRUD operations, and any
	// hand-written query-builder SQL run on the same tx, compose into one
	// atomic unit: the operation joins the outer transaction and the outer
	// owner commits or rolls back. We must NOT begin a nested tx or commit here.
	// The operation runs under a savepoint, so one that fails undoes its own
	// writes: a caller that handles the error and commits keeps only what
	// succeeded, and on Postgres the failed statement does not abort the
	// outer transaction.
	if tx, ok := db.TxFromContext(ctx); ok {
		opCtx, held := holdCascadeEmits(ctx)
		if err := db.InSavepoint(opCtx, tx, func(ctx context.Context, tx *sql.Tx) error {
			txCh := *ch
			txCh.DB = tx
			return fn(ctx, &txCh)
		}); err != nil {
			return err
		}
		held.release(ctx, ch.DB)
		return nil
	}
	starter, ok := ch.DB.(db.Beginner)
	if !ok {
		opCtx, held := holdCascadeEmits(ctx)
		if err := fn(opCtx, ch); err != nil {
			return err
		}
		held.release(ctx, ch.DB)
		return nil
	}
	tx, err := starter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txCh := *ch
	txCh.DB = tx
	txCtx, queue := db.WithTxQueue(ctx, tx)
	// The deferred rollback guarantees the tx is closed on EVERY exit path,
	// including a panicking hook inside fn: without it the panic unwinds
	// past both Rollback and Commit and the pooled connection (plus any row
	// locks) leaks until the finalizer. Mirrors App.InTx.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(txCtx, &txCh); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	// Emissions staged inside the tx (e.g. a hook calling another entity's
	// CreateOne with the tx context) fire only now, on confirmed commit;
	// every other exit path drops the queue with the rollback.
	queue.RunAfterCommit()
	return nil
}

// cascadeEmits holds what a cascade's child writes announce until the
// operation they belong to has succeeded. A child write runs on the
// operation's transaction, so its handler is bound to the *sql.Tx and
// EmitEvent has no pool to confirm the write against: it published at
// once, and a cascade that failed on a later item had already announced
// the first child. An operation inTx owns parks those emissions on its
// commit queue, which drops them with the rollback; an operation that
// joins a caller's transaction with no queue, or runs on a handler bound
// to one, holds them here and releases them only when it succeeds.
type cascadeEmits struct {
	mu      sync.Mutex
	pending []cascadeEmit
}

type cascadeEmit struct {
	ch        *CrudHandler
	ctx       context.Context
	eventType string
	record    any
}

type cascadeEmitsKey struct{}

// holdCascadeEmits gives the operation about to run a collector.
func holdCascadeEmits(ctx context.Context) (context.Context, *cascadeEmits) {
	held := &cascadeEmits{}
	return context.WithValue(ctx, cascadeEmitsKey{}, held), held
}

// emitCascade announces a cascade child's write. A commit queue in ctx
// comes first: it holds the emission until commit and drops it when the
// savepoint or transaction rolls back, and the transaction it belongs to
// can commit on its own even if an enclosing held operation fails. Under a
// collector the emission waits for the operation to succeed; otherwise it
// goes out as EmitEvent sends it.
func (ch *CrudHandler) emitCascade(ctx context.Context, eventType string, record any) {
	if _, queued := db.CommitQueueFromContext(ctx); !queued {
		if held, ok := ctx.Value(cascadeEmitsKey{}).(*cascadeEmits); ok {
			child := *ch
			held.mu.Lock()
			held.pending = append(held.pending, cascadeEmit{ch: &child, ctx: ctx, eventType: eventType, record: record})
			held.mu.Unlock()
			return
		}
	}
	ch.EmitEvent(ctx, eventType, record)
}

// release sends what the succeeded operation held. Inside an enclosing
// operation that also holds (a hook's write within a cascade), the
// emissions join that operation's and wait for it. Otherwise each goes
// out on a handler bound to base, the operation's own connection: on a
// pool, EmitEvent confirms the write once the caller's transaction ends,
// the check the operation's own record gets.
func (held *cascadeEmits) release(ctx context.Context, base DBExecutor) {
	if held == nil {
		return
	}
	held.mu.Lock()
	pending := held.pending
	held.pending = nil
	held.mu.Unlock()
	if outer, ok := ctx.Value(cascadeEmitsKey{}).(*cascadeEmits); ok {
		outer.mu.Lock()
		outer.pending = append(outer.pending, pending...)
		outer.mu.Unlock()
		return
	}
	for _, e := range pending {
		e.ch.DB = base
		e.ch.EmitEvent(e.ctx, e.eventType, e.record)
	}
}
