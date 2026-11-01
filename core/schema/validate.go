package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"slices"
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
			return violate(ReasonRequired, nil)
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
		return violate(ReasonUnsupportedType, map[string]any{"type": int(f.Type)})
	}
}

// --- String / Text ---

func validateString(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	if f.Required && s == "" {
		return violate(ReasonRequired, nil)
	}
	n := utf8.RuneCountInString(s)
	if f.Min != nil && float64(n) < *f.Min {
		return violate(ReasonMinLength, map[string]any{"min": *f.Min})
	}
	if f.Max != nil && float64(n) > *f.Max {
		return violate(ReasonMaxLength, map[string]any{"max": *f.Max})
	}
	if f.Pattern != "" {
		re, err := regexp.Compile(f.Pattern)
		if err != nil {
			return violate(ReasonPatternInvalid, map[string]any{"detail": err.Error()})
		}
		if !re.MatchString(s) {
			return violate(ReasonPattern, map[string]any{"pattern": strconv.Quote(f.Pattern)})
		}
	}
	return nil
}

// --- Int ---

func validateInt(f Field, value any) error {
	n, ok := toInt64(value)
	if !ok {
		return violate(ReasonNotInteger, nil)
	}
	// Compare bounds in integer space. The bounds are stored as *float64,
	// so first clamp them to the int64 range; widening n to float64 instead
	// would lose precision above 2^53 and admit values strictly over Max.
	if f.Min != nil && (math.IsNaN(*f.Min) || *f.Min >= math.Exp2(63) || n < floatBoundToInt64(*f.Min, true)) {
		return violate(ReasonMin, map[string]any{"min": *f.Min})
	}
	if f.Max != nil && (math.IsNaN(*f.Max) || *f.Max < math.MinInt64 || n > floatBoundToInt64(*f.Max, false)) {
		return violate(ReasonMax, map[string]any{"max": *f.Max})
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
		return violate(ReasonNotNumber, nil)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		// NaN/Inf defeat every Min/Max comparison (IEEE-754); reject them
		// so the bound can't be bypassed.
		return violate(ReasonNotFinite, nil)
	}
	if f.Min != nil && n < *f.Min {
		return violate(ReasonMin, map[string]any{"min": *f.Min})
	}
	if f.Max != nil && n > *f.Max {
		return violate(ReasonMax, map[string]any{"max": *f.Max})
	}
	return nil
}

// --- Decimal ---

func validateDecimal(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotDecimalString, nil)
	}
	if f.Required && s == "" {
		return violate(ReasonRequired, nil)
	}
	if s == "" {
		return nil
	}
	// decimalRe is the primary form guard: it admits only canonical decimal
	// text and rejects the float-literal forms storage can't reparse
	// (underscore separators, hex floats) and the non-finite literals (Inf/NaN)
	// that would bypass the Min/Max bounds. The ParseFloat + finiteness checks
	// below are defense-in-depth: with the current regex they are unreachable
	// (kept by policy), but they keep the validator fail-closed if the
	// regex is ever loosened or a typed caller bypasses it.
	if !decimalRe.MatchString(s) {
		return violate(ReasonNotDecimal, nil)
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return violate(ReasonNotDecimal, nil)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return violate(ReasonNotFiniteDecimal, nil)
	}
	// Compare bounds in exact decimal space. Decimal is a fixed-precision type
	// stored as a string precisely to avoid float rounding, so the bound check
	// must not round-trip through float64 (which would lose precision above
	// 2^53 and admit values strictly over Max / under Min). big.Rat parses the
	// canonical decimal text exactly, and each bound uses its shortest decimal spelling, so configured decimal
	// values compare exactly at the boundary.
	dv, ok := new(big.Rat).SetString(s)
	if !ok {
		// decimalRe already vetted the form; defensive fail-closed.
		return violate(ReasonNotDecimal, nil)
	}
	if f.Min != nil {
		// decimalBound returns nil for a NaN/Inf bound; such a bound can never be
		// satisfied, so reject (fail closed) rather than panic in Cmp.
		min := decimalBound(*f.Min)
		if min == nil || dv.Cmp(min) < 0 {
			return violate(ReasonMin, map[string]any{"min": *f.Min})
		}
	}
	if f.Max != nil {
		max := decimalBound(*f.Max)
		if max == nil || dv.Cmp(max) > 0 {
			return violate(ReasonMax, map[string]any{"max": *f.Max})
		}
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
		return violate(ReasonNotTrueFalse, nil)
	case int64:
		// crud decodes an integer literal to int64, so a body's 1 arrives
		// here as one.
		if v == 0 || v == 1 {
			return nil
		}
		return violate(ReasonNotTrueFalse, nil)
	case string:
		switch strings.ToLower(v) {
		case "true", "false", "1", "0":
			return nil
		}
		return violate(ReasonNotTrueFalse, nil)
	case float64:
		if v == 0 || v == 1 {
			return nil
		}
		return violate(ReasonNotTrueFalse, nil)
	default:
		return violate(ReasonNotBoolean, nil)
	}
}

// --- Enum ---

func validateEnum(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	if slices.Contains(f.Values, s) {
		return nil
	}
	return violate(ReasonNotOneOf, map[string]any{"values": fmt.Sprint(f.Values)})
}

// --- UUID ---

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateUUID(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	if !uuidRe.MatchString(s) {
		return violate(ReasonNotUUID, nil)
	}
	return nil
}

// --- Timestamp ---

func validateTimestamp(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	ts, err := time.Parse(time.RFC3339, s)
	// Go reads year 0000; Postgres refuses it as out of range, so the
	// write failed there as a 500. crud stores the UTC instant, so that
	// is the year checked.
	if err != nil || ts.UTC().Year() < 1 {
		return violate(ReasonNotTimestamp, nil)
	}
	return nil
}

// --- Date ---

func validateDate(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	d, err := time.Parse("2006-01-02", s)
	// Year 0000 parses in Go and is out of range on Postgres.
	if err != nil || d.Year() < 1 {
		return violate(ReasonNotDate, nil)
	}
	return nil
}

// --- JSON ---

func validateJSON(value any) error {
	switch v := value.(type) {
	case string:
		if !json.Valid([]byte(v)) {
			return violate(ReasonNotJSON, nil)
		}
		return nil
	default:
		// try marshaling and re-checking
		b, err := json.Marshal(v)
		if err != nil {
			return violate(ReasonNotJSON, nil)
		}
		if !json.Valid(b) {
			return violate(ReasonNotJSON, nil)
		}
		return nil
	}
}

// --- Relation ---

func validateRelation(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotReference, nil)
	}
	if s == "" {
		return violate(ReasonEmptyReference, nil)
	}
	return nil
}

// --- Image / File ---

func validateImageOrFile(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotPath, nil)
	}
	if s == "" {
		return violate(ReasonEmptyPath, nil)
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
		// Same overflow check as uint64: on 64-bit platforms uint is
		// 64 bits wide, and converting an out-of-int64-range value
		// wraps negative — uint(MaxUint64) read as -1 slips past any
		// Max-only bound. The two unsigned spellings of one number
		// must agree. The comparison runs in uint64 so a 32-bit
		// build, whose uint cannot hold MaxInt64, compiles.
		if uint64(n) > math.MaxInt64 {
			return 0, false
		}
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
	case json.Number:
		parsed, err := n.Int64()
		return parsed, err == nil
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
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
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

// Decimal bounds use their shortest decimal spelling, matching configuration
// and JSON, rather than the binary floating-point approximation. A float64
// keeps every decimal of up to 15 significant digits as written, so a bound
// whose shortest spelling fits is that spelling: Min 1e30 is 1e30, not
// 1000000000000000019884624838656. An integral bound that needs 16 or more
// digits (2^60, spelled in full or computed) was not written as a short
// decimal, and its exact binary value is the bound.
func decimalBound(v float64) *big.Rat {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	short := strconv.FormatFloat(v, 'e', -1, 64)
	if math.Trunc(v) == v && significantDigits(short) > 15 {
		return new(big.Rat).SetFloat64(v)
	}
	n, _ := new(big.Rat).SetString(short)
	return n
}

// significantDigits counts the mantissa digits of a FormatFloat 'e'
// spelling.
func significantDigits(e string) int {
	n := 0
	for _, c := range e {
		if c == 'e' {
			break
		}
		if c >= '0' && c <= '9' {
			n++
		}
	}
	return n
}
