package framework

import (
	"database/sql"
	"testing"
)

// TestHelper_ForEachDialect_Smoke confirms the harness opens a working DB on
// each dialect and that simple DDL plus a round-trip query works. Postgres
// is auto-skipped if neither TEST_POSTGRES_DSN nor Docker is available.
func TestHelper_ForEachDialect_Smoke(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		if _, err := db.Exec(`CREATE TABLE smoke (id TEXT PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := db.Exec("INSERT INTO smoke(id, name) VALUES ($1, $2)", "a", "alice"); err != nil {
			t.Fatalf("insert: %v", err)
		}
		var name string
		if err := db.QueryRow("SELECT name FROM smoke WHERE id = $1", "a").Scan(&name); err != nil {
			t.Fatalf("select: %v", err)
		}
		if name != "alice" {
			t.Fatalf("expected alice, got %q", name)
		}
	})
}
