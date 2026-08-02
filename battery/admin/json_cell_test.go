package admin

import "testing"

// A schema.JSON column now reads back from the CRUD layer as a decoded
// value, not a string. cellText feeds both the list cell and the edit
// form's textarea, so rendering Go's map syntax would show
// "map[seats:5]" to the operator AND post that back on save, failing
// validation and losing the row's other edits.
func TestCellTextRendersJSONValueAsJSON(t *testing.T) {
	got := cellText(map[string]any{"seats": float64(5)})
	if got != `{"seats":5}` {
		t.Errorf("JSON object cell rendered as %q, want JSON text", got)
	}

	got = cellText([]any{"a", "b"})
	if got != `["a","b"]` {
		t.Errorf("JSON array cell rendered as %q, want JSON text", got)
	}
}
