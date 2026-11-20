package uihost

import (
	"regexp"
	"strings"
	"testing"
)

// The built-in floor's visually-hidden recipe (.cui-visually-hidden,
// .fui-visually-hidden, the skip link) clips with clip-path:
// inset(50%), never the deprecated clip property (#704).
func TestBuiltinCSSHidesWithClipPath(t *testing.T) {
	css := frameworkBuiltinCSS + frameworkPageLoadingCSS + frameworkProgressStripCSS + frameworkDimCSS
	if m := regexp.MustCompile(`(^|[^-])clip\s*:[^;]*`).FindString(css); m != "" {
		t.Errorf("the built-in sheet uses the deprecated clip property (%q); clip with clip-path: inset(50%%)", strings.TrimSpace(m))
	}
	if n := strings.Count(css, "clip-path: inset(50%)"); n < 2 {
		t.Errorf("the visually-hidden helper and the skip link should both clip with clip-path: inset(50%%), found %d", n)
	}
}
