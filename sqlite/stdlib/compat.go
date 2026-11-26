package stdlib

import (
	"context"
	"database/sql/driver"
	"net/url"
	"strings"
)

// GoFastr and the apps built on it were written against mattn/go-sqlite3.
// modernc.org/sqlite differs from it in two defaults that are silent when they
// change and expensive when they bite, so every DSN opened through the
// "sqlite3" name gets them restored. A third default below is not a
// compatibility restoration at all — see foreignKeysParam. A caller who sets
// any of them explicitly wins.

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

// busy_timeout only covers waiting for a lock. A DEFERRED transaction (SQLite's
// default BEGIN) that reads first holds a snapshot; when another connection
// commits before its first write, that write cannot upgrade and fails with
// SQLITE_BUSY_SNAPSHOT at once, with no wait. The CRUD delete reads the row
// for the audit snapshot before it deletes, so a background writer (a job
// queue) turned a plain DELETE into a 500. BEGIN IMMEDIATE takes the write
// lock up front, where busy_timeout does apply. modernc keeps read-only
// transactions (sql.TxOptions{ReadOnly: true}) deferred.
const txLockParam = "_txlock=immediate"

// SQLite ignores FOREIGN KEY constraints unless foreign_keys is enabled on
// the connection, and it is off by default — in modernc, in mattn, and in
// the SQLite C library itself. This is therefore NOT a mattn-parity fix like
// the two above: both drivers behave the same, and both behave badly for us.
//
// AutoMigrate emits FOREIGN KEY clauses for every declared relation
// (framework/migrate: foreignKeyClauses), so an app reads its schema as
// though referential integrity were enforced. With the pragma off none of it
// is: a create that sets author_id to an id naming no row inserts cleanly and
// the dangling reference is permanent. PostgreSQL enforces the same
// constraints without being asked, so the same application code was already
// getting two different guarantees depending on the database behind it.
//
// Turning it on is a behavior change for any app that has been writing
// dangling references: those writes now fail. That is the point — but it is
// why it ships as a documented breaking change and why "_pragma=foreign_keys(0)"
// in the DSN still wins for an app that needs the old behavior while it
// cleans up.
const foreignKeysParam = "_pragma=foreign_keys(1)"

// hasPragmaAssignment recognizes the assignment forms accepted by SQLite.
func hasPragmaAssignment(values url.Values, name string) bool {
	for _, value := range values["_pragma"] {
		value = strings.ToLower(strings.TrimSpace(value))
		for _, spelling := range []string{name, `"` + name + `"`, "'" + name + "'", "`" + name + "`", "[" + name + "]"} {
			rest, ok := strings.CutPrefix(value, spelling)
			if ok {
				rest = strings.TrimSpace(rest)
				if strings.HasPrefix(rest, "(") || strings.HasPrefix(rest, "=") {
					return true
				}
			}
		}
	}
	return false
}

func hasForeignKeysChoice(values url.Values) bool {
	return hasPragmaAssignment(values, "foreign_keys") ||
		values.Get("_foreign_keys") != "" || values.Get("_fk") != ""
}

// withCompatDefaults adds the defaults above to a DSN, skipping either one the
// caller already chose. _time_integer_format counts as choosing a time format:
// modernc ignores _time_format entirely when it is set.
func withCompatDefaults(dsn string) string {
	path, query, _ := strings.Cut(dsn, "?")
	values, queryErr := url.ParseQuery(query)
	out := dsn
	add := func(param string) {
		sep := "?"
		if strings.IndexByte(out, '?') >= 0 {
			sep = "&"
		}
		out += sep + param
	}
	// Modernc uses the first value for these settings. Insert the default
	// ahead of an explicitly blank value so it can take effect.
	addFirstValue := func(key, param string) {
		if values.Has(key) {
			_, existing, _ := strings.Cut(out, "?")
			out = path + "?" + param + "&" + existing
			return
		}
		add(param)
	}
	if values.Get("_time_format") == "" && values.Get("_time_integer_format") == "" {
		addFirstValue("_time_format", timeFormatParam)
	}
	if values.Get("_busy_timeout") == "" && values.Get("_timeout") == "" && !hasPragmaAssignment(values, "busy_timeout") {
		add(busyTimeoutParam)
	}
	// Keep the conservative foreign-key fallback for malformed queries.
	if !hasForeignKeysChoice(values) && (queryErr == nil || !strings.Contains(strings.ToLower(query), "foreign_keys")) {
		add(foreignKeysParam)
	}
	if values.Get("_txlock") == "" {
		addFirstValue("_txlock", txLockParam)
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
		c, err := dc.OpenConnector(normalized)
		if err != nil {
			return nil, err
		}
		return compatConnector{Connector: c, driver: d}, nil
	}
	return compatConnector{Connector: dsnConnector{dsn: normalized, driver: d.inner}, driver: d}, nil
}

// compatConnector answers Driver with the compat driver, not the one it
// wraps. sql.DB.Driver returns its connector's driver, and a caller that
// opens a second handle from it (core/tracing.OpenDB does, to wrap the
// connections) would otherwise get modernc's bare driver: no time
// format, no foreign keys, none of the defaults above. Timestamps then
// went to disk in Go's String format, which the next start could not
// read back.
type compatConnector struct {
	driver.Connector
	driver compatDriver
}

func (c compatConnector) Driver() driver.Driver { return c.driver }

type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.driver }
