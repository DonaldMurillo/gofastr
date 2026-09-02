package progress

import (
	"strings"
	"testing"
)

// TestUserTextEscaped asserts the user-influenced strings the pattern
// interpolates (Config.Label into aria-label, Config.Description into
// the visible text next to the bar) render as escaped text, never live
// markup. Descriptions are the canonical place apps stamp request-borne
// state ("Uploading <filename>…").
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		out := string(New(Config{Value: 3, Max: 10, Label: h, Description: h}))
		low := strings.ToLower(out)
		for _, live := range []string{"<img", "<svg", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] progress rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("progress dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
