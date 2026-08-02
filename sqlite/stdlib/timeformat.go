package stdlib

import (
	"context"
	"database/sql/driver"
	"strings"
)

// modernc's default bind format for time.Time is Go's String() output —
// "2026-07-20 23:59:59.123456789 +0000 UTC". Nothing in GoFastr parses that:
// battery/auth's parseTimeFlex, framework/outbox's layout probe, and every
// hand-rolled timestamp scan expect either RFC3339 or the SQLite/mattn
// layout. A timestamp written in the default format therefore reads back as
// the zero time, which made every session look expired.
//
// "_time_format=sqlite" yields YYYY-MM-DD HH:MM:SS.SSSSSSSSS[+-]HH:MM, which
// is byte-identical to what mattn/go-sqlite3 wrote. Databases created before
// the driver swap stay readable, and no data migration is required.
const timeFormatParam = "_time_format=sqlite"

// withTimeFormat injects the canonical time format into a DSN unless the
// caller already chose one. _time_integer_format also counts as a deliberate
// choice: modernc ignores _time_format entirely when it is set.
func withTimeFormat(dsn string) string {
	query := ""
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		query = dsn[i+1:]
	}
	if strings.Contains(query, "_time_format=") || strings.Contains(query, "_time_integer_format=") {
		return dsn
	}
	sep := "?"
	if strings.IndexByte(dsn, '?') >= 0 {
		sep = "&"
	}
	return dsn + sep + timeFormatParam
}

// timeFormatDriver wraps modernc's driver so every DSN opened through the
// "sqlite3" name gets the canonical time format, including DSNs written by
// host applications that know nothing about this package.
type timeFormatDriver struct{ inner driver.Driver }

func (d timeFormatDriver) Open(dsn string) (driver.Conn, error) {
	return d.inner.Open(withTimeFormat(dsn))
}

// OpenConnector keeps database/sql on the DriverContext path (one DSN parse
// per sql.Open rather than per connection) when the inner driver supports it.
func (d timeFormatDriver) OpenConnector(dsn string) (driver.Connector, error) {
	normalized := withTimeFormat(dsn)
	if dc, ok := d.inner.(driver.DriverContext); ok {
		return dc.OpenConnector(normalized)
	}
	return dsnConnector{dsn: normalized, driver: d.inner}, nil
}

type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.driver }
