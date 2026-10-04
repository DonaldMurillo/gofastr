package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// queryTestPost models the posts entity used in typed-query tests.
type queryTestPost struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Views int    `json:"views,omitempty"`
}

// hand-rolled "generated" column constants, a sneak-preview of what codegen
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

// ============================================================================
// WhereNested multi-hop query and count
// ============================================================================

func TestTypedQuery_WhereNested_MultiHop(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ent, _ := app.Registry.Get("posts")
		ch := crud.NewCrudHandler(ent, db)
		ch.Registry = app.Registry

		// 2 hops: author.profile.bio LIKE %Alice%
		got, err := NewTypedQuery[queryTestPost](ch).
			WhereNested(crud.NestedFilter{
				Relation: "author.profile",
				Field:    "bio",
				Op:       filter.OpLike,
				Value:    "Alice",
			}).
			Find(context.Background())
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(got) != 1 || got[0].ID != "p1" {
			t.Fatalf("expected post p1, got %+v", got)
		}

		// Count
		cnt, err := NewTypedQuery[queryTestPost](ch).
			WhereNested(crud.NestedFilter{
				Relation: "author.profile",
				Field:    "bio",
				Op:       filter.OpLike,
				Value:    "Alice",
			}).
			Count(context.Background())
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if cnt != 1 {
			t.Fatalf("expected count 1, got %d", cnt)
		}
	})
}

// TestTypedQuery_WhereNested_ErrorPropagation asserts that invalid nested filters
// (e.g. unknown relation or field) return an error on Find and Count rather than being swallowed.
func TestTypedQuery_WhereNested_ErrorPropagation(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ent, _ := app.Registry.Get("posts")
		ch := crud.NewCrudHandler(ent, db)
		ch.Registry = app.Registry

		// Unknown relation
		_, err := NewTypedQuery[queryTestPost](ch).
			WhereNested(crud.NestedFilter{
				Relation: "unknown_rel",
				Field:    "name",
				Op:       filter.OpEq,
				Value:    "Alice",
			}).
			Find(context.Background())
		if err == nil {
			t.Fatalf("expected error on Find with unknown relation, got nil")
		}

		// Count with unknown relation
		_, err = NewTypedQuery[queryTestPost](ch).
			WhereNested(crud.NestedFilter{
				Relation: "unknown_rel",
				Field:    "name",
				Op:       filter.OpEq,
				Value:    "Alice",
			}).
			Count(context.Background())
		if err == nil {
			t.Fatalf("expected error on Count with unknown relation, got nil")
		}
	})
}

// TestTypedQuery_WhereNested_UpdateAll_DeleteAll asserts that UpdateAll and DeleteAll
// correctly apply WhereNested filters.
func TestTypedQuery_WhereNested_UpdateAll_DeleteAll(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ent, _ := app.Registry.Get("posts")
		ch := crud.NewCrudHandler(ent, db)
		ch.Registry = app.Registry

		// Alice has p1. Bob has p2.
		// Update posts where author.name == 'Alice'
		n, err := NewTypedQuery[queryTestPost](ch).
			WhereNested(crud.NestedFilter{
				Relation: "author",
				Field:    "name",
				Op:       filter.OpEq,
				Value:    "Alice",
			}).
			UpdateAll(context.Background(), map[string]any{"title": "Alice Post"})
		if err != nil {
			t.Fatalf("UpdateAll: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 updated row for Alice, got %d", n)
		}

		// Verify Alice's post has title="Alice Post", Bob's post (p2) does not
		var p1Title, p2Title string
		if err := db.QueryRow("SELECT title FROM posts WHERE id = 'p1'").Scan(&p1Title); err != nil {
			t.Fatalf("p1: %v", err)
		}
		if p1Title != "Alice Post" {
			t.Fatalf("p1 title=%q want %q", p1Title, "Alice Post")
		}
		if err := db.QueryRow("SELECT title FROM posts WHERE id = 'p2'").Scan(&p2Title); err != nil {
			t.Fatalf("p2: %v", err)
		}
		if p2Title == "Alice Post" {
			t.Fatalf("p2 title was modified to Alice Post!")
		}

		// Delete posts where author.name == 'Alice'
		delN, err := NewTypedQuery[queryTestPost](ch).
			WhereNested(crud.NestedFilter{
				Relation: "author",
				Field:    "name",
				Op:       filter.OpEq,
				Value:    "Alice",
			}).
			DeleteAll(context.Background())
		if err != nil {
			t.Fatalf("DeleteAll: %v", err)
		}
		if delN != 1 {
			t.Fatalf("expected 1 deleted row for Alice, got %d", delN)
		}

		// Verify p2 still exists
		var p2ID string
		if err := db.QueryRow("SELECT id FROM posts WHERE id = 'p2'").Scan(&p2ID); err != nil {
			t.Fatalf("p2 was deleted: %v", err)
		}
	})
}

// TestTypedQuery_Exists_DoesNotMutateLimit asserts that Exists does not mutate query state (limit).
func TestTypedQuery_Exists_DoesNotMutateLimit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		q := NewTypedQuery[queryTestPost](ch)
		exists, err := q.Exists(context.Background())
		if err != nil {
			t.Fatalf("Exists: %v", err)
		}
		if !exists {
			t.Fatalf("expected true, got false")
		}

		// Subsequent Find should return all 4 rows, not 1
		rows, err := q.Find(context.Background())
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(rows) != 4 {
			t.Fatalf("expected 4 rows after Exists, got %d (limit was corrupted)", len(rows))
		}
	})
}

// TestTypedQuery_First_DoesNotMutateLimit verifies that calling First on a query
// does not mutate the builder's limit, allowing subsequent Find calls to return all rows.
func TestTypedQuery_First_DoesNotMutateLimit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := queryApp(t, db)
		seedQueryPosts(t, db)

		q := NewTypedQuery[queryTestPost](ch)
		row, err := q.First(context.Background())
		if err != nil {
			t.Fatalf("First: %v", err)
		}
		if row == nil {
			t.Fatalf("expected non-nil row")
		}

		rows, err := q.Find(context.Background())
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(rows) != 4 {
			t.Fatalf("expected 4 rows after First, got %d (limit was corrupted)", len(rows))
		}
	})
}
