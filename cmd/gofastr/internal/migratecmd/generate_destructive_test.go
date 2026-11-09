package migratecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `gofastr migrate generate` used to write DROP TABLE / DROP COLUMN into
// the migration file with no warning. A table or field rename is
// indistinguishable from a removal, so an undeclared rename became
// DROP + CREATE and destroyed every row (#656).

func genSpec(t *testing.T, entities string) string {
	t.Helper()
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	spec := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, spec, "app:\n  name: testapp\nentities:\n"+entities)
	return spec
}

func runGenerate(t *testing.T, args ...string) string {
	t.Helper()
	return clitest.CovT_capStdout(t, func() {
		if code := clitest.CovT_capExit(t, func() { RunMigrate(args) }); code != -1 {
			t.Fatalf("generate exited %d: %s", code, strings.Join(args, " "))
		}
	})
}

func TestGenerateWarnsOnDropTableAndDropColumn(t *testing.T) {
	spec := genSpec(t, "  - name: posts\n    fields:\n      - name: title\n        type: string\n      - name: count\n        type: int\n  - name: tags\n    fields:\n      - name: label\n        type: string\n")
	runGenerate(t, "generate", "init", "--from="+spec)

	// Remove the tags entity and the count field: the next migration
	// drops a table and a column.
	clitest.WriteTestFile(t, spec, "app:\n  name: testapp\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n")
	out := runGenerate(t, "generate", "trim", "--from="+spec)

	if !strings.Contains(out, "Warning") && !strings.Contains(out, "⚠") {
		t.Fatalf("no warning for destructive output:\n%s", out)
	}
	for _, want := range []string{"tags", "count"} {
		if !strings.Contains(out, want) {
			t.Fatalf("warning does not name %s:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "DROP TABLE") || !strings.Contains(out, "DROP COLUMN") {
		t.Fatalf("warning does not say what is dropped:\n%s", out)
	}
}

func TestGenerateQuietWhenNothingDropped(t *testing.T) {
	spec := genSpec(t, "  - name: posts\n    fields:\n      - name: title\n        type: string\n")
	runGenerate(t, "generate", "init", "--from="+spec)

	clitest.WriteTestFile(t, spec, "app:\n  name: testapp\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n      - name: count\n        type: int\n        default: 0\n")
	out := runGenerate(t, "generate", "grow", "--from="+spec)
	if strings.Contains(out, "DROP TABLE") || strings.Contains(out, "DROP COLUMN") {
		t.Fatalf("additive change warned about drops:\n%s", out)
	}

	// The Down section's own DROP (rolling back an added column) is not
	// a destructive Up either: only what `migrate up` runs is warned on.
	migs, err := filepath.Glob(filepath.Join(".", "migrations", "*.sql"))
	if err != nil || len(migs) == 0 {
		t.Fatalf("no migrations written: %v", err)
	}
	body, err := os.ReadFile(migs[len(migs)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "-- +migrate Down") {
		t.Fatalf("additive migration lost its Down:\n%s", body)
	}
}

func TestDestructiveDropsIgnoresLiterals(t *testing.T) {
	file := "-- +migrate Version 1\n-- +migrate Name m\n-- +migrate Up\n" +
		"ALTER TABLE posts ADD COLUMN note TEXT DEFAULT 'DROP TABLE victims;';\n" +
		"-- +migrate Down\nALTER TABLE posts DROP COLUMN note;\n"
	if drops := destructiveDrops(file); len(drops) != 0 {
		t.Fatalf("a string literal counted as a drop: %v", drops)
	}
	destructive := "-- +migrate Up\nDROP TABLE IF EXISTS \"tags\";\nALTER TABLE \"posts\" DROP COLUMN \"count\";\n" +
		"DROP TABLE archive.posts;\nALTER TABLE archive.posts DROP COLUMN \"order\";\n"
	got := destructiveDrops(destructive)
	want := []string{"DROP TABLE tags", "DROP COLUMN posts.count", "DROP TABLE archive.posts", "DROP COLUMN archive.posts.order"}
	if len(got) != len(want) {
		t.Fatalf("destructiveDrops = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("destructiveDrops[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
	// A Down section's drops are rollbacks, not losses.
	if drops := destructiveDrops("-- +migrate Up\nSELECT 1;\n-- +migrate Down\nDROP TABLE tags;\n"); len(drops) != 0 {
		t.Fatalf("a Down drop counted as destructive: %v", drops)
	}
}

func TestDestructiveDropsAfterComments(t *testing.T) {
	for _, prefix := range []string{"-- review this change\n", "/* review */ ", "/* outer /* inner */ outer */\n"} {
		file := "-- +migrate Up\n" + prefix + "DROP TABLE tags;\nALTER /* change */ TABLE posts DROP /* loss */ COLUMN title;\n"
		got := destructiveDrops(file)
		if strings.Join(got, ",") != "DROP TABLE tags,DROP COLUMN posts.title" {
			t.Errorf("prefix %q: drops = %v", prefix, got)
		}
	}
}

func TestDestructiveDropsIgnoresRoutineBodies(t *testing.T) {
	for _, quote := range []string{"$$", "$body$"} {
		file := "-- +migrate Up\nCREATE FUNCTION f() RETURNS void AS " + quote + "\nBEGIN\nPERFORM 1;\nDROP TABLE secret;\nEND;\n" + quote + " LANGUAGE plpgsql;\nDROP TABLE tags;\n"
		if got := destructiveDrops(file); strings.Join(got, ",") != "DROP TABLE tags" {
			t.Errorf("quote %s: drops = %v", quote, got)
		}
	}
}
