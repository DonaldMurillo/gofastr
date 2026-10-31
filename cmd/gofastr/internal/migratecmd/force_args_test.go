package migratecmd

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `migrate force 1x` read 1 and marked migration 1 applied without running
// its SQL; with two versions the last parseable one won (2026-10-27 G1).
// force takes exactly one version, parsed whole, and refuses before it
// touches the tracking table.
func TestMigrateForceRefusesBadVersion(t *testing.T) {
	dir := covT_migrationsDir(t)
	path := filepath.Join(dir, "x.db")
	dbURL := "--db-url=" + path
	for _, bad := range [][]string{{"1x"}, {"abc"}, {"-1"}, {"1.0"}, {"99999999999999999999"}, {"2", "1"}} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { runMigrateForce(append(bad, dbURL)) })
		})
		if code != 1 {
			t.Errorf("force %s exited %d, want 1", strings.Join(bad, " "), code)
		}
	}

	clitest.CovT_capStdout(t, func() { runMigrateUp([]string{dbURL}) })
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
		t.Fatal("a refused force marked migration 1 applied, so up skipped it")
	}
}
