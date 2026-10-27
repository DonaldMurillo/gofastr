package schema

import "time"

// TimestampLayout is the one spelling a Timestamp column holds: UTC,
// microseconds, trailing zeros kept. crud writes every Timestamp in it,
// a keyset cursor carries it, and every filter and typed query condition
// binds a Timestamp value in it. SQLite compares the column as text, so a
// value bound as the "...00Z" a caller sent matched no row stored as
// "...00.000000Z" for the same instant, and gt/lt compared spellings.
const TimestampLayout = "2006-01-02T15:04:05.000000Z07:00"

// FormatTimestamp is t in TimestampLayout.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}
