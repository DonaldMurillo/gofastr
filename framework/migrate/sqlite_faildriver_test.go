package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
)

// failLegacyPragmaDriver is the sqlite3 driver with one statement wired to
// fail: `PRAGMA legacy_alter_table=ON`. RepairStaleOwnerForeignKeys turns
// foreign keys OFF before that statement, so the early return it takes on the
// failure is the one window where a pooled connection can go back unenforced.
// Nothing in a real SQLite makes that pragma fail on demand, and a test that
// cannot enter the window cannot tell a restored pragma from a lucky one.
const failLegacyPragmaDriver = "sqlite3-fail-legacy-pragma"

var errLegacyPragmaInjected = errors.New("injected: legacy_alter_table refused")

func init() {
	// sql.Open never dials, so this only reaches the registered driver value.
	probe, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		panic(err)
	}
	base := probe.Driver()
	_ = probe.Close()
	sql.Register(failLegacyPragmaDriver, failPragmaDriver{base})
}

type failPragmaDriver struct{ driver.Driver }

func (d failPragmaDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return failPragmaConn{c}, nil
}

// failPragmaConn embeds driver.Conn, so Prepare/Close/Begin pass through and
// everything database/sql cannot route through ExecContext falls back to the
// prepared-statement path on the real connection.
type failPragmaConn struct{ driver.Conn }

func (c failPragmaConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	// Matched on the whole statement, not a substring: `=ON` sits one byte
	// away from the `=OFF` the restore path runs, and failing that one too
	// would test the wrong window.
	if strings.EqualFold(strings.TrimSpace(q), "PRAGMA legacy_alter_table=ON") {
		return nil, errLegacyPragmaInjected
	}
	ec, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return ec.ExecContext(ctx, q, args)
}
