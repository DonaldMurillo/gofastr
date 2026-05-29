package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// decimalRe matches a canonical decimal numeric literal: an optional sign
// followed by digits with an optional fractional part, or a bare fractional
// part, optionally with a base-10 exponent. It deliberately rejects Go
// float-literal forms that storage layers cannot reparse (underscore
// separators, hex floats) as well as Inf/NaN.
var decimalRe = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

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
	n := utf8.RuneCountInString(s)
	if f.Min != nil && float64(n) < *f.Min {
		return fmt.Errorf("must be at least %v characters", *f.Min)
	}
	if f.Max != nil && float64(n) > *f.Max {
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
	// Compare bounds in integer space. The bounds are stored as *float64,
	// so first clamp them to the int64 range; widening n to float64 instead
	// would lose precision above 2^53 and admit values strictly over Max.
	if f.Min != nil && n < floatBoundToInt64(*f.Min, true) {
		return fmt.Errorf("must be at least %v", *f.Min)
	}
	if f.Max != nil && n > floatBoundToInt64(*f.Max, false) {
		return fmt.Errorf("must be at most %v", *f.Max)
	}
	return nil
}

// floatBoundToInt64 converts a *float64 Int bound to an int64 for exact
// integer comparison. Bounds beyond the int64 range are clamped: a Min above
// MaxInt64 becomes MaxInt64 (nothing can satisfy it), a Max above MaxInt64
// becomes MaxInt64 (no int64 can exceed it), and symmetrically for the low
// end. isMin selects rounding that never widens the admitted range.
func floatBoundToInt64(b float64, isMin bool) int64 {
	if math.IsNaN(b) {
		// A NaN bound can never be satisfied / never be exceeded; pick the
		// extreme that rejects everything for a Min and accepts for a Max.
		if isMin {
			return math.MaxInt64
		}
		return math.MinInt64
	}
	if b >= math.MaxInt64 {
		return math.MaxInt64
	}
	if b <= math.MinInt64 {
		return math.MinInt64
	}
	if isMin {
		return int64(math.Ceil(b))
	}
	return int64(math.Floor(b))
}

// --- Float ---

func validateFloat(f Field, value any) error {
	n, ok := toFloat64(value)
	if !ok {
		return fmt.Errorf("must be a number")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		// NaN/Inf defeat every Min/Max comparison (IEEE-754); reject them
		// so the bound can't be bypassed.
		return fmt.Errorf("must be a finite number")
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
	// Require a canonical decimal literal. strconv.ParseFloat also accepts
	// underscore separators, hex floats, and Inf/NaN, none of which are valid
	// decimal text and several of which (NaN/Inf) bypass the Min/Max bounds.
	if !decimalRe.MatchString(s) {
		return fmt.Errorf("must be a valid decimal number")
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("must be a valid decimal number")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("must be a finite decimal number")
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

// isInt64Representable reports whether a float is a finite, integral value
// that fits in int64. Go's float->int64 conversion silently saturates for
// out-of-range inputs, so an explicit guard is required to avoid accepting a
// value (e.g. 1e30) that the validator would then range-check against the
// wrong (saturated) number. The upper bound is strict because MaxInt64 itself
// is not exactly representable as float64 and rounds up to 2^63.
func isInt64Representable(n float64) bool {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return false
	}
	if n != math.Trunc(n) {
		return false
	}
	return n >= math.MinInt64 && n < math.MaxInt64
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
		if isInt64Representable(n) {
			return int64(n), true
		}
		return 0, false
	case float32:
		if isInt64Representable(float64(n)) {
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
	default:
		return 0, false
	}
}
