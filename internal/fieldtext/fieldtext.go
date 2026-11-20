// Package fieldtext turns a field value into the text a screen shows and a
// form control holds. framework/entityui draws its screens with it, and the
// app generator (cmd/gofastr/internal/appgen) emits a create control's
// starting value with it, so a generated form and an entityui form start
// from the same text.
package fieldtext

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Cell is a value as plain text: "" for nil and the zero time, RFC 3339
// for a time, JSON for a map or slice decoded from JSON.
func Cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case map[string]any, []any:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
		return fmt.Sprint(v)
	default:
		return fmt.Sprint(v)
	}
}

// Truthy reports whether s is one of the spellings of true.
func Truthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on", "t":
		return true
	}
	return false
}

// Bool is a bool value as "true", "false" or "" for nil.
func Bool(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		if Truthy(t) {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return Cell(v)
}

// Form is the text a form control of field f holds for v.
func Form(f schema.Field, v any) string {
	switch f.Type {
	case schema.Date:
		switch t := v.(type) {
		case time.Time:
			return t.Format(time.DateOnly)
		case *time.Time:
			if t == nil {
				return ""
			}
			return t.Format(time.DateOnly)
		}
	case schema.Timestamp:
		switch t := v.(type) {
		case time.Time:
			return timestamp(t)
		case *time.Time:
			if t == nil {
				return ""
			}
			return timestamp(*t)
		}
	case schema.Bool:
		return Bool(v)
	default:
	}
	return Cell(v)
}

// DateInput is the value a date input holds for s: its YYYY-MM-DD prefix,
// or "" when s does not start with one.
func DateInput(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return ""
}

// TimestampInput is the value a datetime-local input holds for s: its UTC
// wall time, or "" when s is not a time.
func TimestampInput(s string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return timestamp(t)
		}
	}
	return ""
}

func timestamp(t time.Time) string {
	t = t.UTC().Truncate(time.Millisecond)
	switch {
	case t.Nanosecond() != 0:
		return t.Format("2006-01-02T15:04:05.000")
	case t.Second() != 0:
		return t.Format("2006-01-02T15:04:05")
	}
	return t.Format("2006-01-02T15:04")
}

// TimestampStep is the step a datetime-local input needs to hold v, or 0
// for the browser's default of a minute.
func TimestampStep(v string) float64 {
	switch {
	case strings.Contains(v, "."):
		return 0.001
	case len(v) > len("2006-01-02T15:04"):
		return 1
	}
	return 0
}
