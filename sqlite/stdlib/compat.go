package stdlib

import (
	"context"
	"database/sql/driver"
	"strings"
)

// GoFastr and the apps built on it were written against mattn/go-sqlite3.
// modernc.org/sqlite differs from it in two defaults that are silent when they
// change and expensive when they bite, so every DSN opened through the
// "sqlite3" name gets them restored. A caller who sets either explicitly wins.

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

// mattn defaulted busy_timeout to 5000ms; modernc defaults to 0, so a second
// writer gets SQLITE_BUSY immediately instead of waiting for the first to
// finish. SQLite allows one writer at a time, so any app serving concurrent
// requests relies on that wait.
const busyTimeoutParam = "_pragma=busy_timeout(5000)"

// withCompatDefaults adds the defaults above to a DSN, skipping either one the
// caller already chose. _time_integer_format counts as choosing a time format:
// modernc ignores _time_format entirely when it is set.
func withCompatDefaults(dsn string) string {
	query := ""
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		query = dsn[i+1:]
	}
	out := dsn
	add := func(param string) {
		sep := "?"
		if strings.IndexByte(out, '?') >= 0 {
			sep = "&"
		}
		out += sep + param
	}
	if !strings.Contains(query, "_time_format=") && !strings.Contains(query, "_time_integer_format=") {
		add(timeFormatParam)
	}
	if !strings.Contains(query, "busy_timeout") {
		add(busyTimeoutParam)
	}
	return out
}

// compatDriver wraps modernc's driver so the defaults reach every DSN,
// including DSNs written by host applications that know nothing about this
// package.
type compatDriver struct{ inner driver.Driver }

func (d compatDriver) Open(dsn string) (driver.Conn, error) {
	return d.inner.Open(withCompatDefaults(dsn))
}

// OpenConnector keeps database/sql on the DriverContext path (one DSN parse
// per sql.Open rather than per connection) when the inner driver supports it.
func (d compatDriver) OpenConnector(dsn string) (driver.Connector, error) {
	normalized := withCompatDefaults(dsn)
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
