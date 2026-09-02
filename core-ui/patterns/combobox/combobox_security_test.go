package combobox

import (
	"strings"
	"testing"
)

// TestUserTextEscaped asserts every user-influenced string the pattern
// interpolates (Option.Label and Option.Meta into the visible row text,
// Option.Value into the data-value attribute, Config.Label into the
// label element AND the listbox aria-label, Config.Placeholder into the
// input placeholder) renders as escaped text, never live markup.
// Option lists are the canonical place apps stamp request-borne content
// (command palettes over stored records).
//
// Option.Href's scheme allow-list is pinned separately by
// core-ui/html's TestHTMLBuildersDropJSSchemes (the shared urlsafe
// policy); Config.EmptyHTML is raw markup by design (trusted) and is
// out of scope.
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		out := string(Render(Config{
			ID:          "q",
			Name:        "q",
			Label:       h,
			Placeholder: h,
			Options:     []Option{{Value: h, Label: h, Meta: h}},
		}))
		low := strings.ToLower(out)
		for _, live := range []string{"<img", "<svg", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] combobox rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("combobox dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
