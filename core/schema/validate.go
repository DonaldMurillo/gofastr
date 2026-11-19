package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// DecimalRegexp matches a decimal literal with an optional sign and exponent.
var DecimalRegexp = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

func validateField(f Field, value any) error {
	if value == nil {
		if f.Required {
			return violate(ReasonRequired, nil)
		}
		return nil
	}
	if f.Required {
		if s, ok := value.(string); ok && s == "" {
			switch f.Type {
			case Int, Float, Bool, Enum, UUID, Timestamp, Date, JSON, Relation, Image, File:
				return violate(ReasonRequired, nil)
			case String, Text, Decimal:
				// Their validators check for empty strings after conversion.
			}
		}
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

func validateString(f Field, value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	if !textsafe.Storable(s) {
		return violate(ReasonInvalidText, nil)
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

func validateInt(f Field, value any) error {
	n, ok := toInt64(value)
	if !ok {
		return violate(ReasonNotInteger, nil)
	}
	if f.Min != nil && (math.IsNaN(*f.Min) || *f.Min >= math.Exp2(63) || n < floatBoundToInt64(*f.Min, true)) {
		return violate(ReasonMin, map[string]any{"min": *f.Min})
	}
	if f.Max != nil && (math.IsNaN(*f.Max) || *f.Max < math.MinInt64 || n > floatBoundToInt64(*f.Max, false)) {
		return violate(ReasonMax, map[string]any{"max": *f.Max})
	}
	return nil
}

func floatBoundToInt64(b float64, isMin bool) int64 {
	if math.IsNaN(b) {
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

func validateFloat(f Field, value any) error {
	n, ok := toFloat64(value)
	if !ok {
		return violate(ReasonNotNumber, nil)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
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
	if !DecimalRegexp.MatchString(s) {
		return violate(ReasonNotDecimal, nil)
	}
	if !DecimalExponentFits(s) {
		return violate(ReasonDecimalExponent, map[string]any{"max": MaxDecimalExponent})
	}
	if precision, scale, bounded := DecimalColumn(f); bounded {
		intDigits, fracDigits := decimalDigits(s)
		if fracDigits > scale {
			return violate(ReasonDecimalFractionDigits, map[string]any{"max": scale})
		}
		if intDigits > precision-scale {
			return violate(ReasonDecimalIntegerDigits, map[string]any{"max": precision - scale})
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return violate(ReasonNotDecimal, nil)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return violate(ReasonNotFiniteDecimal, nil)
	}
	dv, ok := new(big.Rat).SetString(s)
	if !ok {
		return violate(ReasonNotDecimal, nil)
	}
	if f.Min != nil {
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

func validateTimestamp(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil || ts.UTC().Year() < 1 {
		return violate(ReasonNotTimestamp, nil)
	}
	return nil
}

func validateDate(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotString, nil)
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil || d.Year() < 1 {
		return violate(ReasonNotDate, nil)
	}
	return nil
}

func validateJSON(value any) error {
	var encoded []byte
	switch v := value.(type) {
	case string:
		encoded = []byte(v)
	case []byte:
		encoded = v
	case json.RawMessage:
		encoded = v
	default:
		var err error
		encoded, err = json.Marshal(v)
		if err != nil {
			return violate(ReasonNotJSON, nil)
		}
	}
	if !json.Valid(encoded) {
		return violate(ReasonNotJSON, nil)
	}
	if !utf8.Valid(encoded) {
		return violate(ReasonInvalidText, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return violate(ReasonNotJSON, nil)
		}
		if text, ok := token.(string); ok && strings.IndexByte(text, 0) >= 0 {
			return violate(ReasonInvalidText, nil)
		}
	}
}

func validateRelation(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotReference, nil)
	}
	if !textsafe.Storable(s) {
		return violate(ReasonInvalidText, nil)
	}
	if s == "" {
		return violate(ReasonEmptyReference, nil)
	}
	return nil
}

func validateImageOrFile(value any) error {
	s, ok := toString(value)
	if !ok {
		return violate(ReasonNotPath, nil)
	}
	if !textsafe.Storable(s) {
		return violate(ReasonInvalidText, nil)
	}
	if s == "" {
		return violate(ReasonEmptyPath, nil)
	}
	return nil
}

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
