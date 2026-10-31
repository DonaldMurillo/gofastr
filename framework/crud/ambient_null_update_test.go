package crud

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// nullUpdateInAmbientTx seeds a note with a body, sets the body to NULL
// inside a caller-owned transaction, commits or rolls it back, and reports
// whether an EntityUpdated arrived on the bus.
func nullUpdateInAmbientTx(t *testing.T, commit bool) bool {
	t.Helper()
	ch, dbc := covNotesHandler(t)
	created, err := ch.CreateOne(context.Background(), map[string]any{"title": "kept", "body": "text"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[ch.convertKey(ch.PrimaryKey[0])]

	bus := event.NewEventBus()
	ch.Events = bus
	got := make(chan event.Event, 4)
	cancel := bus.Subscribe(event.EntityUpdated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	defer cancel()

	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx := db.WithTx(context.Background(), tx)
	if _, err := ch.UpdateOne(ctx, fmt.Sprint(id), map[string]any{"body": nil}); err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if commit {
		err = tx.Commit()
	} else {
		err = tx.Rollback()
	}
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
		return true
	case <-time.After(time.Second):
		return false
	}
}

// An update that only set a column to NULL was confirmed on its key alone,
// because the confirm query skipped NULL values, so its rollback still
// published EntityUpdated (2026-10-27 R11). NULLs are matched with IS NULL.
func TestAmbientNullUpdateRollbackSilent(t *testing.T) {
	if nullUpdateInAmbientTx(t, false) {
		t.Fatal("EntityUpdated published for a NULL-only edit the transaction rolled back")
	}
}

// The committed NULL-only edit still publishes: the IS NULL match finds it.
func TestAmbientNullUpdateCommitEmits(t *testing.T) {
	if !nullUpdateInAmbientTx(t, true) {
		t.Fatal("no EntityUpdated for a committed NULL-only edit")
	}
}
