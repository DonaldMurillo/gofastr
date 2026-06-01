package framework

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// requireDropColumn skips on SQLite builds older than 3.35 (no DROP COLUMN).
func requireDropColumn(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()
	if dialect != DialectSQLite {
		return
	}
	var ver string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&ver); err != nil {
		t.Fatalf("version: %v", err)
	}
	if ver < "3.35" {
		t.Skipf("SQLite %s lacks DROP COLUMN", ver)
	}
}

// TestDiffSchema_DropMarkedDestructive pins that a DROP COLUMN change carries
// Destructive=true while ADD COLUMN does not.
func TestDiffSchema_DropMarkedDestructive(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, legacy TEXT)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "blurb", Type: schema.Text}, // ADD (non-destructive)
			},
		}.WithTimestamps(false)))

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		var sawDrop, sawAdd bool
		for _, c := range changes {
			switch {
			case strings.Contains(c.SQL, "DROP COLUMN"):
				sawDrop = true
				if !c.Destructive {
					t.Errorf("DROP COLUMN change not marked Destructive: %+v", c)
				}
			case strings.Contains(c.SQL, "ADD COLUMN"):
				sawAdd = true
				if c.Destructive {
					t.Errorf("ADD COLUMN change wrongly marked Destructive: %+v", c)
				}
			}
		}
		if !sawDrop || !sawAdd {
			t.Fatalf("expected both an ADD and a DROP change, got %+v", changes)
		}
	})
}

// TestApplySchemaDiff_RefusesDestructive asserts the default Apply path rejects
// a change set containing a destructive change, returns a *DestructiveChangeError,
// and runs NO DDL (not even the safe changes alongside it).
func TestApplySchemaDiff_RefusesDestructive(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		changes := []SchemaChange{
			{Summary: "posts: add column views", SQL: "ALTER TABLE posts ADD COLUMN views INTEGER"},
			{Summary: "posts: drop column title", SQL: "ALTER TABLE posts DROP COLUMN title", Destructive: true},
		}
		n, err := ApplySchemaDiff(context.Background(), db, changes)
		if err == nil {
			t.Fatal("expected ApplySchemaDiff to refuse the destructive change")
		}
		var de *DestructiveChangeError
		if !errors.As(err, &de) {
			t.Fatalf("expected *DestructiveChangeError, got %T: %v", err, err)
		}
		if n != 0 {
			t.Fatalf("expected 0 changes applied, got %d", n)
		}
		// The safe ADD must NOT have run — the gate rejects before the tx.
		cols, _ := migrate.ReadLiveColumns(context.Background(), db, "posts", migrate.DetectDialect(db))
		if _, ok := cols["views"]; ok {
			t.Error("safe ADD COLUMN ran despite the destructive change blocking the set")
		}
	})
}

// TestApplySchemaDiff_AllowsDestructiveOptIn asserts the opt-in path actually
// performs the destructive change.
func TestApplySchemaDiff_AllowsDestructiveOptIn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		requireDropColumn(t, db, dialect)
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, legacy TEXT)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		changes := []SchemaChange{
			{Summary: "posts: drop column legacy", SQL: "ALTER TABLE posts DROP COLUMN legacy", Destructive: true},
		}
		n, err := ApplySchemaDiffWithOptions(context.Background(), db, changes, ApplyOptions{AllowDestructive: true})
		if err != nil {
			t.Fatalf("ApplySchemaDiffWithOptions(allow): %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 applied, got %d", n)
		}
		cols, _ := migrate.ReadLiveColumns(context.Background(), db, "posts", migrate.DetectDialect(db))
		if _, ok := cols["legacy"]; ok {
			t.Error("legacy column still present after opted-in destructive apply")
		}
	})
}
