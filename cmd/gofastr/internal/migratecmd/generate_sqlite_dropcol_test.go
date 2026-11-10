package migratecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A removed field generates a migration `gofastr migrate up` applies on
// the default SQLite, whatever the column carried, and the generate
// warning names the column. SQLite refuses ALTER TABLE … DROP COLUMN on a
// UNIQUE column and on one a foreign key names, so those leave with a
// table rebuild; the rebuild holds no DROP COLUMN statement, and the
// warning reads the comment line the generator writes above it. The bug:
// removing a `unique: true` or `type: relation` field wrote a bare DROP
// COLUMN that `migrate up` failed on.
//
// Each case generates the first migration, applies it, removes one field,
// generates the second migration and applies it.
func TestGenerateSQLiteColumnDropApplies(t *testing.T) {
	const base = "  - name: authors\n    fields:\n      - name: name\n        type: string\n" +
		"  - name: posts\n    fields:\n      - name: title\n        type: string\n"
	cases := map[string]struct{ column, field string }{
		// Control: a plain column drops with DROP COLUMN.
		"plain":    {"views", "      - name: views\n        type: int\n"},
		"unique":   {"slug", "      - name: slug\n        type: string\n        unique: true\n"},
		"relation": {"author_id", "      - name: author_id\n        type: relation\n        to: authors\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			clitest.CovT_chdir(t, dir)
			spec := filepath.Join(dir, "gofastr.yml")
			dbURL := "--db-url=" + filepath.Join(dir, "app.db")

			clitest.WriteTestFile(t, spec, "app:\n  name: testapp\nentities:\n"+base+tc.field)
			runGenerate(t, "generate", "init", "--from="+spec)
			if out, code := migrateUp(t, dbURL); code != -1 {
				t.Fatalf("setup: migrate up of the first migration exited %d:\n%s", code, out)
			}

			clitest.WriteTestFile(t, spec, "app:\n  name: testapp\nentities:\n"+base)
			warned := runGenerate(t, "generate", "trim", "--from="+spec)
			body, err := os.ReadFile(filepath.Join(dir, "migrations", "0002_trim.sql"))
			if err != nil {
				t.Fatal(err)
			}
			if want := "DROP COLUMN posts." + tc.column; !strings.Contains(warned, want) {
				t.Errorf("the generate warning does not name %q:\n%s\n--- 0002_trim.sql ---\n%s", want, warned, body)
			}
			if out, code := migrateUp(t, dbURL); code != -1 {
				t.Fatalf("migrate generate wrote a migration SQLite refuses; migrate up exited %d:\n%s\n--- 0002_trim.sql ---\n%s", code, strings.TrimSpace(out), body)
			}
		})
	}
}

// migrateUp runs `gofastr migrate up` and returns what it printed and its
// exit code, -1 when it returned normally.
func migrateUp(t *testing.T, dbURL string) (string, int) {
	t.Helper()
	code := -1
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunMigrate([]string{"up", dbURL}) })
	})
	return out, code
}

// The rebuild note is read like the statement it stands in for: quoted
// and schema-qualified names come out bare, and the note counts only as a
// real comment in the Up, not inside a string literal or the Down.
func TestDestructiveDropsReadsRebuildNote(t *testing.T) {
	file := "-- +migrate Up\n" +
		"-- DROP COLUMN posts.slug by table rebuild: SQLite cannot drop it in place\n" +
		"-- DROP COLUMN archive.posts.\"order\" by table rebuild: SQLite cannot drop it in place\n" +
		"CREATE TABLE posts__rebuild (id TEXT);\nDROP TABLE posts;\n" +
		"ALTER TABLE posts ADD COLUMN note TEXT DEFAULT '\n-- DROP COLUMN posts.fake by table rebuild\n';\n" +
		"-- +migrate Down\n-- DROP COLUMN posts.down by table rebuild\n"
	got := strings.Join(destructiveDrops(file), ",")
	if want := "DROP COLUMN posts.slug,DROP COLUMN archive.posts.order,DROP TABLE posts"; got != want {
		t.Fatalf("destructiveDrops = %q, want %q", got, want)
	}
}
