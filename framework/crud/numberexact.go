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

// relationIntText is a reference to an integer key in canonical decimal, or false.
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

// exactFloatInt64 converts an integral float64 strictly inside ±2^53; at or
// past it the float may be another literal's rounding, so it reports false.
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

// normalizeJSONNumbers turns each json.Number into int64 or float64 in place,
// keeping the json.Number when neither holds it exactly.
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
			return x
		}
		if roundsInteger(fl) || (fl == math.Trunc(fl) && !integerText(x.String())) {
			return x
		}
		return fl
	default:
		return v
	}
}

// integerText reports whether a JSON number literal names an integer, from
// its digits, in time linear in its length.
func integerText(s string) bool {
	mant, exp := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mant, exp = s[:i], s[i+1:]
	}
	mant = strings.TrimLeft(mant, "+-")
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimRight(intPart+frac, "0")
	if strings.Trim(digits, "0") == "" {
		return true
	}
	shift := len(intPart+frac) - len(digits) - len(frac)
	if exp == "" {
		return shift >= 0
	}
	e, err := strconv.Atoi(exp)
	if err != nil || e > 1<<30 || e < -(1<<30) {
		return !strings.HasPrefix(exp, "-")
	}
	return e+shift >= 0
}

func roundsInteger(f float64) bool {
	return f == math.Trunc(f) && math.Abs(f) >= 1<<53
}

// decodeStrictUseNumber decodes a JSON object body with UseNumber, refusing
// ambiguous keys at any depth and trailing data.
func decodeStrictUseNumber(data []byte) (map[string]any, error) {
	if err := handler.CheckObjectKeys(data, handler.FoldJSONKey); err != nil {
		return nil, err
	}
	//gofastr:allow(GOFASTR1407) CheckObjectKeys already verified key uniqueness and casing (see doc comment); this decode only preserves integer precision via UseNumber.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		return nil, handler.Fail(http.StatusBadRequest, handler.ReasonInvalidJSON, map[string]any{"detail": handler.JSONErrorDetail(err)})
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, handler.Fail(http.StatusBadRequest, handler.ReasonInvalidJSON, map[string]any{"detail": handler.JSONTrailingData()})
	}
	normalizeJSONNumbers(body)
	return body, nil
}

func normalizeItemNumbers(items []map[string]any) {
	for _, item := range items {
		normalizeJSONNumbers(item)
	}
}

// zonelessTimestamp reads a datetime-local value as UTC, in RFC 3339.
func zonelessTimestamp(s string) (string, bool) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(time.RFC3339Nano), true
		}
	}
	return "", false
}

// floatTextRe refuses hex floats, underscores and Inf/NaN.
var floatTextRe = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

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

// coerceNumberColumnValues converts a write body's values to their column's
// bind type, after the Before hooks and before validation. An Int column
// refuses a float or number it cannot hold exactly (reasonBeyondPrecision).
func (ch *CrudHandler) coerceNumberColumnValues(body map[string]any) error {
	return ch.coerceColumnValues(body, false)
}

// coerceBeforeHooks drops blank form values and converts what it can before
// the Before hooks, refusing nothing.
func (ch *CrudHandler) coerceBeforeHooks(s schema.Schema, body map[string]any, update bool) (cleared []string) {
	cleared = dropEmptyFormValues(s, body, update)
	_ = ch.coerceColumnValues(body, true)
	return cleared
}

func (ch *CrudHandler) coerceColumnValues(body map[string]any, lenient bool) error {
	for _, f := range ch.snapshotFields() {
		raw, ok := body[f.Name]
		if !ok {
			continue
		}
		if f.Type == schema.Bool {
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
			if i, err := strconv.ParseInt(x, 10, 64); err == nil {
				body[f.Name] = i
			}
		default:
		}
	}
	return nil
}
