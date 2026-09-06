package resource

import (
	"context"
	"strings"
	"testing"
)

// A bool form field must submit exactly "true" or "false" through the
// runtime's form intercept: a hidden "false" precedes the checkbox, and
// the checkbox carries value="true". A bare checkbox submits "on"
// (refused by the schema validator) or nothing (false unsaveable),
// which is how a desktop settings form failed to save.
func TestFormBoolFieldRoundTripsAsTrueFalse(t *testing.T) {
	var c Config
	for _, tc := range []struct {
		cur     string
		checked bool
	}{{"true", true}, {"false", false}, {"", false}, {"on", true}} {
		out := string(c.formInput(context.Background(), Field{Key: "notify_on_save", Type: "bool"}, tc.cur, nil))
		// Attributes render sorted, so inspect whole tags, not offsets.
		tags := strings.Split(out, "<input")
		if len(tags) != 3 {
			t.Fatalf("cur=%q: want exactly two inputs (hidden then checkbox), got %s", tc.cur, out)
		}
		hidden, box := tags[1], tags[2]
		if !strings.Contains(hidden, `type="hidden"`) || !strings.Contains(hidden, `name="notify_on_save"`) || !strings.Contains(hidden, `value="false"`) {
			t.Fatalf("cur=%q: first input must be hidden name=notify_on_save value=false, got %s", tc.cur, out)
		}
		if !strings.Contains(box, `type="checkbox"`) || !strings.Contains(box, `value="true"`) {
			t.Fatalf("cur=%q: second input must be the checkbox carrying value=true, got %s", tc.cur, out)
		}
		if got := strings.Contains(box, "checked"); got != tc.checked {
			t.Fatalf("cur=%q: checked=%v, want %v: %s", tc.cur, got, tc.checked, out)
		}
	}
}
