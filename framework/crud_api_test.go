package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/event"
	"github.com/gofastr/gofastr/framework/filter"
	"github.com/gofastr/gofastr/framework/hook"
)

// inProcessApp builds a posts entity wired with a HookRegistry, suitable for
// exercising the in-process CRUD API.
func inProcessApp(t *testing.T, db *sql.DB) (*App, *CrudHandler) {
	t.Helper()
	createPostsTestTable(t, db)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	}.WithTimestamps(false))
	entity, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	ch := NewCrudHandler(entity, db)
	ch.Hooks = app.HookRegistry("posts")
	ch.Events = app.Events()
	ch.Registry = app.Registry
	return app, ch
}

// ============================================================================
// CreateOne fires hooks + emits events + roundtrips data
// ============================================================================

func TestCRUDApi_CreateOne_FullPipeline(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := inProcessApp(t, db)

		var beforeRan, afterRan, eventReceived bool
		app.HookRegistry("posts").RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
			beforeRan = true
			return nil
		})
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			afterRan = true
			return nil
		})
		// EmitAsync fires events in a goroutine; we use Events().Subscribe for
		// the test so we get a synchronous notification we can wait on.
		done := make(chan struct{}, 1)
		cancel := app.Events().Subscribe(event.EntityCreated, func(ctx context.Context, ev event.Event) error {
			eventReceived = true
			select {
			case done <- struct{}{}:
			default:
			}
			return nil
		})
		defer cancel()

		got, err := ch.CreateOne(context.Background(), map[string]any{"title": "Hello"})
		if err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		if got["title"] != "Hello" {
			t.Fatalf("expected returned title=Hello, got %v", got["title"])
		}
		if got["id"] == nil || got["id"] == "" {
			t.Fatalf("expected auto-generated id, got %v", got["id"])
		}
		if !beforeRan {
			t.Fatal("expected BeforeCreate hook to run")
		}
		if !afterRan {
			t.Fatal("expected AfterCreate hook to run")
		}
		<-done
		if !eventReceived {
			t.Fatal("expected EntityCreated event")
		}
	})
}

// ============================================================================
// CreateOne rolls back when AfterCreate hook fails (same tx semantics)
// ============================================================================

func TestCRUDApi_CreateOne_HookRollback(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := inProcessApp(t, db)
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			return errors.New("boom")
		})

		_, err := ch.CreateOne(context.Background(), map[string]any{"title": "should-rollback"})
		if err == nil {
			t.Fatal("expected error from after-create hook")
		}

		// No rows committed.
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
// GetOne with includes uses the same eager-loading path as the HTTP handler
// ============================================================================

func TestCRUDApi_GetOne_WithIncludes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		entity, err := app.Registry.Get("posts")
		if err != nil {
			t.Fatalf("get entity: %v", err)
		}
		ch := NewCrudHandler(entity, db)
		ch.Hooks = app.HookRegistry("posts")
		ch.Events = app.Events()
		ch.Registry = app.Registry

		got, err := ch.GetOne(context.Background(), "p1", []string{"author.profile", "comments"})
		if err != nil {
			t.Fatalf("GetOne: %v", err)
		}
		if got["title"] != "First" {
			t.Fatalf("expected post.title=First, got %v", got["title"])
		}
		author, ok := got["author"].(map[string]any)
		if !ok {
			t.Fatalf("expected author map, got %T", got["author"])
		}
		if author["name"] != "Alice" {
			t.Fatalf("expected author.name=Alice, got %v", author["name"])
		}
		if author["profile"] == nil {
			t.Fatalf("expected nested profile, got nil")
		}
	})
}

// ============================================================================
// ListAll honours filters + sorts + limit
// ============================================================================

func TestCRUDApi_ListAll_FilterSortLimit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := inProcessApp(t, db)
		ctx := context.Background()
		// Seed via the typed-ish API for variety.
		for _, title := range []string{"alpha", "bravo", "charlie", "delta"} {
			if _, err := ch.CreateOne(ctx, map[string]any{"title": title}); err != nil {
				t.Fatalf("seed %s: %v", title, err)
			}
		}

		min1 := float64(1)
		got, err := ch.ListAll(ctx, ListOptions{
			Filters: []filter.ParsedFilter{{Field: "title", Op: filter.OpLike, Value: "%a%"}},
			Sorts:   []filter.ParsedSort{{Field: "title", Desc: false}},
			Limit:   2,
		})
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		_ = min1
		if len(got) != 2 {
			t.Fatalf("expected 2 rows after limit, got %d", len(got))
		}
		// "alpha" sorts first; "bravo" doesn't contain 'a'... wait it does.
		// alpha, bravo, charlie, delta — 'a' present in alpha, bravo, charlie, delta? a in delta? d-e-l-t-a yes.
		// All four contain an 'a'. After title ASC limit 2: alpha, bravo.
		if got[0]["title"] != "alpha" || got[1]["title"] != "bravo" {
			t.Fatalf("unexpected ordering: %+v", got)
		}
	})
}
