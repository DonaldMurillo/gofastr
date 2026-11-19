package schema

import "time"

// TimestampLayout is the one spelling a Timestamp column holds and every
// bound Timestamp value uses: UTC, microseconds, trailing zeros kept.
const TimestampLayout = "2006-01-02T15:04:05.000000Z07:00"

// FormatTimestamp is t in TimestampLayout.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}

// TimestampArg is v in the shape a Timestamp column binds: a time.Time, a
// non-nil *time.Time or a parseable RFC 3339 string becomes
// FormatTimestamp; any other value passes through unchanged.
func TimestampArg(v any) any {
	switch t := v.(type) {
	case time.Time:
		return FormatTimestamp(t)
	case *time.Time:
		if t != nil {
			return FormatTimestamp(*t)
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return FormatTimestamp(parsed)
		}
	}
	return v
}
