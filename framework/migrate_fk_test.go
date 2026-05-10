package framework

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// captureCreateTable records the DDL emitted by AutoMigrate so tests can
// inspect FK clauses without relying on dialect-specific catalog views.
//
// Implemented via a simple SQLite memory DB plus sqlite_master scrape.
func captureCreateTable(t *testing.T, registry *Registry) map[string]string {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := AutoMigrate(db, registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	rows, err := db.Query("SELECT name, sql FROM sqlite_master WHERE type = 'table' AND sql IS NOT NULL")
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var name, sqlText string
		if err := rows.Scan(&name, &sqlText); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[name] = sqlText
	}
	return out
}

// ============================================================================
// Test: BelongsTo emits FK constraint in CREATE TABLE
// ============================================================================

func TestMigrate_FK_BelongsToEmitsForeignKey(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Define("users", EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)))
	reg.Register(Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []Relation{
			BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false)))

	tables := captureCreateTable(t, reg)
	posts := tables["posts"]
	if !strings.Contains(posts, "FOREIGN KEY (author_id) REFERENCES users(id)") {
		t.Fatalf("expected FK clause on posts, got:\n%s", posts)
	}
}

// ============================================================================
// Test: FK constraint actually enforces under PRAGMA foreign_keys = ON
// ============================================================================

func TestMigrate_FK_EnforcedAtRuntime(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Define("users", EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)))
	reg.Register(Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []Relation{
			BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false)))

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	if err := AutoMigrate(db, reg); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	// Insert a post pointing at a non-existent user — must fail.
	_, err = db.Exec("INSERT INTO posts(id, title, author_id) VALUES (?, ?, ?)", "p1", "orphan", "no-such-user")
	if err == nil {
		t.Fatal("expected FK violation when inserting post with bogus author_id, got nil")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY") && !strings.Contains(err.Error(), "constraint") {
		t.Fatalf("expected FK error, got %v", err)
	}
}

// ============================================================================
// Test: AutoMigrate creates referenced tables before referencers
// ============================================================================

func TestMigrate_FK_TopologicallySorted(t *testing.T) {
	reg := NewRegistry()
	// Register in reverse dependency order to prove sort works.
	reg.Register(Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []Relation{BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false)))
	reg.Register(Define("users", EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)))

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	// Should not error even though posts was registered first.
	if err := AutoMigrate(db, reg); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
}

// ============================================================================
// Test: missing FK target → error before any DDL runs
// ============================================================================

func TestMigrate_FK_MissingTarget_Errors(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []Relation{
			BelongsTo("author", "users_does_not_exist", "author_id"),
		},
	}.WithTimestamps(false)))

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	err = AutoMigrate(db, reg)
	if err == nil {
		t.Fatal("expected error for missing FK target, got nil")
	}
	if !strings.Contains(err.Error(), "users_does_not_exist") {
		t.Fatalf("expected error to name missing entity, got %v", err)
	}
}

// ============================================================================
// Test: HasMany / HasOne do NOT add FK on the source entity (they live on target)
// ============================================================================

func TestMigrate_FK_HasManyDoesNotAddSourceFK(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Define("users", EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
		Relations: []Relation{
			HasMany("posts", "posts", "author_id"), // FK lives on posts, not users
		},
	}.WithTimestamps(false)))
	reg.Register(Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []Relation{
			BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false)))

	tables := captureCreateTable(t, reg)
	if strings.Contains(tables["users"], "FOREIGN KEY") {
		t.Fatalf("expected no FK on users (HasMany side); got:\n%s", tables["users"])
	}
	if !strings.Contains(tables["posts"], "FOREIGN KEY (author_id) REFERENCES users(id)") {
		t.Fatalf("expected FK on posts (BelongsTo side); got:\n%s", tables["posts"])
	}
}
