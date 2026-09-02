package sortablelist

import (
	"strings"
	"testing"
)

// TestUserTextEscaped asserts every user-influenced string the pattern
// interpolates (Item.Label into the visible label AND the aria-label
// "Drag …", Item.Key into data-fui-sort-key, Config.Label into
// aria-label) renders as escaped text, never live markup. List rows are
// the canonical place apps stamp request-borne names (task titles,
// customer names), so the escaping contract is pinned at every sink
// the pattern owns.
//
// Item.Content is raw render.HTML by design (trusted builder output,
// same contract as store.BindHTML) and is out of scope.
func TestUserTextEscaped(t *testing.T) {
	hostile := []string{
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`" onmouseover="alert(1)`,
		`<SCRIPT>alert(1)</SCRIPT>`,
	}
	for _, h := range hostile {
		out := string(Render(Config{
			Label: "Rows",
			Items: []Item{{Key: h, Label: h}},
		}))
		low := strings.ToLower(out)
		// <svg excluded: the pattern's own trusted grip icon is an svg.
		for _, live := range []string{"<img", "<script", `onerror="`, `onload="`, `onmouseover="`} {
			if strings.Contains(low, live) {
				t.Errorf("SECURITY: [pattern-xss] sortablelist rendered live markup %q for input %q\nout: %s", live, h, out)
			}
		}
		if !strings.Contains(out, "&lt;") && !strings.Contains(out, "&quot;") {
			t.Errorf("sortablelist dropped the hostile text entirely for %q (guard must escape, not delete)\nout: %s", h, out)
		}
	}
}
