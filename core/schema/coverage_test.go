package schema

import (
	"math"
	"testing"
)

func fp(v float64) *float64 { return &v }

type stringerT struct{ s string }

func (s stringerT) String() string { return s.s }

// --- numeric/string conversion helpers (called directly, same package) ---

func TestToInt64_AllKinds(t *testing.T) {
	cases := []struct {
		in   any
		want int64
		ok   bool
	}{
		{int(1), 1, true},
		{int8(2), 2, true},
		{int16(3), 3, true},
		{int32(4), 4, true},
		{int64(5), 5, true},
		{uint(6), 6, true},
		{uint8(7), 7, true},
		{uint16(8), 8, true},
		{uint32(9), 9, true},
		{uint64(10), 10, true},
		{uint64(math.MaxUint64), 0, false}, // overflow
		{float64(11), 11, true},
		{float64(1.5), 0, false},  // non-integral
		{float64(1e30), 0, false}, // out of range
		{float32(12), 12, true},
		{float32(1.5), 0, false},
		{"13", 13, true},
		{"nope", 0, false},
		{[]byte("x"), 0, false}, // default
	}
	for _, c := range cases {
		got, ok := toInt64(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("toInt64(%#v)=%d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestToFloat64_AllKinds(t *testing.T) {
	for _, in := range []any{float64(1), float32(1), int(1), int8(1), int16(1), int32(1), int64(1), uint(1), uint8(1), uint16(1), uint32(1), uint64(1)} {
		if v, ok := toFloat64(in); !ok || v != 1 {
			t.Errorf("toFloat64(%#v)=%v,%v", in, v, ok)
		}
	}
	if _, ok := toFloat64("x"); ok {
		t.Error("toFloat64(string) should fail")
	}
}

func TestToString_StringerAndDefault(t *testing.T) {
	if s, ok := toString("a"); !ok || s != "a" {
		t.Error("plain string")
	}
	if s, ok := toString(stringerT{"b"}); !ok || s != "b" {
		t.Error("stringer")
	}
	if _, ok := toString(123); ok {
		t.Error("int should not stringify")
	}
}

func TestIsInt64Representable(t *testing.T) {
	cases := []struct {
		in   float64
		want bool
	}{
		{math.NaN(), false},
		{math.Inf(1), false},
		{1.5, false},
		{1e30, false},
		{42, true},
		{math.MinInt64, true},
	}
	for _, c := range cases {
		if got := isInt64Representable(c.in); got != c.want {
			t.Errorf("isInt64Representable(%v)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestFloatBoundToInt64(t *testing.T) {
	if floatBoundToInt64(math.NaN(), true) != math.MaxInt64 {
		t.Error("NaN min")
	}
	if floatBoundToInt64(math.NaN(), false) != math.MinInt64 {
		t.Error("NaN max")
	}
	if floatBoundToInt64(1e30, true) != math.MaxInt64 {
		t.Error("huge -> MaxInt64")
	}
	if floatBoundToInt64(-1e30, false) != math.MinInt64 {
		t.Error("tiny -> MinInt64")
	}
	if floatBoundToInt64(2.3, true) != 3 {
		t.Error("min ceil")
	}
	if floatBoundToInt64(2.7, false) != 2 {
		t.Error("max floor")
	}
}

// --- validateField dispatch + per-type validators ---

func TestValidateField_NilAndUnsupported(t *testing.T) {
	if err := Validate(Field{Required: true}, nil); err == nil {
		t.Error("nil required should error")
	}
	if err := Validate(Field{}, nil); err != nil {
		t.Error("nil optional should pass")
	}
	if err := Validate(Field{Type: FieldType(999)}, "x"); err == nil {
		t.Error("unsupported type should error")
	}
}

func TestValidateString(t *testing.T) {
	if err := validateString(Field{}, 123); err == nil {
		t.Error("non-string")
	}
	if err := validateString(Field{Required: true}, ""); err == nil {
		t.Error("required empty")
	}
	if err := validateString(Field{Min: fp(3)}, "ab"); err == nil {
		t.Error("min")
	}
	if err := validateString(Field{Max: fp(2)}, "abc"); err == nil {
		t.Error("max")
	}
	if err := validateString(Field{Pattern: "("}, "x"); err == nil {
		t.Error("invalid pattern regex")
	}
	if err := validateString(Field{Pattern: "^a$"}, "b"); err == nil {
		t.Error("pattern mismatch")
	}
	if err := validateString(Field{Pattern: "^a$", Min: fp(1), Max: fp(1)}, "a"); err != nil {
		t.Errorf("valid: %v", err)
	}
}

func TestValidateInt(t *testing.T) {
	if err := validateField(Field{Type: Int}, "x"); err == nil {
		t.Error("non-int")
	}
	if err := validateField(Field{Type: Int, Min: fp(5)}, 4); err == nil {
		t.Error("below min")
	}
	if err := validateField(Field{Type: Int, Max: fp(5)}, 6); err == nil {
		t.Error("above max")
	}
	if err := validateField(Field{Type: Int, Min: fp(1), Max: fp(10)}, 5); err != nil {
		t.Errorf("valid: %v", err)
	}
}

func TestValidateFloat(t *testing.T) {
	if err := validateField(Field{Type: Float}, "x"); err == nil {
		t.Error("non-number")
	}
	if err := validateField(Field{Type: Float}, math.Inf(1)); err == nil {
		t.Error("inf")
	}
	if err := validateField(Field{Type: Float}, math.NaN()); err == nil {
		t.Error("nan")
	}
	if err := validateField(Field{Type: Float, Min: fp(1)}, 0.5); err == nil {
		t.Error("below min")
	}
	if err := validateField(Field{Type: Float, Max: fp(1)}, 1.5); err == nil {
		t.Error("above max")
	}
	if err := validateField(Field{Type: Float, Min: fp(0), Max: fp(2)}, 1.0); err != nil {
		t.Errorf("valid: %v", err)
	}
}

func TestValidateDecimal(t *testing.T) {
	if err := validateField(Field{Type: Decimal}, 123); err == nil {
		t.Error("non-string")
	}
	if err := validateField(Field{Type: Decimal, Required: true}, ""); err == nil {
		t.Error("required empty")
	}
	if err := validateField(Field{Type: Decimal}, ""); err != nil {
		t.Error("optional empty ok")
	}
	if err := validateField(Field{Type: Decimal}, "1_000"); err == nil {
		t.Error("underscore rejected")
	}
	if err := validateField(Field{Type: Decimal}, "1e999"); err == nil {
		t.Error("overflow (ParseFloat err) rejected")
	}
	if err := validateField(Field{Type: Decimal, Min: fp(10)}, "5"); err == nil {
		t.Error("below min")
	}
	if err := validateField(Field{Type: Decimal, Max: fp(10)}, "15"); err == nil {
		t.Error("above max")
	}
	// NaN bound -> SetFloat64 returns nil -> fail closed.
	if err := validateField(Field{Type: Decimal, Min: fp(math.NaN())}, "5"); err == nil {
		t.Error("NaN min should fail closed")
	}
	if err := validateField(Field{Type: Decimal, Max: fp(math.NaN())}, "5"); err == nil {
		t.Error("NaN max should fail closed")
	}
	if err := validateField(Field{Type: Decimal, Min: fp(0), Max: fp(100)}, "12.50"); err != nil {
		t.Errorf("valid: %v", err)
	}
	// The regex (not the parser) is what fails non-finite literals closed —
	// ParseFloat/big.Rat would otherwise accept these and let them slip past
	// the Min/Max bounds. Guard the property explicitly.
	for _, bad := range []string{"NaN", "nan", "Inf", "inf", "Infinity", "+Inf", "0x1p4"} {
		if err := validateField(Field{Type: Decimal, Max: fp(1)}, bad); err == nil {
			t.Errorf("SECURITY: decimal %q must be rejected (bypasses bounds)", bad)
		}
	}
}

func TestValidateBool(t *testing.T) {
	for _, v := range []any{true, false, 0, 1, "true", "FALSE", "1", "0", 0.0, 1.0} {
		if err := validateBool(v); err != nil {
			t.Errorf("validateBool(%#v) unexpected err: %v", v, err)
		}
	}
	for _, v := range []any{2, "maybe", 2.0, []byte("x")} {
		if err := validateBool(v); err == nil {
			t.Errorf("validateBool(%#v) should error", v)
		}
	}
}

func TestValidateEnum(t *testing.T) {
	f := Field{Type: Enum, Values: []string{"a", "b"}}
	if err := validateField(f, 1); err == nil {
		t.Error("non-string enum")
	}
	if err := validateField(f, "c"); err == nil {
		t.Error("not allowed")
	}
	if err := validateField(f, "a"); err != nil {
		t.Errorf("allowed: %v", err)
	}
}

func TestValidateUUIDTimestampDate(t *testing.T) {
	if err := validateField(Field{Type: UUID}, 1); err == nil {
		t.Error("uuid non-string")
	}
	if err := validateField(Field{Type: UUID}, "not-a-uuid"); err == nil {
		t.Error("bad uuid")
	}
	if err := validateField(Field{Type: UUID}, "12345678-1234-1234-1234-123456789012"); err != nil {
		t.Errorf("uuid: %v", err)
	}
	if err := validateField(Field{Type: Timestamp}, 1); err == nil {
		t.Error("ts non-string")
	}
	if err := validateField(Field{Type: Timestamp}, "nope"); err == nil {
		t.Error("bad ts")
	}
	if err := validateField(Field{Type: Timestamp}, "2026-01-02T03:04:05Z"); err != nil {
		t.Errorf("ts: %v", err)
	}
	if err := validateField(Field{Type: Date}, 1); err == nil {
		t.Error("date non-string")
	}
	if err := validateField(Field{Type: Date}, "nope"); err == nil {
		t.Error("bad date")
	}
	if err := validateField(Field{Type: Date}, "2026-01-02"); err != nil {
		t.Errorf("date: %v", err)
	}
}

func TestValidateJSON(t *testing.T) {
	if err := validateField(Field{Type: JSON}, `{"a":1}`); err != nil {
		t.Errorf("valid json string: %v", err)
	}
	if err := validateField(Field{Type: JSON}, `{bad`); err == nil {
		t.Error("invalid json string")
	}
	if err := validateField(Field{Type: JSON}, map[string]any{"a": 1}); err != nil {
		t.Errorf("map: %v", err)
	}
	if err := validateField(Field{Type: JSON}, []any{1, 2}); err != nil {
		t.Errorf("slice: %v", err)
	}
	if err := validateField(Field{Type: JSON}, 42); err != nil {
		t.Errorf("marshalable default: %v", err)
	}
	if err := validateField(Field{Type: JSON}, make(chan int)); err == nil {
		t.Error("unmarshalable should error")
	}
}

func TestValidateRelationAndFile(t *testing.T) {
	if err := validateField(Field{Type: Relation}, 1); err == nil {
		t.Error("relation non-string")
	}
	if err := validateField(Field{Type: Relation}, ""); err == nil {
		t.Error("relation empty")
	}
	if err := validateField(Field{Type: Relation}, "id-1"); err != nil {
		t.Errorf("relation: %v", err)
	}
	if err := validateField(Field{Type: Image}, 1); err == nil {
		t.Error("image non-string")
	}
	if err := validateField(Field{Type: File}, ""); err == nil {
		t.Error("file empty")
	}
	if err := validateField(Field{Type: File}, "/x"); err != nil {
		t.Errorf("file: %v", err)
	}
}

// --- Schema-level helpers ---

func TestFieldSelectors(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "id", AutoGenerate: AutoUUID, ReadOnly: true, Hidden: true},
		{Name: "title", Required: true},
		{Name: "secret", Hidden: true},
		{Name: "ro", ReadOnly: true},
	}}
	if got := len(s.RequiredFields()); got != 1 {
		t.Errorf("required=%d", got)
	}
	if got := len(s.AutoGeneratedFields()); got != 1 {
		t.Errorf("autogen=%d", got)
	}
	// writable = not autogen and not readonly: title + secret (Hidden doesn't gate writes)
	if got := len(s.WritableFields()); got != 2 {
		t.Errorf("writable=%d", got)
	}
	// visible = not hidden: title, ro
	if got := len(s.VisibleFields()); got != 2 {
		t.Errorf("visible=%d", got)
	}
	if _, ok := s.FieldByName("title"); !ok {
		t.Error("FieldByName")
	}
	if _, ok := s.FieldByName("nope"); ok {
		t.Error("FieldByName miss")
	}
	if len(s.Names()) != 4 {
		t.Error("Names")
	}
}

func TestValidateAllAndPartial(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "id", AutoGenerate: AutoUUID}, // skipped
		{Name: "title", Type: String, Required: true},
		{Name: "opt", Type: Int},
		{Name: "def", Type: String, Required: true, Default: "x"}, // required+default -> not reported when absent
	}}
	// ValidateAll: missing required title reported, def not reported, autogen skipped.
	r := ValidateAll(s, map[string]any{"opt": "bad"})
	if r.Valid {
		t.Error("expected invalid")
	}
	if len(r.Errors["title"]) == 0 {
		t.Error("missing title should be reported")
	}
	if len(r.Errors["opt"]) == 0 {
		t.Error("opt type error")
	}
	if len(r.Errors["def"]) != 0 {
		t.Error("def has default; absence not reported")
	}
	// All present + valid.
	if r2 := ValidateAll(s, map[string]any{"title": "t", "opt": 1, "def": "y"}); !r2.Valid {
		t.Errorf("expected valid: %+v", r2.Errors)
	}
	// ValidatePartial: absent fields ignored (even required), present validated.
	rp := ValidatePartial(s, map[string]any{"opt": "bad"})
	if rp.Valid || len(rp.Errors["opt"]) == 0 {
		t.Error("partial should validate present field")
	}
	if len(rp.Errors["title"]) != 0 {
		t.Error("partial should ignore absent required")
	}
	if rp2 := ValidatePartial(s, map[string]any{"opt": 3}); !rp2.Valid {
		t.Error("partial valid")
	}
}

// --- JSON Schema generation ---

func TestJSONSchema_EveryFieldType(t *testing.T) {
	fields := []Field{
		{Name: "s", Type: String, Required: true, Min: fp(1), Max: fp(5), Pattern: "^.*$"},
		{Name: "txt", Type: Text, Min: fp(1), Max: fp(5), Pattern: "^.*$"},
		{Name: "i", Type: Int, Min: fp(0), Max: fp(9)},
		{Name: "f", Type: Float, Min: fp(0), Max: fp(9)},
		{Name: "d", Type: Decimal, Min: fp(0), Max: fp(9)},
		{Name: "b", Type: Bool},
		{Name: "e", Type: Enum, Values: []string{"a"}},
		{Name: "u", Type: UUID},
		{Name: "ts", Type: Timestamp},
		{Name: "dt", Type: Date},
		{Name: "j", Type: JSON},
		{Name: "r", Type: Relation, To: "other", Many: true},
		{Name: "img", Type: Image},
		{Name: "file", Type: File, Default: "x"},
	}
	js := JSONSchema(fields)
	props, ok := js["properties"].(map[string]any)
	if !ok || len(props) != len(fields) {
		t.Fatalf("properties missing/short: %v", js["properties"])
	}
	req, ok := js["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "s" {
		t.Errorf("required=%v", js["required"])
	}
	if r := props["r"].(map[string]any); r["x-relation"] != "other" || r["x-many"] != true {
		t.Errorf("relation props: %v", r)
	}
	if f := props["file"].(map[string]any); f["default"] != "x" {
		t.Errorf("default prop: %v", f)
	}
	// Empty field set: no properties/required keys.
	empty := JSONSchema(nil)
	if _, ok := empty["properties"]; ok {
		t.Error("empty schema should omit properties")
	}
	if _, ok := empty["required"]; ok {
		t.Error("empty schema should omit required")
	}
}
