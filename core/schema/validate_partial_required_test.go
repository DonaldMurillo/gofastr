package schema

import "testing"

func float64Ptr(v float64) *float64 { return &v }

// TestValidatePartial_RejectsPresentButEmptyRequired pins the data-corruption
// fix surfaced in adversarial review: when a sparse-update body INCLUDES a
// required field but sends an empty zero-value, validation must reject —
// the client is trying to blank a required column.
//
// Absence (field not in body) is OK; presence-with-zero-value is the
// distinct error we have to catch.
func TestValidatePartial_RejectsPresentButEmptyRequired(t *testing.T) {
	// No Min set — the only thing that should reject "" is the
	// Required-but-empty check. Without the fix this passes, which
	// lets PUT {"name":""} silently blank a Required column.
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true},
	}}

	cases := map[string]map[string]any{
		"empty string": {"name": ""},
		"explicit nil": {"name": nil},
	}
	for label, body := range cases {
		label, body := label, body
		t.Run(label, func(t *testing.T) {
			vr := ValidatePartial(s, body)
			if vr.Valid {
				t.Errorf("ValidatePartial(%v) should reject required field with %s; got Valid=true (data-corruption vector)", body, label)
			}
		})
	}
}

// TestValidatePartial_AbsentRequiredIsOK is the non-regression: a sparse
// update that doesn't touch a required field should NOT fail validation
// (that's the whole point of ValidatePartial). The data-corruption fix
// must distinguish "absent" from "present-empty".
func TestValidatePartial_AbsentRequiredIsOK(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true, Min: float64Ptr(1)},
		{Name: "notes", Type: String},
	}}
	vr := ValidatePartial(s, map[string]any{"notes": "just adding notes"})
	if !vr.Valid {
		t.Errorf("absent required should be OK in partial validation; errors=%v", vr.Errors)
	}
}

// TestValidatePartial_NonEmptyRequiredPasses keeps the happy path.
func TestValidatePartial_NonEmptyRequiredPasses(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "name", Type: String, Required: true},
	}}
	vr := ValidatePartial(s, map[string]any{"name": "Alice"})
	if !vr.Valid {
		t.Errorf("present non-empty required should pass; errors=%v", vr.Errors)
	}
}
