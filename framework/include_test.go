package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// setupBlogDB creates posts, comments, users, tags, and post_tags tables to
// exercise the four relation kinds.
func setupBlogDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, bio TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, author_id TEXT)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, body TEXT NOT NULL, post_id TEXT NOT NULL)`,
		`CREATE TABLE tags (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE post_tags (post_id TEXT NOT NULL, tag_id TEXT NOT NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	seeds := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users(id, name) VALUES (?, ?)", []any{"u1", "Alice"}},
		{"INSERT INTO users(id, name) VALUES (?, ?)", []any{"u2", "Bob"}},
		{"INSERT INTO profiles(id, user_id, bio) VALUES (?, ?, ?)", []any{"prof1", "u1", "Hello from Alice"}},
		{"INSERT INTO posts(id, title, author_id) VALUES (?, ?, ?)", []any{"p1", "First", "u1"}},
		{"INSERT INTO posts(id, title, author_id) VALUES (?, ?, ?)", []any{"p2", "Second", "u2"}},
		{"INSERT INTO comments(id, body, post_id) VALUES (?, ?, ?)", []any{"c1", "nice", "p1"}},
		{"INSERT INTO comments(id, body, post_id) VALUES (?, ?, ?)", []any{"c2", "great", "p1"}},
		{"INSERT INTO tags(id, name) VALUES (?, ?)", []any{"t1", "go"}},
		{"INSERT INTO tags(id, name) VALUES (?, ?)", []any{"t2", "framework"}},
		{"INSERT INTO post_tags(post_id, tag_id) VALUES (?, ?)", []any{"p1", "t1"}},
		{"INSERT INTO post_tags(post_id, tag_id) VALUES (?, ?)", []any{"p1", "t2"}},
	}
	for _, s := range seeds {
		if _, err := db.Exec(s.sql, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.sql, err)
		}
	}
	return db
}

// blogApp registers users, profiles, posts, comments, tags with relations.
func blogApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("users", EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
		Relations: []Relation{
			HasOne("profile", "profiles", "user_id"),
		},
	}.WithTimestamps(false))
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []Relation{
			HasMany("comments", "comments", "post_id"),
			BelongsTo("author", "users", "author_id"),
			ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	app.Entity("comments", EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
		},
		Relations: []Relation{
			BelongsTo("post", "posts", "post_id"),
		},
	}.WithTimestamps(false))
	return app
}

// ============================================================================
// Test: HasMany via ?include=
// ============================================================================

func TestInclude_HasMany(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/posts/p1?include=comments")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	comments, ok := got["comments"].([]any)
	if !ok {
		t.Fatalf("expected comments to be a list, got %T (%v)", got["comments"], got["comments"])
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d: %v", len(comments), comments)
	}
}

// ============================================================================
// Test: HasMany returns empty slice when no related rows exist
// ============================================================================

func TestInclude_HasMany_EmptyDefault(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	// p2 has zero comments
	resp := ta.Get("/posts/p2?include=comments")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	comments, ok := got["comments"].([]any)
	if !ok {
		t.Fatalf("expected comments key with list value, got %T", got["comments"])
	}
	if len(comments) != 0 {
		t.Fatalf("expected empty slice, got %v", comments)
	}
}

// ============================================================================
// Test: BelongsTo via ?include= on Get
// ============================================================================

func TestInclude_BelongsTo(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/posts/p1?include=author")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	author, ok := got["author"].(map[string]any)
	if !ok {
		t.Fatalf("expected author object, got %T (%v)", got["author"], got["author"])
	}
	if author["name"] != "Alice" {
		t.Fatalf("expected author.name=Alice, got %v", author["name"])
	}
}

// ============================================================================
// Test: HasOne via ?include= on Get
// ============================================================================

func TestInclude_HasOne(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/users/u1?include=profile")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	profile, ok := got["profile"].(map[string]any)
	if !ok {
		t.Fatalf("expected profile object, got %T (%v)", got["profile"], got["profile"])
	}
	if profile["bio"] != "Hello from Alice" {
		t.Fatalf("expected profile.bio, got %v", profile["bio"])
	}
}

// ============================================================================
// Test: HasOne returns nil when no related row exists
// ============================================================================

func TestInclude_HasOne_NilDefault(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	// u2 has no profile
	resp := ta.Get("/users/u2?include=profile")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, present := got["profile"]; !present || v != nil {
		t.Fatalf("expected profile=nil, got present=%v value=%v", present, v)
	}
}

// ============================================================================
// Test: ManyToMany via pivot table
// ============================================================================

func TestInclude_ManyToMany(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/posts/p1?include=tags")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tags, ok := got["tags"].([]any)
	if !ok {
		t.Fatalf("expected tags list, got %T (%v)", got["tags"], got["tags"])
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %v", len(tags), tags)
	}
}

// ============================================================================
// Test: List with multiple includes batches relation queries (no N+1)
// ============================================================================

func TestInclude_ListMultipleIncludes(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/posts?include=comments,author")
	resp.AssertStatus(t, http.StatusOK)

	var env ListResponse
	if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 2 {
		t.Fatalf("expected 2 posts, got %d", env.Total)
	}
	for _, row := range env.Data {
		if _, ok := row["comments"]; !ok {
			t.Fatalf("expected comments key on every row, got %v", row)
		}
		if _, ok := row["author"]; !ok {
			t.Fatalf("expected author key on every row, got %v", row)
		}
	}
}

// ============================================================================
// Test: Unknown include name returns 400 (not silently ignored)
// ============================================================================

func TestInclude_Unknown_400(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/posts/p1?include=bogus")
	resp.AssertStatus(t, http.StatusBadRequest).
		AssertBodyContains(t, "bogus")
}

// ============================================================================
// Test: No include param leaves response shape unchanged (regression pin)
// ============================================================================

func TestInclude_AbsentLeavesResponseUnchanged(t *testing.T) {
	db := setupBlogDB(t)
	app := blogApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Get("/posts/p1")
	resp.AssertStatus(t, http.StatusOK)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"comments", "author", "tags"} {
		if _, present := got[key]; present {
			t.Fatalf("did not request %q via include, but it appeared in response: %v", key, got)
		}
	}
}
