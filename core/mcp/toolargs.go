package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func validateToolArgs(schema map[string]any, args map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	return validateValue(schema, any(args), "arguments")
}

func validateValue(node map[string]any, value any, path string) error {
	if len(node) == 0 {
		return nil
	}

	if t, ok := node["type"]; ok {
		if err := validateType(t, value, path); err != nil {
			return err
		}
	}

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
		if integralNumber(n.String()) {
			return "integer"
		}
		return "number"
	case float64:
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return "integer"
		}
		return "number"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
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

func integralNumber(s string) bool {
	mant, exp, _ := strings.Cut(strings.ToLower(s), "e")
	whole, frac, _ := strings.Cut(strings.TrimLeft(mant, "+-"), ".")
	digits := whole + frac
	if strings.Trim(digits, "0") == "" {
		return true
	}
	scale := len(digits) - len(strings.TrimRight(digits, "0")) - len(frac)
	if exp == "" {
		return scale >= 0
	}
	e, err := strconv.Atoi(exp)
	if err != nil || e > 1<<30 || e < -(1<<30) {
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
				continue
			}
			allowed = append(allowed, s)
		}
	default:
		return nil
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
		return nil
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

type decimal struct {
	neg    bool
	digits string
	exp    *big.Int
}

func (d decimal) equal(o decimal) bool {
	return d.neg == o.neg && d.digits == o.digits && d.exp.Cmp(o.exp) == 0
}

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
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return parseDecimal(strconv.FormatInt(rv.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return parseDecimal(strconv.FormatUint(rv.Uint(), 10))
	}
	return decimal{}, false
}

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
		return decimal{exp: new(big.Int)}, true
	}
	exp.Add(exp, big.NewInt(int64(len(whole)-(len(all)-len(trimmed)))))
	return decimal{neg: neg, digits: digits, exp: exp}, true
}

func validateObject(node map[string]any, obj map[string]any, path string) error {
	props, _ := node["properties"].(map[string]any)

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

	for _, name := range slices.Sorted(maps.Keys(props)) {
		sub := props[name]
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

	if ap, ok := node["additionalProperties"]; ok {
		switch apv := ap.(type) {
		case bool:
			if !apv {
				for _, name := range slices.Sorted(maps.Keys(obj)) {
					if _, declared := props[name]; !declared {
						return fmt.Errorf("%s: unknown argument %q (schema declares additionalProperties: false)", path, name)
					}
				}
			}
		case map[string]any:
			for _, name := range slices.Sorted(maps.Keys(obj)) {
				val := obj[name]
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
		return nil
	}
	for i, el := range arr {
		if err := validateValue(items, el, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}
