package outbox

import (
	"context"
	"testing"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestOutboxReadsPropagateInvalidTime(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	o, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := o.Append(context.Background(), tx, "test.event", map[string]any{"ok": true})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE "+o.qt()+" SET created_at='not-a-time' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := o.List(context.Background(), "", 10); err == nil {
		t.Fatal("List hid malformed created_at")
	}
}
