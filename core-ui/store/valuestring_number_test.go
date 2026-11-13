package store

import (
	"math"
	"testing"
)

// TestValueStringMatchesJSNumber: the SSR stamp must equal what the
// runtime's String(value) paints, or the text changes on the first
// client write. JS goes exponential below 1e-6 and from 1e21.
func TestValueStringMatchesJSNumber(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{1e-7, "1e-7"},
		{1.23e23, "1.23e+23"},
		{1e21, "1e+21"},
		{123456.5, "123456.5"},
		{0.000001, "0.000001"},
		{float32(1e-7), "1e-7"},
		{float32(0.1), "0.1"},
		{-2.5, "-2.5"},
		// JSON keeps negative zero as -0; String(-0) is "0".
		{math.Copysign(0, -1), "0"},
		{math.Float32frombits(1 << 31), "0"},
		{0.0, "0"},
	} {
		if got := valueString(tc.in); got != tc.want {
			t.Errorf("valueString(%v %T) = %q, want %q (JS String)", tc.in, tc.in, got, tc.want)
		}
	}
}

// TestValueStringMatchesJSStringify: a non-scalar stamp must equal the
// runtime's JSON.stringify (no HTML escapes) and a nil one the empty
// string the runtime paints for null.
func TestValueStringMatchesJSStringify(t *testing.T) {
	type pair struct {
		A string `json:"a"`
	}
	for _, tc := range []struct {
		in   any
		want string
	}{
		{[]string{"a<b", "c&d", "e>f"}, `["a<b","c&d","e>f"]`},
		{map[string]string{"k": "<x>"}, `{"k":"<x>"}`},
		{pair{A: "&"}, `{"a":"&"}`},
		{nil, ""},
		{[]string(nil), ""},
		{map[string]int(nil), ""},
		{(*pair)(nil), ""},
	} {
		if got := valueString(tc.in); got != tc.want {
			t.Errorf("valueString(%#v) = %q, want %q (JSON.stringify / null paint)", tc.in, got, tc.want)
		}
	}
}

// TestValueStringJSStringAndKeyOrder: the runtime JSON.parses the seed and
// paints a string raw, so a value that marshals to a JSON string ([]byte,
// *string, a TextMarshaler) stamps its text, not its quoted JSON. An
// object it re-stringifies lists integer-like keys first, ascending, as
// JSON.parse then JSON.stringify does; the rest keep their order.
func TestValueStringJSStringAndKeyOrder(t *testing.T) {
	s := `say "hi" <b>`
	type keyed struct {
		B   int `json:"b"`
		One int `json:"1"`
		A   int `json:"a"`
	}
	for _, tc := range []struct {
		in   any
		want string
	}{
		{&s, `say "hi" <b>`},
		{[]byte("hi"), "aGk="},
		{map[string]int{"10": 1, "9": 2, "a": 3, "01": 4}, `{"9":2,"10":1,"01":4,"a":3}`},
		{keyed{B: 1, One: 2, A: 3}, `{"1":2,"b":1,"a":3}`},
		{[]any{map[string]any{"10": "x", "9": []int{1}}}, `[{"9":[1],"10":"x"}]`},
		// 2^32-1 is no array index: it keeps its place.
		{map[string]int{"!": 0, "4294967295": 1, "7": 2}, `{"7":2,"!":0,"4294967295":1}`},
	} {
		if got := valueString(tc.in); got != tc.want {
			t.Errorf("valueString(%#v) = %q, want %q (runtime paint)", tc.in, got, tc.want)
		}
	}
}
