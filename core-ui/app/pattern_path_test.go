package app

import "testing"

// One substitution for the static export and the sitemap: two copies
// drifted once (the sitemap escaped a catch-all's "/" and named no file).
func TestPatternPathRoutingForm(t *testing.T) {
	for _, c := range []struct {
		pattern string
		params  map[string]string
		want    string
	}{
		{"/items/:id", map[string]string{"id": "a/b"}, "/items/a%2Fb"},
		{"/items/:id", map[string]string{"id": "a%b"}, "/items/a%25b"},
		{"/items/:id:int", map[string]string{"id": "7"}, "/items/7"},
		{"/docs/:rest*", map[string]string{"rest": "x%y/z"}, "/docs/x%25y/z"},
		{"/items/:id", map[string]string{}, "/items/:id"},
		{"/items/:id", map[string]string{"id": "café?"}, "/items/café?"},
	} {
		if got := PatternPath(c.pattern, c.params); got != c.want {
			t.Errorf("PatternPath(%q, %v) = %q, want %q", c.pattern, c.params, got, c.want)
		}
	}
}
