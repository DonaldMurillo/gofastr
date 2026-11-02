package filter

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// decimalLiteral is the numeric text both dialects parse: an optional sign,
// digits with an optional fraction (or a bare fraction), and an optional
// exponent. strconv.ParseFloat also takes hex floats, underscores, Inf and
// NaN, which Postgres refuses with 22P02 and SQLite compares as text, so
// the shape is checked before the parse. Same pattern as core/schema's
// Decimal write validator.
var decimalLiteral = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// timestampLayouts are the Timestamp filter spellings CoerceValue reads:
// RFC 3339, the zone-less forms an <input type=datetime-local> sends
// (minutes or seconds; time.Parse takes a fraction after the seconds
// without the layout naming one), SQLite's datetime() form with a space,
// and a bare date. time.Parse reads the zone-less ones as UTC.
var timestampLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	time.DateTime,
	time.DateOnly,
}

// CoerceValue checks that raw is a value of a column of type t and returns
// what a query binder binds for it. Every filter surface (?field_<op>=,
// ?where=, dsl predicates, nested ?rel.field= and ?include=rel(…) filters,
// ListOptions.Filters, a keyset cursor's values) runs its values through
// here, so a value that is not of the column's type is a 400 naming the
// field on both dialects. Before it, Postgres refused the bind with 22P02
// (a 500) while SQLite compared the text and answered 200 with no rows.
//
//   - Int: base-10 integer (strconv.ParseInt), bound as int64.
//   - Float: decimal literal, bound as float64. Hex floats, underscores,
//     Inf and NaN are refused.
//   - Decimal: decimal literal, bound as the text sent so the comparison
//     keeps every digit.
//   - Bool: exactly what strconv.ParseBool accepts (1, t, T, TRUE, true,
//     True and the false spellings), bound as bool. "yes"/"on" are refused:
//     Postgres would accept them and SQLite would match nothing.
//   - Timestamp: RFC 3339, a zone-less date and time (T or a space, to the
//     minute or second), or a bare date, bound in schema.TimestampLayout. A value
//     with no zone is UTC, the zone crud stores every Timestamp in; a bare
//     date is its midnight.
//   - Date: YYYY-MM-DD, bound as the text sent.
//
// Every other type binds raw unchanged: String, Text, Enum, UUID and
// Relation columns are TEXT on both dialects, so any text is a value
// that simply may match nothing.
//
// Under every type the value must be text Postgres can store: valid
// UTF-8 with no NUL byte. Postgres refuses either in a text bind with
// 22021 (a 500) while SQLite compared the bytes and answered 200, so
// ?title=a%FFb split the dialects the way a mistyped value did. A
// Decimal exponent past ±1000 (Postgres numeric_in refuses it) and a
// Timestamp or Date in year 0 or before (Postgres has no year 0) are
// refused for the same reason.
func CoerceValue(field string, t schema.FieldType, raw string) (any, error) {
	if !storableText(raw) {
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
		// decimalLiteral already keeps out NaN and Inf, and an overflow
		// is err; the finite check stays as the one the nonfinite
		// analyzer reads, so the shape can never drift on its own.
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, valueTypeError(field, t)
		}
		return f, nil
	case schema.Decimal:
		// The write check (schema.Validate) shares the exponent rule.
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
				// Go reads year 0000; Postgres refuses it as out of
				// range, a 500 there and a 200 on SQLite. The bound
				// value is the UTC instant, so that is the one checked.
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
		// Postgres reads the bind as JSONB and refused non-JSON text as
		// a 500; SQLite compared it as text and answered no rows.
		if !json.Valid([]byte(raw)) {
			return nil, valueTypeError(field, t)
		}
		return raw, nil
	default:
		return raw, nil
	}
}

// storableText reports whether raw is text both dialects store and
// compare: valid UTF-8 holding no NUL byte. Postgres refuses anything
// else in a text bind (22021).
func storableText(raw string) bool {
	return utf8.ValidString(raw) && strings.IndexByte(raw, 0) < 0
}

// TypedBind is CoerceValue for a value already checked, or one the program
// controls: the typed value when raw is of type t, raw itself otherwise.
// Binders call it after validation so the check and the bind cannot drift.
func TypedBind(t schema.FieldType, raw string) any {
	if v, err := CoerceValue("", t, raw); err == nil {
		return v
	}
	return raw
}

func valueTypeError(field string, t schema.FieldType) error {
	return i18n.NewError(reasonValueType, map[string]any{"field": strconv.Quote(field), "type": fieldTypeName(t)})
}

// LikeClause returns the case-insensitive "contains" predicate every
// `_like` / `contains` filter compiles to, and the bind value for its
// placeholder $n. It is the ?q= search shape (LOWER on the column, the
// value lowered in Go) so the two agree, with the caller's LIKE wildcards
// escaped so they match literally. col is spliced as SQL: pass an
// allow-listed, possibly table-qualified, column name.
//
// Postgres LIKE is case-sensitive and SQLite's folds ASCII case only, so a
// bare LIKE answered differently per dialect. The case contract is ?q='s:
// ASCII-case-insensitive everywhere, Unicode folding a Postgres bonus
// (SQLite's LOWER folds only ASCII, so a stored "É" is not matched there).
func LikeClause(col string, n int, value string) (string, string) {
	return "LOWER(" + col + ") LIKE $" + strconv.Itoa(n) + LikeEscapeSuffix, LikeArg(value)
}

// LikeArg is the bind value LikeClause pairs with its placeholder: value
// lowered, wildcards escaped, wrapped in %…%.
func LikeArg(value string) string {
	return EscapeLikePattern(strings.ToLower(value))
}
