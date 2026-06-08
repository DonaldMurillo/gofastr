package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextMigrationVersion(t *testing.T) {
	dir := t.TempDir()
	if v := nextMigrationVersion(dir); v != 1 {
		t.Fatalf("empty dir version = %d, want 1", v)
	}
	for _, name := range []string{"0001_a.sql", "0002_b.sql", "notes.txt", "0007_c.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if v := nextMigrationVersion(dir); v != 8 {
		t.Fatalf("version after 0007 = %d, want 8", v)
	}
}

func TestSanitizeMigrationName(t *testing.T) {
	cases := map[string]string{
		"Add Views Column": "add_views_column",
		"  add-email!!  ":  "add_email",
		"____":             "migration",
		"CamelCase":        "camelcase",
	}
	for in, want := range cases {
		if got := sanitizeMigrationName(in); got != want {
			t.Errorf("sanitizeMigrationName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMigrateGenerate_EndToEnd exercises the full declarative workflow through
// the installed binary: declare an entity, generate a create migration, change
// the entity, generate an incremental migration, then `migrate up` and confirm
// the schema landed. Skips if gofastr isn't on PATH.
func TestMigrateGenerate_EndToEnd(t *testing.T) {
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		t.Skip("gofastr binary not on PATH (run `go install ./cmd/gofastr`)")
	}
	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")

	// writeBlueprint writes a gofastr.yml with a posts entity carrying the
	// given fields YAML (indented under the entity's `fields:`).
	writeBlueprint := func(fields string) {
		bp := "app:\n  name: testapp\nentities:\n  - name: posts\n    table: posts\n    fields:\n" + fields
		if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(bp), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gen := func(name string) string {
		cmd := exec.Command(bin, "migrate", "generate", name, "--from=gofastr.yml", "--migrations=migrations")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("generate %s: %v\n%s", name, err, out)
		}
		return string(out)
	}

	// 1) Initial schema → create migration.
	writeBlueprint("      - name: title\n        type: string\n        required: true\n")
	gen("create_posts")
	first := filepath.Join(migDir, "0001_create_posts.sql")
	body, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read first migration: %v", err)
	}
	if !strings.Contains(string(body), "CREATE TABLE IF NOT EXISTS posts") {
		t.Fatalf("first migration missing CREATE TABLE:\n%s", body)
	}
	if !strings.Contains(string(body), "-- +migrate Down") || !strings.Contains(string(body), "DROP TABLE") {
		t.Fatalf("first migration missing reversible Down:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(migDir, "schema.snapshot.json")); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	// 2) Add a column → incremental migration.
	writeBlueprint("      - name: title\n        type: string\n        required: true\n      - name: views\n        type: int\n")
	gen("add_views")
	second := filepath.Join(migDir, "0002_add_views.sql")
	body2, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("read second migration: %v", err)
	}
	if strings.Contains(string(body2), "CREATE TABLE") {
		t.Fatalf("incremental migration should not recreate the table:\n%s", body2)
	}
	if !strings.Contains(string(body2), "ADD COLUMN views") {
		t.Fatalf("second migration missing ADD COLUMN views:\n%s", body2)
	}

	// 3) No change → nothing generated.
	out := gen("noop")
	if !strings.Contains(out, "up to date") {
		t.Fatalf("expected 'up to date', got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(migDir, "0003_noop.sql")); !os.IsNotExist(err) {
		t.Fatal("a no-op generate should not write a migration file")
	}

	// 4) Apply the generated migrations against a SQLite DB.
	dbPath := filepath.Join(dir, "app.db")
	apply := exec.Command(bin, "migrate", "up", "--db-url="+dbPath, "--driver=sqlite3")
	apply.Dir = dir
	if out, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("migrate up: %v\n%s", err, out)
	}
}
