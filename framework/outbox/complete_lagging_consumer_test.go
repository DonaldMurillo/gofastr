package outbox

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Expansion creates each consumer's delivery in its own batch-limited
// INSERT, so two relays (or one consumer catching up on a backlog) leave a
// parent holding consumer a's delivery before consumer b's exists. A relay
// that dispatched a's delivery then completed the parent: "no pending
// delivery" held, and once the parent was older than the handler grace
// (any backlog after an outage) nothing else stopped it. Expansion only
// visits pending parents, so b never got its delivery and the event was
// lost for b with no dead letter. Two relays over a 300-event backlog lost
// five of 600 deliveries this way on SQLite. A parent now completes only
// once every consumer this relay declares for its type holds a delivery.
func TestParentWaitsForLaggingConsumer(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...Option) (*sql.DB, *Outbox)
	}{
		{"sqlite", openOutbox},
		{"postgres", pgOutbox},
	}
	for _, dl := range dialects {
		t.Run(dl.name, func(t *testing.T) {
			db, o := dl.open(t) // zero handler grace: the parent is "old" at once
			var bRuns atomic.Int32
			o.Consume("a", "t", noopHandler)
			o.Consume("b", "t", func(context.Context, event.Event) error {
				bRuns.Add(1)
				return nil
			})
			ctx := context.Background()
			id := appendCommitted(t, db, o, "t")
			// Another relay's expansion reached this parent for a only.
			if _, err := db.Exec("INSERT INTO "+o.qd()+
				" (row_id, consumer, status, attempts, created_at) VALUES ($1, 'a', 'pending', 0, $2)",
				id, o.now().UTC()); err != nil {
				t.Fatal(err)
			}
			claimed, err := o.claimDeliveries(ctx)
			if err != nil || len(claimed) != 1 {
				t.Fatalf("claim: %v (n=%d)", err, len(claimed))
			}
			o.processDelivery(ctx, claimed[0])
			if err := o.sweepParents(ctx); err != nil {
				t.Fatal(err)
			}
			if st := parentStatus(t, db, o, id); st != "pending" {
				t.Fatalf("parent %q after a's delivery while b had none, want pending", st)
			}
			for range 3 {
				o.pump(ctx)
			}
			if n := bRuns.Load(); n != 1 {
				t.Fatalf("consumer b ran %d times, want 1", n)
			}
			if st := parentStatus(t, db, o, id); st != "dispatched" {
				t.Fatalf("parent %q once both delivered, want dispatched", st)
			}
		})
	}
}

func parentStatus(t *testing.T, db *sql.DB, o *Outbox, id string) string {
	t.Helper()
	var st string
	if err := db.QueryRow("SELECT status FROM "+o.qt()+" WHERE id = $1", id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}
