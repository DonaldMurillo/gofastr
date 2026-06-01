package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

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
		ch := crud.NewCrudHandler(ent, db)

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
		ch := crud.NewCrudHandler(ent, db)

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
