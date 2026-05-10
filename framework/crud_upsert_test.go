package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

func upsertApp(t *testing.T, db *sql.DB) (*App, *CrudHandler) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	}.WithTimestamps(false))
	entity, _ := app.Registry.Get("posts")
	ch := NewCrudHandler(entity, db)
	ch.Hooks = app.HookRegistry("posts")
	ch.Registry = app.Registry
	return app, ch
}

// ============================================================================
// First UpsertOne acts as INSERT — row didn't exist
// ============================================================================

func TestUpsert_InsertOnNew(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := upsertApp(t, db)
		got, err := ch.UpsertOne(context.Background(), map[string]any{
			"id":    "p1",
			"title": "hello",
			"body":  "world",
		})
		if err != nil {
			t.Fatalf("UpsertOne: %v", err)
		}
		if got["title"] != "hello" {
			t.Fatalf("expected title=hello, got %v", got["title"])
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 row, got %d", n)
		}
	})
}

// ============================================================================
// Second UpsertOne with same id acts as UPDATE
// ============================================================================

func TestUpsert_UpdateOnExisting(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := upsertApp(t, db)
		if _, err := ch.UpsertOne(context.Background(), map[string]any{
			"id": "p1", "title": "v1", "body": "old",
		}); err != nil {
			t.Fatalf("first: %v", err)
		}
		got, err := ch.UpsertOne(context.Background(), map[string]any{
			"id": "p1", "title": "v2", "body": "new",
		})
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if got["title"] != "v2" || got["body"] != "new" {
			t.Fatalf("expected post-upsert title=v2 body=new, got %+v", got)
		}
		// Still exactly one row.
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = $1", "p1").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 row, got %d", n)
		}
	})
}
