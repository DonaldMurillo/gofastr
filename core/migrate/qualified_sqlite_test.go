package migrate

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
)

// A tracking table in an ATTACHed SQLite database ("aux._migrations") broke
// group mode: the column probe trimmed quotes off `"aux"."_migrations"` and
// asked pragma_table_info for `aux"."_migrations`, which never matched, and
// the key upgrade derived a temp name SafeIdent refused, so every Up failed
// (2026-10-27 C4). The probes pass the schema and the bare table apart.
func TestQualifiedSQLiteTrackingTableGroups(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "main.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// ATTACH is per connection.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`ATTACH DATABASE ? AS aux`, filepath.Join(dir, "aux.db")); err != nil {
		t.Fatal(err)
	}
	// A decoy of the same bare name in main, already group-aware: a probe
	// that drops the schema reads this table instead of aux's.
	if _, err := db.Exec(`CREATE TABLE _migrations (group_name TEXT NOT NULL DEFAULT '', version BIGINT NOT NULL, PRIMARY KEY (group_name, version))`); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	m := New(db, WithDialect(DialectSQLite), WithTableName("aux._migrations"))
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with groups: %v", err)
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_info('_migrations', 'aux') WHERE pk > 0 ORDER BY pk`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"group_name", "version"}) {
		t.Fatalf("aux._migrations PK = %v, want [group_name version]", cols)
	}
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Applied) != 2 {
		t.Fatalf("applied = %d, want 2", len(st.Applied))
	}
}
