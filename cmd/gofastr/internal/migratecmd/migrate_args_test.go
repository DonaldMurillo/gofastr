package migratecmd

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// `migrate down all` read no number, kept the default of one and rolled
// a migration back. A count that is not a positive integer is refused
// before anything is rolled back.
func TestMigrateDownRefusesBadCount(t *testing.T) {
	dir := covT_migrationsDir(t)
	path := filepath.Join(dir, "x.db")
	dbURL := "--db-url=" + path
	clitest.CovT_capStdout(t, func() { runMigrateUp([]string{dbURL}) })
	for _, bad := range []string{"all", "3x", "0", "-1", "99999999999999999999"} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runMigrateDown([]string{bad, dbURL}) })
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
