package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func covT_migrationsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	md := filepath.Join(dir, "migrations")
	if err := os.Mkdir(md, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMigration(t, md, "001_create_posts.sql", 1, "create_posts")
	covT_chdir(t, dir)
	return dir
}

func TestRunMigrateDispatch(t *testing.T) {
	dir := covT_migrationsDir(t)
	dbURL := "--db-url=" + filepath.Join(dir, "d.db")
	covT_capStdout(t, func() { runMigrate([]string{"up", dbURL}) })
	covT_capStdout(t, func() { runMigrate([]string{"status", dbURL}) })
	covT_capStdout(t, func() { runMigrate([]string{"down", "1", dbURL}) })
	covT_capStdout(t, func() { runMigrate([]string{"force", "1", dbURL}) })
}

func TestRunMigrateDefaultsToUp(t *testing.T) {
	dir := covT_migrationsDir(t)
	covT_capStdout(t, func() { runMigrate([]string{"up", "--db-url=" + filepath.Join(dir, "d.db")}) })
}

func TestRunMigrateUnknownSubcmdExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrate([]string{"bogus"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateUpMissingDirExits(t *testing.T) {
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateUp([]string{"--db-url=x.db"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateForceNoVersionExits(t *testing.T) {
	covT_chdir(t, t.TempDir())
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateForce([]string{"--not-applied"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunMigrateUpCreateDB(t *testing.T) {
	dir := covT_migrationsDir(t)
	dbURL := "--db-url=" + filepath.Join(dir, "new.db")
	covT_capStdout(t, func() { runMigrateUp([]string{"--create-db", dbURL}) })
}

func TestMigrateHelperFlags(t *testing.T) {
	if !hasFlag([]string{"--create-db"}, "--create-db") {
		t.Fatal("hasFlag true")
	}
	if hasFlag([]string{"x"}, "--create-db") {
		t.Fatal("hasFlag false")
	}
	if getMigrateDriver([]string{"--driver=postgres"}) != "postgres" {
		t.Fatal("driver override")
	}
	if getMigrateDriver(nil) != "sqlite3" {
		t.Fatal("driver default")
	}
	if getMigrateDBURL([]string{"--db-url=abc"}) != "abc" {
		t.Fatal("dburl override")
	}
}

func TestGetMigrateDBURLFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DATABASE_URL=fromenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getMigrateDBURL(nil); got != "fromenv" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureDriverRegistered(t *testing.T) {
	if err := ensureDriverRegistered("sqlite3"); err != nil {
		t.Fatalf("sqlite3 should be registered: %v", err)
	}
	if err := ensureDriverRegistered("nonexistent-driver"); err == nil {
		t.Fatal("expected error for unregistered driver")
	}
}

func TestRunMigrateDiffInProcess(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	dbPath := filepath.Join(dir, "live.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	bp := filepath.Join(dir, "gofastr.yml")
	blueprint := `app:
  name: testapp
entities:
  - name: posts
    table: posts
    fields:
      - name: title
        type: string
        required: true
      - name: views
        type: int
`
	if err := os.WriteFile(bp, []byte(blueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { runMigrateDiff([]string{"--db-url=file:" + dbPath, "--from=" + bp}) })
	if !strings.Contains(out, "views") {
		t.Fatalf("diff output: %s", out)
	}
	// --apply path
	covT_capStdout(t, func() { runMigrateDiff([]string{"--db-url=file:" + dbPath, "--from=" + bp, "--apply"}) })
	// up-to-date path now
	out2 := covT_capStdout(t, func() { runMigrateDiff([]string{"--db-url=file:" + dbPath, "--from=" + bp}) })
	if !strings.Contains(out2, "up to date") {
		t.Fatalf("expected up to date, got: %s", out2)
	}
}

func TestRunMigrateDiffNoEntitiesExits(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(bp, []byte("app:\n  name: testapp\nentities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateDiff([]string{"--db-url=file:" + filepath.Join(dir, "x.db"), "--from=" + bp}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestParseDiffOptions(t *testing.T) {
	opts := parseDiffOptions([]string{"--db-url=u", "--driver=postgres", "--from=bp.yml", "--apply", "--allow-destructive"})
	if opts.dbURL != "u" || opts.driver != "postgres" || opts.from != "bp.yml" || !opts.apply || !opts.allowDestructive {
		t.Fatalf("opts = %#v", opts)
	}
	t.Setenv("DATABASE_URL", "envurl")
	if parseDiffOptions(nil).dbURL != "envurl" {
		t.Fatal("env fallback")
	}
}

func TestOpenDiffDBErrors(t *testing.T) {
	if _, err := openDiffDB("", "sqlite3"); err == nil {
		t.Fatal("empty url should error")
	}
}

func TestRunMigrateGenerateInProcess(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "gofastr.yml")
	blueprint := `app:
  name: testapp
entities:
  - name: posts
    table: posts
    fields:
      - name: title
        type: string
`
	if err := os.WriteFile(bp, []byte(blueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { runMigrateGenerate([]string{"create_posts", "--from=" + bp}) })
	matches, _ := filepath.Glob(filepath.Join(dir, "migrations", "0001_*.sql"))
	if len(matches) == 0 {
		t.Fatal("no migration generated")
	}
	// Second run with no schema change → up to date.
	out := covT_capStdout(t, func() { runMigrateGenerate([]string{"noop", "--from=" + bp}) })
	if !strings.Contains(out, "up to date") {
		t.Fatalf("expected up to date: %s", out)
	}
}

func TestRunMigrateGenerateNoNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runMigrateGenerate(nil) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestParseMigrateGenOptions(t *testing.T) {
	opts := parseMigrateGenOptions([]string{"myname", "--from=bp.yml", "--migrations=m", "--snapshot=s.json", "--driver=postgres", "--unknown"})
	if opts.name != "myname" || opts.from != "bp.yml" || opts.migrationsDir != "m" || opts.snapshotPath != "s.json" || opts.driver != "postgres" {
		t.Fatalf("opts = %#v", opts)
	}
	def := parseMigrateGenOptions([]string{"n"})
	if def.snapshotPath != filepath.Join("migrations", "schema.snapshot.json") {
		t.Fatalf("default snapshot = %q", def.snapshotPath)
	}
}
