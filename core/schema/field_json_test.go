package schema

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// A schema.Field is declarative data (name, type, bounds, default) and
// encodes as JSON: entity.Endpoint publishes []Field under "inputSchema" /
// "outputSchema". Mask is the one member with no JSON form, and it is left
// out of the encoding whether or not it is set.
//
// The bug this holds: Mask was added without a `json:"-"` tag, and
// encoding/json refuses a func-typed struct field even when it is nil, so
// every Field stopped encoding.
func TestFieldEncodesAsJSON(t *testing.T) {
	mask := func(context.Context, any) any { return "****" }
	for _, v := range []any{
		Field{Name: "title", Type: String, Required: true},
		[]Field{{Name: "rank", Type: Int}},
		Schema{Fields: []Field{{Name: "rank", Type: Int}}},
		Field{Name: "card", Type: String, Mask: mask},
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Errorf("json.Marshal(%T) = %v; a Field must encode", v, err)
			continue
		}
		if len(b) == 0 {
			t.Errorf("json.Marshal(%T) wrote nothing", v)
		}
		if strings.Contains(string(b), "Mask") {
			t.Errorf("json.Marshal(%T) = %s; Mask has no JSON form and must be left out", v, b)
		}
	}
}

// Every member of Field that encoding/json cannot encode carries
// `json:"-"`, so the next func-typed member cannot break the encoding the
// way Mask did.
func TestFieldMembersWithNoJSONFormAreTagged(t *testing.T) {
	ft := reflect.TypeFor[Field]()
	for i := range ft.NumField() {
		f := ft.Field(i)
		switch f.Type.Kind() {
		case reflect.Func, reflect.Chan, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
			if f.Tag.Get("json") != "-" {
				t.Errorf("Field.%s is a %s with no `json:\"-\"` tag: json.Marshal refuses every Field", f.Name, f.Type.Kind())
			}
		}
	}
}
