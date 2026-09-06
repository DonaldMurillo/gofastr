package crud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
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
//     normalizeJSONNumbers). A literal that is neither an exact integer
//     nor a parseable float (1e999) is refused, matching stdlib.
//
//   - bind: a float64 that REACHES an Int column through some other route
//     (a host json.Unmarshal before CreateOne, the MCP argument bridge)
//     cannot be trusted above 2^53 — the value may have been rounded on
//     its way in and crud cannot tell. coerceIntColumnValues converts
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
// int64 when the literal is an integer, float64 otherwise. A number that
// is neither (1e999) returns an error so the caller can refuse the body,
// the same answer stdlib's float64 decode would have given.
func normalizeJSONNumbers(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			norm, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			x[k] = norm
		}
		return x, nil
	case []any:
		for i, item := range x {
			norm, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			x[i] = norm
		}
		return x, nil
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		fl, err := x.Float64()
		if err != nil {
			return nil, fmt.Errorf("number %s is out of range", x.String())
		}
		return fl, nil
	default:
		return v, nil
	}
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
	if err := handler.CheckObjectKeys(data, strings.ToLower); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		return nil, handler.Errorf(400, "invalid JSON: %s", err.Error())
	}
	norm, err := normalizeJSONNumbers(body)
	if err != nil {
		return nil, handler.Errorf(400, "invalid JSON: %s", err.Error())
	}
	return norm.(map[string]any), nil
}

// redecodeUseNumber re-decodes a body that already passed the strict
// struct decode (gate) into a second instance of the same type with
// UseNumber + number normalization, so integer literals inside it keep
// their exact values. Used by the batch endpoints, whose envelope decode
// is struct-shaped and therefore float64-based.
func redecodeUseNumber(data []byte, v any) error {
	//gofastr:allow(GOFASTR1407) the strict struct decode already gated this body (see doc comment); this re-decode only preserves integer precision via UseNumber.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return handler.Errorf(400, "invalid JSON: %s", err.Error())
	}
	_, err := normalizeJSONNumbers(v)
	return err
}

// coerceIntColumnValues normalizes the Int-typed columns present in a
// write body so an integer column can never be bound from a value that
// lost precision on its way in:
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
// Runs after Before-hooks (so hook-injected values are covered) and
// before schema validation.
func (ch *CrudHandler) coerceIntColumnValues(body map[string]any) error {
	for _, f := range ch.snapshotFields() {
		if f.Type != schema.Int {
			continue
		}
		raw, ok := body[f.Name]
		if !ok {
			continue
		}
		switch x := raw.(type) {
		case json.Number:
			if i, err := x.Int64(); err == nil {
				body[f.Name] = i
				continue
			}
			fl, err := x.Float64()
			if err != nil {
				return &ValidationError{fields: map[string][]string{f.Name: {"must be an integer"}}}
			}
			if i, ok := exactFloatInt64(fl); ok {
				body[f.Name] = i
				continue
			}
			if fl == math.Trunc(fl) {
				return &ValidationError{fields: map[string][]string{f.Name: {
					"number is beyond exact float64 precision; send the integer as a JSON integer literal or a string",
				}}}
			}
		case float64:
			if i, ok := exactFloatInt64(x); ok {
				body[f.Name] = i
				continue
			}
			if x == math.Trunc(x) {
				return &ValidationError{fields: map[string][]string{f.Name: {
					"number is beyond exact float64 precision; send the integer as a JSON integer literal or a string",
				}}}
			}
		case float32:
			if i, ok := exactFloatInt64(float64(x)); ok {
				body[f.Name] = i
				continue
			}
			if float64(x) == math.Trunc(float64(x)) {
				return &ValidationError{fields: map[string][]string{f.Name: {
					"number is beyond exact float64 precision; send the integer as a JSON integer literal or a string",
				}}}
			}
		default:
			// int64 already exact; strings bind by affinity / parse in
			// validation; anything else is validation's call.
		}
	}
	return nil
}
