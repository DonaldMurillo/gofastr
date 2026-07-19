package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestPureSQLiteTypedEventRelay(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	o, err := New(db, WithHandlerGrace(0), WithPollInterval(time.Millisecond))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}

	ctx := context.Background()
	got := make(chan event.Event, 1)
	o.Consume("billing-projection", "invoice.paid", func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := o.Append(ctx, tx, "invoice.paid", map[string]any{
		"invoice_id": "inv-42",
		"amount":     1250,
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	stop := o.StartRelay(ctx)
	defer stop()

	select {
	case delivered := <-got:
		if delivered.ID != id {
			t.Fatalf("event id = %q, want %q", delivered.ID, id)
		}
		if delivered.Type != "invoice.paid" {
			t.Fatalf("event type = %q, want invoice.paid", delivered.Type)
		}
		data, ok := delivered.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data type = %T, want map[string]any", delivered.Data)
		}
		if data["invoice_id"] != "inv-42" {
			t.Fatalf("invoice_id = %v, want inv-42", data["invoice_id"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pure-SQLite relay delivery")
	}

	waitForParent(t, o, id, "dispatched")
	delivery := findDelivery(t, mustDeliveries(t, o, id), "billing-projection")
	if delivery.Status != "dispatched" {
		t.Fatalf("delivery status = %q, want dispatched", delivery.Status)
	}
}
