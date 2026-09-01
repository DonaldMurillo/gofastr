package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Live-bus emissions for CRUD writes joined to an ambient transaction. The
// framework-owned path (App.InTx) resolves them through the commit queue
// the tx owner drains — never by probing the live *sql.Tx, which raced the
// owner's own statements on the transaction's single connection (#353).
// The foreign-owner path (a caller's own tx wrapped with db.WithTx) still
// observes the database after the tx resolves.

// notesEventApp returns ONE handler with the bus attached — CrudHandler
// builds a fresh handler per call, so the caller must reuse this one.
func notesEventApp(t *testing.T, dbc *sql.DB) (*App, *crud.CrudHandler, chan event.Event) {
	t.Helper()
	app := NewApp(WithDB(dbc))
	app.Registry.Register(entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "body", Type: schema.Text}},
	}.WithTimestamps(false)))
	if err := AutoMigrate(dbc, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	bus := event.NewEventBus()
	ch := app.MustCrudHandler("notes")
	ch.Events = bus
	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	t.Cleanup(cancel)
	return app, ch, got
}

// TestInTxCommitDeliversEvents pins the commit-queue path: a CreateOne
// joined to App.InTx publishes entity.created once the InTx commits.
func TestInTxCommitDeliversEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dbc *sql.DB, _ Dialect) {
		app, ch, got := notesEventApp(t, dbc)

		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			_, e := ch.CreateOne(ctx, map[string]any{"body": "durable"})
			return e
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("entity.created not delivered after InTx commit")
		}
	})
}

// TestInTxRollbackWithholdsEvents pins the drop side of the same queue: a
// CreateOne whose App.InTx rolls back publishes nothing.
func TestInTxRollbackWithholdsEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dbc *sql.DB, _ Dialect) {
		app, ch, got := notesEventApp(t, dbc)

		boom := errors.New("boom")
		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			if _, e := ch.CreateOne(ctx, map[string]any{"body": "phantom"}); e != nil {
				return e
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("InTx: %v, want the rollback error", err)
		}
		select {
		case ev := <-got:
			t.Fatalf("entity.created published for a rolled-back write: %v", ev.Data)
		case <-time.After(600 * time.Millisecond):
		}
	})
}

// TestForeignTxCommitDeliversEvents pins the fallback path for a caller
// that opens its own transaction and wraps it with db.WithTx: the emission
// is confirmed against the database after the tx resolves. On Postgres this
// confirm query was built with `?` placeholders and failed as a syntax
// error, so every such event was silently dropped.
func TestForeignTxCommitDeliversEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dbc *sql.DB, _ Dialect) {
		_, ch, got := notesEventApp(t, dbc)

		tx, err := dbc.Begin()
		if err != nil {
			t.Fatal(err)
		}
		ctx := db.WithTx(context.Background(), tx)
		if _, err := ch.CreateOne(ctx, map[string]any{"body": "foreign"}); err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatal("entity.created not delivered after foreign tx commit")
		}
	})
}
