package migratecmd

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// `migrate down all` read no number, kept the default of one and rolled
// a migration back, and `migrate down 1 3` let the last count win. A
// count that is not one positive integer is refused before anything is
// rolled back.
func TestMigrateDownRefusesBadCount(t *testing.T) {
	dir := covT_migrationsDir(t)
	path := filepath.Join(dir, "x.db")
	dbURL := "--db-url=" + path
	clitest.CovT_capStdout(t, func() { runMigrateUp([]string{dbURL}) })
	for _, bad := range [][]string{{"all"}, {"3x"}, {"0"}, {"-1"}, {"99999999999999999999"}, {"1", "1"}, {"1", dbURL, "2"}} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runMigrateDown(append(bad, dbURL)) })
		})
		if code != 1 {
			t.Errorf("down %q exited %d, want 1", bad, code)
		}
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'posts'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("a refused down rolled back the posts migration")
	}
}

// `migrate up 1` read no count and applied every pending migration, not
// one; `migrate status extra` and `migrate generate add users` dropped
// the extra word in silence. A subcommand refuses an argument it does
// not read before it opens anything.
func TestMigrateRefusesStrayArguments(t *testing.T) {
	dir := covT_migrationsDir(t)
	writeMigration(t, filepath.Join(dir, "migrations"), "002_create_tags.sql", 2, "create_tags")
	path := filepath.Join(dir, "x.db")
	dbURL := "--db-url=" + path
	for _, args := range [][]string{
		{"up", "1", dbURL},
		{"up", dbURL, "1"},
		{"status", "extra", dbURL},
		{"repair", "extra", "--from=a.yml", dbURL},
		{"generate", "add", "users", "--from=a.yml"},
	} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { RunMigrate(args) })
		})
		if code != 2 {
			t.Errorf("migrate %q exited %d, want 2 (usage error)", args, code)
		}
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('posts', 'tags')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a refused `migrate up 1` still applied %d migration(s)", n)
	}
	for sub, args := range map[string][]string{
		"down":     {"2", dbURL},
		"force":    {"1", dbURL},
		"generate": {"add_users", "--from=a.yml"},
	} {
		if err := migratePositionalError(sub, args); err != nil {
			t.Errorf("migrate %s refused its own arguments %q: %v", sub, args, err)
		}
	}
	// The spec above does not exist, so the RunMigrate rows for these two
	// would fail anyway; check the refusal itself.
	for sub, args := range map[string][]string{
		"generate": {"add", "users", "--from=a.yml"},
		"repair":   {"extra", "--from=a.yml"},
	} {
		if err := migratePositionalError(sub, args); err == nil {
			t.Errorf("migrate %s accepted the stray arguments %q", sub, args)
		}
	}
}

// `migrate up --create-db` created the database, then found no
// migrations directory and exited, leaving an empty database behind.
func TestCreateDBChecksArgsFirst(t *testing.T) {
	target, drop := pgtest.UnusedDSN(t)
	defer drop()
	clitest.CovT_chdir(t, t.TempDir()) // no migrations/
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			runMigrateUp([]string{"--create-db", "--db-url=" + target, "--driver=postgres"})
		})
	})
	if code != 1 {
		t.Fatalf("up --create-db with no migrations/ exited %d, want 1", code)
	}
	db, err := sql.Open("postgres", target)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err == nil {
		t.Fatal("up --create-db created the database before refusing")
	}
}

// The flag readers match by prefix and skipped the rest: `migrate down
// --db-ur=<url>` rolled back DATABASE_URL's database, and so did the
// space form --db-url <url>. A flag the subcommand does not read is
// refused before anything opens.
func TestMigrateRefusesUnknownFlag(t *testing.T) {
	dir := covT_migrationsDir(t)
	path := filepath.Join(dir, "x.db")
	clitest.CovT_capStdout(t, func() { runMigrateUp([]string{"--db-url=" + path}) })
	t.Setenv("DATABASE_URL", path)
	for _, args := range [][]string{
		{"down", "--db-ur=" + path},
		{"down", "--db-url", path},
		{"down", "--group"},
		{"up", "--create-db=yes"},
		{"force", "1", "--not-aplied"},
		{"generate", "x", "--db-url=" + path},
	} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { RunMigrate(args) })
		})
		if code != 1 {
			t.Errorf("migrate %q exited %d, want 1", args, code)
		}
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'posts'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("a refused flag still rolled back the posts migration")
	}
	if err := migrateFlagsError("up", []string{"--db-url=x", "--db=sqlite", "--group=a", "--create-db"}); err != nil {
		t.Errorf("up refused its own flags: %v", err)
	}
	if err := migrateFlagsError("down", []string{"2", "--driver=sqlite3", "--group=a"}); err != nil {
		t.Errorf("down refused its own flags: %v", err)
	}
	if err := migrateFlagsError("repair", []string{"--from=a.yml", "--apply", "--db-url=x"}); err != nil {
		t.Errorf("repair refused its own flags: %v", err)
	}
	// generate diffs a snapshot and opens no database.
	if err := migrateFlagsError("generate", []string{"x", "--from=a.yml", "--db-url=x"}); err == nil {
		t.Error("generate accepted --db-url")
	}
}
