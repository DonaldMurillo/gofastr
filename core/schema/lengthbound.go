package schema

import "math"

// MaxVarcharWidth is the widest VARCHAR(n) every supported dialect
// accepts: Postgres refuses n past 10485760 at CREATE TABLE.
const MaxVarcharWidth = 10485760

// LengthBound converts a String/Text field's Min or Max to the integer a
// JSON Schema minLength/maxLength (or a column width) takes: rounded
// with round, then clamped to [0, math.MaxInt32]. ok is false for NaN,
// and the caller leaves the keyword out.
//
// A bare int(*f.Max) is implementation-defined for a finite value past
// the int range (MinInt64 on amd64, MaxInt64 on arm64), so a huge Max
// published a negative or nonsense maxLength.
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

// VarcharWidth reports the VARCHAR width for a String field's Max, and
// false when the column should be TEXT instead: no Max, a Max under 1,
// or one wider than MaxVarcharWidth (validation still enforces the Max).
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
