package schema

import (
	"math"
	"testing"
)

func TestLengthBoundClampsToInt32(t *testing.T) {
	cases := []struct {
		v     float64
		round func(float64) float64
		want  int
		ok    bool
	}{
		{1e30, math.Floor, math.MaxInt32, true},
		{math.Inf(1), math.Floor, math.MaxInt32, true},
		{-5, math.Ceil, 0, true},
		{3.2, math.Ceil, 4, true},
		{3.8, math.Floor, 3, true},
		{math.NaN(), math.Floor, 0, false},
	}
	for _, c := range cases {
		got, ok := LengthBound(c.v, c.round)
		if got != c.want || ok != c.ok {
			t.Errorf("LengthBound(%v) = %d, %v; want %d, %v", c.v, got, ok, c.want, c.ok)
		}
	}
}

func TestVarcharWidthFallsBackToText(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		max  *float64
		want int
		ok   bool
	}{
		{nil, 0, false},
		{f(255), 255, true},
		{f(255.9), 255, true},
		{f(0.5), 0, false},
		{f(MaxVarcharWidth), MaxVarcharWidth, true},
		{f(MaxVarcharWidth + 1), 0, false},
		{f(1e30), 0, false},
		{f(math.NaN()), 0, false},
	}
	for _, c := range cases {
		got, ok := VarcharWidth(c.max)
		if got != c.want || ok != c.ok {
			t.Errorf("VarcharWidth(%v) = %d, %v; want %d, %v", c.max, got, ok, c.want, c.ok)
		}
	}
}

func TestJSONSchemaHugeMaxLength(t *testing.T) {
	huge := 1e30
	for _, typ := range []FieldType{String, Text} {
		prop := fieldToJSONSchema(Field{Name: "s", Type: typ, Min: &huge, Max: &huge})
		if prop["maxLength"] != math.MaxInt32 || prop["minLength"] != math.MaxInt32 {
			t.Errorf("%v: minLength %v maxLength %v, want both clamped to MaxInt32", typ, prop["minLength"], prop["maxLength"])
		}
	}
}
