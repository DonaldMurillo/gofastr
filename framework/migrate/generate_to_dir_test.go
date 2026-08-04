package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// These tests cover GenerateMigrationFile — the registry→versioned-files
// entrypoint a host binary calls from its own main() to emit versioned
// migrations from its compiled entity registry (the Ent/Django shape). They
// assert (a) first-run output matches the blueprint generator's format
// byte-for-byte, (b) an added column produces an incremental up/down,
// (c) an EntityConfig.Renames declaration flows through as a rename,
// (d) generation is deterministic, plus the Group directive and up-to-date
// (no-op) paths.

// blogReg builds a registry of two entities (posts, authors) — the shape a
// host app compiles in. Fields are kept minimal so the asserted SQL is stable.
func blogReg(extraField *schema.Field) testReg {
	postFields := []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "title", Type: schema.String},
	}
	if extraField != nil {
		postFields = append(postFields, *extraField)
	}
	posts := rawEnt("posts", "posts", postFields, nil, "id")
	authors := rawEnt("authors", "authors", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "name", Type: schema.String},
	}, nil, "id")
	return testReg{"posts": posts, "authors": authors}
}

// TestGenerateMigrationFileFirstRun proves the first generation from a
// registry writes migrations/0001_<name>.sql + schema.snapshot.json whose
// bytes are byte-identical to what the blueprint generator's primitives
// (GenerateMigration + RenderMigrationFile + SaveSnapshot) produce — i.e. the
// new entrypoint is format-compatible with the standalone CLI, not a parallel
// scheme.
func TestGenerateMigrationFileFirstRun(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		SnapshotPath:  filepath.Join(dir, "migrations", "schema.snapshot.json"),
		Dialect:       DialectSQLite,
	}
	reg := blogReg(nil)

	path, err := GenerateMigrationFile(Plan{Registry: reg}, "initial", opts)
	if err != nil {
		t.Fatalf("GenerateMigrationFile: %v", err)
	}
	if want := filepath.Join(opts.MigrationsDir, "0001_initial.sql"); path != want {
		t.Fatalf("written path = %q, want %q", path, want)
	}

	// File + snapshot both exist.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	if _, err := os.Stat(opts.SnapshotPath); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	// Parity: independently compute what the blueprint path would emit for the
	// same registry + empty snapshot, and require the file matches exactly.
	up, down, _, gerr := GenerateMigration(reg,
		SchemaSnapshot{Tables: map[string]map[string]string{}}, DialectSQLite)
	if gerr != nil {
		t.Fatalf("GenerateMigration parity: %v", gerr)
	}
	if up == "" {
		t.Fatal("expected non-empty first migration")
	}
	want := RenderMigrationFile(1, "initial", up, down)
	if string(got) != want {
		t.Errorf("generated file does not match blueprint format:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}

	// Snapshot must now describe both tables (no remaining diff).
	snap, _ := LoadSnapshot(opts.SnapshotPath)
	if _, ok := snap.Tables["posts"]; !ok {
		t.Error("snapshot missing posts table")
	}
	if _, ok := snap.Tables["authors"]; !ok {
		t.Error("snapshot missing authors table")
	}
}

// TestGenerateMigrationFileIncremental proves a second generation after a
// column is added produces 0002_<name>.sql with an ADD COLUMN Up and a DROP
// COLUMN Down — the incremental, reversible loop.
func TestGenerateMigrationFileIncremental(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		SnapshotPath:  filepath.Join(dir, "migrations", "schema.snapshot.json"),
		Dialect:       DialectSQLite,
	}
	if _, err := GenerateMigrationFile(Plan{Registry: blogReg(nil)}, "initial", opts); err != nil {
		t.Fatalf("first generate: %v", err)
	}

	// Add a column to posts and generate again.
	body := schema.Field{Name: "body", Type: schema.String}
	path, err := GenerateMigrationFile(Plan{Registry: blogReg(&body)}, "add_body", opts)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if want := filepath.Join(opts.MigrationsDir, "0002_add_body.sql"); path != want {
		t.Fatalf("written path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "ADD COLUMN") || !strings.Contains(string(got), "body") {
		t.Errorf("incremental Up missing ADD COLUMN body:\n%s", got)
	}
	if !strings.Contains(string(got), "DROP COLUMN") || !strings.Contains(string(got), "body") {
		t.Errorf("incremental Down missing DROP COLUMN body:\n%s", got)
	}
}

// TestGenerateMigrationFileRename proves an EntityConfig.Renames declaration on
// a Go-registered entity emits a non-destructive RENAME COLUMN through the new
// entrypoint — not a data-losing drop+add.
func TestGenerateMigrationFileRename(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		SnapshotPath:  filepath.Join(dir, "migrations", "schema.snapshot.json"),
		Dialect:       DialectSQLite,
	}

	// First run: posts has a "name" column.
	first := rawEnt("posts", "posts", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "name", Type: schema.String},
	}, nil, "id")
	if _, err := GenerateMigrationFile(Plan{Registry: testReg{"posts": first}}, "create_posts", opts); err != nil {
		t.Fatalf("first generate: %v", err)
	}

	// Second run: same column is now declared as "label" with a rename hint.
	renamed := rawEnt("posts", "posts", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "label", Type: schema.String},
	}, nil, "id")
	renamed.Config.Renames = map[string]string{"name": "label"}
	path, err := GenerateMigrationFile(Plan{Registry: testReg{"posts": renamed}}, "rename_name", opts)
	if err != nil {
		t.Fatalf("rename generate: %v", err)
	}
	if path == "" {
		t.Fatal("expected a generated migration for the rename, got none")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "RENAME COLUMN") {
		t.Errorf("expected RENAME COLUMN in:\n%s", got)
	}
	if strings.Contains(string(got), "DROP COLUMN") {
		t.Errorf("rename must not emit a data-losing DROP COLUMN:\n%s", got)
	}
}

// TestGenerateMigrationFileDeterministic proves the same Plan + name generates
// byte-identical migration files and snapshots across runs (no map-order or
// timestamp leakage) — required for reviewable diffs and checksum stability.
func TestGenerateMigrationFileDeterministic(t *testing.T) {
	gen := func(t *testing.T) (string, string) {
		dir := t.TempDir()
		opts := MigrationFileOptions{
			MigrationsDir: filepath.Join(dir, "migrations"),
			SnapshotPath:  filepath.Join(dir, "migrations", "schema.snapshot.json"),
			Dialect:       DialectPostgres,
		}
		path, err := GenerateMigrationFile(Plan{Registry: blogReg(nil)}, "initial", opts)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		sql, _ := os.ReadFile(path)
		snap, _ := os.ReadFile(opts.SnapshotPath)
		return string(sql), string(snap)
	}
	sql1, snap1 := gen(t)
	sql2, snap2 := gen(t)
	if sql1 != sql2 {
		t.Errorf("migration file not deterministic:\n--- a ---\n%s\n--- b ---\n%s", sql1, sql2)
	}
	if snap1 != snap2 {
		t.Errorf("snapshot not deterministic:\n--- a ---\n%s\n--- b ---\n%s", snap1, snap2)
	}
}

// TestGenerateMigrationFileUpToDate proves a second call with an unchanged
// registry returns an empty path (up to date) and writes no migration file.
func TestGenerateMigrationFileUpToDate(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		SnapshotPath:  filepath.Join(dir, "migrations", "schema.snapshot.json"),
		Dialect:       DialectSQLite,
	}
	reg := testReg{
		"posts": rawEnt("posts", "posts", []schema.Field{
			{Name: "id", Type: schema.String},
		}, nil, "id"),
	}
	if _, err := GenerateMigrationFile(Plan{Registry: reg}, "initial", opts); err != nil {
		t.Fatalf("first generate: %v", err)
	}

	path, err := GenerateMigrationFile(Plan{Registry: reg}, "noop", opts)
	if err != nil {
		t.Fatalf("up-to-date generate: %v", err)
	}
	if path != "" {
		t.Fatalf("up-to-date should return empty path, got %q", path)
	}
	noopPath := filepath.Join(opts.MigrationsDir, "0002_noop.sql")
	if _, err := os.Stat(noopPath); !os.IsNotExist(err) {
		t.Fatal("a no-op generate must not write a migration file")
	}
}

// TestGenerateMigrationFileGroup proves the Group option stamps the
// -- +migrate Group directive before the Up section, matching the CLI's
// --group flag behavior.
func TestGenerateMigrationFileGroup(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		SnapshotPath:  filepath.Join(dir, "migrations", "schema.snapshot.json"),
		Dialect:       DialectSQLite,
		Group:         "knowledge",
	}
	reg := testReg{
		"posts": rawEnt("posts", "posts", []schema.Field{
			{Name: "id", Type: schema.String},
		}, nil, "id"),
	}
	path, err := GenerateMigrationFile(Plan{Registry: reg}, "initial", opts)
	if err != nil {
		t.Fatalf("GenerateMigrationFile: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "-- +migrate Group knowledge\n") {
		t.Errorf("missing Group directive:\n%s", s)
	}
	// Group must precede Up.
	if gi := strings.Index(s, "-- +migrate Group"); gi < 0 ||
		strings.Index(s, "-- +migrate Up") < gi {
		t.Errorf("Group directive not before Up:\n%s", s)
	}
}

// Version-numbering and name-sanitization used to live in
// cmd/gofastr/migrate_generate.go, copied alongside the CLI's own inline
// generation sequence. The CLI now calls GenerateMigrationFile, so these tests
// moved here with the one surviving implementation.
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

// TestGenerateMigrationFileRejectsBadGroup pins that an invalid group name is
// refused before anything is written: the directive would otherwise be stamped
// into a committed migration the runner then refuses to apply.
func TestGenerateMigrationFileRejectsBadGroup(t *testing.T) {
	dir := t.TempDir()
	opts := MigrationFileOptions{
		MigrationsDir: filepath.Join(dir, "migrations"),
		Dialect:       DialectSQLite,
		Group:         "not a valid group!",
	}
	reg := testReg{"posts": rawEnt("posts", "posts", []schema.Field{
		{Name: "id", Type: schema.String},
	}, nil, "id")}

	if _, err := GenerateMigrationFile(Plan{Registry: reg}, "initial", opts); err == nil {
		t.Fatal("expected an invalid group name to be rejected")
	}
	if entries, err := os.ReadDir(opts.MigrationsDir); err == nil && len(entries) > 0 {
		t.Errorf("rejected generation still wrote %d file(s)", len(entries))
	}
}
