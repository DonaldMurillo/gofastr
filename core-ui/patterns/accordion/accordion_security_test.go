package accordion

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestUserTextEscaped asserts the user-influenced strings the pattern
// interpolates (Item.Summary into the visible <summary> text,
// GroupConfig/StackConfig AriaLabel into the wrapper aria-label)
// render as escaped text, never live markup. FAQ/section summaries
// are the canonical place apps stamp request-borne titles.
//
// Item.Content is raw markup by design (trusted builder output, the
// same contract as disclosure's body and store.BindHTML) and is out
// of scope.
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		cfg := StackConfig{AriaLabel: h}
		out := string(Stack(cfg, Item{Summary: h, Content: render.Text("body")}))
		low := strings.ToLower(out)
		for _, live := range []string{"<img", "<svg", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] accordion rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("accordion dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
