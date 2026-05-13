package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// queryTestPost models the posts entity used in typed-query tests.
type queryTestPost struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Views int    `json:"views,omitempty"`
}

// hand-rolled "generated" column constants — a sneak-preview of what codegen
// will emit per entity once Task #25's generator side lands.
var (
	queryPostsTitle = entity.NewStringColumn("title")
	queryPostsViews = entity.NewIntColumn("views")
	queryPostsID    = entity.NewStringColumn("id")
)

func queryApp(t *testing.T, db *sql.DB) (*App, *crud.CrudHandler) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		views INTEGER DEFAULT 0
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
		},
	}.WithTimestamps(false))
	ent, _ := app.Registry.Get("posts")
	ch := crud.NewCrudHandler(ent, db)
	ch.Hooks = app.HookRegistry("posts")
	ch.Registry = app.Registry
	return app, ch
}

func seedQueryPosts(t *testing.T, db *sql.DB) {
	t.Helper()
	rows := []struct {
		id, title string
		views     int
	}{
		{"p1", "alpha", 10},
		{"p2", "bravo", 25},
		{"p3", "charlie", 50},
		{"p4", "delta", 100},
	}
	for _, r := range rows {
		if _, err := db.Exec("INSERT INTO posts(id, title, views) VALUES ($1, $2, $3)", r.id, r.title, r.views); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// ============================================================================
// Where + Order + Find returns []*T in the right order
// ============================================================================

func TestTypedQuery_WhereOrderFind(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		got, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsViews.Gte(25)).
			Order(queryPostsViews.Asc()).
			Find(context.Background())
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(got))
		}
		want := []string{"bravo", "charlie", "delta"}
		for i, p := range got {
			if p.Title != want[i] {
				t.Fatalf("row %d: title=%q want %q", i, p.Title, want[i])
			}
		}
	})
}

// ============================================================================
// First returns sql.ErrNoRows when no rows match (use IsNotFound)
// ============================================================================

func TestTypedQuery_First_NotFound(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		_, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsTitle.Eq("does-not-exist")).
			First(context.Background())
		if !IsNotFound(err) && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected not-found, got %v", err)
		}
	})
}

// ============================================================================
// Count over the same WHERE returns the right number
// ============================================================================

func TestTypedQuery_Count(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		n, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsViews.Gt(20)).
			Count(context.Background())
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected 3, got %d", n)
		}
	})
}

// ============================================================================
// Like / In / IsNull
// ============================================================================

func TestTypedQuery_StringOps(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		// Like
		got, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsTitle.Like("%a%")).
			Order(queryPostsTitle.Asc()).
			Find(context.Background())
		if err != nil {
			t.Fatalf("Like: %v", err)
		}
		if len(got) != 4 { // alpha, bravo, charlie, delta all contain 'a'
			t.Fatalf("expected 4 rows, got %d", len(got))
		}

		// In
		gotIn, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsID.In("p1", "p3")).
			Order(queryPostsID.Asc()).
			Find(context.Background())
		if err != nil {
			t.Fatalf("In: %v", err)
		}
		if len(gotIn) != 2 || gotIn[0].ID != "p1" || gotIn[1].ID != "p3" {
			t.Fatalf("In returned %+v", gotIn)
		}
	})
}

// ============================================================================
// UpdateAll bulk updates rows matching the WHERE chain
// ============================================================================

func TestTypedQuery_UpdateAll(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		n, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsViews.Gte(50)).
			UpdateAll(context.Background(), map[string]any{"title": "boosted"})
		if err != nil {
			t.Fatalf("UpdateAll: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected 2 rows touched, got %d", n)
		}

		// Verify
		boosted, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsTitle.Eq("boosted")).
			Find(context.Background())
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if len(boosted) != 2 {
			t.Fatalf("expected 2 boosted, got %d", len(boosted))
		}
	})
}

// ============================================================================
// DeleteAll bulk-deletes rows matching the WHERE chain
// ============================================================================

func TestTypedQuery_DeleteAll(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		n, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsViews.Lt(30)).
			DeleteAll(context.Background())
		if err != nil {
			t.Fatalf("DeleteAll: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected 2 rows deleted, got %d", n)
		}

		remaining, err := NewTypedQuery[queryTestPost](ch).Find(context.Background())
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if len(remaining) != 2 {
			t.Fatalf("expected 2 remaining, got %d", len(remaining))
		}
		for _, p := range remaining {
			if p.Views < 30 {
				t.Fatalf("expected views >= 30, got %d", p.Views)
			}
		}
	})
}

// ============================================================================
// Empty In() clause returns no rows (1 = 0 fragment)
// ============================================================================

func TestTypedQuery_EmptyInReturnsZero(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		got, err := NewTypedQuery[queryTestPost](ch).
			Where(queryPostsID.In()).
			Find(context.Background())
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 rows from empty In(), got %d", len(got))
		}
	})
}
