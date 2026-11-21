package schema

import (
	"encoding/json"
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

func TestSweepSchemaRequiredEmptyAnswersRequired(t *testing.T) {
	for _, ft := range []struct {
		name string
		typ  FieldType
	}{
		{"string", String}, {"text", Text}, {"int", Int}, {"float", Float},
		{"decimal", Decimal}, {"bool", Bool}, {"enum", Enum}, {"uuid", UUID},
		{"timestamp", Timestamp}, {"date", Date}, {"json", JSON},
		{"relation", Relation}, {"image", Image}, {"file", File},
	} {
		f := Field{Name: "f", Type: ft.typ, Required: true}
		if ft.typ == Enum {
			f.Values = []string{"a", "b"}
		}
		err := Validate(f, "")
		if err == nil {
			t.Errorf("%s: required empty string accepted", ft.name)
			continue
		}
		if got := AsViolation(err).Reason.Code(); got != ReasonRequired.Code() {
			t.Errorf("%s: required empty string answers %s, want required", ft.name, got)
		}
	}
}

func TestSweepSchemaRequiredEmptyStringerAnswersRequired(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  FieldType
	}{
		{"string", String}, {"text", Text}, {"decimal", Decimal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Field{Name: "f", Type: tc.typ, Required: true}
			err := Validate(f, json.Number(""))
			if err == nil {
				t.Fatal("required empty Stringer accepted")
			}
			if got := AsViolation(err).Code(); got != "required" {
				t.Errorf("required empty Stringer answers %q, want required", got)
			}
		})
	}
}

func TestSweepSchemaRequiredEmptyUnsupportedType(t *testing.T) {
	const unsupported = FieldType(999)
	f := Field{Name: "f", Type: unsupported, Required: true}
	err := Validate(f, "")
	if err == nil {
		t.Fatal("unsupported field type accepted")
	}
	v := AsViolation(err)
	if v.Code() != "unsupported_type" || v.Params["type"] != int(unsupported) {
		t.Errorf("unsupported field type answers %q with params %v, want unsupported_type with type=%d", v.Code(), v.Params, unsupported)
	}
}
