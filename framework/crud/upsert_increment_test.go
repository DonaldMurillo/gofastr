package crud

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The upsert's auto-increment key handling decides, per JSON-decoded
// shape, whether the caller named a real primary key (which must reach
// the INSERT so ON CONFLICT can target it) or asked the database to
// assign one; each spelling of "absent or zero" and each numeric shape
// is pinned here.
func TestCallerSuppliedIncrementShapes(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want bool
	}{
		{"nil", nil, false},
		{"int zero", 0, false},
		{"int32 zero", int32(0), false},
		{"int64 zero", int64(0), false},
		{"float zero", float64(0), false},
		{"json.Number zero", json.Number("0"), false},
		{"empty string", "", false},
		{"string zero", "0", false},
		{"int", 7, true},
		{"int32", int32(7), true},
		{"int64", int64(7), true},
		{"float", float64(7), true},
		{"json.Number", json.Number("7"), true},
		{"string", "7", true},
		{"other type", true, true},
	}
	for _, c := range cases {
		if got := callerSuppliedIncrement(c.v); got != c.want {
			t.Errorf("%s: callerSuppliedIncrement(%v) = %v, want %v", c.name, c.v, got, c.want)
		}
	}
}

// Every numeric spelling binds as int64 so a Postgres SERIAL column
// receives an integer, and a non-numeric value passes through untouched
// for the driver to refuse.
func TestIncrementBindValueCoercion(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{7, int64(7)},
		{int32(7), int64(7)},
		{float64(7), int64(7)},
		{json.Number("7"), int64(7)},
		{json.Number("x"), json.Number("x")},
		{"7", "7"},
	}
	for _, c := range cases {
		if got := incrementBindValue(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("incrementBindValue(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// The MCP list tool forwards a JSON array as one query entry per element
// and everything else as its single printed spelling.
func TestToolParamValuesShapes(t *testing.T) {
	if got := toolParamValues([]any{"draft", 2, true}); !reflect.DeepEqual(got, []string{"draft", "2", "true"}) {
		t.Errorf("array: %v", got)
	}
	if got := toolParamValues("draft"); !reflect.DeepEqual(got, []string{"draft"}) {
		t.Errorf("scalar: %v", got)
	}
	if got := toolParamValues(3.5); !reflect.DeepEqual(got, []string{"3.5"}) {
		t.Errorf("number: %v", got)
	}
}
