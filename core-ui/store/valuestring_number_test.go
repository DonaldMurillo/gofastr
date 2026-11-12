package store

import "testing"

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
	} {
		if got := valueString(tc.in); got != tc.want {
			t.Errorf("valueString(%v %T) = %q, want %q (JS String)", tc.in, tc.in, got, tc.want)
		}
	}
}
