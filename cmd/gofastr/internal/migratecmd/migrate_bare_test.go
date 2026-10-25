package migratecmd

import (
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
