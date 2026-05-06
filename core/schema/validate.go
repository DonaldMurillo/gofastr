package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// validateField dispatches to type-specific validation.
func validateField(f Field, value any) error {
	if value == nil {
		if f.Required {
			return fmt.Errorf("is required")
		}
		return nil
	}

	switch f.Type {
	case String, Text:
		return validateString(f, value)
	case Int:
		return validateInt(f, value)
	case Float:
		return validateFloat(f, value)
	case Decimal:
		return validateDecimal(f, value)
	case Bool:
		return validateBool(value)
	case Enum:
		return validateEnum(f, value)
	case UUID:
		return validateUUID(value)
	case Timestamp:
		return validateTimestamp(value)
	case Date:
		return validateDate(value)
	case JSON:
		return validateJSON(value)
	case Relation:
		return validateRelation(value)
	case Image, File:
		return validateImageOrFile(value)
	default:
		return fmt.Errorf("unsupported field type %d", f.Type)
	}
}

// --- String / Text ---

func validateString(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if f.Required && s == "" {
		return fmt.Errorf("is required")
	}
	if f.Min != nil && float64(len(s)) < *f.Min {
		return fmt.Errorf("must be at least %v characters", *f.Min)
	}
	if f.Max != nil && float64(len(s)) > *f.Max {
		return fmt.Errorf("must be at most %v characters", *f.Max)
	}
	if f.Pattern != "" {
		re, err := regexp.Compile(f.Pattern)
		if err != nil {
			return fmt.Errorf("invalid pattern: %v", err)
		}
		if !re.MatchString(s) {
			return fmt.Errorf("must match pattern %q", f.Pattern)
		}
	}
	return nil
}

// --- Int ---

func validateInt(f Field, value any) error {
	n, ok := toInt64(value)
	if !ok {
		return fmt.Errorf("must be an integer")
	}
	if f.Min != nil && float64(n) < *f.Min {
		return fmt.Errorf("must be at least %v", *f.Min)
	}
	if f.Max != nil && float64(n) > *f.Max {
		return fmt.Errorf("must be at most %v", *f.Max)
	}
	return nil
}

// --- Float ---

func validateFloat(f Field, value any) error {
	n, ok := toFloat64(value)
	if !ok {
		return fmt.Errorf("must be a number")
	}
	if f.Min != nil && n < *f.Min {
		return fmt.Errorf("must be at least %v", *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return fmt.Errorf("must be at most %v", *f.Max)
	}
	return nil
}

// --- Decimal ---

func validateDecimal(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a decimal string")
	}
	if f.Required && s == "" {
		return fmt.Errorf("is required")
	}
	if s == "" {
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("must be a valid decimal number")
	}
	if f.Min != nil && n < *f.Min {
		return fmt.Errorf("must be at least %v", *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return fmt.Errorf("must be at most %v", *f.Max)
	}
	return nil
}

// --- Bool ---

func validateBool(value any) error {
	switch v := value.(type) {
	case bool:
		return nil
	case int:
		if v == 0 || v == 1 {
			return nil
		}
		return fmt.Errorf("must be true or false")
	case string:
		switch strings.ToLower(v) {
		case "true", "false", "1", "0":
			return nil
		}
		return fmt.Errorf("must be true or false")
	case float64:
		if v == 0 || v == 1 {
			return nil
		}
		return fmt.Errorf("must be true or false")
	default:
		return fmt.Errorf("must be a boolean")
	}
}

// --- Enum ---

func validateEnum(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	for _, allowed := range f.Values {
		if s == allowed {
			return nil
		}
	}
	return fmt.Errorf("must be one of %v", f.Values)
}

// --- UUID ---

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateUUID(value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if !uuidRe.MatchString(s) {
		return fmt.Errorf("must be a valid UUID")
	}
	return nil
}

// --- Timestamp ---

func validateTimestamp(value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	_, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("must be a valid RFC 3339 timestamp")
	}
	return nil
}

// --- Date ---

func validateDate(value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	_, err := time.Parse("2006-01-02", s)
	if err != nil {
		return fmt.Errorf("must be a valid date (YYYY-MM-DD)")
	}
	return nil
}

// --- JSON ---

func validateJSON(value any) error {
	switch v := value.(type) {
	case string:
		if !json.Valid([]byte(v)) {
			return fmt.Errorf("must be valid JSON")
		}
		return nil
	case map[string]any, []any:
		// already parsed Go value — always valid
		return nil
	default:
		// try marshaling and re-checking
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("must be valid JSON")
		}
		if !json.Valid(b) {
			return fmt.Errorf("must be valid JSON")
		}
		return nil
	}
}

// --- Relation ---

func validateRelation(value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string reference")
	}
	if s == "" {
		return fmt.Errorf("must be a non-empty reference")
	}
	return nil
}

// --- Image / File ---

func validateImageOrFile(value any) error {
	s, ok := toString(value)
	if !ok {
		return fmt.Errorf("must be a string path or URL")
	}
	if s == "" {
		return fmt.Errorf("must be a non-empty path or URL")
	}
	return nil
}

// --- helpers ---

func toString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case fmt.Stringer:
		return val.String(), true
	default:
		return "", false
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		// overflow check
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case float64:
		if n == math.Trunc(n) {
			return int64(n), true
		}
		return 0, false
	case float32:
		if float64(n) == math.Trunc(float64(n)) {
			return int64(n), true
		}
		return 0, false
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
