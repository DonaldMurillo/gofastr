package style

import (
	"strings"
	"testing"
)

// TestStyleSheetRejectsNonIdentPropNames pins the CSS property-name
// validation at the registration APIs, added by the 2026-09-04
// emitident sweep; the emitter (writeRuleInner) writes "  %s: %s;" with
// the name verbatim, so a non-identifier name is a declaration
// breakout, not a typo.
//
// Property: a property name reaching the stylesheet emitter is a valid
// CSS identifier (or --custom property); anything else panics at the
// builder call, like the odd-count Set panic.
// Surfaces: StyleSheet.Set, Pseudo, Child, Keyframes (via Step props) —
// every variadic prop/value entry point.
func TestStyleSheetRejectsNonIdentPropNames(t *testing.T) {
	bad := []string{
		"",
		"color; } * { color:red",
		"color:url(evil)",
		"color:x;y",
		"1color",
		"-2color",
		"--",
		"--1x",
		"co lor",
		"color\n",
	}
	good := []string{
		"color",
		"-webkit-line-clamp",
		"--ui-record-summary-accent",
		"grid-template-columns",
		"_private",
		"a9",
	}

	for _, name := range bad {
		for _, call := range []struct {
			what string
			fn   func(*StyleSheet)
		}{
			{"Set", func(ss *StyleSheet) { ss.Rule(".x").Set(name, "red").End() }},
			{"Pseudo", func(ss *StyleSheet) { ss.Rule(".x").Pseudo(":hover", name, "red").End() }},
			{"Child", func(ss *StyleSheet) { ss.Rule(".x").Child(".y", name, "red").End() }},
			{"Keyframes", func(ss *StyleSheet) { ss.Keyframes("k", Step("0%", name, "red")) }},
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("SECURITY: [stylesheet-propname] %s(%q) must panic: the name is written verbatim into an identifier slot of the emitted CSS", call.what, name)
					}
				}()
				call.fn(NewStyleSheet(DefaultTheme()))
			}()
		}
	}

	for _, name := range good {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("valid property name %q must not panic: %v", name, r)
				}
			}()
			ss := NewStyleSheet(DefaultTheme())
			ss.Rule(".x").Set(name, "red").Pseudo(":hover", name, "blue").Child(".y", name, "green").End()
			ss.Keyframes("k", Step("0%", name, "red"))
			css := ss.CSS()
			if !strings.Contains(css, name+":") {
				t.Errorf("property %q missing from emitted CSS:\n%s", name, css)
			}
		}()
	}
}
