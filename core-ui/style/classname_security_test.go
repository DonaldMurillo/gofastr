package style

import (
	"strings"
	"testing"
)

// Pins: a utility class name interpolated into a CSS selector must be a
// valid CSS identifier — non-identifier names are skipped at both emission
// sites (GenerateUtilityCSS, GenerateCSS/GenerateChunk), mirroring the
// identifier-slot contract the family pins for StyleSheet property names
// and FontFaceCSS. (2026-09-06 adversarial pass, round 5; contract answer:
// yes, reject before interpolation.)
//
// Property: a utility class name interpolated into a CSS selector
// (.NAME { ... }) must be a valid CSS identifier — braces, semicolons, or
// url( in the name must never survive into the emitted rule, because the
// selector slot is unquoted: whatever the name spells becomes live CSS.
//
// Surfaces: core-ui/style/classes.go::GenerateUtilityCSS (".%s { %s }"),
// extract.go::GenerateCSS (same shape), extract.go::GenerateChunk
// (ExtractFromHTML's class regex feeds raw reflected names straight through).
//
// Finding (verified): class "text-lg{position:fixed;...;background:url(//evil/x)}x"
// emits .text-lg{...} with the braces as a REAL rule (full-viewport overlay +
// off-origin fetch); same via "bg-red{color:red}div",
// "w-4;}@media print{*{display:none}}",
// "font-bold;}*{background:url(https://evil.example/x)}"; the hostile name
// also lands verbatim inside the var() value slot.
//
// Fix direction: reject (or escape) class names that are not CSS identifiers
// before interpolation, mirroring the identifier-slot contract the package
// already enforces for StyleSheet property names.
func TestClassNameRedSelectorBreakout(t *testing.T) {
	hostile := []string{
		`text-lg{position:fixed;top:0;left:0;right:0;bottom:0;background:url(//evil/x)}x`,
		`bg-red{color:red}div`,
		`w-4;}@media print{*{display:none}}`,
		`font-bold;}*{background:url(https://evil.example/x)}`,
	}
	forbidden := []string{
		"url(//evil",
		"url(https://evil",
		";}@",
		"} *{",
		"}*{",
	}

	for _, class := range hostile {
		// Both emission sites, fed one hostile class each. The payloads are
		// space-free so the chunk path's whitespace split keeps them intact.
		surfaces := []struct {
			name string
			css  string
		}{
			{"GenerateUtilityCSS", GenerateUtilityCSS([]string{class}, DefaultTheme())},
			{"GenerateChunk", NewCSSExtractor(DefaultTheme()).GenerateChunk(`<div class="` + class + `">x</div>`)},
		}
		for _, s := range surfaces {
			for _, bad := range forbidden {
				if strings.Contains(s.css, bad) {
					t.Errorf("SECURITY: [pat-css-classname] %s: hostile class %q broke out of the .NAME selector slot — output contains %q, so the payload parses as live CSS. Emitted:\n%s", s.name, class, bad, s.css)
				}
			}
		}
	}

	// Control: the guard must not break legitimate utility classes.
	if css := GenerateUtilityCSS([]string{"bg-red"}, DefaultTheme()); !strings.Contains(css, "background-color: var(--color-red)") {
		t.Fatal("setup broken: legitimate class bg-red stopped emitting its theme declaration: " + css)
	}
}
