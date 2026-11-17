package filter

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

var decimalLiteral = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

var timestampLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	time.DateTime,
	time.DateOnly,
}

// CoerceValue returns what a binder binds for raw on a column of type t,
// or an error naming field when raw is not a value of that type: Int as
// int64, Float as float64 (decimal literals only), Bool as
// strconv.ParseBool reads it, Timestamp in schema.TimestampLayout (RFC
// 3339, a zone-less date-time read as UTC, or a bare date), and Decimal,
// Date and JSON checked and bound as sent. Other types bind raw. Every
// type refuses text Postgres cannot store, and Decimal, Timestamp and
// Date refuse what Postgres refuses (an exponent past 1000, year 0).
func CoerceValue(field string, t schema.FieldType, raw string) (any, error) {
	if !textsafe.Storable(raw) {
		return nil, valueTypeError(field, t)
	}
	switch t {
	case schema.Int:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, valueTypeError(field, t)
		}
		return n, nil
	case schema.Float:
		if !decimalLiteral.MatchString(raw) {
			return nil, valueTypeError(field, t)
		}
		f, err := strconv.ParseFloat(raw, 64)

		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, valueTypeError(field, t)
		}
		return f, nil
	case schema.Decimal:

		if !decimalLiteral.MatchString(raw) || !schema.DecimalExponentFits(raw) {
			return nil, valueTypeError(field, t)
		}
		return raw, nil
	case schema.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, valueTypeError(field, t)
		}
		return b, nil
	case schema.Timestamp:
		for _, layout := range timestampLayouts {
			if ts, err := time.Parse(layout, raw); err == nil {
				if ts.UTC().Year() < 1 {
					return nil, valueTypeError(field, t)
				}
				return schema.FormatTimestamp(ts), nil
			}
		}
		return nil, valueTypeError(field, t)
	case schema.Date:
		d, err := time.Parse(time.DateOnly, raw)
		if err != nil || d.Year() < 1 {
			return nil, valueTypeError(field, t)
		}
		return raw, nil
	case schema.JSON:

		if !json.Valid([]byte(raw)) {
			return nil, valueTypeError(field, t)
		}
		return raw, nil
	default:
		return raw, nil
	}
}

// TypedBind is CoerceValue for a value already checked or one the
// program controls: the typed value when raw is of type t, raw otherwise.
func TypedBind(t schema.FieldType, raw string) any {
	if v, err := CoerceValue("", t, raw); err == nil {
		return v
	}
	return raw
}

func valueTypeError(field string, t schema.FieldType) error {
	return i18n.NewError(reasonValueType, map[string]any{"field": strconv.Quote(field), "type": fieldTypeName(t)})
}

// LikeClause returns the case-insensitive, wildcard-escaped "contains"
// predicate on col with placeholder $n, and its bind value. col is
// spliced as SQL: pass an allow-listed column name.
func LikeClause(col string, n int, value string) (string, string) {
	return "LOWER(" + query.QuoteKeyword(col) + ") LIKE $" + strconv.Itoa(n) + LikeEscapeSuffix, LikeArg(value)
}

// LikeArg is LikeClause's bind value: value lowered, escaped, in %…%.
func LikeArg(value string) string {
	return EscapeLikePattern(strings.ToLower(value))
}
