package crud

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// forDefaultNotes runs fn on SQLite and Postgres against a migrated notes
// entity whose status has a Default and no States.
func forDefaultNotes(t *testing.T, fn func(t *testing.T, ch *CrudHandler, conn *sql.DB)) {
	run := func(t *testing.T, conn *sql.DB) {
		ent := entity.Define("dnotes", entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "status", Type: schema.String, Default: "published"},
			},
		}.WithTimestamps(false))
		if err := migrate.MigrateEntity(conn, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		ent.SetDB(conn)
		fn(t, NewCrudHandler(ent, conn).WithJSONCase(CaseSnake), conn)
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

// The conflict arm of an upsert writes only what the caller sent. An
// omitted column with a Default entered the INSERT list for the insert
// arm, and the DO UPDATE SET then wrote it over the stored value too: an
// upsert that edited a draft's title published it.
func TestUpsertConflictKeepsOmittedColumns(t *testing.T) {
	forDefaultNotes(t, func(t *testing.T, ch *CrudHandler, conn *sql.DB) {
		ctx := context.Background()
		if _, err := conn.Exec(`INSERT INTO dnotes (id, title, status) VALUES ('n1', 'orig', 'draft')`); err != nil {
			t.Fatalf("seed: %v", err)
		}
		got, err := ch.UpsertOne(ctx, map[string]any{"id": "n1", "title": "edited"})
		if err != nil {
			t.Fatalf("upsert onto n1: %v", err)
		}
		var title, status string
		if err := conn.QueryRow(`SELECT title, status FROM dnotes WHERE id = 'n1'`).Scan(&title, &status); err != nil {
			t.Fatal(err)
		}
		if title != "edited" || status != "draft" || got["status"] != "draft" {
			t.Errorf("after upsert: stored title %q status %q, returned status %v; want edited, draft, draft", title, status, got["status"])
		}
		// The insert arm still takes the default.
		if _, err := ch.UpsertOne(ctx, map[string]any{"id": "n2", "title": "new"}); err != nil {
			t.Fatalf("upsert n2: %v", err)
		}
		if err := conn.QueryRow(`SELECT status FROM dnotes WHERE id = 'n2'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "published" {
			t.Errorf("inserted status = %q, want the default published", status)
		}
	})
}

// An upsert that lands on an existing row is an update, and announces
// one. Under a caller-owned transaction with no commit queue the event is
// confirmed against the database once the transaction ends; a create is
// confirmed by the row being there, and an upsert onto an existing row
// that rolled back still found the row, so it announced values the
// database never held. Announced as an update, it is matched on its values
// and a rollback announces nothing.
func TestUpsertRollbackAnnouncesNothing(t *testing.T) {
	forDefaultNotes(t, func(t *testing.T, ch *CrudHandler, conn *sql.DB) {
		if _, err := conn.Exec(`INSERT INTO dnotes (id, title, status) VALUES ('n1', 'before', 'draft')`); err != nil {
			t.Fatalf("seed: %v", err)
		}
		bus := event.NewEventBus()
		ch.Events = bus
		got := make(chan event.Event, 8)
		for _, typ := range []string{event.EntityCreated, event.EntityUpdated} {
			cancel := bus.Subscribe(typ, func(_ context.Context, ev event.Event) error {
				got <- ev
				return nil
			})
			defer cancel()
		}
		upsert := func(title string, commit bool) {
			t.Helper()
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ch.UpsertOne(db.WithTx(context.Background(), tx), map[string]any{"id": "n1", "title": title}); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			if commit {
				err = tx.Commit()
			} else {
				err = tx.Rollback()
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		upsert("after", false)
		select {
		case ev := <-got:
			t.Fatalf("rolled-back upsert announced %s %v", ev.Type, ev.Data)
		case <-time.After(time.Second):
		}
		upsert("kept", true)
		select {
		case ev := <-got:
			if ev.Type != event.EntityUpdated {
				t.Errorf("committed upsert onto n1 announced %s, want %s", ev.Type, event.EntityUpdated)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("committed upsert announced nothing")
		}
		// A key with no row is still a create.
		if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "n9", "title": "new"}); err != nil {
			t.Fatalf("upsert n9: %v", err)
		}
		select {
		case ev := <-got:
			if ev.Type != event.EntityCreated {
				t.Errorf("upsert of a new key announced %s, want %s", ev.Type, event.EntityCreated)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("upsert of a new key announced nothing")
		}
	})
}
