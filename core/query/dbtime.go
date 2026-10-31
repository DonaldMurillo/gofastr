package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ParseDBTime converts a database time-column value, as handed to Scan, into
// a time.Time. It accepts the shapes the Go SQL drivers produce for time
// columns — time.Time, non-nil *time.Time, and the text layouts SQLite
// drivers fall back to — and rejects everything else (including a nil
// *time.Time) with an error.
//
// It replaces the package-local parsers formerly duplicated as
// framework/outbox's outboxTime/parseOutboxTime and battery/queue's
// queueTime/parseQueueTime, which differed only in their error prefixes.
func ParseDBTime(src any) (time.Time, error) {
	switch value := src.(type) {
	case time.Time:
		return value, nil
	case *time.Time:
		if value != nil {
			return *value, nil
		}
	case string:
		return ParseDBTimeString(value)
	case []byte:
		return ParseDBTimeString(string(value))
	}
	return time.Time{}, fmt.Errorf("query: unsupported database time value %T", src)
}

// ParseDBTimeString parses the text layouts a SQLite driver may hand back
// for a time column: RFC3339 (with or without fractional seconds) and the
// space-separated forms mattn/go-sqlite3 writes. It is the string half of
// [ParseDBTime], extracted for callers that already hold the stored text.
func ParseDBTimeString(raw string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("query: invalid database time %q", raw)
}

// ProbeSQLiteBindLayout detects the text layout the connected SQLite driver
// produces when a time.Time is bound as a parameter: the pure driver binds
// RFC3339Nano, mattn/go-sqlite3 a space-separated form. Callers whose SQL
// predicates compare stored time text lexicographically use the probed
// layout as their canonical normalization target, and rows already in it are
// canonical for this host and skipped, which keeps normalization idempotent
// on either driver. An unrecognized probe result falls back to RFC3339Nano
// (a rewrite that binds time.Time values still self-corrects, because the
// driver formats the bound value).
//
// It replaces the identical package-local probes formerly duplicated as
// framework/outbox's (Outbox).probeBindLayout and battery/queue's dead
// (DBQueue).probeBindLayout.
func ProbeSQLiteBindLayout(ctx context.Context, db *sql.DB) string {
	ref := time.Date(2001, 2, 3, 4, 5, 6, 789012345, time.UTC)
	var got string
	if err := db.QueryRowContext(ctx, `SELECT CAST($1 AS TEXT)`, ref).Scan(&got); err != nil {
		return time.RFC3339Nano
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00", // mattn/go-sqlite3
	} {
		if ref.Format(layout) == got {
			return layout
		}
	}
	return time.RFC3339Nano
}

// StoredTime is one time column of a row as read back with
// CAST(col AS TEXT): its name and the driver's raw value (nil for NULL).
type StoredTime struct {
	Col string
	Raw any
}

// TimeFix is a time column to rewrite: the stored text it was read with
// and the instant that text parses to, in UTC.
type TimeFix struct {
	Col  string
	Read string
	At   time.Time
}

// SQLiteTimeFixes returns the columns whose stored text is not already in
// layout (from [ProbeSQLiteBindLayout]). A NULL column produces no fix,
// and a canonical value parses and formats back to the same text, so it
// is skipped and a normalization pass is idempotent. A value that is not
// text, or that [ParseDBTime] cannot read, is an error: it is corrupt, and
// keeping it would leave the bug in place. The caller binds At as a
// time.Time, so the driver writes exactly what its own binds compare
// against, and guards the write on Read so a value rewritten since the
// read is kept.
func SQLiteTimeFixes(layout string, cols []StoredTime) ([]TimeFix, error) {
	var fixes []TimeFix
	for _, c := range cols {
		if c.Raw == nil {
			continue
		}
		var read string
		switch v := c.Raw.(type) {
		case string:
			read = v
		case []byte:
			read = string(v)
		default:
			return nil, fmt.Errorf("decode legacy %s: stored as %T, want text", c.Col, c.Raw)
		}
		parsed, err := ParseDBTime(read)
		if err != nil {
			return nil, fmt.Errorf("decode legacy %s: %w", c.Col, err)
		}
		if read == parsed.UTC().Format(layout) {
			continue
		}
		fixes = append(fixes, TimeFix{Col: c.Col, Read: read, At: parsed.UTC()})
	}
	return fixes, nil
}
