package main

import "testing"

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
				if _, ok := r.(covT_exitSentinel); ok {
					return // a captured osExit is not a panic
				}
				panicked = r
			}
		}()
		covT_capStdout(t, func() {
			covT_capExit(t, func() { runMigrate(nil) })
		})
	}()
	if panicked != nil {
		t.Fatalf("bare `gofastr migrate` panicked instead of defaulting to up: %v", panicked)
	}
}
