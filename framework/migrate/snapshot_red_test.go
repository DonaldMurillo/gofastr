//go:build red

// RED TEST — open finding, 2026-09-03 round 3 (tests-only; no fix applied).
// Property: RenderMigrationFileChecked is the guarded renderer — nothing it
// renders may synthesize a line the runner's parser reads as a `-- +migrate`
// directive (core/migrate parseMigration is line-based and quote-blind,
// pinned in TestSQLDefaultCannotSynthesizeDirective). It guards the up and
// down bodies but renders the Name directive line verbatim.
// Surfaces: framework/migrate/snapshot.go RenderMigrationFileChecked (:344)
// delegates to RenderMigrationFile (:329) `-- +migrate Name %s\n`; the
// containsDirectiveLine guard (:345-349) never sees `name`.
// Finding: a Name carrying "\n-- +migrate Down\nDROP TABLE victims;-- "
// returns nil error and is committed across lines: the runner flips to its
// Down section early, so the attacker's statements sit in Down and execute
// verbatim on rollback of that version — the exact synthesis
// ErrDirectiveInSQL exists to refuse, one parameter left of where it
// checks. Severity: low — unreachable today: the only caller,
// GenerateMigrationFile (generate_file.go:83-87), passes
// sanitizeMigrationName's slug ([a-z0-9_]), which cannot carry a newline;
// the pin holds the exported checked renderer to its own contract, which
// every future caller inherits.
// Fix direction: run the same refusal over name — a Name whose rendering
// spans a directive line returns ErrDirectiveInSQL (not silent
// sanitization: rewriting a migration's Name changes its identity).
package migrate

import (
	"errors"
	"strings"
	"testing"
)

func TestSnapshotRedValidatesNameLine(t *testing.T) {
	for _, name := range []string{
		"fine\n-- +migrate Down\nDROP TABLE victims;-- ",
		"x\n-- +migrate Up\nSELECT 1;-- ",
	} {
		content, err := RenderMigrationFileChecked(7, name,
			"CREATE TABLE t (id TEXT);", "DROP TABLE t;")
		if err == nil {
			t.Errorf("SECURITY: [migrate] RenderMigrationFileChecked accepted Name %q: "+
				"the Name line is rendered verbatim, so the runner's line-based, "+
				"quote-blind parser reads a synthesized directive inside it and the "+
				"attacker's statements land in a section that executes verbatim.\n"+
				"rendered file:\n%s", name, content)
			continue
		}
		if !errors.Is(err, ErrDirectiveInSQL) {
			t.Errorf("[migrate] unexpected rejection for Name %q: %v (want ErrDirectiveInSQL)", name, err)
		}
	}

	// Control: an ordinary name must keep rendering — the fix cannot be
	// "reject every name".
	content, err := RenderMigrationFileChecked(7, "add_email", "SELECT 1;", "")
	if err != nil {
		t.Errorf("[migrate] RenderMigrationFileChecked regressed on the benign name add_email: %v", err)
	} else if !strings.Contains(content, "-- +migrate Name add_email\n") {
		t.Errorf("[migrate] benign Name line no longer rendered:\n%s", content)
	}
}
