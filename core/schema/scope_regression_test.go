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

// wideBoundColumn holds every value the bound tests below probe, so the
// bound decides rather than the default DECIMAL(19,4) column.
const wideBoundColumn = "NUMERIC(60,20)"

// An integral bound written as a short decimal is that decimal, not its
// binary expansion: Min 1e30 was 1000000000000000019884624838656, so
// "1e30" itself failed the minimum (C22).
func TestDecimalShortIntegralBoundAsWritten(t *testing.T) {
	b := 1e30
	if e := Validate(Field{Type: Decimal, RawType: wideBoundColumn, Min: &b, Max: &b}, "1e30"); e != nil {
		t.Fatal(e)
	}
	if Validate(Field{Type: Decimal, RawType: wideBoundColumn, Max: &b}, "1000000000000000000000000000001") == nil {
		t.Fatal("accepted a value above Max 1e30")
	}
	// Fifteen digits are still as written; the exponent is not a digit.
	wide := 1.23456789012345e20
	if e := Validate(Field{Type: Decimal, RawType: wideBoundColumn, Min: &wide, Max: &wide}, "123456789012345000000"); e != nil {
		t.Fatal(e)
	}
	// A fraction keeps its shortest spelling at any length.
	sum := 0.1
	sum += 0.2
	if e := Validate(Field{Type: Decimal, RawType: wideBoundColumn, Min: &sum, Max: &sum}, "0.30000000000000004"); e != nil {
		t.Fatal(e)
	}
}

func TestDecimalIntegralBoundPreservesExactInteger(t *testing.T) {
	b := math.Exp2(60)
	if Validate(Field{Type: Decimal, RawType: wideBoundColumn, Max: &b}, "1152921504606846977") == nil {
		t.Fatal("accepted amount above exact integer cap")
	}
	if e := Validate(Field{Type: Decimal, RawType: wideBoundColumn, Min: &b, Max: &b}, "1152921504606846976"); e != nil {
		t.Fatal(e)
	}
}
