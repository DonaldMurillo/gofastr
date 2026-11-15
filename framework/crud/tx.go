package crud

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/db"
)

// conn is the transaction ctx carries, else ch.DB.
func (ch *CrudHandler) conn(ctx context.Context) DBExecutor {
	if tx, ok := db.TxFromContext(ctx); ok {
		return tx
	}
	return ch.DB
}

// inTx runs fn in a transaction, or in a savepoint of the one ctx or
// ch.DB already holds, with a handler bound to it.
func (ch *CrudHandler) inTx(ctx context.Context, fn func(ctx context.Context, ch *CrudHandler) error) error {
	ctx = withoutAuditOperation(ctx)
	if tx, ok := db.TxFromContext(ctx); ok {
		opCtx, held := holdCascadeEmits(ctx)
		if err := db.InSavepoint(opCtx, tx, func(ctx context.Context, tx *sql.Tx) error {
			txCh := *ch
			txCh.root = ch.dialectHandle()
			txCh.DB = tx
			return fn(ctx, &txCh)
		}); err != nil {
			return err
		}
		held.release(ctx, ch.DB)
		return nil
	}
	if tx, ok := ch.DB.(*sql.Tx); ok {
		opCtx, held := holdCascadeEmits(ctx)
		if err := db.InSavepoint(opCtx, tx, func(ctx context.Context, _ *sql.Tx) error {
			return fn(ctx, ch)
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
	txCh.root = ch.dialectHandle()
	txCh.DB = tx
	txCtx, queue := db.WithTxQueue(ctx, tx)
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
	queue.RunAfterCommit()
	return nil
}

// cascadeEmits holds a cascade's child events until the operation succeeds.
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

func holdCascadeEmits(ctx context.Context) (context.Context, *cascadeEmits) {
	held := &cascadeEmits{}
	return context.WithValue(ctx, cascadeEmitsKey{}, held), held
}

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
