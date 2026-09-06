package mcp

import (
	"fmt"
	"math"
	"reflect"
	"sort"
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
		return "number"
	default:
		return reflect.TypeOf(v).String()
	}
}

func validateType(t any, value any, path string) error {
	var allowed []string
	switch tv := t.(type) {
	case string:
		allowed = []string{tv}
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
			af, aok := toFloat(a)
			bf, bok := toFloat(b)
			return aok && bok && af == bf
		}
		return false
	}
	return reflect.DeepEqual(a, b)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
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
