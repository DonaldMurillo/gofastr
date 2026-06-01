package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestApp_CrudHandler covers the in-process handler accessor: it returns a
// wired handler for a registered entity and errors otherwise.
func TestApp_CrudHandler(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("things", entity.EntityConfig{
		Table: "things", Fields: []schema.Field{{Name: "n", Type: schema.Int}},
	}.WithTimestamps(false)))

	ch, err := app.CrudHandler("things")
	if err != nil || ch == nil || ch.Registry == nil {
		t.Fatalf("CrudHandler(things): %v / %+v", err, ch)
	}
	if _, err := app.CrudHandler("missing"); err == nil {
		t.Error("expected error for an unregistered entity")
	}
	if _, err := NewApp().CrudHandler("x"); err == nil {
		t.Error("expected error when the app has no DB")
	}
}

// TestInTx_ComposesCrudOperations proves the ambient-transaction reuse: two
// CRUD writes invoked inside one App.InTx join the SAME transaction, so when
// the second fails the first is rolled back too. Before the fix each CreateOne
// opened and committed its own transaction, so the first insert would survive.
func TestInTx_ComposesCrudOperations(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		ent := entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "slug", Type: schema.String, Unique: true, Required: true},
			},
		}.WithTimestamps(false))
		app.Registry.Register(ent)
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		ch := app.MustCrudHandler("posts") // the documented in-process accessor

		// A succeeds, B violates the UNIQUE(slug) constraint → the whole
		// InTx must roll back.
		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			if _, e := ch.CreateOne(ctx, map[string]any{"slug": "dup"}); e != nil {
				return e
			}
			_, e := ch.CreateOne(ctx, map[string]any{"slug": "dup"}) // duplicate → fails
			return e
		})
		if err == nil {
			t.Fatal("expected the duplicate insert to fail the transaction")
		}

		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("expected 0 rows after rollback (ambient tx not joined), got %d", n)
		}
	})
}

// TestInTx_ComposesCommit confirms the happy path commits both writes together.
func TestInTx_ComposesCommit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		ent := entity.Define("notes", entity.EntityConfig{
			Table:  "notes",
			Fields: []schema.Field{{Name: "body", Type: schema.Text}},
		}.WithTimestamps(false))
		app.Registry.Register(ent)
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		ch := app.MustCrudHandler("notes")

		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			if _, e := ch.CreateOne(ctx, map[string]any{"body": "one"}); e != nil {
				return e
			}
			_, e := ch.CreateOne(ctx, map[string]any{"body": "two"})
			return e
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected 2 committed rows, got %d", n)
		}
	})
}
