package ui

import (
	"strings"
	"testing"
)

func TestBackToTopExtraAttrsOnRoot(t *testing.T) {
	h := BackToTop(BackToTopConfig{
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("button root missing data-test:\n%s", root)
	}
}

// The zero Position is documented as bottom-right, but no corner class
// rode the button, so position: fixed with no offsets left it where it
// fell in the flow: over a section heading mid-page.
func TestBackToTopDefaultsToBottomRight(t *testing.T) {
	out := string(BackToTop(BackToTopConfig{}))
	if !strings.Contains(out, "fui-back-to-top--br") {
		t.Fatalf("zero Position must anchor bottom-right:\n%s", out)
	}
}
