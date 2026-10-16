package schema

import (
	"encoding/json"
	"math"
	"testing"
)

func TestJSONContainersMustBeSerializable(t *testing.T) {
	for _, v := range []any{map[string]any{"x": math.NaN()}, []any{make(chan int)}} {
		if Validate(Field{Type: JSON}, v) == nil {
			t.Errorf("accepted invalid JSON container: %T", v)
		}
	}
}
func TestIntegerImpossibleBounds(t *testing.T) {
	min, max := math.Exp2(63), math.Inf(-1)
	for _, f := range []Field{{Type: Int, Min: &min}, {Type: Int, Max: &max}} {
		for _, v := range []int64{math.MinInt64, math.MaxInt64} {
			if Validate(f, v) == nil {
				t.Errorf("accepted %d with impossible bound", v)
			}
		}
	}
}
func TestDecimalHumanReadableBounds(t *testing.T) {
	b := 0.1
	if err := Validate(Field{Type: Decimal, Min: &b, Max: &b}, "0.1"); err != nil {
		t.Fatal(err)
	}
}
func TestJSONNumberIntegersKeepPrecision(t *testing.T) {
	if err := Validate(Field{Type: Int}, json.Number("9007199254740993")); err != nil {
		t.Fatal(err)
	}
}
func TestFractionalStringBoundsSchema(t *testing.T) {
	b := 1.5
	p := fieldToJSONSchema(Field{Type: String, Min: &b})
	if p["minLength"] != 2 {
		t.Fatalf("minLength=%v want 2", p["minLength"])
	}
}

func TestWireAliasJSONSchema(t *testing.T) {
	got := JSONSchema([]Field{{Name: "db_name", WireName: "name", Required: true}})
	if _, ok := got["properties"].(map[string]any)["name"]; !ok {
		t.Fatal(got)
	}
}
func TestEnumJSONSchemaOwnsValues(t *testing.T) {
	v := []string{"a"}
	p := fieldToJSONSchema(Field{Type: Enum, Values: v})
	p["enum"].([]string)[0] = "b"
	if v[0] != "a" {
		t.Fatal("schema mutated source")
	}
}
func TestDecimalIntegralBoundPreservesExactInteger(t *testing.T) {
	b := math.Exp2(60)
	if Validate(Field{Type: Decimal, Max: &b}, "1152921504606846977") == nil {
		t.Fatal("accepted amount above exact integer cap")
	}
	if e := Validate(Field{Type: Decimal, Min: &b, Max: &b}, "1152921504606846976"); e != nil {
		t.Fatal(e)
	}
}
