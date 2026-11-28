package axecov

import "testing"

func TestNormalizeQueryOrFragmentOnlyUsesRoot(t *testing.T) {
	for _, raw := range []string{"?lang=en", "#main"} {
		if got := normalizePath(raw); got != "/" {
			t.Errorf("normalizePath(%q) = %q, want root", raw, got)
		}
	}
}
