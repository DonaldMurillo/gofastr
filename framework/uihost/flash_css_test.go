package uihost

import (
	"regexp"
	"strings"
	"testing"
)

// data-cui-flash-on-update toggles .cui-flash on a signal-bound node
// after each update (core-ui/runtime/frag/signals.js). The class needs
// a rule every page ships, or the documented flash shows nowhere but
// kiln's chat sheet. The rule animates only for users who have not
// asked for reduced motion, and reads its timing and colour from the
// theme.
func TestBuiltinCSSStylesSignalFlash(t *testing.T) {
	css := frameworkBuiltinCSS
	guard := regexp.MustCompile(`@media \(prefers-reduced-motion: no-preference\) \{\s*\.cui-flash \{[^}]*animation:[^}]*\}`)
	block := guard.FindString(css)
	if block == "" {
		t.Fatal("frameworkBuiltinCSS has no .cui-flash animation inside a prefers-reduced-motion: no-preference block")
	}
	if !strings.Contains(block, "var(--duration-") || !strings.Contains(block, "var(--easing-") {
		t.Errorf(".cui-flash timing is not read from the theme's duration and easing tokens:\n%s", block)
	}
	kf := regexp.MustCompile(`@keyframes cui-flash \{[^@]*\}\s*\}`).FindString(css)
	if kf == "" {
		t.Fatal("frameworkBuiltinCSS defines no @keyframes cui-flash")
	}
	if !strings.Contains(kf, "var(--color-primary") {
		t.Errorf("the cui-flash highlight is not drawn from --color-primary:\n%s", kf)
	}
	if regexp.MustCompile(`(?m)^\.cui-flash`).MatchString(css) {
		t.Error(".cui-flash is styled outside the reduced-motion guard")
	}
}
