package style

import (
	"strings"
	"testing"
)

// blockOf returns the declarations of the first rule whose selector is
// exactly sel, or "" when there is none.
func blockOf(css, sel string) string {
	i := strings.Index(css, "\n"+sel+" {\n")
	if i < 0 {
		if !strings.HasPrefix(css, sel+" {\n") {
			return ""
		}
	} else {
		css = css[i+1:]
	}
	css = css[len(sel)+3:]
	return css[:strings.Index(css, "}")]
}

// The root declares the schemes it can paint, and each forced scheme
// names its own, so native controls follow the tokens with JS off and
// under a toggle that disagrees with the OS.
func TestSchemeIsDeclaredInCSS(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = map[string]string{"background": "#000000", "text": "#FFFFFF"}
	css := th.CSSCustomProperties()
	for sel, want := range map[string]string{
		":root":                             "color-scheme: light dark;",
		`:root[data-color-scheme="light"]`:  "color-scheme: light;",
		`:root[data-color-scheme="dark"]`:   "color-scheme: dark;",
		`:root [data-color-scheme="dark"]`:  "color-scheme: dark;",
		`:root [data-color-scheme="light"]`: "color-scheme: light;",
	} {
		if !strings.Contains(blockOf(css, sel), want) {
			t.Errorf("%s does not declare %q:\n%s", sel, want, css)
		}
	}
	// A light-only theme pins light: the static "light dark" meta must
	// not give a light page dark native controls under a dark OS.
	light := DefaultTheme().CSSCustomProperties()
	if !strings.Contains(light, ":root {\n  color-scheme: light;\n}") || strings.Contains(light, "light dark") {
		t.Errorf("a light-only theme must pin color-scheme: light and never claim dark:\n%s", light)
	}
}

// The subtree blocks carry the scheme's palette, the other scheme's
// values for the light block, and every root line that reads another
// variable, re-declared so it resolves against the subtree.
func TestSubtreeSchemeBlocksRebind(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Background = Color{Name: "background", Value: "#FAFAFA"}
	th.DarkColors = map[string]string{"background": "#111111"}
	th.Knobs = map[string]string{"ui-edge": "var(--color-background)"}
	css := th.CSSCustomProperties()
	dark := blockOf(css, `:root [data-color-scheme="dark"]`)
	light := blockOf(css, `:root [data-color-scheme="light"]`)
	for _, want := range []string{"--color-background: #111111;", "--ui-edge: var(--color-background);", "background-color: var(--color-background);"} {
		if !strings.Contains(dark, want) {
			t.Errorf("dark subtree block lacks %q:\n%s", want, dark)
		}
	}
	for _, want := range []string{"--color-background: #FAFAFA;", "--ui-edge: var(--color-background);"} {
		if !strings.Contains(light, want) {
			t.Errorf("light subtree block lacks %q:\n%s", want, light)
		}
	}
}

// A scoped theme names the scheme it paints: light in its own block,
// dark in its dark blocks when it has a dark palette.
func TestThemeOverrideDeclaresScheme(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = map[string]string{"background": "#000000"}
	css := ThemeOverrideCSS("abc", th)
	if !strings.Contains(css, "  color-scheme: light;\n}") {
		t.Errorf("the override's light block does not declare color-scheme: light:\n%s", css)
	}
	if strings.Count(css, "color-scheme: dark;") != 2 {
		t.Errorf("want color-scheme: dark in both dark blocks:\n%s", css)
	}
}
