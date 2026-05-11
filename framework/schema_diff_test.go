package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
)

// ============================================================================
// Test: DiffSchema on an empty DB emits CREATE TABLE per entity
// ============================================================================

func TestSchemaDiff_NewTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text},
			},
		}.WithTimestamps(false)))

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
		}
		if !strings.Contains(changes[0].SQL, "CREATE TABLE IF NOT EXISTS posts") {
			t.Fatalf("expected CREATE TABLE, got %s", changes[0].SQL)
		}

		// Apply and confirm round-trip
		n, err := ApplySchemaDiff(context.Background(), db, changes)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 applied, got %d", n)
		}

		// Re-diff: should be empty now.
		again, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("re-diff: %v", err)
		}
		if len(again) != 0 {
			t.Fatalf("expected no changes after apply, got %+v", again)
		}
	})
}

// ============================================================================
// Test: DiffSchema emits ADD COLUMN for a field the DB doesn't have
// ============================================================================

func TestSchemaDiff_AddColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// Live table has only id + title.
		if _, err := db.Exec(`CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}

		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "views", Type: schema.Int}, // new
				{Name: "published", Type: schema.Bool, Default: false},
			},
		}.WithTimestamps(false)))

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 2 {
			t.Fatalf("expected 2 changes (views, published), got %d: %+v", len(changes), changes)
		}
		for _, c := range changes {
			if !strings.Contains(c.SQL, "ALTER TABLE posts ADD COLUMN") {
				t.Fatalf("expected ADD COLUMN, got %s", c.SQL)
			}
		}

		// Apply + verify the columns are actually there.
		if _, err := ApplySchemaDiff(context.Background(), db, changes); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if _, err := db.Exec("INSERT INTO posts(id, title, views, published) VALUES ($1, $2, $3, $4)", "p1", "hi", 0, false); err != nil {
			t.Fatalf("insert post-apply: %v", err)
		}
	})
}

// ============================================================================
// Test: DiffSchema emits DROP COLUMN for a column the entity no longer
// declares (skipping framework-managed columns).
// ============================================================================

func TestSchemaDiff_DropColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		if dialect == DialectSQLite {
			// SQLite supports DROP COLUMN only from 3.35+. The mattn/go-sqlite3
			// build typically includes it; if not, skip rather than fail.
			var ver string
			if err := db.QueryRow("SELECT sqlite_version()").Scan(&ver); err != nil {
				t.Fatalf("version: %v", err)
			}
			if ver < "3.35" {
				t.Skipf("SQLite %s lacks DROP COLUMN", ver)
			}
		}

		if _, err := db.Exec(`CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			legacy TEXT
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}

		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false)))

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 1 {
			t.Fatalf("expected 1 change (drop legacy), got %d: %+v", len(changes), changes)
		}
		if !strings.Contains(changes[0].SQL, "DROP COLUMN legacy") {
			t.Fatalf("expected DROP COLUMN legacy, got %s", changes[0].SQL)
		}
	})
}

// ============================================================================
// Test: Framework-managed columns aren't dropped just because they aren't
// in the entity's Fields list (the framework adds them implicitly).
// ============================================================================

func TestSchemaDiff_KeepsFrameworkManagedColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			deleted_at TIMESTAMP,
			tenant_id TEXT,
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}

		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table:       "posts",
			SoftDelete:  true,
			MultiTenant: true,
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(true)))

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		for _, c := range changes {
			if strings.Contains(c.SQL, "DROP COLUMN") {
				t.Fatalf("framework-managed column should not be dropped: %s", c.SQL)
			}
		}
	})
}

// ============================================================================
// Test: ApplySchemaDiff is transactional — failure mid-way rolls back.
// ============================================================================

func TestSchemaDiff_ApplyTransactional(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Hand-craft a changes slice where the second entry is bad SQL.
		changes := []SchemaChange{
			{Summary: "add views", SQL: "ALTER TABLE posts ADD COLUMN views INTEGER"},
			{Summary: "bad", SQL: "ALTER TABLE posts ADD COLUMN AND syntax error"},
		}
		_, err := ApplySchemaDiff(context.Background(), db, changes)
		if err == nil {
			t.Fatal("expected apply error")
		}
		// The first ALTER should NOT have committed.
		cols, err := readLiveColumns(context.Background(), db, "posts", detectDialect(db))
		if err != nil {
			t.Fatalf("readLive: %v", err)
		}
		if _, ok := cols["views"]; ok {
			t.Fatal("expected views column to be rolled back, but it exists")
		}
	})
}
