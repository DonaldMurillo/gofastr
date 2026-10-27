package crud

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// A cascade child announced itself the moment it was written. Inside a
// transaction the operation did not open itself, with no commit queue to
// hold the emission, that was before the operation knew whether a later
// item would fail it: [{"body":"phantom"},17] announced the first note,
// then refused the 17 and rolled the note back. Child emissions now wait
// for the operation to succeed.
func TestFailedCascadeAnnouncesNoChild(t *testing.T) {
	body := func() map[string]any {
		return map[string]any{"title": "t", "notes": []any{
			map[string]any{"status": "published", "body": "phantom"}, 17,
		}}
	}
	cases := []struct {
		name string
		run  func(t *testing.T, ch *CrudHandler, conn *sql.DB) *sql.Tx
	}{
		{"caller-owned tx", func(t *testing.T, ch *CrudHandler, conn *sql.DB) *sql.Tx {
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ch.CreateOne(db.WithTx(context.Background(), tx), body()); err == nil {
				t.Fatal("cascade with a non-object item succeeded")
			}
			return tx
		}},
		{"handler bound to a tx", func(t *testing.T, ch *CrudHandler, conn *sql.DB) *sql.Tx {
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal(err)
			}
			bound := *ch
			bound.DB = tx
			if _, err := bound.CreateOne(context.Background(), body()); err == nil {
				t.Fatal("cascade with a non-object item succeeded")
			}
			return tx
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, conn := cascadeNotesHandler(t, CaseSnake, "")
			bus := event.NewEventBus()
			ch.Events = bus
			got := make(chan event.Event, 8)
			cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
				got <- ev
				return nil
			})
			defer cancel()
			tx := tc.run(t, ch, conn)
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			select {
			case ev := <-got:
				t.Fatalf("failed cascade announced %v", ev.Data)
			case <-time.After(time.Second):
			}
		})
	}
}

// A cascade that succeeded inside a caller's transaction that then rolled
// back announces nothing either: the held emissions go out on the pool,
// where EmitEvent confirms them against the finished transaction.
func TestRolledBackCascadeAnnouncesNoChild(t *testing.T) {
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
	if _, err := ch.CreateOne(db.WithTx(context.Background(), tx), map[string]any{
		"title": "t", "notes": []any{map[string]any{"status": "published", "body": "gone"}},
	}); err != nil {
		t.Fatalf("cascade: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-got:
		t.Fatalf("rolled-back cascade announced %v", ev.Data)
	case <-time.After(time.Second):
	}
}

// A commit queue outranks an enclosing hold: the queue's transaction
// commits on its own, and the queue already drops the emission on
// rollback.
func TestCommitQueueOutranksHold(t *testing.T) {
	ch, conn := cascadeNotesHandler(t, CaseSnake, "")
	ch.Events = event.NewEventBus()
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	heldCtx, held := holdCascadeEmits(context.Background())
	queueCtx, _ := db.WithTxQueue(heldCtx, tx)
	ch.emitCascade(queueCtx, event.EntityCreated, map[string]any{"id": "n1"})
	if len(held.pending) != 0 {
		t.Fatalf("hold took %d emissions a commit queue owns", len(held.pending))
	}
}

// A write nested inside another held operation (a hook's write within a
// cascade) hands what it held to the enclosing operation when it succeeds:
// the enclosing one can still fail and roll it back.
func TestNestedHoldJoinsEnclosing(t *testing.T) {
	ch, _ := cascadeNotesHandler(t, CaseSnake, "")
	bus := event.NewEventBus()
	ch.Events = bus
	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()
	outerCtx, outer := holdCascadeEmits(context.Background())
	innerCtx, inner := holdCascadeEmits(outerCtx)
	ch.emitCascade(innerCtx, event.EntityCreated, map[string]any{"id": "n1"})
	inner.release(outerCtx, ch.DB)
	if len(outer.pending) != 1 {
		t.Fatalf("enclosing operation holds %d emissions, want 1", len(outer.pending))
	}
	select {
	case ev := <-got:
		t.Fatalf("nested release announced %v before the enclosing operation finished", ev.Data)
	case <-time.After(200 * time.Millisecond):
	}
}

// The other side: a cascade that succeeds still announces its children,
// once the operation is done, through the same commit check its parent
// gets.
func TestCascadeChildAnnouncedAfterCommit(t *testing.T) {
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
	if _, err := ch.CreateOne(db.WithTx(context.Background(), tx), map[string]any{
		"title": "t", "notes": []any{map[string]any{"status": "published", "body": "kept"}},
	}); err != nil {
		t.Fatalf("cascade: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 2 {
		select {
		case ev := <-got:
			data, _ := ev.Data.(map[string]any)
			name, _ := data[eventKeyEntity].(string)
			seen[name]++
		case <-deadline:
			t.Fatalf("announced %v, want one posts and one notes create", seen)
		}
	}
	if seen["posts"] != 1 || seen["notes"] != 1 {
		t.Errorf("announced %v, want one posts and one notes create", seen)
	}
}
