package schema

import "testing"

// TestFieldTypeLabelPlain pins the llm.md audience: the bare name for
// every field type, and "string" for an unknown one.
func TestFieldTypeLabelPlain(t *testing.T) {
	cases := map[FieldType]string{
		String: "string", Text: "text", Int: "integer",
		Float: "float", Decimal: "decimal", Bool: "boolean",
		Enum: "enum", UUID: "uuid", Timestamp: "timestamp",
		Date: "date", JSON: "json", Relation: "relation",
		Image: "image", File: "file",
	}
	for ft, want := range cases {
		if got := FieldTypeLabel(ft, false); got != want {
			t.Errorf("FieldTypeLabel(%v, false) = %q, want %q", ft, got, want)
		}
	}
	if got := FieldTypeLabel(FieldType(9999), false); got != "string" {
		t.Errorf("unknown type fallback = %q", got)
	}
}

// TestFieldTypeLabelWire pins the SDK reference audience: decimal,
// image, and file carry the wire note; every other label is the plain
// one, and an unknown type still reads as "string".
func TestFieldTypeLabelWire(t *testing.T) {
	cases := map[FieldType]string{
		String: "string", Text: "text", Int: "integer",
		Float: "float", Decimal: "decimal (string on the wire)",
		Bool: "boolean", Enum: "enum", UUID: "uuid",
		Timestamp: "timestamp", Date: "date", JSON: "json",
		Relation: "relation", Image: "image (url)",
		File: "file (url)",
	}
	for ft, want := range cases {
		if got := FieldTypeLabel(ft, true); got != want {
			t.Errorf("FieldTypeLabel(%v, true) = %q, want %q", ft, got, want)
		}
	}
	if got := FieldTypeLabel(FieldType(9999), true); got != "string" {
		t.Errorf("unknown type fallback = %q", got)
	}
}
