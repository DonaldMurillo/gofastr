package outbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A mixed-case outbox table folds on Postgres the way unquoted DDL would:
// the parent and delivery tables are created as events and
// events_delivery, and the append, fan-out, claim and settle all name
// those same tables.
func TestPostgres_MixedCaseTable(t *testing.T) {
	db, o := pgOutbox(t, WithTable("Events"), WithPollInterval(5*time.Millisecond))
	ctx := context.Background()
	for _, want := range []string{"events", "events_delivery"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1`, want).Scan(&n); err != nil {
			t.Fatalf("query information_schema: %v", err)
		}
		if n != 1 {
			t.Errorf("table %q: count=%d, want 1", want, n)
		}
	}
	got := make(chan event.Event, 1)
	o.Consume("logger", "order.placed", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := o.Append(ctx, tx, "order.placed", map[string]any{"total": 7})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	stop := o.StartRelay(ctx)
	defer stop()
	select {
	case e := <-got:
		if e.ID != id {
			t.Fatalf("event id = %s, want %s", e.ID, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delivery from the Events outbox")
	}
	waitForParent(t, o, id, "dispatched")
}

// An older build kept "Events" and "Events_delivery" case-kept. This
// build names events and would relay from an empty table beside them, so
// New refuses, with or without WithoutEnsureTable, and prints the rename.
func TestPostgres_CaseKeptTableRefused(t *testing.T) {
	for _, older := range []string{"Events", "Events_delivery"} {
		t.Run(older, func(t *testing.T) {
			db := pgtest.DB(t)
			if _, err := db.Exec(`CREATE TABLE "` + older + `" (id TEXT PRIMARY KEY)`); err != nil {
				t.Fatalf("create the older table: %v", err)
			}
			_, err := New(db, WithTable("Events"), WithoutEnsureTable())
			want := `ALTER TABLE "` + older + `" RENAME TO "` + strings.ToLower(older) + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("New over %q: err = %v, want %s", older, err, want)
			}
		})
	}
}
