package tabs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestUserTextEscaped asserts every user-influenced string the pattern
// interpolates (Tab.Label into the visible <summary> text, Config.Label
// into the wrapper aria-label) renders as escaped text, never live
// markup. Tab strips are the canonical place apps stamp request-borne
// section names.
//
// Tab.Content is raw render.HTML by design (trusted builder output)
// and is out of scope.
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		out := string(New(Config{Name: "t", Label: h},
			Tab{Label: h, Content: render.Text("panel")}))
		low := strings.ToLower(out)
		for _, live := range []string{"<img", "<svg", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] tabs rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("tabs dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
