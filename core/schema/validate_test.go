package schema

import (
	"encoding/json"
	"testing"
)

// helper to create a float64 pointer
func fPtr(v float64) *float64 { return &v }

// ---------- String ----------

func TestValidateString_Valid(t *testing.T) {
	f := Field{Name: "name", Type: String, Required: true}
	if err := Validate(f, "hello"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateString_EmptyRequired(t *testing.T) {
	f := Field{Name: "name", Type: String, Required: true}
	if err := Validate(f, ""); err == nil {
		t.Fatal("expected error for empty required string")
	}
}

func TestValidateString_NilRequired(t *testing.T) {
	f := Field{Name: "name", Type: String, Required: true}
	if err := Validate(f, nil); err == nil {
		t.Fatal("expected error for nil required string")
	}
}

func TestValidateString_NilOptional(t *testing.T) {
	f := Field{Name: "name", Type: String}
	if err := Validate(f, nil); err != nil {
		t.Fatalf("expected valid for nil optional, got %v", err)
	}
}

func TestValidateString_WrongType(t *testing.T) {
	f := Field{Name: "name", Type: String}
	if err := Validate(f, 42); err == nil {
		t.Fatal("expected error for non-string value")
	}
}

func TestValidateString_MinLength(t *testing.T) {
	f := Field{Name: "name", Type: String, Min: fPtr(3)}
	if err := Validate(f, "ab"); err == nil {
		t.Fatal("expected error for string shorter than min")
	}
	if err := Validate(f, "abc"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateString_MaxLength(t *testing.T) {
	f := Field{Name: "name", Type: String, Max: fPtr(5)}
	if err := Validate(f, "abcdef"); err == nil {
		t.Fatal("expected error for string longer than max")
	}
	if err := Validate(f, "abcde"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateString_Pattern(t *testing.T) {
	f := Field{Name: "code", Type: String, Pattern: `^[A-Z]{3}$`}
	if err := Validate(f, "abc"); err == nil {
		t.Fatal("expected error for pattern mismatch")
	}
	if err := Validate(f, "ABC"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

// ---------- Text ----------

func TestValidateText_Valid(t *testing.T) {
	f := Field{Name: "body", Type: Text, Required: true}
	if err := Validate(f, "long text here"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateText_MinMax(t *testing.T) {
	f := Field{Name: "body", Type: Text, Min: fPtr(10), Max: fPtr(100)}
	if err := Validate(f, "short"); err == nil {
		t.Fatal("expected error for text shorter than min")
	}
	if err := Validate(f, "this is a longer text that passes"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

// ---------- Int ----------

func TestValidateInt_Valid(t *testing.T) {
	f := Field{Name: "age", Type: Int}
	if err := Validate(f, 42); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateInt_FromFloat(t *testing.T) {
	f := Field{Name: "age", Type: Int}
	if err := Validate(f, float64(42)); err != nil {
		t.Fatalf("expected valid for whole float64, got %v", err)
	}
	if err := Validate(f, 42.5); err == nil {
		t.Fatal("expected error for non-integer float64")
	}
}

func TestValidateInt_WrongType(t *testing.T) {
	f := Field{Name: "age", Type: Int}
	if err := Validate(f, "hello"); err == nil {
		t.Fatal("expected error for string")
	}
}

func TestValidateInt_MinMax(t *testing.T) {
	f := Field{Name: "age", Type: Int, Min: fPtr(0), Max: fPtr(150)}
	if err := Validate(f, -1); err == nil {
		t.Fatal("expected error below min")
	}
	if err := Validate(f, 151); err == nil {
		t.Fatal("expected error above max")
	}
	if err := Validate(f, 25); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

// ---------- Float ----------

func TestValidateFloat_Valid(t *testing.T) {
	f := Field{Name: "price", Type: Float}
	if err := Validate(f, 3.14); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if err := Validate(f, 42); err != nil {
		t.Fatalf("expected valid for int, got %v", err)
	}
}

func TestValidateFloat_WrongType(t *testing.T) {
	f := Field{Name: "price", Type: Float}
	if err := Validate(f, true); err == nil {
		t.Fatal("expected error for bool")
	}
}

func TestValidateFloat_MinMax(t *testing.T) {
	f := Field{Name: "price", Type: Float, Min: fPtr(0), Max: fPtr(100)}
	if err := Validate(f, -0.1); err == nil {
		t.Fatal("expected error below min")
	}
	if err := Validate(f, 100.1); err == nil {
		t.Fatal("expected error above max")
	}
	if err := Validate(f, 50.0); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

// ---------- Decimal ----------

func TestValidateDecimal_Valid(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal}
	if err := Validate(f, "99.99"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateDecimal_InvalidString(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal}
	if err := Validate(f, "notanumber"); err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestValidateDecimal_WrongType(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal}
	if err := Validate(f, 42); err == nil {
		t.Fatal("expected error for non-string")
	}
}

func TestValidateDecimal_MinMax(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal, Min: fPtr(0), Max: fPtr(1000)}
	if err := Validate(f, "-1.00"); err == nil {
		t.Fatal("expected error below min")
	}
	if err := Validate(f, "1001.00"); err == nil {
		t.Fatal("expected error above max")
	}
	if err := Validate(f, "500.00"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateDecimal_RequiredEmpty(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal, Required: true}
	if err := Validate(f, ""); err == nil {
		t.Fatal("expected error for empty required decimal")
	}
}

// ---------- Bool ----------

func TestValidateBool_Valid(t *testing.T) {
	f := Field{Name: "active", Type: Bool}
	cases := []any{true, false, 1, 0, "true", "false", "1", "0"}
	for _, v := range cases {
		if err := Validate(f, v); err != nil {
			t.Fatalf("expected valid for %v, got %v", v, err)
		}
	}
}

func TestValidateBool_Invalid(t *testing.T) {
	f := Field{Name: "active", Type: Bool}
	cases := []any{2, "yes", "no", 1.5}
	for _, v := range cases {
		if err := Validate(f, v); err == nil {
			t.Fatalf("expected error for %v", v)
		}
	}
}

// ---------- Enum ----------

func TestValidateEnum_Valid(t *testing.T) {
	f := Field{Name: "status", Type: Enum, Values: []string{"pending", "active", "closed"}}
	if err := Validate(f, "active"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateEnum_Invalid(t *testing.T) {
	f := Field{Name: "status", Type: Enum, Values: []string{"pending", "active"}}
	if err := Validate(f, "unknown"); err == nil {
		t.Fatal("expected error for value not in enum")
	}
}

func TestValidateEnum_WrongType(t *testing.T) {
	f := Field{Name: "status", Type: Enum, Values: []string{"pending"}}
	if err := Validate(f, 42); err == nil {
		t.Fatal("expected error for non-string")
	}
}

// ---------- UUID ----------

func TestValidateUUID_Valid(t *testing.T) {
	f := Field{Name: "id", Type: UUID}
	if err := Validate(f, "550e8400-e29b-41d4-a716-446655440000"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if err := Validate(f, "6BA7B810-9DAD-11D1-80B4-00C04FD430C8"); err != nil {
		t.Fatalf("expected valid for uppercase, got %v", err)
	}
}

func TestValidateUUID_Invalid(t *testing.T) {
	f := Field{Name: "id", Type: UUID}
	cases := []any{"not-a-uuid", "550e8400-e29b-41d4-a716", "", 12345}
	for _, v := range cases {
		if err := Validate(f, v); err == nil {
			t.Fatalf("expected error for %v", v)
		}
	}
}

// ---------- Timestamp ----------

func TestValidateTimestamp_Valid(t *testing.T) {
	f := Field{Name: "created", Type: Timestamp}
	if err := Validate(f, "2024-01-15T10:30:00Z"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if err := Validate(f, "2024-01-15T10:30:00+02:00"); err != nil {
		t.Fatalf("expected valid with timezone, got %v", err)
	}
}

func TestValidateTimestamp_Invalid(t *testing.T) {
	f := Field{Name: "created", Type: Timestamp}
	cases := []any{"2024-01-15", "not-a-timestamp", 12345}
	for _, v := range cases {
		if err := Validate(f, v); err == nil {
			t.Fatalf("expected error for %v", v)
		}
	}
}

// ---------- Date ----------

func TestValidateDate_Valid(t *testing.T) {
	f := Field{Name: "birthday", Type: Date}
	if err := Validate(f, "2024-01-15"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateDate_Invalid(t *testing.T) {
	f := Field{Name: "birthday", Type: Date}
	cases := []any{"2024-13-01", "not-a-date", "2024/01/15", 12345}
	for _, v := range cases {
		if err := Validate(f, v); err == nil {
			t.Fatalf("expected error for %v", v)
		}
	}
}

// ---------- JSON ----------

func TestValidateJSON_Valid(t *testing.T) {
	f := Field{Name: "meta", Type: JSON}
	if err := Validate(f, `{"key": "value"}`); err != nil {
		t.Fatalf("expected valid object, got %v", err)
	}
	if err := Validate(f, `[1,2,3]`); err != nil {
		t.Fatalf("expected valid array, got %v", err)
	}
	if err := Validate(f, `"hello"`); err != nil {
		t.Fatalf("expected valid string, got %v", err)
	}
	if err := Validate(f, "42"); err != nil {
		t.Fatalf("expected valid number, got %v", err)
	}
	// already-parsed Go values
	if err := Validate(f, map[string]any{"a": 1}); err != nil {
		t.Fatalf("expected valid map, got %v", err)
	}
	if err := Validate(f, []any{1, 2}); err != nil {
		t.Fatalf("expected valid slice, got %v", err)
	}
}

func TestValidateJSON_Invalid(t *testing.T) {
	f := Field{Name: "meta", Type: JSON}
	if err := Validate(f, `{invalid`); err == nil {
		t.Fatal("expected error for invalid JSON string")
	}
}

// ---------- Relation ----------

func TestValidateRelation_Valid(t *testing.T) {
	f := Field{Name: "author", Type: Relation, To: "users"}
	if err := Validate(f, "user_123"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateRelation_Empty(t *testing.T) {
	f := Field{Name: "author", Type: Relation, To: "users"}
	if err := Validate(f, ""); err == nil {
		t.Fatal("expected error for empty relation")
	}
}

func TestValidateRelation_WrongType(t *testing.T) {
	f := Field{Name: "author", Type: Relation}
	if err := Validate(f, 42); err == nil {
		t.Fatal("expected error for non-string")
	}
}

// ---------- Image ----------

func TestValidateImage_Valid(t *testing.T) {
	f := Field{Name: "photo", Type: Image}
	if err := Validate(f, "/uploads/photo.jpg"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if err := Validate(f, "https://example.com/img.png"); err != nil {
		t.Fatalf("expected valid URL, got %v", err)
	}
}

func TestValidateImage_Empty(t *testing.T) {
	f := Field{Name: "photo", Type: Image}
	if err := Validate(f, ""); err == nil {
		t.Fatal("expected error for empty image path")
	}
}

func TestValidateImage_WrongType(t *testing.T) {
	f := Field{Name: "photo", Type: Image}
	if err := Validate(f, 42); err == nil {
		t.Fatal("expected error for non-string")
	}
}

// ---------- File ----------

func TestValidateFile_Valid(t *testing.T) {
	f := Field{Name: "doc", Type: File}
	if err := Validate(f, "/files/report.pdf"); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateFile_Empty(t *testing.T) {
	f := Field{Name: "doc", Type: File}
	if err := Validate(f, ""); err == nil {
		t.Fatal("expected error for empty file path")
	}
}

func TestValidateFile_WrongType(t *testing.T) {
	f := Field{Name: "doc", Type: File}
	if err := Validate(f, 42); err == nil {
		t.Fatal("expected error for non-string")
	}
}

// ---------- ValidateAll ----------

func TestValidateAll_Valid(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true},
		{Name: "age", Type: Int},
	}}
	result := ValidateAll(s, map[string]any{"name": "Alice", "age": 30})
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateAll_MissingRequired(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true},
		{Name: "age", Type: Int},
	}}
	result := ValidateAll(s, map[string]any{"age": 30})
	if result.Valid {
		t.Fatal("expected invalid for missing required field")
	}
	if len(result.Errors["name"]) == 0 {
		t.Fatal("expected error for 'name' field")
	}
}

func TestValidateAll_MultipleErrors(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true},
		{Name: "age", Type: Int, Min: fPtr(0)},
	}}
	result := ValidateAll(s, map[string]any{"age": -1})
	if result.Valid {
		t.Fatal("expected invalid")
	}
	if len(result.Errors["name"]) == 0 {
		t.Fatal("expected error for missing 'name'")
	}
	if len(result.Errors["age"]) == 0 {
		t.Fatal("expected error for invalid 'age'")
	}
}

func TestValidateAll_UnknownFieldsIgnored(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String},
	}}
	result := ValidateAll(s, map[string]any{"name": "Alice", "extra": "ignored"})
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateAll_DefaultSkipsRequired(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "role", Type: String, Required: true, Default: "user"},
	}}
	result := ValidateAll(s, map[string]any{})
	if !result.Valid {
		t.Fatalf("expected valid (has default), got errors: %v", result.Errors)
	}
}

// ---------- Schema helpers ----------

func TestSchema_FieldByName(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String},
		{Name: "age", Type: Int},
	}}
	f, ok := s.FieldByName("age")
	if !ok {
		t.Fatal("expected to find 'age'")
	}
	if f.Type != Int {
		t.Fatalf("expected Int, got %d", f.Type)
	}
	_, ok = s.FieldByName("missing")
	if ok {
		t.Fatal("expected not found for 'missing'")
	}
}

func TestSchema_Names(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String},
		{Name: "age", Type: Int},
	}}
	names := s.Names()
	if len(names) != 2 || names[0] != "name" || names[1] != "age" {
		t.Fatalf("expected [name age], got %v", names)
	}
}

func TestSchema_RequiredFields(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true},
		{Name: "age", Type: Int},
		{Name: "email", Type: String, Required: true},
	}}
	req := s.RequiredFields()
	if len(req) != 2 {
		t.Fatalf("expected 2 required fields, got %d", len(req))
	}
}

// ---------- JSONSchema ----------

func TestJSONSchema_Basic(t *testing.T) {
	fields := []Field{
		{Name: "name", Type: String, Required: true},
		{Name: "age", Type: Int},
		{Name: "active", Type: Bool},
	}
	schema := JSONSchema(fields)

	// Check top-level keys
	if schema["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("wrong $schema: %v", schema["$schema"])
	}
	if schema["type"] != "object" {
		t.Fatalf("wrong type: %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing or wrong type")
	}
	if len(props) != 3 {
		t.Fatalf("expected 3 properties, got %d", len(props))
	}

	// Check required list
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required missing or wrong type")
	}
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("expected [name] required, got %v", required)
	}

	// Check string property
	nameProp := props["name"].(map[string]any)
	if nameProp["type"] != "string" {
		t.Fatalf("expected string type for name, got %v", nameProp["type"])
	}

	// Check integer property
	ageProp := props["age"].(map[string]any)
	if ageProp["type"] != "integer" {
		t.Fatalf("expected integer type for age, got %v", ageProp["type"])
	}
}

func TestJSONSchema_WithConstraints(t *testing.T) {
	fields := []Field{
		{Name: "username", Type: String, Min: fPtr(3), Max: fPtr(20), Pattern: `^[a-z]+$`},
		{Name: "score", Type: Float, Min: fPtr(0), Max: fPtr(100)},
		{Name: "status", Type: Enum, Values: []string{"active", "inactive"}},
	}
	schema := JSONSchema(fields)
	props := schema["properties"].(map[string]any)

	// String constraints
	username := props["username"].(map[string]any)
	if username["minLength"] != 3 {
		t.Fatalf("expected minLength 3, got %v", username["minLength"])
	}
	if username["maxLength"] != 20 {
		t.Fatalf("expected maxLength 20, got %v", username["maxLength"])
	}
	if username["pattern"] != `^[a-z]+$` {
		t.Fatalf("expected pattern, got %v", username["pattern"])
	}

	// Float constraints
	score := props["score"].(map[string]any)
	if score["minimum"] != 0.0 {
		t.Fatalf("expected minimum 0, got %v", score["minimum"])
	}
	if score["maximum"] != 100.0 {
		t.Fatalf("expected maximum 100, got %v", score["maximum"])
	}

	// Enum
	status := props["status"].(map[string]any)
	enumVals := status["enum"].([]string)
	if len(enumVals) != 2 {
		t.Fatalf("expected 2 enum values, got %d", len(enumVals))
	}
}

func TestJSONSchema_AllTypes(t *testing.T) {
	fields := []Field{
		{Name: "s", Type: String},
		{Name: "t", Type: Text},
		{Name: "i", Type: Int},
		{Name: "f", Type: Float},
		{Name: "d", Type: Decimal},
		{Name: "b", Type: Bool},
		{Name: "e", Type: Enum, Values: []string{"a", "b"}},
		{Name: "u", Type: UUID},
		{Name: "ts", Type: Timestamp},
		{Name: "dt", Type: Date},
		{Name: "j", Type: JSON},
		{Name: "r", Type: Relation, To: "users", Many: true},
		{Name: "img", Type: Image},
		{Name: "file", Type: File},
	}
	schema := JSONSchema(fields)
	props := schema["properties"].(map[string]any)

	// Verify each type produces a property
	for _, f := range fields {
		if _, ok := props[f.Name]; !ok {
			t.Fatalf("missing property for field %q", f.Name)
		}
	}

	// Spot-check specific formats
	ts := props["ts"].(map[string]any)
	if ts["format"] != "date-time" {
		t.Fatalf("expected date-time format for timestamp, got %v", ts["format"])
	}
	dt := props["dt"].(map[string]any)
	if dt["format"] != "date" {
		t.Fatalf("expected date format for date, got %v", dt["format"])
	}
	u := props["u"].(map[string]any)
	if u["format"] != "uuid" {
		t.Fatalf("expected uuid format, got %v", u["format"])
	}
	r := props["r"].(map[string]any)
	if r["format"] != "relation" {
		t.Fatalf("expected relation format, got %v", r["format"])
	}
	if r["x-relation"] != "users" {
		t.Fatalf("expected x-relation users, got %v", r["x-relation"])
	}
	if r["x-many"] != true {
		t.Fatalf("expected x-many true, got %v", r["x-many"])
	}
	img := props["img"].(map[string]any)
	if img["format"] != "uri" {
		t.Fatalf("expected uri format for image, got %v", img["format"])
	}
}

func TestJSONSchema_MarshalsToJSON(t *testing.T) {
	fields := []Field{
		{Name: "name", Type: String, Required: true},
		{Name: "age", Type: Int},
	}
	schema := JSONSchema(fields)
	b, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	// Verify it round-trips
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("round-trip lost $schema: %v", parsed["$schema"])
	}
}

func TestJSONSchema_Default(t *testing.T) {
	fields := []Field{
		{Name: "role", Type: String, Default: "user"},
	}
	schema := JSONSchema(fields)
	props := schema["properties"].(map[string]any)
	role := props["role"].(map[string]any)
	if role["default"] != "user" {
		t.Fatalf("expected default 'user', got %v", role["default"])
	}
}

func TestJSONSchema_NoRequired(t *testing.T) {
	fields := []Field{
		{Name: "name", Type: String},
	}
	schema := JSONSchema(fields)
	if _, has := schema["required"]; has {
		t.Fatal("expected no 'required' key when no fields are required")
	}
}
