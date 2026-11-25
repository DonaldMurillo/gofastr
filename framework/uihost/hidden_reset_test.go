package uihost

import (
	"strings"
	"testing"
)

// A server-rendered hidden="" element must not render. ui.Button's own
// display: inline-flex beat the user agent's [hidden] rule, and a
// timer screen showed all four of its phase buttons at once; the
// builtin CSS now carries the reset every page gets.
func TestBuiltinCSSHidesHiddenAttribute(t *testing.T) {
	if !strings.Contains(frameworkBuiltinCSS, `[hidden]:not([hidden="until-found" i]) { display: none !important; }`) {
		t.Fatal("frameworkBuiltinCSS lacks the [hidden] reset; a component display rule can show a hidden element")
	}
}

// hidden="until-found" stays out of the reset: display: none would stop
// find-in-page and fragment links from revealing a tab strip's
// inactive panels, which the browser hides with content-visibility.
func TestBuiltinCSSLeavesUntilFoundToTheBrowser(t *testing.T) {
	for _, line := range strings.Split(frameworkBuiltinCSS, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[hidden]") && strings.Contains(line, "display: none") &&
			!strings.Contains(line, `:not([hidden="until-found" i])`) {
			t.Fatalf("the [hidden] reset also hides hidden=until-found with display: none: %s", line)
		}
	}
}
