package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Minimal JSON Schema validation for tools/call arguments, enforced at
// the dispatch chokepoint (callTool) before any handler runs. The
// declared inputSchema is the contract tools/list advertises; until the
// 2026-09-04 red-probe round it was purely descriptive, and every
// handler received attacker-typed JSON (float64-for-int,
// nested-object-for-scalar) it had to defend against itself.
//
// This is deliberately NOT a full JSON Schema implementation. It
// supports exactly the keywords the repo's declared schemas use —
// type, enum, properties, required, additionalProperties, items — plus
// recursive descent, per the maintainer's 2026-09-04 decision. Bounds
// keywords (minimum, maximum, minLength, maxLength) appear in some
// declared schemas but are NOT enforced here; handlers that document
// clamp semantics (framework_docs_search's limit) keep them. No
// third-party validator dependency: unknown keywords are ignored, and
// a schema that declares nothing validates anything (the handler stays
// responsible, as before).
//
// additionalProperties follows JSON Schema semantics, which is also the
// convention every schema in this repo is authored under: explicit
// `false` closes the object (unknown arguments are refused with
// invalid-params), an explicit subschema validates them, and ABSENT
// means extras are allowed. Schemas that mean closed say so — the
// harness builtins and kiln descriptors spell `additionalProperties:
// false` when they want it, and `additionalProperties: {type: string}`
// (bash env, webfetch headers, kiln stringMap) when they want an open
// map.

// validateToolArgs validates a tools/call arguments object against a
// tool's declared inputSchema. It returns nil when the arguments
// conform, or an error describing the first violation. A nil or empty
// schema validates anything: nothing was declared, so there is nothing
// to enforce.
func validateToolArgs(schema map[string]any, args map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	return validateValue(schema, any(args), "arguments")
}

// validateValue checks one decoded-JSON value against one schema node.
// path names the value for the error message.
func validateValue(node map[string]any, value any, path string) error {
	if len(node) == 0 {
		return nil
	}

	// type: a string or a list of strings; the value must match one.
	if t, ok := node["type"]; ok {
		if err := validateType(t, value, path); err != nil {
			return err
		}
	}

	// enum: the value must equal one of the declared constants.
	if e, ok := node["enum"]; ok {
		if err := validateEnum(e, value, path); err != nil {
			return err
		}
	}

	switch v := value.(type) {
	case map[string]any:
		return validateObject(node, v, path)
	case []any:
		return validateArray(node, v, path)
	}
	return nil
}

// jsonTypeOf maps a decoded-JSON value to its JSON Schema type name.
func jsonTypeOf(v any) string {
	switch n := v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	case json.Number:
		// The wire decode uses UseNumber (tools.go), so a numeric
		// argument arrives as json.Number to keep its exact decimal
		// above 2^53, or a fraction float64 would round to an integer.
		// Its digits decide: 9007199254740993e0 and 9007199254740993.0
		// are integers, 9007199254740993.5 is not.
		if integralNumber(n.String()) {
			return "integer"
		}
		return "number"
	case float64:
		// JSON integers decode as float64; a value with no fractional
		// part satisfies both "number" and "integer" (draft-06+).
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return "integer"
		}
		return "number"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		// In-process callers (Server.CallTool, host tests) pass Go
		// natives; the wire path only ever sees float64.
		return "integer"
	case float32:
		if f := float64(n); f == math.Trunc(f) && !math.IsInf(f, 0) {
			return "integer"
		}
		return "number"
	default:
		return reflect.TypeOf(v).String()
	}
}

// integralNumber reports whether s, a JSON number literal, names an
// integer. It counts digits instead of parsing: a float64 rounds
// 1.0000000000000001 to 1, and big.Rat would build the 10^999999999 an
// attacker's 1e-999999999 asks for. The value is digits × 10^(exp −
// len(frac)), and each trailing zero of the digits adds one back.
func integralNumber(s string) bool {
	mant, exp, _ := strings.Cut(strings.ToLower(s), "e")
	whole, frac, _ := strings.Cut(strings.TrimLeft(mant, "+-"), ".")
	digits := whole + frac
	if strings.Trim(digits, "0") == "" {
		return true // zero, at any exponent
	}
	scale := len(digits) - len(strings.TrimRight(digits, "0")) - len(frac)
	if exp == "" {
		return scale >= 0
	}
	e, err := strconv.Atoi(exp)
	if err != nil || e > 1<<30 || e < -(1<<30) {
		// An exponent this size outweighs any literal's digits: a
		// positive one leaves an integer, a negative one a fraction.
		return !strings.HasPrefix(exp, "-")
	}
	return e+scale >= 0
}

func validateType(t any, value any, path string) error {
	var allowed []string
	switch tv := t.(type) {
	case string:
		allowed = []string{tv}
	case []string:
		allowed = tv
	case []any:
		for _, e := range tv {
			s, ok := e.(string)
			if !ok {
				continue // malformed schema: ignore that entry
			}
			allowed = append(allowed, s)
		}
	default:
		return nil // malformed or unsupported type declaration: ignore
	}
	got := jsonTypeOf(value)
	for _, want := range allowed {
		if got == want || (want == "number" && got == "integer") {
			return nil
		}
	}
	return fmt.Errorf("%s: expected type %s, got %s", path, strings.Join(allowed, " or "), got)
}

func validateEnum(e any, value any, path string) error {
	var list []any
	switch ev := e.(type) {
	case []any:
		list = ev
	case []string:
		list = make([]any, len(ev))
		for i, s := range ev {
			list[i] = s
		}
	default:
		return nil // malformed enum: ignore rather than fail every call
	}
	for _, cand := range list {
		if jsonEqual(value, cand) {
			return nil
		}
	}
	return fmt.Errorf("%s: value not in enum", path)
}
func jsonEqual(a, b any) bool {
	if jsonTypeOf(a) == "integer" || jsonTypeOf(a) == "number" {
		if jsonTypeOf(b) == "integer" || jsonTypeOf(b) == "number" {
			ad, aok := decimalOf(a)
			bd, bok := decimalOf(b)
			return aok && bok && ad.equal(bd)
		}
		return false
	}
	// Objects and arrays recurse, so a number inside one compares the way a
	// scalar does: the wire's json.Number or float64 against a declared Go
	// int. Declared members may be typed Go values (map[string]int,
	// []string), hence reflect rather than a []any/map[string]any switch.
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	switch {
	case isJSONArray(av) && isJSONArray(bv):
		if av.Len() != bv.Len() {
			return false
		}
		for i := range av.Len() {
			if !jsonEqual(av.Index(i).Interface(), bv.Index(i).Interface()) {
				return false
			}
		}
		return true
	case isJSONObject(av) && isJSONObject(bv):
		if av.Len() != bv.Len() {
			return false
		}
		for it := av.MapRange(); it.Next(); {
			bval := bv.MapIndex(it.Key().Convert(bv.Type().Key()))
			if !bval.IsValid() || !jsonEqual(it.Value().Interface(), bval.Interface()) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

func isJSONArray(v reflect.Value) bool {
	return v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) && v.Type().Elem().Kind() != reflect.Uint8
}

func isJSONObject(v reflect.Value) bool {
	return v.IsValid() && v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String
}

// decimal is a number's exact value as 0.digits × 10^exp, with no
// leading or trailing zero in digits; zero has no digits. Two numbers
// are equal exactly when their decimals are, so comparison reads text
// and never builds the number: big.Rat.SetString on an argument like
// 1e999999 built a 10^999999 per enum member. Comparing through float64
// was the other wrong answer: it rounded integers above 2^53 onto their
// neighbours, so a neighbour of an enum member matched it.
type decimal struct {
	neg    bool
	digits string
	exp    *big.Int // the exponent's own digits, as many as the text has
}

func (d decimal) equal(o decimal) bool {
	return d.neg == o.neg && d.digits == o.digits && d.exp.Cmp(o.exp) == 0
}

// decimalOf reads v's exact decimal. A float64 holds a dyadic fraction,
// and 767 significant digits spell any of them exactly, so 'e' at that
// precision is the float's true value, not its shortest spelling: the
// float 0.1 is not one tenth.
func decimalOf(v any) (decimal, bool) {
	switch n := v.(type) {
	case json.Number:
		return parseDecimal(n.String())
	case float64:
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return decimal{}, false
		}
		return parseDecimal(strconv.FormatFloat(n, 'e', 767, 64))
	case float32:
		return decimalOf(float64(n))
	}
	// In-process callers pass any Go integer type.
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return parseDecimal(strconv.FormatInt(rv.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return parseDecimal(strconv.FormatUint(rv.Uint(), 10))
	}
	return decimal{}, false
}

// parseDecimal normalizes a JSON number literal. Its cost is the
// literal's length: the exponent stays the digits it was written with.
func parseDecimal(s string) (decimal, bool) {
	mant, expText, hasExp := strings.Cut(strings.ToLower(s), "e")
	neg := strings.HasPrefix(mant, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(mant, "-"), ".")
	all := whole + frac
	if all == "" {
		return decimal{}, false
	}
	exp := new(big.Int)
	if hasExp {
		if _, ok := exp.SetString(expText, 10); !ok {
			return decimal{}, false
		}
	}
	trimmed := strings.TrimLeft(all, "0")
	digits := strings.TrimRight(trimmed, "0")
	if digits == "" {
		return decimal{exp: new(big.Int)}, true // zero, whatever its sign or exponent
	}
	// 0.digits × 10^exp: the point moves to just before the first
	// nonzero digit, which sits len(whole) − leading zeros places in.
	exp.Add(exp, big.NewInt(int64(len(whole)-(len(all)-len(trimmed)))))
	return decimal{neg: neg, digits: digits, exp: exp}, true
}

func validateObject(node map[string]any, obj map[string]any, path string) error {
	props, _ := node["properties"].(map[string]any)

	// required: every named key must be present. Declared schemas hold
	// either a Go []string literal (in-process registration) or []any
	// (JSON round-trip); both spell the same contract.
	var reqNames []string
	switch req := node["required"].(type) {
	case []string:
		reqNames = req
	case []any:
		for _, r := range req {
			if s, ok := r.(string); ok {
				reqNames = append(reqNames, s)
			}
		}
	}
	if len(reqNames) > 0 {
		missing := make([]string, 0, len(reqNames))
		for _, name := range reqNames {
			if _, present := obj[name]; !present {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("%s: missing required argument(s) %s", path, strings.Join(missing, ", "))
		}
	}

	// properties: present keys must conform to their declared subschema.
	for name, sub := range props {
		val, present := obj[name]
		if !present {
			continue
		}
		subSchema, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if err := validateValue(subSchema, val, path+"."+name); err != nil {
			return err
		}
	}

	// additionalProperties: absent = allow extras (JSON Schema default,
	// and the convention this repo's schemas are authored under);
	// explicit false = refuse unknown keys; a subschema = validate them.
	if ap, ok := node["additionalProperties"]; ok {
		switch apv := ap.(type) {
		case bool:
			if !apv {
				for name := range obj {
					if _, declared := props[name]; !declared {
						return fmt.Errorf("%s: unknown argument %q (schema declares additionalProperties: false)", path, name)
					}
				}
			}
		case map[string]any:
			for name, val := range obj {
				if _, declared := props[name]; !declared {
					if err := validateValue(apv, val, path+"."+name); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func validateArray(node map[string]any, arr []any, path string) error {
	raw, present := node["items"]
	if !present {
		return nil
	}
	items, ok := raw.(map[string]any)
	if !ok || len(items) == 0 {
		return nil // malformed items declaration: ignore
	}
	for i, el := range arr {
		if err := validateValue(items, el, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}
