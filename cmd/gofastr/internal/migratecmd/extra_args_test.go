package migratecmd

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `migrate up 3` applied every pending migration, `migrate status x` and
// `migrate generate a b` dropped the extras in silence. A positional
// argument a subcommand does not take is a usage error (exit 2), refused
// before the database is opened.
func TestMigrateRefusesExtraPositionalArgs(t *testing.T) {
	dir := covT_migrationsDir(t)
	path := filepath.Join(dir, "x.db")
	dbURL := "--db-url=" + path
	for _, args := range [][]string{
		{"up", "3", dbURL},
		{"up", dbURL, "3"},
		{"status", "x", dbURL},
		{"generate", "a", "b", "--from=gofastr.yml"},
		{"repair", "x", "--from=gofastr.yml", dbURL},
	} {
		var code int
		out := clitest.CovT_capStdout(t, func() {
			code = clitest.CovT_capExit(t, func() { RunMigrate(args) })
		})
		if code != 2 {
			t.Errorf("migrate %q exited %d, want 2\n%s", args, code, out)
		}
		if !strings.Contains(out, "Usage") {
			t.Errorf("migrate %q printed no usage:\n%s", args, out)
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
	if n != 0 {
		t.Fatal("a refused `migrate up 3` applied the posts migration")
	}
}

// A Postgres DATABASE_URL with no --db fell through to the sqlite3
// default, which opened the URL as a file name and failed with a SQLite
// error. The URL's scheme picks the driver when no flag names one.
func TestMigrateDriverFollowsPostgresURL(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	for _, url := range []string{"postgres://u@h/db", "postgresql://u@h/db", "POSTGRES://u@h/db"} {
		t.Setenv("DATABASE_URL", url)
		if got := getMigrateDriver(nil); got != "postgres" {
			t.Errorf("DATABASE_URL=%s: driver %q, want postgres", url, got)
		}
		if got := getMigrateDriver([]string{"--db-url=" + url}); got != "postgres" {
			t.Errorf("--db-url=%s: driver %q, want postgres", url, got)
		}
	}
	t.Setenv("DATABASE_URL", "postgres://u@h/db")
	if got := getMigrateDriver([]string{"--db=sqlite"}); got != "sqlite3" {
		t.Errorf("an explicit --db=sqlite must win, got %q", got)
	}
	t.Setenv("DATABASE_URL", "./app.db")
	if got := getMigrateDriver(nil); got != "sqlite3" {
		t.Errorf("a file path keeps the sqlite3 default, got %q", got)
	}
}
