package schema

import "math"

// MaxVarcharWidth is the widest VARCHAR(n) every supported dialect accepts.
const MaxVarcharWidth = 10485760

// LengthBound rounds a String/Text Min or Max with round and clamps it to
// [0, math.MaxInt32]; ok is false for NaN.
func LengthBound(v float64, round func(float64) float64) (n int, ok bool) {
	if math.IsNaN(v) {
		return 0, false
	}
	r := round(v)
	if r <= 0 {
		return 0, true
	}
	if r >= math.MaxInt32 {
		return math.MaxInt32, true
	}
	return int(r), true
}

// VarcharWidth reports the VARCHAR width for a String field's Max, or false
// when the column is TEXT: no Max, under 1, or past MaxVarcharWidth.
func VarcharWidth(max *float64) (int, bool) {
	if max == nil {
		return 0, false
	}
	n, ok := LengthBound(*max, math.Floor)
	if !ok || n < 1 || n > MaxVarcharWidth {
		return 0, false
	}
	return n, true
}
