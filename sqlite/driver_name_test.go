package sqlite

import (
	"database/sql"
	"slices"
	"testing"

	// Registers modernc.org/sqlite as "sqlite3" — the thing this test asserts
	// the in-house engine is NOT. Imported here rather than relied on
	// transitively: the only other import of it in this package is
	// cgo_bench_test.go, which is behind `//go:build cgo`, so under
	// CGO_ENABLED=0 "sqlite3" went unregistered and the assertion below
	// skipped itself. A guard against a silent failure must not fail
	// silently.
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestEngineNeverClaimsTheDefaultDriverName is the load-bearing guard on this
// package's whole reason for existing.
//
// Two SQLite implementations live in this repo. Applications and the framework
// get modernc.org/sqlite, registered as "sqlite3" by sqlite/stdlib. This
// package is a second, from-scratch engine — pager, B-tree, lexer, parser, file
// format — registered under its own name and used by ~20 test files across
// framework/{access,outbox,crud} and battery/{auth,queue} as an independent
// cross-check of the SQL the framework emits. See docs/sqlite-engine.md.
//
// The cross-check is only worth anything while the two stay distinct. If this
// engine ever claimed "sqlite3", the framework's entire suite would silently
// start validating against it instead of real SQLite: tests green, production
// on a different engine.
//
// What each half actually catches, since they are not equally strong:
//
//   - The registration check is the part this test uniquely adds. Renaming the
//     init()'s driver string fails it — verified by mutation — which matters
//     because ~20 files open "gofastr-sqlite" by name and would otherwise all
//     fail at once with a less obvious message.
//   - The "sqlite3 is not this engine" check is a backstop. The blunt version
//     of that mistake — registering "sqlite3" here directly — already panics
//     inside database/sql as a duplicate registration, so this assertion is
//     for a subtler route that hands the conventional name over without a
//     collision.
func TestEngineNeverClaimsTheDefaultDriverName(t *testing.T) {
	drivers := sql.Drivers()
	if !slices.Contains(drivers, "gofastr-sqlite") {
		t.Fatalf("gofastr-sqlite is not registered; drivers = %v — this package's init() is the only thing that registers it", drivers)
	}

	// Opening "sqlite3" must not reach this engine. The check is behavioural
	// rather than a name comparison: a future indirection could register the
	// engine under the conventional name without the literal appearing here.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver not registered in this test binary: %v", err)
	}
	defer db.Close()
	if _, ok := db.Driver().(*sqliteDriver); ok {
		t.Fatal("the in-house engine is registered as \"sqlite3\" — the framework suite would validate against it instead of modernc.org/sqlite, and every app would still ship modernc. Register it as gofastr-sqlite.")
	}
}
