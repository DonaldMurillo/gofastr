// Package stdlib registers modernc.org/sqlite — a pure-Go SQLite — under the
// conventional "sqlite3" database/sql driver name used by generated apps.
//
// Keeping this adapter in the GoFastr module means applications that import
// GoFastr do not need a transitive replace directive for mattn/go-sqlite3.
// They can therefore build on Windows with CGO_ENABLED=0.
//
// The driver is wrapped to restore the two mattn/go-sqlite3 defaults the
// framework was written against: the time bind layout and busy_timeout. See
// compat.go — both are silent when wrong, and one is an auth outage.
package stdlib

import (
	"database/sql"
	"slices"

	modernsqlite "modernc.org/sqlite"
)

func init() {
	if slices.Contains(sql.Drivers(), "sqlite3") {
		return
	}
	// modernc keeps user-defined functions, collations, connection hooks,
	// and virtual-table registrations on its package-level registered Driver.
	// Reusing sql.Open("sqlite").Driver() aliases that initialized singleton;
	// registering a fresh zero-value Driver would silently drop those hooks.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic("sqlite/stdlib: modernc sqlite driver is unavailable: " + err.Error())
	}
	inner := db.Driver()
	_ = db.Close()
	sql.Register("sqlite3", compatDriver{inner: inner})
}

// SQLiteDriver preserves the small public wrapper surface used by fault
// injection tests and downstream code that constructs the traditional driver.
//
// Note this is modernc's raw driver: a caller registering it directly gets
// modernc's default time bind format, not the canonical layout this package
// installs on the "sqlite3" name.
type SQLiteDriver = modernsqlite.Driver
