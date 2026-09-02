package disclosure

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestUserTextEscaped asserts the user-influenced string the pattern
// interpolates (Config.Title into the always-visible <summary> text)
// renders as escaped text, never live markup. Disclosure titles are
// the canonical place apps stamp request-borne headings (stored FAQ
// questions, admin-written labels).
//
// The body children are raw render.HTML by design (trusted builder
// output) and are out of scope.
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		out := string(Render(Config{Title: h}, render.Text("body")))
		low := strings.ToLower(out)
		for _, live := range []string{"<img", "<svg", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] disclosure rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("disclosure dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
