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
