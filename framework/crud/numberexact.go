package crud

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// JSON numbers and 64-bit integers disagree above 2^53: encoding/json
// decodes every number to float64, so the literal 9007199254740993
// (2^53+1) becomes 9007199254740992 before any layer sees it. Validation
// accepts the rounded float (it IS an integral int64), the driver binds
// it, and the row silently stores a different value than the caller sent
// while the response echoes success. Ids and money columns (snowflakes,
// external ids, cent amounts) are silently altered; the write path can
// never produce the value the read path can address.
//
// Two defenses, one per loss site:
//
//   - decode: request bodies are decoded with json.Decoder.UseNumber and
//     normalized so an integer JSON literal becomes an exact int64 before
//     validation or a hook ever sees it (decodeStrictUseNumber +
//     normalizeJSONNumbers). A literal past float64's range (1e999) stays
//     a json.Number: a JSON field stores its text, and every other column
//     refuses it (coerceNumberColumnValues).
//
//   - bind: a float64 that REACHES an Int column through some other route
//     (a host json.Unmarshal before CreateOne, the MCP argument bridge)
//     cannot be trusted above 2^53 — the value may have been rounded on
//     its way in and crud cannot tell. coerceNumberColumnValues converts
//     exactly-representable integral floats to int64 and refuses the
//     rest with 400. Send big integers as JSON integer literals or as
//     strings; both spellings round-trip exactly.

// exactFloatInt64 converts an integral float64 below ±2^53 to int64.
// Integers below 2^53 are exactly representable in float64, so such a
// value is provably what the caller's number was. AT or beyond 2^53 the
// answer is false even for values that happen to be representable: the
// float64 that arrives may be the rounded remains of a different literal
// (2^53+1 decodes to exactly 2^53) and crud cannot tell — refusing is the
// only answer that never silently stores a different number than was
// sent. Callers that need big integers send JSON integer literals or
// strings, both of which decode exactly.
// relationIntText renders a reference to an integer key in canonical
// decimal: "01", "+1", 1.0 and json.Number("1") are all "1". It reports
// false for a value that names no integer.
func relationIntText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		i, err := strconv.ParseInt(x, 10, 64)
		return strconv.FormatInt(i, 10), err == nil
	case json.Number:
		i, err := x.Int64()
		return strconv.FormatInt(i, 10), err == nil
	case float64:
		i, ok := exactFloatInt64(x)
		return strconv.FormatInt(i, 10), ok
	case int64:
		return strconv.FormatInt(x, 10), true
	case int:
		return strconv.Itoa(x), true
	}
	return "", false
}

func exactFloatInt64(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f != math.Trunc(f) {
		return 0, false
	}
	const exactLimit = 1 << 53
	if f >= exactLimit || f <= -exactLimit {
		return 0, false
	}
	return int64(f), true
}

// normalizeJSONNumbers walks a decoded JSON value and returns the same
// structure with every json.Number replaced by its exact Go spelling:
// int64 when the literal is an integer, float64 otherwise. A number
// neither can hold stays the json.Number.
func normalizeJSONNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = normalizeJSONNumbers(item)
		}
		return x
	case []any:
		for i, item := range x {
			x[i] = normalizeJSONNumbers(item)
		}
		return x
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		fl, err := x.Float64()
		if err != nil {
			// Past float64's range (1e999). A JSON field's document may
			// hold it, validation takes the text and a read emits it as
			// stored, so refusing it here refused the value a read had
			// just returned. coerceNumberColumnValues refuses it for
			// every other column.
			return x
		}
		// An integer a float64 cannot hold exactly stays json.Number:
		// a JSON field stores its text, and coerceNumberColumnValues
		// gives every other column the float64 or refusal it had. So
		// does a fraction that rounds to an integer (1.0000000000000001
		// is exactly 1.0), or an Int column could not tell it from 1.
		if roundsInteger(fl) || (fl == math.Trunc(fl) && !integerText(x.String())) {
			return x
		}
		return fl
	default:
		return v
	}
}

// integerText reports whether s, a JSON number literal, names an integer.
// It reads the digits, not a float64: 1.0000000000000001 parses to
// exactly 1.0, and an Int column stored 1 for it. 1.0 and 1e3 are
// integers. It never builds the number, so an exponent like 1e-999999999
// costs its length, not a 10^999999999 the way big.Rat.SetString would.
func integerText(s string) bool {
	mant, exp := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mant, exp = s[:i], s[i+1:]
	}
	mant = strings.TrimLeft(mant, "+-")
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimRight(intPart+frac, "0")
	if strings.Trim(digits, "0") == "" {
		return true // zero, at any exponent
	}
	// value = digits × 10^(e - len(frac) + trailing zeros dropped)
	shift := len(intPart+frac) - len(digits) - len(frac)
	if exp == "" {
		return shift >= 0
	}
	e, err := strconv.Atoi(exp)
	if err != nil || e > 1<<30 || e < -(1<<30) {
		// An exponent this large outweighs any literal's digits: a
		// positive one leaves an integer, a negative one a fraction.
		return !strings.HasPrefix(exp, "-")
	}
	return e+shift >= 0
}

// roundsInteger reports an integral float at or past ±2^53, where a
// float64 no longer holds every integer.
func roundsInteger(f float64) bool {
	return f == math.Trunc(f) && math.Abs(f) >= 1<<53
}

// decodeStrictUseNumber decodes an already-gated JSON object body into a
// map with UseNumber, then normalizes numbers to exact int64/float64.
//
// The key-ambiguity gate is handler.UnmarshalStrict's own posture for map
// destinations (duplicate and case-folded keys at ANY depth refused);
// readRequestBody already ran CheckTopLevelKeys with the wire-key fold on
// the same bytes. The two exported walks here + that fold reproduce the
// gate; a float64 decode is NOT used because it is the precision loss
// this function exists to prevent.
func decodeStrictUseNumber(data []byte) (map[string]any, error) {
	if err := handler.CheckObjectKeys(data, handler.FoldJSONKey); err != nil {
		return nil, err
	}
	//gofastr:allow(GOFASTR1407) CheckObjectKeys already verified key uniqueness and casing (see doc comment); this decode only preserves integer precision via UseNumber.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		return nil, handler.Fail(http.StatusBadRequest, handler.ReasonInvalidJSON, map[string]any{"detail": err.Error()})
	}
	// A body is one JSON value: a second object after the first was
	// dropped while the request succeeded.
	if _, err := dec.Token(); err != io.EOF {
		return nil, handler.Fail(http.StatusBadRequest, handler.ReasonInvalidJSON, map[string]any{"detail": "unexpected data after the JSON object"})
	}
	normalizeJSONNumbers(body)
	return body, nil
}

// normalizeItemNumbers gives each batch item's json.Numbers their exact
// Go spellings (normalizeJSONNumbers). Each item, not the envelope: the
// walk does not enter structs.
func normalizeItemNumbers(items []map[string]any) {
	for _, item := range items {
		normalizeJSONNumbers(item)
	}
}

// zonelessTimestamp reads s as the date and time an <input
// type=datetime-local> submits, to the minute or the second (time.Parse
// takes a fraction after the seconds), as UTC, and returns it in RFC 3339
// with the fraction kept. The Timestamp filter reads the same spellings
// as UTC (filter.CoerceValue), so a form value written and then filtered
// on names one instant. RFC 3339 text is not zone-less and returns false.
func zonelessTimestamp(s string) (string, bool) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(time.RFC3339Nano), true
		}
	}
	return "", false
}

// floatTextRe is the canonical decimal spelling a number input submits.
// It refuses what strconv.ParseFloat would otherwise read: hex floats,
// underscore separators, and the Inf/NaN words.
var floatTextRe = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// floatText reads s as a finite float in canonical decimal spelling.
func floatText(s string) (float64, bool) {
	if !floatTextRe.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// boolSpelling is the boolean a Bool field's accepted value names: true,
// false, "true"/"false" in any case, and 1 or 0 as a number or string.
func boolSpelling(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(x) {
		case "true", "1":
			return true, true
		case "false", "0":
			return false, true
		}
	case json.Number:
		// The digits decide, not the float64: 1.0000000000000001 rounds
		// to 1 and 1e-400 to 0. An integer that reads as 0 or 1 is one.
		if f, err := x.Float64(); err == nil && (f == 0 || f == 1) && integerText(x.String()) {
			return f == 1, true
		}
	case int64:
		if x == 0 || x == 1 {
			return x == 1, true
		}
	case float64:
		if x == 0 || x == 1 {
			return x == 1, true
		}
	}
	return false, false
}

// coerceNumberColumnValues normalizes the numeric and boolean columns
// present in a write body. A Bool column binds every spelling validation
// accepts as the boolean (boolSpelling). A Float column takes canonical decimal text as the number
// it spells: an HTML form carries every value as a string, and the
// runtime's form RPC posts it as one (dropEmptyFormValues has already
// taken out the blanks). Any other string is left for schema validation,
// which refuses it. A Timestamp column takes the zone-less date and time
// an <input type=datetime-local> submits as UTC (zonelessTimestamp).
//
// An integer column can never be bound from a value that lost precision
// on its way in:
//
//   - json.Number: integer literals become exact int64; other literals
//     fall through to the float rule.
//   - float64/float32: exactly-representable integral values become
//     int64 (also the bind type every dialect wants for an INTEGER
//     column); an integral value at or beyond ±2^53 is REFUSED — crud
//     cannot distinguish a legitimately-round number from one an
//     upstream decode rounded, and silently storing the rounded value is
//     the bug. Non-integral values are left for schema validation, which
//     already refuses them.
//
// An Int column takes the decimal text validation accepts as the int64
// it spells, as the multipart path's coerceFormValue does.
//
// Runs after Before-hooks (so hook-injected values are covered) and
// before schema validation.
func (ch *CrudHandler) coerceNumberColumnValues(body map[string]any) error {
	return ch.coerceColumnValues(body, false)
}

// coerceBeforeHooks is the pass a write body takes before its Before
// hooks run. A typed hook decodes the body into its struct, so a form's
// "" or "2" for an int64 field, or "true" for a bool, failed that decode
// with a 400 before the cleanup that reads them ran. It drops the blanks
// and converts what it can, and it refuses nothing: a value it cannot
// read stays as sent, so a hook that rewrites one (a slug resolved to an
// id) still can. The pass after the hooks refuses whatever is left. Both
// steps are idempotent, so that second pass only touches values a hook
// added or changed.
func (ch *CrudHandler) coerceBeforeHooks(s schema.Schema, body map[string]any, update bool) (cleared []string) {
	cleared = dropEmptyFormValues(s, body, update)
	_ = ch.coerceColumnValues(body, true) // lenient: never an error
	return cleared
}

// coerceColumnValues is coerceNumberColumnValues. With lenient set, a
// value it would refuse stays in the body unchanged.
func (ch *CrudHandler) coerceColumnValues(body map[string]any, lenient bool) error {
	for _, f := range ch.snapshotFields() {
		raw, ok := body[f.Name]
		if !ok {
			continue
		}
		if f.Type == schema.Bool {
			// Every spelling validation accepts binds as the boolean:
			// SQLite kept "true" as text, which a boolean filter (bound
			// as 1) never matched. Anything else is left for validation.
			if b, ok := boolSpelling(raw); ok {
				body[f.Name] = b
			}
			continue
		}
		if s, isText := raw.(string); isText && f.Type == schema.Float {
			if n, ok := floatText(s); ok {
				body[f.Name] = n
			}
			continue
		}
		if s, isText := raw.(string); isText && f.Type == schema.Timestamp {
			if ts, ok := zonelessTimestamp(s); ok {
				body[f.Name] = ts
			}
			continue
		}
		if f.Type == schema.Relation && ch.Entity != nil && entity.RelationHoldsInt(ch.Registry, ch.Entity, f.Name) {
			// A reference to an integer key is stored in canonical
			// decimal, so a TEXT column on SQLite never holds "01" for
			// key 1: the canonical id binds 1, which TEXT affinity
			// compares as "1".
			if raw == nil || raw == "" {
				continue
			}
			s, ok := relationIntText(raw)
			if !ok {
				if lenient {
					continue
				}
				return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: schema.ReasonNotInteger}}}}
			}
			body[f.Name] = s
			continue
		}
		if n, isNum := raw.(json.Number); isNum && f.Type != schema.Int && f.Type != schema.JSON {
			// Only a JSON field stores a number's text. Elsewhere it is
			// the float64 it always was, for validation to judge.
			fl, err := n.Float64()
			if err != nil {
				if lenient {
					continue
				}
				return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: schema.ReasonNotFinite}}}}
			}
			body[f.Name] = fl
			continue
		}
		if f.Type != schema.Int {
			continue
		}
		switch x := raw.(type) {
		case json.Number:
			if i, err := x.Int64(); err == nil {
				body[f.Name] = i
				continue
			}
			fl, err := x.Float64()
			// The digits decide, not the float64 they round to:
			// 1.0000000000000001 reads as exactly 1.
			if err != nil || !integerText(x.String()) {
				if lenient {
					continue
				}
				return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: schema.ReasonNotInteger}}}}
			}
			if i, ok := exactFloatInt64(fl); ok {
				body[f.Name] = i
				continue
			}
			if fl == math.Trunc(fl) && !lenient {
				return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: reasonBeyondPrecision}}}}
			}
		case float64:
			if i, ok := exactFloatInt64(x); ok {
				body[f.Name] = i
				continue
			}
			if x == math.Trunc(x) && !lenient {
				return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: reasonBeyondPrecision}}}}
			}
		case float32:
			if i, ok := exactFloatInt64(float64(x)); ok {
				body[f.Name] = i
				continue
			}
			if float64(x) == math.Trunc(float64(x)) && !lenient {
				return &ValidationError{fields: map[string][]schema.Violation{f.Name: {{Reason: reasonBeyondPrecision}}}}
			}
		case string:
			// A form posts the number as text. The spelling validation's
			// toInt64 accepts binds as the int64 it names.
			if i, err := strconv.ParseInt(x, 10, 64); err == nil {
				body[f.Name] = i
			}
		default:
			// int64 already exact; anything else is validation's call.
		}
	}
	return nil
}
