package queue

import (
	"fmt"
	"time"
)

func queueTime(src any) (time.Time, error) {
	switch value := src.(type) {
	case time.Time:
		return value, nil
	case *time.Time:
		if value != nil {
			return *value, nil
		}
	case string:
		return parseQueueTime(value)
	case []byte:
		return parseQueueTime(string(value))
	}
	return time.Time{}, fmt.Errorf("queue: unsupported database time value %T", src)
}

func parseQueueTime(raw string) (time.Time, error) {
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
	return time.Time{}, fmt.Errorf("queue: invalid database time %q", raw)
}
