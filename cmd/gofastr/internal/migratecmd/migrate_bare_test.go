package migratecmd

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestBareMigrateDefaultsToUpNoPanic pins the documented contract that
// `gofastr migrate` (no subcommand) defaults to "up" instead of slicing
// args[1:] on an empty vector and panicking. The help text advertises
// the subcommand as optional (`main.go` printHelp: "migrate (m) [up|down|…]"),
// so the bare form must reach the up path without a runtime panic.
func TestBareMigrateDefaultsToUpNoPanic(t *testing.T) {
	covT_migrationsDir(t) // chdir + a migrations/ dir so runMigrateUp can resolve

	var panicked interface{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(clitest.CovT_exitSentinel); ok {
					return // a captured osExit is not a panic
				}
				panicked = r
			}
		}()
		clitest.CovT_capStdout(t, func() {
			clitest.CovT_capExit(t, func() { RunMigrate(nil) })
		})
	}()
	if panicked != nil {
		t.Fatalf("bare `gofastr migrate` panicked instead of defaulting to up: %v", panicked)
	}
}

// The subcommand is optional, so `gofastr migrate --db-url=<url>` is the
// default up with a flag. It read the flag as the subcommand and failed
// with `Unknown migrate subcommand: "--db-url=..."`.
func TestMigrateFlagsWithoutSubcommandRunUp(t *testing.T) {
	dir := covT_migrationsDir(t)
	path := filepath.Join(dir, "x.db")
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunMigrate([]string{"--db-url=" + path}) })
	})
	if code != -1 {
		t.Fatalf("migrate --db-url=<url> exited %d, want it to run", code)
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
		t.Fatal("migrate --db-url=<url> did not apply the pending migration")
	}
}
