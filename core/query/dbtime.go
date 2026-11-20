package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ParseDBTime converts a scanned time-column value (time.Time, non-nil *time.Time, or SQLite text) into a time.Time, erroring on anything else.
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

// ParseDBTimeString parses the RFC3339 and space-separated text layouts SQLite drivers store for a time column.
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

// ProbeSQLiteBindLayout returns the layout the SQLite driver writes for a bound time.Time, RFC3339Nano when unrecognized.
func ProbeSQLiteBindLayout(ctx context.Context, db *sql.DB) string {
	ref := time.Date(2001, 2, 3, 4, 5, 6, 789012345, time.UTC)
	var got string
	if err := db.QueryRowContext(ctx, `SELECT CAST($1 AS TEXT)`, ref).Scan(&got); err != nil {
		return time.RFC3339Nano
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
	} {
		if ref.Format(layout) == got {
			return layout
		}
	}
	return time.RFC3339Nano
}

// StoredTime is one time column read back with CAST(col AS TEXT); Raw is nil for NULL.
type StoredTime struct {
	Col string
	Raw any
}

// TimeFix is a time column to rewrite: the text it was read with and the UTC instant it parses to.
type TimeFix struct {
	Col  string
	Read string
	At   time.Time
}

// SQLiteTimeFixes returns the non-NULL columns whose stored text is not already in layout, erroring on a value that is not readable time text.
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
