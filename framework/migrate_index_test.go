package framework

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// indexInfo returns the index names + the columns each indexes on the given
// table, for the current dialect. Used to confirm CREATE INDEX actually
// produced what we asked for.
func indexInfo(t *testing.T, db *sql.DB, table string, dialect Dialect) map[string][]string {
	t.Helper()
	if dialect == DialectPostgres {
		rows, err := db.Query(`
			SELECT i.relname, array_to_string(
				ARRAY(SELECT a.attname FROM pg_attribute a WHERE a.attrelid = i.oid ORDER BY a.attnum),
				',')
			FROM pg_class t
			JOIN pg_index ix ON t.oid = ix.indrelid
			JOIN pg_class i ON i.oid = ix.indexrelid
			WHERE t.relname = $1 AND NOT ix.indisprimary
		`, table)
		if err != nil {
			t.Fatalf("pg index query: %v", err)
		}
		defer rows.Close()
		out := map[string][]string{}
		for rows.Next() {
			var name, cols string
			if err := rows.Scan(&name, &cols); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out[name] = strings.Split(cols, ",")
		}
		return out
	}

	// SQLite — parse the original CREATE INDEX DDL stored in sqlite_master.
	// PRAGMA index_info is unreliable in this test harness; the recorded sql
	// is authoritative.
	rows, err := db.Query("SELECT name, sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND name NOT LIKE 'sqlite_%' AND sql IS NOT NULL", table)
	if err != nil {
		t.Fatalf("sqlite index list: %v", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[name] = extractIndexCols(ddl)
	}
	return out
}

// extractIndexCols pulls the comma-separated column list out of a SQLite
// CREATE INDEX statement: "...ON posts (status, author_id)" → [status, author_id].
func extractIndexCols(ddl string) []string {
	open := strings.LastIndex(ddl, "(")
	close := strings.LastIndex(ddl, ")")
	if open < 0 || close < 0 || close <= open {
		return nil
	}
	inner := ddl[open+1 : close]
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ============================================================================
// Test: declared index lands as CREATE INDEX in both dialects
// ============================================================================

func TestMigrate_Indices_CreateIndex(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "status", Type: schema.String},
				{Name: "author_id", Type: schema.String},
			},
			Indices: []entity.Index{
				{Columns: []string{"status"}},
				{Columns: []string{"author_id", "status"}, Name: "idx_posts_author_status"},
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}

		ix := indexInfo(t, db, "posts", dialect)
		// Expected: idx_posts_status (auto-named) + idx_posts_author_status
		// (explicit name). Postgres also has the implicit PK index but
		// indexInfo excludes the primary.
		if _, ok := ix["idx_posts_status"]; !ok {
			t.Fatalf("expected idx_posts_status, got %v", keys(ix))
		}
		if _, ok := ix["idx_posts_author_status"]; !ok {
			t.Fatalf("expected idx_posts_author_status, got %v", keys(ix))
		}
		if cols := ix["idx_posts_author_status"]; len(cols) != 2 {
			t.Fatalf("expected 2-col index, got %v", cols)
		}
	})
}

// ============================================================================
// Test: unique index actually enforces uniqueness at runtime
// ============================================================================

func TestMigrate_Indices_UniqueEnforced(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "slug", Type: schema.String, Required: true},
			},
			Indices: []entity.Index{
				{Columns: []string{"slug"}, Unique: true},
			},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}

		if _, err := db.Exec("INSERT INTO posts(id, slug) VALUES ($1, $2)", "p1", "hello"); err != nil {
			t.Fatalf("insert: %v", err)
		}
		_, err := db.Exec("INSERT INTO posts(id, slug) VALUES ($1, $2)", "p2", "hello")
		if err == nil {
			t.Fatal("expected unique violation, got nil")
		}
	})
}

// ============================================================================
// Test: re-running AutoMigrate is idempotent for indices
// ============================================================================

func TestMigrate_Indices_Idempotent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "status", Type: schema.String},
			},
			Indices: []entity.Index{{Columns: []string{"status"}}},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("first: %v", err)
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("second: %v", err)
		}
	})
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
