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

// TestMigrate_Indices_ExpressionUniqueEnforced pins V3 #3: a functional
// index `UNIQUE(user_id, lower(food))` deduplicates case-insensitively,
// which the column-list form can't express. Tested by inserting two
// rows whose `food` differs only in case — the second must fail.
func TestMigrate_Indices_ExpressionUniqueEnforced(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("triggers", entity.EntityConfig{
			Table: "triggers",
			Fields: []schema.Field{
				{Name: "user_id", Type: schema.String, Required: true},
				{Name: "food", Type: schema.String, Required: true},
			},
			Indices: []entity.Index{
				{
					Name:       "uniq_triggers_user_lowerfood",
					Expression: "user_id, lower(food)",
					Unique:     true,
				},
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}

		if _, err := db.Exec(`INSERT INTO triggers(id, user_id, food) VALUES ($1, $2, $3)`, "t1", "u1", "Coffee"); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		// Different casing — must collide via lower().
		if _, err := db.Exec(`INSERT INTO triggers(id, user_id, food) VALUES ($1, $2, $3)`, "t2", "u1", "COFFEE"); err == nil {
			t.Fatal("expected functional-unique violation on COFFEE vs Coffee, got nil")
		}
		// Different user — must still succeed.
		if _, err := db.Exec(`INSERT INTO triggers(id, user_id, food) VALUES ($1, $2, $3)`, "t3", "u2", "Coffee"); err != nil {
			t.Fatalf("different-user insert should succeed: %v", err)
		}
	})
}

// TestMigrate_Indices_ExpressionRequiresName pins the safety contract:
// expression indices can't auto-derive a name, so omitting Name must
// panic at startup rather than silently emit broken DDL.
func TestMigrate_Indices_ExpressionRequiresName(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic when Expression set without Name")
			}
		}()
		reg := NewRegistry()
		reg.Register(entity.Define("triggers", entity.EntityConfig{
			Table: "triggers",
			Fields: []schema.Field{
				{Name: "user_id", Type: schema.String},
				{Name: "food", Type: schema.String},
			},
			Indices: []entity.Index{
				{Expression: "user_id, lower(food)", Unique: true},
			},
		}.WithTimestamps(false)))
		_ = AutoMigrate(db, reg)
	})
}

// TestMigrate_Indices_ExpressionRejectsStatementTerminator guards
// against the operator-pasted-SQL-statement footgun: a stray semicolon
// or comment in the expression must fail loud, not stitch a second
// statement into the CREATE INDEX output.
func TestMigrate_Indices_ExpressionRejectsStatementTerminator(t *testing.T) {
	cases := []string{
		"user_id, lower(food);",
		"user_id, lower(food) -- danger",
		"user_id /* comment */, lower(food)",
		"user_id, lower(food) */",
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
				defer func() {
					if recover() == nil {
						t.Fatalf("expected panic for expression %q", expr)
					}
				}()
				reg := NewRegistry()
				reg.Register(entity.Define("t", entity.EntityConfig{
					Table:  "t",
					Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "food", Type: schema.String}},
					Indices: []entity.Index{
						{Name: "bad", Expression: expr, Unique: true},
					},
				}.WithTimestamps(false)))
				_ = AutoMigrate(db, reg)
			})
		})
	}
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
