package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// seedBlogDB creates the blog test schema (users, profiles, posts, comments,
// tags, post_tags) on the given db and inserts fixture rows. Both dialects
// accept this DDL — TEXT PRIMARY KEY and $N placeholders are portable.
func seedBlogDB(t *testing.T, db *sql.DB) {
	t.Helper()
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
		{"INSERT INTO users(id, name) VALUES ($1, $2)", []any{"u1", "Alice"}},
		{"INSERT INTO users(id, name) VALUES ($1, $2)", []any{"u2", "Bob"}},
		{"INSERT INTO profiles(id, user_id, bio) VALUES ($1, $2, $3)", []any{"prof1", "u1", "Hello from Alice"}},
		{"INSERT INTO posts(id, title, author_id) VALUES ($1, $2, $3)", []any{"p1", "First", "u1"}},
		{"INSERT INTO posts(id, title, author_id) VALUES ($1, $2, $3)", []any{"p2", "Second", "u2"}},
		{"INSERT INTO comments(id, body, post_id) VALUES ($1, $2, $3)", []any{"c1", "nice", "p1"}},
		{"INSERT INTO comments(id, body, post_id) VALUES ($1, $2, $3)", []any{"c2", "great", "p1"}},
		{"INSERT INTO tags(id, name) VALUES ($1, $2)", []any{"t1", "go"}},
		{"INSERT INTO tags(id, name) VALUES ($1, $2)", []any{"t2", "framework"}},
		{"INSERT INTO post_tags(post_id, tag_id) VALUES ($1, $2)", []any{"p1", "t1"}},
		{"INSERT INTO post_tags(post_id, tag_id) VALUES ($1, $2)", []any{"p1", "t2"}},
	}
	for _, s := range seeds {
		if _, err := db.Exec(s.sql, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.sql, err)
		}
	}
}

// nestedBlogApp registers EVERY entity (users, profiles, posts, comments,
// tags) so the registry has a complete graph for nested ?include= resolution.
// (blogApp below is the older variant that omits profiles+tags from the
// registry — kept for tests that exercise the no-target-registered path.)
func nestedBlogApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.HasOne("profile", "profiles", "user_id"),
		},
	}.WithTimestamps(false))
	app.Entity("profiles", entity.EntityConfig{
		Table: "profiles",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "bio", Type: schema.String},
		},
	}.WithTimestamps(false))
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.HasMany("comments", "comments", "post_id"),
			entity.BelongsTo("author", "users", "author_id"),
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	app.Entity("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("post", "posts", "post_id"),
		},
	}.WithTimestamps(false))
	app.Entity("tags", entity.EntityConfig{
		Table: "tags",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	return app
}

// blogApp registers users, profiles, posts, comments, tags with relations.
func blogApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.HasOne("profile", "profiles", "user_id"),
		},
	}.WithTimestamps(false))
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.HasMany("comments", "comments", "post_id"),
			entity.BelongsTo("author", "users", "author_id"),
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	app.Entity("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("post", "posts", "post_id"),
		},
	}.WithTimestamps(false))
	return app
}

// runIncludeTest fans the body out across both dialects, seeding the blog
// schema and wiring the entity registrations once per dialect.
func runIncludeTest(t *testing.T, body func(t *testing.T, ta *TestApp)) {
	t.Helper()
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := blogApp(t, db)
		ta := TestHarness(t, app)
		body(t, ta)
	})
}

// ============================================================================
// Test: HasMany via ?include=
// ============================================================================

func TestInclude_HasMany(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {
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
	})
}

// ============================================================================
// Test: HasMany returns empty slice when no related rows exist
// ============================================================================

func TestInclude_HasMany_EmptyDefault(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

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
	})
}

// ============================================================================
// Test: BelongsTo via ?include= on Get
// ============================================================================

func TestInclude_BelongsTo(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

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
	})
}

// ============================================================================
// Test: HasOne via ?include= on Get
// ============================================================================

func TestInclude_HasOne(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

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
	})
}

// ============================================================================
// Test: HasOne returns nil when no related row exists
// ============================================================================

func TestInclude_HasOne_NilDefault(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

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
	})
}

// ============================================================================
// Test: ManyToMany via pivot table
// ============================================================================

func TestInclude_ManyToMany(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

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
	})
}

// ============================================================================
// Test: List with multiple includes batches relation queries (no N+1)
// ============================================================================

func TestInclude_ListMultipleIncludes(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

		resp := ta.Get("/posts?include=comments,author")
		resp.AssertStatus(t, http.StatusOK)

		var env crud.ListResponse
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
	})
}

// ============================================================================
// Test: Unknown include name returns 400 (not silently ignored)
// ============================================================================

func TestInclude_Unknown_400(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

		resp := ta.Get("/posts/p1?include=bogus")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "bogus")
	})
}

// ============================================================================
// Test: No include param leaves response shape unchanged (regression pin)
// ============================================================================

func TestInclude_AbsentLeavesResponseUnchanged(t *testing.T) {
	runIncludeTest(t, func(t *testing.T, ta *TestApp) {

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
	})
}

// ============================================================================
// Test: Nested includes — ?include=author.profile loads two levels deep
// ============================================================================

func TestInclude_Nested_AuthorProfile(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?include=author.profile")
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
		profile, ok := author["profile"].(map[string]any)
		if !ok {
			t.Fatalf("expected nested profile object on author, got %T (%v)", author["profile"], author["profile"])
		}
		if profile["bio"] != "Hello from Alice" {
			t.Fatalf("expected profile.bio, got %v", profile["bio"])
		}
	})
}

// ============================================================================
// Test: Nested includes alongside flat ones — ?include=author.profile,comments
// ============================================================================

func TestInclude_Nested_Mixed(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?include=author.profile,comments")
		resp.AssertStatus(t, http.StatusOK)

		var got map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		author := got["author"].(map[string]any)
		if author["profile"] == nil {
			t.Fatalf("expected nested profile, got %v", author)
		}
		comments, ok := got["comments"].([]any)
		if !ok || len(comments) != 2 {
			t.Fatalf("expected 2 comments alongside nested author.profile, got %v", comments)
		}
	})
}

// ============================================================================
// Test: Unknown nested segment returns 400 with the bad segment named
// ============================================================================

func TestInclude_Nested_UnknownSegment_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?include=author.bogus")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "bogus")
	})
}

// ============================================================================
// Test: List with nested includes batches every relation across all rows
// (i.e. one query per relation, not per row).
// ============================================================================

func TestInclude_Nested_OnList(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts?include=author.profile")
		resp.AssertStatus(t, http.StatusOK)

		var env crud.ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Total != 2 {
			t.Fatalf("expected 2 posts, got %d", env.Total)
		}
		// p1 has author=Alice (profile present); p2 has author=Bob (no profile).
		for _, row := range env.Data {
			author, ok := row["author"].(map[string]any)
			if !ok {
				t.Fatalf("missing author on row: %v", row)
			}
			if _, present := author["profile"]; !present {
				t.Fatalf("expected author.profile key on every row (nil for u2 OK), got %v", author)
			}
		}
	})
}

// ============================================================================
// Test: scoped include — ?include=comments(body_like=nice) attaches only the
// matching subset.
// ============================================================================

func TestInclude_Scoped_FilterChildren(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		// Add a third comment that should NOT match the scoped filter.
		if _, err := db.Exec(
			"INSERT INTO comments(id, body, post_id) VALUES ($1, $2, $3)",
			"c3", "boring", "p1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?include=comments(body_like=%25nice%25)")
		resp.AssertStatus(t, http.StatusOK)

		var got map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		comments, ok := got["comments"].([]any)
		if !ok {
			t.Fatalf("expected comments slice, got %T (%v)", got["comments"], got["comments"])
		}
		if len(comments) != 1 {
			t.Fatalf("expected 1 matching comment, got %d (%v)", len(comments), comments)
		}
	})
}

// ============================================================================
// Test: scoped include composes with the regular include list — comments
// filtered, author unfiltered.
// ============================================================================

func TestInclude_Scoped_Mixed(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?include=author,comments(body=nice)")
		resp.AssertStatus(t, http.StatusOK)

		var got map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if author, _ := got["author"].(map[string]any); author == nil || author["name"] != "Alice" {
			t.Fatalf("expected unfiltered author, got %v", got["author"])
		}
		comments, _ := got["comments"].([]any)
		if len(comments) != 1 {
			t.Fatalf("expected 1 scoped comment, got %d (%v)", len(comments), comments)
		}
		first := comments[0].(map[string]any)
		if first["body"] != "nice" {
			t.Fatalf("expected body=nice, got %v", first["body"])
		}
	})
}

// ============================================================================
// Test: scoped include with unknown field returns 400
// ============================================================================

func TestInclude_Scoped_UnknownField_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?include=comments(does_not_exist=x)")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "does_not_exist")
	})
}
