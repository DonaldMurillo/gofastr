package style

import (
	"strings"
	"testing"
)

// The theme-value CSS boundary lives at INGESTION, not emission:
// ApplyTokens validates every config-borne value (validateFreeFormCSS
// / validateColorValue), and kiln's world.SafeThemeValue validates
// agent-authored values before they reach a Theme. FontFaceCSS is the
// remaining ingestion path with no validator: the blueprint generator
// feeds it font family names straight out of the host app's gofastr.yml
// theme map (cmd/gofastr/blueprint.go: blueprintFontFaceCSS(theme)),
// so the strings arrive file-borne, not developer-typed.

// TestFontFaceCSSRejectsDeclarationBreakers asserts a WebFont field
// arriving from host config cannot break out of its @font-face rule.
//
// Property: whatever a config file spells in Family/File/Style/Weight/
// Display (or the dir prefix), the emitted CSS contains exactly one
// balanced rule per font — no extra declarations, no extra rules.
// A `'` closes the quoted family, a `;` ends a declaration early, a
// `}` closes the whole rule and lets everything after it parse as new
// CSS (a `} * { ... }` payload restyles the page; a `} @import url(...)`
// fetches off-origin).
//
// Surfaces: every WebFont field FontFaceCSS interpolates, plus the dir
// prefix, all through the single FontFaceCSS emission site. The shape
// check (balanced braces, one rule per font) is used instead of
// blacklisting payloads so any breakout, however spelled, fails.
func TestFontFaceCSSRejectsDeclarationBreakers(t *testing.T) {
	hostile := []struct {
		name string
		font WebFont
		dir  string
	}{
		{"family quote breakout", WebFont{Family: `Inter';} *{color:red}`}, ""},
		{"family rule close", WebFont{Family: "Inter}"}, ""},
		{"family decl breaker", WebFont{Family: "Inter;x:y"}, ""},
		{"file url breakout", WebFont{Family: "Inter", File: `inter') format('woff2');} *{background:red}`}, ""},
		{"style decl breaker", WebFont{Family: "Inter", Style: "normal; } *{x:y"}, ""},
		{"weight rule close", WebFont{Family: "Inter", Weight: "400}"}, ""},
		{"display breakout", WebFont{Family: "Inter", Display: "swap'} *{x:y"}, ""},
		{"dir prefix breakout", WebFont{Family: "Inter"}, "/f';} *{x:y"},
	}

	for _, h := range hostile {
		css := FontFaceCSS(h.dir, h.font)
		opens := strings.Count(css, "{")
		closes := strings.Count(css, "}")
		decls := strings.Count(css, ";")
		// One rule per font: 1 brace pair and exactly the 5 declarations
		// FontFaceCSS itself writes (family, style, weight, display, src).
		if opens != 1 || closes != 1 {
			t.Errorf("SECURITY: [fontface-css] %s: payload broke out of the @font-face rule (braces: %d open / %d close for 1 font). Emitted:\n%s", h.name, opens, closes, css)
		}
		if decls != 5 {
			t.Errorf("SECURITY: [fontface-css] %s: payload added declarations (%d semicolons, FontFaceCSS writes 5). Emitted:\n%s", h.name, decls, css)
		}
	}

	// Happy path: a legitimate font still renders its one balanced rule.
	ok := FontFaceCSS("", WebFont{Family: "Bricolage Grotesque"})
	if strings.Count(ok, "{") != 1 || strings.Count(ok, "}") != 1 || !strings.Contains(ok, "font-family: 'Bricolage Grotesque'") {
		t.Errorf("FontFaceCSS broke the legitimate font after guarding: %s", ok)
	}
}
