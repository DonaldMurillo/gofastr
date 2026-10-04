package schema

import "testing"

// TestFieldTypeLabel pins both audiences: the plain label for every
// field type, and the SDK reference spelling where decimal, image and
// file carry the wire note. An unknown type reads as "string" in both.
func TestFieldTypeLabel(t *testing.T) {
	plain := map[FieldType]string{
		String: "string", Text: "text", Int: "integer",
		Float: "float", Decimal: "decimal", Bool: "boolean",
		Enum: "enum", UUID: "uuid", Timestamp: "timestamp",
		Date: "date", JSON: "json", Relation: "relation",
		Image: "image", File: "file",
	}
	wireNotes := map[FieldType]string{
		Decimal: "decimal (string on the wire)",
		Image:   "image (url)",
		File:    "file (url)",
	}
	for _, wire := range []bool{false, true} {
		for ft, want := range plain {
			if w, ok := wireNotes[ft]; ok && wire {
				want = w
			}
			if got := FieldTypeLabel(ft, wire); got != want {
				t.Errorf("FieldTypeLabel(%v, %v) = %q, want %q", ft, wire, got, want)
			}
		}
		if got := FieldTypeLabel(FieldType(9999), wire); got != "string" {
			t.Errorf("FieldTypeLabel(unknown, %v) = %q, want \"string\"", wire, got)
		}
	}
}
