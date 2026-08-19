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

// hasForeignKeysChoice reports whether the DSN already expresses a
// foreign_keys setting in a form the driver acts on.
//
// A plain substring test on "foreign_keys" was wrong in the one direction that
// matters. modernc acts on `_pragma=foreign_keys(N)` and on mattn's
// `_foreign_keys=N`, and ignores anything else — so a DSN saying
// `?foreign_keys=1` (the bare SQLite-URI spelling, an easy thing to write from
// memory) suppressed the default AND set nothing itself. The DSN read as
// opted-in while enforcement was off, which is worse than either outcome
// alone. An unrecognized spelling now gets the default appended; the caller's
// parameter is still there, still ignored by the driver, and no longer
// silently decisive.
func hasForeignKeysChoice(query string) bool {
	// Parse rather than substring-match. The driver runs the query string
	// through url.ParseQuery, so values arrive DECODED
	// (`foreign_keys%280%29` is `foreign_keys(0)`), and it applies _pragma
	// settings in sorted order — which means `foreign_keys(1)` sorts after
	// `foreign_keys(0)` and executes last. A missed spelling therefore does
	// not merely fail to opt out: the appended default silently OVERRIDES the
	// caller's explicit choice. Pragma names are also case-insensitive to
	// SQLite, so `FOREIGN_KEYS(0)` is a real opt-out too.
	vals, err := url.ParseQuery(query)
	if err != nil {
		// Unparseable query: fall back to the conservative substring test
		// rather than appending a default that might override something.
		return strings.Contains(strings.ToLower(query), "foreign_keys")
	}
	for _, v := range vals["_pragma"] {
		// Match the pragma NAME, not one spelling of its argument. SQLite's
		// PRAGMA grammar accepts `name(v)`, `name = v`, and `name=v`, with
		// arbitrary whitespace — and the driver executes all of them. Keying
		// on the literal prefix `foreign_keys(` therefore missed
		// `foreign_keys (0)`, `foreign_keys = 0`, and the percent-encoded
		// space forms: the caller's opt-out was honoured by the driver while
		// this function reported "no choice made", so the appended default was
		// added and — because _pragma values are applied in sorted order —
		// executed last and won. That is the third time this guard has
		// silently inverted an explicit choice, each time through a different
		// spelling, which is why it now matches the name and stops.
		//
		// No other SQLite pragma begins with "foreign_keys", so this cannot
		// over-suppress.
		name := strings.ToLower(strings.TrimSpace(v))
		rest, ok := strings.CutPrefix(name, "foreign_keys")
		if !ok {
			continue
		}
		// An ASSIGNMENT is a choice; a bare `_pragma=foreign_keys` is a read
		// that sets nothing, so it must still receive the default. Both
		// argument forms SQLite accepts count, with any whitespace.
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "(") || strings.HasPrefix(rest, "=") {
			return true
		}
	}
	// The mattn-compatible shorthands. The driver applies these AFTER the
	// pragma list, so they already beat any appended default — which means
	// this arm changes no observable behavior today and no test can pin it.
	// It stays as a guard against that ordering changing: if the shorthands
	// ever applied first, skipping the default here is what keeps the caller's
	// choice. Matched by key so an encoded value cannot hide them.
	for _, k := range []string{"_foreign_keys", "_fk"} {
		if _, ok := vals[k]; ok {
			return true
		}
	}
	return false
}

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
	if !hasForeignKeysChoice(query) {
		add(foreignKeysParam)
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
