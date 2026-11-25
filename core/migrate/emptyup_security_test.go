package migrate

import (
	"context"
	"strings"
	"testing"
)

// Pins the silent no-op apply of an Up-less migration file, found by the
// 2026-09-04 red-probe round; fixed in migrate.go by parseMigration rejecting
// a file whose Up section never opened (or whose Up is empty while the file
// carries SQL lines outside any section) and Register refusing an empty Up.
// Family: F14 migration and schema safety (silent no-op apply)
// Property: a migration file that carries no executable Up SQL must fail loud
// at parse/registration/apply time — it must never be recorded as applied,
// because a recorded no-op makes the tracking table assert schema state that
// was never created.
// Surfaces: core/migrate/migrate.go::parseMigration (section directives are
// case-sensitive; a file with no "-- +migrate Up" line used to parse fine
// with Up="" and its SQL silently discarded), core/migrate/migrate.go::
// Register (now refuses an empty Up as it refuses an invalid group name),
// core/migrate/runner.go::runMigrationUp (executes the empty string and
// records the row with a checksum-of-empty — unreachable once the guards
// hold).

// upRowRecorded reports whether version v has a tracking row.
func upRowRecorded(t *testing.T, m *Migrator, version uint64) bool {
	t.Helper()
	var n int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = ?", version).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n > 0
}

// TestMissingUpDirectiveFailsLoud walks the authoring shapes that silently
// discarded the SQL before the fix: no Up directive at all, a lower-case "up"
// spelling, Up SQL stranded before the directive, and a programmatic
// Register of an empty Up.
func TestMissingUpDirectiveFailsLoud(t *testing.T) {
	files := map[string]string{
		"no up directive": "-- +migrate Version 1\n-- +migrate Name ghost\nCREATE TABLE ghost (id INTEGER);\n",
		"lower-case up":   "-- +migrate Version 1\n-- +migrate Name ghost\n-- +migrate up\nCREATE TABLE ghost (id INTEGER);\n-- +migrate Down\nDROP TABLE ghost;\n",
		"sql before up":   "-- +migrate Version 1\n-- +migrate Name ghost\nCREATE TABLE ghost (id INTEGER);\n-- +migrate Up\n-- +migrate Down\nDROP TABLE ghost;\n",
	}
	for name, file := range files {
		t.Run(name, func(t *testing.T) {
			m, db := newSQLiteMigrator(t)
			ctx := context.Background()

			if err := m.RegisterFromReader(strings.NewReader(file)); err != nil {
				// Rejecting at parse/registration time is the loud outcome.
				return
			}
			applyErr := m.Up(ctx)

			if applyErr == nil && upRowRecorded(t, m, 1) {
				t.Fatalf("SECURITY: [migrate] migration %q registered with no executable Up SQL, "+
					"Up() returned nil, and version 1 is recorded applied — the file's DDL was silently "+
					"discarded by the directive parser and the tracking table now asserts a schema state "+
					"that was never created (ghost table exists: %v)", name, tableExists(t, db, "ghost"))
			}
		})
	}

	t.Run("register empty up", func(t *testing.T) {
		m, _ := newSQLiteMigrator(t)
		if err := m.Register(Migration{Version: 1, Name: "ghost", Up: "", Down: "DROP TABLE ghost;"}); err == nil {
			t.Fatal("SECURITY: [migrate] Register accepted a migration with empty Up SQL — it would apply " +
				"nothing yet be recorded applied; refuse it like an invalid group name")
		}
	})
}

func TestMigrationRefusesUnsectionedSQL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix string
	}{
		{"before Up", "CREATE TABLE a (id INTEGER);\n"},
		{"before Down", "-- +migrate NoTransaction\n/* header */ CREATE TABLE a (id INTEGER);\n-- +migrate Down\nDROP TABLE b;\n"},
		{"after SQLite comment", "/* /* */ CREATE TABLE a (id INTEGER);\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, db := newSQLiteMigrator(t)
			file := "-- +migrate Version 1\n-- +migrate Name example\n" + tc.prefix +
				"-- +migrate Up\nCREATE TABLE b (id INTEGER);\n"
			regErr := m.RegisterFromReader(strings.NewReader(file))
			if err := m.Up(context.Background()); err != nil {
				t.Fatalf("Up after registration attempt: %v", err)
			}
			a, b, recorded := tableExists(t, db, "a"), tableExists(t, db, "b"), upRowRecorded(t, m, 1)
			if regErr == nil || !strings.Contains(regErr.Error(), "outside any section") ||
				!strings.Contains(regErr.Error(), "if not yet applied, move it below -- +migrate Up (or into Down for rollback SQL)") ||
				!strings.Contains(regErr.Error(), "if already applied, remove it from this file and put it in a new migration") || a || b || recorded {
				t.Fatalf("want actionable registration error, neither table, and no record; got register=%v, a=%t, b=%t, recorded=%t",
					regErr, a, b, recorded)
			}
		})
	}
}

func TestMigrationAcceptsNestedPGComment(t *testing.T) {
	m := New(nil, WithDialect(DialectPostgres))
	file := "-- +migrate Version 1\n/* /* */ text still inside the outer comment */\n" +
		"-- +migrate Up\nCREATE TABLE b (id INTEGER);\n"
	if err := m.RegisterFromReader(strings.NewReader(file)); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestMigrationAcceptsCommentPreamble(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	file := " \t\n-- heading\n-- +migrate Version 1\n" +
		"/* multi-line\n   comment with SQL: CREATE TABLE ignored (id INTEGER);\n*/ -- trailing comment\n" +
		"-- +migrate Down\nDROP TABLE b;\n" +
		"-- +migrate Up\nCREATE TABLE b (id INTEGER);\n"
	if err := m.RegisterFromReader(strings.NewReader(file)); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !tableExists(t, db, "b") || !upRowRecorded(t, m, 1) {
		t.Fatal("Up did not create and record the migration")
	}
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if tableExists(t, db, "b") || upRowRecorded(t, m, 1) {
		t.Fatal("Down did not roll back the migration")
	}
}
