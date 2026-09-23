package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestGeneratePlan_ExistingTable_NewColumnIndex asserts that adding a column
// that has an index (or is a BelongsTo FK) to an existing table emits both
// ADD COLUMN and CREATE INDEX.
func TestGeneratePlan_ExistingTable_NewColumnIndex(t *testing.T) {
	postEnt := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Indices: []entity.Index{
			{Columns: []string{"title"}},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false))

	userEnt := entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
		},
	}.WithTimestamps(false))

	// Pre-existing table in snapshot only has id and title (no author_id)
	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"posts": {
				"id":    "TEXT",
				"title": "TEXT",
			},
			"users": {
				"id": "TEXT",
			},
		},
		TableDDL: map[string]string{
			"posts": "CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT);",
			"users": "CREATE TABLE users (id TEXT PRIMARY KEY);",
		},
		Indices: map[string][]string{
			"posts": {
				"CREATE INDEX IF NOT EXISTS idx_posts_title ON posts (title)",
			},
		},
	}

	plan := Plan{
		Registry: testReg{"posts": postEnt, "users": userEnt},
	}

	up, down, _, err := GeneratePlan(plan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// Must add author_id column
	if !strings.Contains(up, "ADD COLUMN author_id") {
		t.Fatalf("expected ADD COLUMN author_id in UP, got:\n%s", up)
	}

	// Must create index for the BelongsTo FK author_id
	if !strings.Contains(up, "idx_posts_author_id") {
		t.Fatalf("expected CREATE INDEX for author_id in UP, got:\n%s", up)
	}

	// Down must drop the index
	if !strings.Contains(down, "DROP INDEX IF EXISTS idx_posts_author_id") {
		t.Fatalf("expected DROP INDEX for author_id in DOWN, got:\n%s", down)
	}
}

// TestGeneratePlan_ExistingTable_IndexDrift asserts that adding or removing
// an index on an existing table emits CREATE INDEX / DROP INDEX.
func TestGeneratePlan_ExistingTable_IndexDrift(t *testing.T) {
	postEnt := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "slug", Type: schema.String},
		},
		Indices: []entity.Index{
			{Columns: []string{"slug"}}, // New index added on slug; old title index removed
		},
	}.WithTimestamps(false))

	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"posts": {
				"id":    "TEXT",
				"title": "TEXT",
				"slug":  "TEXT",
			},
		},
		TableDDL: map[string]string{
			"posts": "CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, slug TEXT);",
		},
		Indices: map[string][]string{
			"posts": {
				"CREATE INDEX IF NOT EXISTS idx_posts_title ON posts (title)",
			},
		},
	}

	plan := Plan{
		Registry: testReg{"posts": postEnt},
	}

	up, down, nextSnap, err := GeneratePlan(plan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// UP must create idx_posts_slug and drop idx_posts_title
	if !strings.Contains(up, "idx_posts_slug") {
		t.Fatalf("expected CREATE INDEX idx_posts_slug in UP, got:\n%s", up)
	}
	if !strings.Contains(up, "DROP INDEX IF EXISTS idx_posts_title") {
		t.Fatalf("expected DROP INDEX idx_posts_title in UP, got:\n%s", up)
	}

	// DOWN must recreate idx_posts_title and drop idx_posts_slug
	if !strings.Contains(down, "DROP INDEX IF EXISTS idx_posts_slug") {
		t.Fatalf("expected DROP INDEX idx_posts_slug in DOWN, got:\n%s", down)
	}
	if !strings.Contains(down, "CREATE INDEX IF NOT EXISTS idx_posts_title") {
		t.Fatalf("expected CREATE INDEX idx_posts_title in DOWN, got:\n%s", down)
	}

	// nextSnap must record the new index set
	if len(nextSnap.Indices["posts"]) != 1 || !strings.Contains(nextSnap.Indices["posts"][0], "idx_posts_slug") {
		t.Fatalf("expected nextSnap to have idx_posts_slug, got: %v", nextSnap.Indices["posts"])
	}
}

// TestGeneratePlan_LegacyNilIndices_EmitsIndices asserts that when prevSnap has Indices == nil
// (e.g. legacy snapshots), indices on existing tables are still emitted and not silently skipped.
func TestGeneratePlan_LegacyNilIndices_EmitsIndices(t *testing.T) {
	postEnt := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "slug", Type: schema.String},
		},
		Indices: []entity.Index{
			{Columns: []string{"slug"}},
		},
	}.WithTimestamps(false))

	// Legacy snapshot: Indices is nil
	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"posts": {
				"id":   "TEXT",
				"slug": "TEXT",
			},
		},
		TableDDL: map[string]string{
			"posts": "CREATE TABLE posts (id TEXT PRIMARY KEY, slug TEXT);",
		},
		Indices: nil,
	}

	plan := Plan{
		Registry: testReg{"posts": postEnt},
	}

	up, down, nextSnap, err := GeneratePlan(plan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if !strings.Contains(up, "idx_posts_slug") {
		t.Fatalf("expected CREATE INDEX idx_posts_slug in UP for legacy nil Indices snapshot, got:\n%s", up)
	}
	if !strings.Contains(down, "DROP INDEX IF EXISTS idx_posts_slug") {
		t.Fatalf("expected DROP INDEX idx_posts_slug in DOWN, got:\n%s", down)
	}
	if nextSnap.Indices == nil || len(nextSnap.Indices["posts"]) != 1 {
		t.Fatalf("expected nextSnap to record index, got: %v", nextSnap.Indices)
	}
}

// TestGeneratePlan_NoDuplicateIndexDDL asserts that adding a new column with an index
// emits the CREATE INDEX statement exactly once in UP and DROP INDEX exactly once in DOWN.
func TestGeneratePlan_NoDuplicateIndexDDL(t *testing.T) {
	postEnt := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false))

	userEnt := entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
		},
	}.WithTimestamps(false))

	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"posts": {"id": "TEXT"},
			"users": {"id": "TEXT"},
		},
		TableDDL: map[string]string{
			"posts": "CREATE TABLE posts (id TEXT PRIMARY KEY);",
			"users": "CREATE TABLE users (id TEXT PRIMARY KEY);",
		},
		Indices: map[string][]string{
			"posts": {},
		},
	}

	plan := Plan{
		Registry: testReg{"posts": postEnt, "users": userEnt},
	}

	up, down, _, err := GeneratePlan(plan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	createCount := strings.Count(up, "idx_posts_author_id")
	if createCount != 1 {
		t.Fatalf("expected exactly 1 idx_posts_author_id in UP, got %d:\n%s", createCount, up)
	}

	dropCount := strings.Count(down, "idx_posts_author_id")
	if dropCount != 1 {
		t.Fatalf("expected exactly 1 idx_posts_author_id in DOWN, got %d:\n%s", dropCount, down)
	}
}

// TestGeneratePlan_DroppedTable_RestoresIndicesInDown asserts that rolling back a dropped
// table restores both the table and its secondary indices from the snapshot.
func TestGeneratePlan_DroppedTable_RestoresIndicesInDown(t *testing.T) {
	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"comments": {
				"id":      "TEXT",
				"post_id": "TEXT",
			},
		},
		TableDDL: map[string]string{
			"comments": "CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT);",
		},
		Indices: map[string][]string{
			"comments": {
				"CREATE INDEX IF NOT EXISTS idx_comments_post_id ON comments (post_id)",
			},
		},
	}

	plan := Plan{
		Registry: testReg{}, // comments dropped
	}

	up, down, _, err := GeneratePlan(plan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if !strings.Contains(up, "DROP TABLE IF EXISTS comments") {
		t.Fatalf("expected DROP TABLE comments in UP, got:\n%s", up)
	}

	if !strings.Contains(down, "CREATE TABLE comments") {
		t.Fatalf("expected CREATE TABLE comments in DOWN, got:\n%s", down)
	}

	if !strings.Contains(down, "idx_comments_post_id") {
		t.Fatalf("expected CREATE INDEX idx_comments_post_id in DOWN for dropped table, got:\n%s", down)
	}
}

// TestGeneratePlan_IndexDefinitionDrift_SameName asserts that when an existing index
// has its definition changed (such as toggling Unique: true) without changing its name,
// GeneratePlan emits a DROP + CREATE in UP and restores the previous DDL in DOWN.
func TestGeneratePlan_IndexDefinitionDrift_SameName(t *testing.T) {
	userEnt := entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "email", Type: schema.String},
		},
		Indices: []entity.Index{
			{Name: "idx_users_email", Columns: []string{"email"}, Unique: true},
		},
	}.WithTimestamps(false))

	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"users": {
				"id":    "TEXT",
				"email": "TEXT",
			},
		},
		TableDDL: map[string]string{
			"users": "CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT);",
		},
		Indices: map[string][]string{
			"users": {
				"CREATE INDEX IF NOT EXISTS idx_users_email ON users (email)",
			},
		},
	}

	plan := Plan{
		Registry: testReg{"users": userEnt},
	}

	up, down, nextSnap, err := GeneratePlan(plan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if !strings.Contains(up, "DROP INDEX IF EXISTS idx_users_email") {
		t.Fatalf("expected DROP INDEX idx_users_email in UP, got:\n%s", up)
	}
	if !strings.Contains(up, "CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email") {
		t.Fatalf("expected CREATE UNIQUE INDEX idx_users_email in UP, got:\n%s", up)
	}
	// Assert statement order: DROP must precede CREATE in UP
	dropIdxUP := strings.Index(up, "DROP INDEX IF EXISTS idx_users_email")
	createIdxUP := strings.Index(up, "CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email")
	if dropIdxUP >= createIdxUP {
		t.Fatalf("expected DROP INDEX to precede CREATE INDEX in UP, got UP:\n%s", up)
	}

	if !strings.Contains(down, "DROP INDEX IF EXISTS idx_users_email") {
		t.Fatalf("expected DROP INDEX idx_users_email in DOWN, got:\n%s", down)
	}
	if !strings.Contains(down, "CREATE INDEX IF NOT EXISTS idx_users_email") {
		t.Fatalf("expected original non-unique CREATE INDEX restored in DOWN, got:\n%s", down)
	}
	// Assert statement order: DROP must precede CREATE in DOWN
	dropIdxDOWN := strings.Index(down, "DROP INDEX IF EXISTS idx_users_email")
	createIdxDOWN := strings.Index(down, "CREATE INDEX IF NOT EXISTS idx_users_email")
	if dropIdxDOWN >= createIdxDOWN {
		t.Fatalf("expected DROP INDEX to precede CREATE INDEX in DOWN, got DOWN:\n%s", down)
	}

	if len(nextSnap.Indices["users"]) != 1 || !strings.Contains(nextSnap.Indices["users"][0], "UNIQUE") {
		t.Fatalf("expected nextSnap to have UNIQUE index, got: %v", nextSnap.Indices["users"])
	}
}

// TestParseIndexName asserts that parseIndexName reliably extracts index names
// from various CREATE and DROP statements including CONCURRENTLY and IF EXISTS clauses.
func TestParseIndexName(t *testing.T) {
	cases := []struct {
		ddl  string
		want string
	}{
		{"CREATE INDEX idx_simple ON users (email)", "idx_simple"},
		{"CREATE INDEX IF NOT EXISTS idx_standard ON users (email)", "idx_standard"},
		{"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_concurrent ON users (email)", "idx_concurrent"},
		{"CREATE UNIQUE INDEX IF NOT EXISTS idx_unique ON users (email)", "idx_unique"},
		{"DROP INDEX IF EXISTS idx_drop_me", "idx_drop_me"},
		{"DROP INDEX CONCURRENTLY IF EXISTS idx_drop_concurrent", "idx_drop_concurrent"},
		{"DROP INDEX idx_plain", "idx_plain"},
	}

	for _, tc := range cases {
		got := parseIndexName(tc.ddl)
		if got != tc.want {
			t.Errorf("parseIndexName(%q) = %q, want %q", tc.ddl, got, tc.want)
		}
	}
}
