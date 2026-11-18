package schema

import "time"

// TimestampLayout is the one spelling a Timestamp column holds and every
// bound Timestamp value uses: UTC, microseconds, trailing zeros kept.
const TimestampLayout = "2006-01-02T15:04:05.000000Z07:00"

// FormatTimestamp is t in TimestampLayout.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}
