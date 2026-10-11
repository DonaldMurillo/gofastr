package style

import (
	"strings"
	"testing"
)

// Border and outline widths are theme tokens, so a theme alone can
// draw the kit in thick strokes (neo-brutalism) or none at all.
func TestStrokesAreEmitted(t *testing.T) {
	css := DefaultTheme().CSSCustomProperties()
	for _, decl := range []string{
		"--stroke-thin: 1px;",
		"--stroke-thick: 2px;",
		"--stroke-focus: 2px;",
		"--stroke-focus-offset: 2px;",
	} {
		if !strings.Contains(css, decl) {
			t.Errorf("missing %q in :root", decl)
		}
	}
	if got := DefaultTheme().Strokes.Thin.CSS(); got != "var(--stroke-thin)" {
		t.Errorf("Thin.CSS() = %q", got)
	}
}

func TestZeroStrokeValidates(t *testing.T) {
	th := DefaultTheme()
	th.Strokes.Thin = Stroke{Name: "thin", Value: "0"}
	if err := th.Validate(); err != nil {
		t.Fatalf("a borderless theme must validate: %v", err)
	}
	if css := th.CSSCustomProperties(); !strings.Contains(css, "--stroke-thin: 0;") {
		t.Errorf("--stroke-thin is not emitted as 0")
	}
}

func TestBadStrokeIsRefused(t *testing.T) {
	for _, v := range []string{"-1px", "calc(1px - 2px)", "thick", "1"} {
		th := DefaultTheme()
		th.Strokes.Thick = Stroke{Name: "thick", Value: v}
		if err := th.Validate(); err == nil {
			t.Errorf("stroke %q must fail validation", v)
		}
	}
}

// A theme.go written before strokes existed has no Strokes field. Its
// init runs AutoFillNames, and a named zero would validate and erase
// every border, so an unset stroke stays unnamed; the emitter then
// writes the default width, so a var(--stroke-*) with no fallback (a
// style.Use utility, an owned sheet) still resolves, and the token map
// lists and edits it.
func TestUnsetStrokesEmitDefaults(t *testing.T) {
	th := DefaultTheme()
	th.Strokes = StrokeSet{}
	AutoFillNames(&th)
	if th.Strokes.Thin.Name != "" {
		t.Errorf("AutoFillNames named an unset stroke %q", th.Strokes.Thin.Name)
	}
	if err := th.Validate(); err != nil {
		t.Fatalf("a theme without strokes must validate: %v", err)
	}
	css := th.CSSCustomProperties()
	for _, want := range []string{"--stroke-thin: 1px;", "--stroke-thick: 2px;", "--stroke-focus: 2px;", "--stroke-focus-offset: 2px;"} {
		if !strings.Contains(css, want) {
			t.Errorf("an unset stroke did not emit its default %s:\n%s", want, css)
		}
	}
	if got := ThemeToTokens(th)["stroke-thin"]; got != "1px" {
		t.Errorf("ThemeToTokens[stroke-thin] = %q, want the default 1px", got)
	}
	edited, err := ApplyTokens(th, map[string]string{"stroke-thin": "3px"})
	if err != nil {
		t.Fatalf("ApplyTokens on a theme without strokes: %v", err)
	}
	if edited.Strokes.Thin.Value != "3px" || edited.Strokes.Thick.Value != "2px" {
		t.Errorf("edited strokes = %+v, want thin 3px and the other defaults", edited.Strokes)
	}
	if th.Strokes != (StrokeSet{}) {
		t.Errorf("ApplyTokens wrote through to its base: %+v", th.Strokes)
	}
	// A stroke the theme sets keeps its value beside the defaults.
	th.Strokes.Thick = Stroke{Name: "thick", Value: "4px"}
	if css := th.CSSCustomProperties(); !strings.Contains(css, "--stroke-thick: 4px;") || !strings.Contains(css, "--stroke-thin: 1px;") {
		t.Errorf("a set stroke beside unset ones:\n%s", css)
	}
	th.Strokes = StrokeSet{}
	th.Strokes.Thin = Stroke{Value: "3px"}
	AutoFillNames(&th)
	if th.Strokes.Thin.Name != "thin" {
		t.Errorf("a set stroke was not named: %q", th.Strokes.Thin.Name)
	}
}

func TestStrokeTokensApplyAndParse(t *testing.T) {
	th, err := ApplyTokens(DefaultTheme(), map[string]string{"stroke-thin": "3px"})
	if err != nil {
		t.Fatalf("ApplyTokens: %v", err)
	}
	if th.Strokes.Thin.Value != "3px" {
		t.Errorf("stroke-thin = %q, want 3px", th.Strokes.Thin.Value)
	}
	if got := ThemeToTokens(th)["stroke-thin"]; got != "3px" {
		t.Errorf("ThemeToTokens stroke-thin = %q", got)
	}
	if _, err := ApplyTokens(DefaultTheme(), map[string]string{"stroke-thin": "-3px"}); err == nil {
		t.Error("ApplyTokens accepted a negative stroke")
	}
	if cat := TokenCategory("stroke-focus-offset"); cat != "stroke" {
		t.Errorf("TokenCategory = %q, want stroke", cat)
	}
	tok, err := ParseToken("stroke-heavy", "4px")
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if s, ok := tok.(Stroke); !ok || s.Value != "4px" || s.Name != "heavy" {
		t.Errorf("ParseToken = %#v", tok)
	}
	if got := th.ResolveAll("{strokes.thick}"); got != "var(--stroke-thick)" {
		t.Errorf("Resolve = %q", got)
	}
}
