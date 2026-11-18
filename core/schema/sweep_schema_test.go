package schema

import (
	"math"
	"testing"
)

func TestSweepSchemaIntMinComparesInIntegerSpace(t *testing.T) {
	min := math.Exp2(60)
	f := Field{Name: "n", Type: Int, Min: &min}
	if Validate(f, int64(1<<60)-1) == nil {
		t.Fatal("2^60-1 passed Min 2^60")
	}
	if err := Validate(f, int64(1<<60)); err != nil {
		t.Fatalf("2^60 refused under Min 2^60: %v", err)
	}
}

func TestSweepSchemaBoolAcceptsInt64ZeroOne(t *testing.T) {
	f := Field{Name: "b", Type: Bool}
	for _, v := range []int64{0, 1} {
		if err := Validate(f, v); err != nil {
			t.Errorf("int64 %d refused: %v", v, err)
		}
	}
	if Validate(f, int64(2)) == nil {
		t.Error("int64 2 accepted")
	}
}

func TestSweepSchemaLengthBoundClampsPastInt32(t *testing.T) {
	if n, ok := LengthBound(3e9, math.Floor); n != math.MaxInt32 || !ok {
		t.Fatalf("LengthBound(3e9) = %d, %v; want MaxInt32, true", n, ok)
	}
}

func TestSweepSchemaJSONFieldHasNoTypeConstraint(t *testing.T) {
	prop := fieldToJSONSchema(Field{Name: "j", Type: JSON})
	if _, ok := prop["type"]; ok {
		t.Fatalf("JSON field schema has a type: %v", prop)
	}
}

func TestSweepSchemaUUIDPatternMatchesValidator(t *testing.T) {
	prop := fieldToJSONSchema(Field{Name: "id", Type: UUID})
	if prop["pattern"] != uuidRe.String() {
		t.Fatalf("UUID pattern %v, validator %v", prop["pattern"], uuidRe.String())
	}
}
