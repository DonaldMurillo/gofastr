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
	if !strings.Contains(frameworkBuiltinCSS, "[hidden] { display: none !important; }") {
		t.Fatal("frameworkBuiltinCSS lacks the [hidden] reset; a component display rule can show a hidden element")
	}
}
