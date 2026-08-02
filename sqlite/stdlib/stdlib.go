// Package stdlib registers the repository's pure-Go SQLite implementation
// under the conventional database/sql driver name used by generated apps.
//
// Keeping this adapter in the GoFastr module means applications that import
// GoFastr do not need a transitive replace directive for mattn/go-sqlite3.
// They can therefore build on Windows with CGO_ENABLED=0.
package stdlib

import (
	"database/sql"

	modernsqlite "modernc.org/sqlite"
)

func init() {
	for _, name := range sql.Drivers() {
		if name == "sqlite3" {
			return
		}
	}
	// modernc keeps user-defined functions, collations, connection hooks,
	// and virtual-table registrations on its package-level registered Driver.
	// Reusing sql.Open("sqlite").Driver() aliases that initialized singleton;
	// registering a fresh zero-value Driver would silently drop those hooks.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic("sqlite/stdlib: modernc sqlite driver is unavailable: " + err.Error())
	}
	driver := db.Driver()
	_ = db.Close()
	sql.Register("sqlite3", driver)
}

// SQLiteDriver preserves the small public wrapper surface used by fault
// injection tests and downstream code that constructs the traditional driver.
type SQLiteDriver = modernsqlite.Driver
