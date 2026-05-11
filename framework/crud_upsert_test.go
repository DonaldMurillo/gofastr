package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/hook"
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
// UpsertOne fires BeforeCreate + AfterCreate hooks (same semantics as Create)
// ============================================================================

func TestUpsert_FiresHooks(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := upsertApp(t, db)
		var beforeCalled, afterCalled int
		app.HookRegistry("posts").RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
			beforeCalled++
			return nil
		})
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			afterCalled++
			return nil
		})

		if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "p1", "title": "first"}); err != nil {
			t.Fatalf("first: %v", err)
		}
		if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "p1", "title": "second"}); err != nil {
			t.Fatalf("second: %v", err)
		}
		if beforeCalled != 2 || afterCalled != 2 {
			t.Fatalf("expected hooks fired twice each, got before=%d after=%d", beforeCalled, afterCalled)
		}
	})
}

// ============================================================================
// UpsertOne rolls back when validation fails — required field missing
// ============================================================================

func TestUpsert_ValidationRollsBack(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := upsertApp(t, db)
		_, err := ch.UpsertOne(context.Background(), map[string]any{"id": "p1" /* missing required title */})
		if err == nil {
			t.Fatal("expected validation error")
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("expected 0 rows after validation rollback, got %d", n)
		}
	})
}

// ============================================================================
// UpsertOne with auto-generated id assigns a fresh UUID when none supplied
// ============================================================================

func TestUpsert_AutoGeneratesID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := upsertApp(t, db)
		got, err := ch.UpsertOne(context.Background(), map[string]any{"title": "auto"})
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		id, _ := got["id"].(string)
		if len(id) != 36 { // UUID v4 length
			t.Fatalf("expected auto-generated UUID id, got %q", id)
		}
	})
}

// ============================================================================
// UpsertOne preserves the existing id on conflict (does not regenerate)
// ============================================================================

func TestUpsert_PreservesIDOnConflict(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := upsertApp(t, db)
		// First upsert with explicit id.
		if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "p1", "title": "v1"}); err != nil {
			t.Fatalf("first: %v", err)
		}
		// Re-upsert with the same id — id field should remain "p1" (not regenerate).
		got, err := ch.UpsertOne(context.Background(), map[string]any{"id": "p1", "title": "v2"})
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if got["id"] != "p1" {
			t.Fatalf("expected id preserved as p1, got %v", got["id"])
		}
	})
}

// ============================================================================
// UpsertOne injects tenant_id when MultiTenant is enabled
// ============================================================================

func TestUpsert_InjectsTenantID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			tenant_id TEXT
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", EntityConfig{
			Table:       "posts",
			MultiTenant: true,
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		entity, _ := app.Registry.Get("posts")
		ch := NewCrudHandler(entity, db)
		ch.Hooks = app.HookRegistry("posts")

		ctx := SetTenantID(context.Background(), "tenant-a")
		got, err := ch.UpsertOne(ctx, map[string]any{"id": "p1", "title": "scoped"})
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		// tenant_id is on the table but Hidden isn't set, so it should be in the result.
		// Verify directly in DB to avoid relying on visibility config.
		var tid string
		if err := db.QueryRow("SELECT tenant_id FROM posts WHERE id = $1", "p1").Scan(&tid); err != nil {
			t.Fatalf("read tenant: %v", err)
		}
		if tid != "tenant-a" {
			t.Fatalf("expected tenant-a, got %q (result=%+v)", tid, got)
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
