package multiselect

import (
	"strings"
	"testing"
)

// TestUserTextEscaped asserts every user-influenced string the pattern
// interpolates (Option.Label into the visible row label, Option.Value
// into the checkbox value attribute, Config.Placeholder into the
// summary text) renders as escaped text, never live markup. Option
// lists are the canonical place apps stamp request-borne content
// (tag lists, facets built from stored data).
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		out := string(Render(Config{
			Name:        "tags",
			Label:       "Tags",
			Placeholder: h,
			Options:     []Option{{Value: h, Label: h}},
		}))
		low := strings.ToLower(out)
		for _, live := range []string{"<img", "<svg", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] multiselect rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("multiselect dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
