package crud

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// A batch on a handler bound to a caller's *sql.Tx ran its items straight
// on the transaction: the first bad item returned with the earlier ones
// still written, so a caller that handled the error and committed kept
// half the batch (2026-10-27 bug bash D35). The operation now runs under
// a savepoint there, as it does inside a context-carried transaction.
func TestTxBoundBatchUndoesItsItems(t *testing.T) {
	ch, conn := cascadeNotesHandler(t, CaseSnake, "")
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	bound := *ch
	bound.DB = tx
	if _, err := bound.BatchCreateMany(context.Background(), []map[string]any{
		{"title": "first"},
		{"title": "second", "notes": []any{17}},
	}); err == nil {
		t.Fatal("batch with a non-object note succeeded")
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("posts on the tx after the failed batch = %d, want 0", n)
	}
	// The transaction stays usable: the caller goes on and commits.
	if _, err := bound.CreateOne(context.Background(), map[string]any{"title": "after"}); err != nil {
		t.Fatalf("create after the failed batch: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("posts after commit = %d, want 1", n)
	}
}

// A tx-bound create with the transaction's commit queue on the context
// announces its cascade note with the post, once the owner drains the
// queue after Commit.
func TestTxBoundCascadeAnnouncesChild(t *testing.T) {
	ch, conn := cascadeNotesHandler(t, CaseSnake, "")
	bus := event.NewEventBus()
	ch.Events = bus
	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx, queue := db.WithTxQueue(context.Background(), tx)
	bound := *ch
	bound.DB = tx
	if _, err := bound.CreateOne(ctx, map[string]any{
		"title": "t", "notes": []any{map[string]any{"status": "published", "body": "child"}},
	}); err != nil {
		t.Fatalf("cascade: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	queue.RunAfterCommit()
	for range 2 {
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("the post and its note were not both announced")
		}
	}
}
