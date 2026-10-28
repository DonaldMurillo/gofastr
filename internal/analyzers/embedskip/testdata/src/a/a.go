// Package a holds the embedskip fixture reduced from the real bug site:
// framework/typed_hooks.go mergeStructIntoMap before the 2026-10-26 fix,
// which dropped every field promoted from an embedded struct.
package a

import (
	"fmt"
	"reflect"
	"strings"
)

func jsonFieldName(f reflect.StructField) (string, bool) {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name, name == "-"
}

// Pre-fix mergeStructIntoMap: the key comes from a helper reading the tag.
func mergeStructIntoMap(av reflect.Value, dest map[string]any) {
	t := av.Type()
	for i := 0; i < t.NumField(); i++ { // want `struct field walk keyed by tag skips fields promoted from an embedded struct`
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name, skip := jsonFieldName(f)
		if skip {
			continue
		}
		dest[name] = av.Field(i).Interface()
	}
}

// Pre-fix di.Inject shape: the tag is read inline, range-over-int form.
func inject(ev reflect.Value) int {
	et := ev.Type()
	n := 0
	for i := range et.NumField() { // want `struct field walk keyed by tag skips fields promoted from an embedded struct`
		if _, ok := et.Field(i).Tag.Lookup("inject"); ok {
			n++
		}
	}
	return n
}

// The Go 1.26 iterator spelling of the same top-level walk.
func queryTags(t reflect.Type) []string {
	var out []string
	for f := range t.Fields() { // want `struct field walk keyed by tag skips fields promoted from an embedded struct`
		out = append(out, f.Tag.Get("query"))
	}
	return out
}

// The iterator spelling with the fix.
func queryTagsFixed(t reflect.Type) []string {
	var out []string
	for f := range t.Fields() {
		if f.Anonymous {
			out = append(out, queryTagsFixed(f.Type)...)
			continue
		}
		out = append(out, f.Tag.Get("query"))
	}
	return out
}

// The fix: an anonymous field descends.
func mergeFixed(t reflect.Type, dest map[string]any) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Tag.Get("json") == "" {
			mergeFixed(f.Type, dest)
			continue
		}
		dest[f.Tag.Get("json")] = nil
	}
}

// A walk that descends into every struct-kind field reaches embedded ones.
func scrub(v reflect.Value) {
	t := v.Type()
	for i := range t.NumField() {
		if t.Field(i).Tag.Get("json") == "url" {
			continue
		}
		if v.Field(i).Kind() == reflect.Struct {
			scrub(v.Field(i))
		}
	}
}

// No tag and no field passed on: the walk is not mapping fields to keys.
func count(t reflect.Type) int {
	n := 0
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			n++
		}
	}
	return n
}

// A StructField handed to another package's function is not a key
// mapping this package wrote.
func describe(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		out = append(out, fmt.Sprint(t.Field(i)))
	}
	return out
}

// The marker is how a deliberate top-level-only walk says so.
func topLevel(t reflect.Type) []string {
	var out []string
	//gofastr:allow(embedskip) the fixture's deliberate top-level-only walk
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Tag.Get("col"))
	}
	return out
}

// I2: the bound on the left of the condition is the same top-level walk.
func leftBound(t reflect.Type) []string {
	var out []string
	for i := 0; t.NumField() > i; i++ { // want `struct field walk keyed by tag skips fields promoted from an embedded struct`
		out = append(out, t.Field(i).Tag.Get("json"))
	}
	return out
}

// The left-bound spelling with the fix stays quiet.
func leftBoundFixed(t reflect.Type) []string {
	var out []string
	for i := 0; t.NumField() > i; i++ {
		f := t.Field(i)
		if f.Anonymous {
			out = append(out, leftBoundFixed(f.Type)...)
			continue
		}
		out = append(out, f.Tag.Get("json"))
	}
	return out
}

// A condition that names NumField on neither side is no field walk.
func notAWalk(t reflect.Type, n int) []string {
	var out []string
	for i := 0; n > i; i++ {
		out = append(out, t.Field(i).Tag.Get("json"))
	}
	return out
}
