package outbox

import (
	"fmt"
	"time"
)

func outboxTime(src any) (time.Time, error) {
	switch value := src.(type) {
	case time.Time:
		return value, nil
	case *time.Time:
		if value != nil {
			return *value, nil
		}
	case string:
		return parseOutboxTime(value)
	case []byte:
		return parseOutboxTime(string(value))
	}
	return time.Time{}, fmt.Errorf("outbox: unsupported database time value %T", src)
}

func outboxTimePtr(src any) (*time.Time, error) {
	if src == nil {
		return nil, nil
	}
	value, err := outboxTime(src)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func parseOutboxTime(raw string) (time.Time, error) {
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
	return time.Time{}, fmt.Errorf("outbox: invalid database time %q", raw)
}
