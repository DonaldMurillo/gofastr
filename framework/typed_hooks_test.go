package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
)

// hookTestPost is a minimal generated-style model for the tests below.
type hookTestPost struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// hookApp wires a posts entity ready for typed hook tests.
func hookApp(t *testing.T, db *sql.DB) (*App, *CrudHandler) {
	t.Helper()
	createPostsTestTable(t, db)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	}.WithTimestamps(false))
	ent, _ := app.Registry.Get("posts")
	ch := NewCrudHandler(ent, db)
	ch.Hooks = app.HookRegistry("posts")
	return app, ch
}

// ============================================================================
// Test: typed BeforeCreate hook receives populated *T
// ============================================================================

func TestTypedHook_BeforeCreate_ReceivesT(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)

		var seen *hookTestPost
		OnBeforeCreate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			seen = p
			return nil
		})

		_, err := ch.CreateOne(context.Background(), map[string]any{"title": "hi", "body": "world"})
		if err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		if seen == nil {
			t.Fatal("expected hook to run")
		}
		if seen.Title != "hi" {
			t.Fatalf("expected Title=hi, got %q", seen.Title)
		}
		if seen.Body != "world" {
			t.Fatalf("expected Body=world, got %q", seen.Body)
		}
	})
}

// ============================================================================
// Test: typed BeforeCreate mutations flow back into the persisted row
// ============================================================================

func TestTypedHook_BeforeCreate_MutationFlowsBack(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)

		OnBeforeCreate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			p.Title = p.Title + "-modified"
			return nil
		})

		got, err := ch.CreateOne(context.Background(), map[string]any{"title": "hello"})
		if err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		if got["title"] != "hello-modified" {
			t.Fatalf("expected mutation to flow back, got %v", got["title"])
		}
	})
}

// ============================================================================
// Test: typed AfterCreate sees the persisted record (with auto-generated id)
// ============================================================================

func TestTypedHook_AfterCreate_SeesID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)

		var sawID string
		OnAfterCreate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			sawID = p.ID
			return nil
		})

		got, err := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
		if err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		if sawID == "" {
			t.Fatal("expected hook to receive populated ID")
		}
		if sawID != got["id"] {
			t.Fatalf("hook id %q != map id %v", sawID, got["id"])
		}
	})
}

// ============================================================================
// Test: typed BeforeCreate error rolls back (same tx semantics as untyped)
// ============================================================================

func TestTypedHook_BeforeCreate_ErrorRollsBack(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)
		OnBeforeCreate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			return errors.New("typed reject")
		})
		_, err := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
		if err == nil {
			t.Fatal("expected rejection")
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("expected 0 rows after rollback, got %d", n)
		}
	})
}

// ============================================================================
// Test: typed Update sees partial fields, AfterUpdate sees full result
// ============================================================================

func TestTypedHook_Update_PartialThenFull(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)
		// seed
		created, err := ch.CreateOne(context.Background(), map[string]any{"title": "orig", "body": "b"})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		id := created["id"].(string)

		var beforePartial, afterFull *hookTestPost
		OnBeforeUpdate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			beforePartial = p
			return nil
		})
		OnAfterUpdate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			afterFull = p
			return nil
		})

		_, err = ch.UpdateOne(context.Background(), id, map[string]any{"title": "updated"})
		if err != nil {
			t.Fatalf("update: %v", err)
		}

		if beforePartial == nil || beforePartial.Title != "updated" || beforePartial.Body != "" {
			t.Fatalf("BeforeUpdate should see only sent fields, got %+v", beforePartial)
		}
		if afterFull == nil || afterFull.Title != "updated" || afterFull.Body != "b" {
			t.Fatalf("AfterUpdate should see merged row, got %+v", afterFull)
		}
	})
}

// ============================================================================
// Test: OnBeforeDelete + OnAfterDelete pass the id
// ============================================================================

func TestTypedHook_Delete_PassesID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)
		created, err := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		id := created["id"].(string)

		var beforeID, afterID string
		OnBeforeDelete(app, "posts", func(ctx context.Context, gotID string) error {
			beforeID = gotID
			return nil
		})
		OnAfterDelete(app, "posts", func(ctx context.Context, gotID string) error {
			afterID = gotID
			return nil
		})
		if err := ch.DeleteOne(context.Background(), id); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if beforeID != id {
			t.Fatalf("expected beforeID=%s, got %s", id, beforeID)
		}
		if afterID != id {
			t.Fatalf("expected afterID=%s, got %s", id, afterID)
		}
	})
}
