package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

type asyncTxProbeKey struct{}

// TestEmitAsyncSubscriberDoesNotJoinEmitterTx: an EmitAsync subscriber
// runs on its own goroutine with the emitter's context values, and those
// carried the emitter's transaction. A subscriber that wrote through
// App.InTx joined it: after the owner committed the write failed with
// "transaction has already been committed" and was lost, and while the
// owner was still open it shared the owner's single connection from a
// second goroutine. The subscriber now writes in a transaction of its
// own and keeps every other value (here a probe key).
func TestEmitAsyncSubscriberDoesNotJoinEmitterTx(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)
		type result struct {
			err   error
			value any
		}
		got := make(chan result, 1)
		app.Events().On("post.noted", func(ctx context.Context, _ event.Event) error {
			err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error { return insertPost(ctx, tx, "from-subscriber") })
			got <- result{err, ctx.Value(asyncTxProbeKey{})}
			return nil
		})
		ctx := context.WithValue(context.Background(), asyncTxProbeKey{}, "kept")
		err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
			if err := insertPost(ctx, tx, "from-request"); err != nil {
				return err
			}
			app.Events().EmitAsync(ctx, event.Event{Type: "post.noted"})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case r := <-got:
			if r.err != nil {
				t.Errorf("subscriber's own InTx failed: %v (it joined the emitter's transaction)", r.err)
			}
			if r.value != "kept" {
				t.Errorf("subscriber context value = %v, want the emitter's", r.value)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("subscriber never ran")
		}
		if ids := postIDs(t, db); ids != "from-request,from-subscriber" {
			t.Errorf("rows = %q, want both writes", ids)
		}
	})
}
