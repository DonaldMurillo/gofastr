package desktopui_test

import (
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The desktop theme must be a complete canonical theme: WithTheme
// AutoFills and MustValidates, so a missing token panics at boot. Every
// color must also pass the token grammar, because this package assigns
// color values directly and the emitter trusts the producer.
func TestThemeValidates(t *testing.T) {
	th := desktopui.Theme()
	if err := th.Validate(); err != nil {
		t.Fatalf("desktop theme does not validate: %v", err)
	}
	if th.Name != "desktop" {
		t.Errorf("Name = %q, want %q", th.Name, "desktop")
	}
}

// Every color token, light and dark, passes the bounded color grammar.
func TestThemeColorsPassGrammar(t *testing.T) {
	th := desktopui.Theme()
	validate := func(path, v string) {
		t.Helper()
		if err := style.ValidateColorValue(v); err != nil {
			t.Errorf("%s = %q: %v", path, v, err)
		}
	}
	validate("Colors.Background", th.Colors.Background.Value)
	validate("Colors.Text", th.Colors.Text.Value)
	validate("Colors.Accent", th.Colors.Accent.Value)
	validate("Colors.Primary", th.Colors.Primary.Value)
	validate("Colors.PrimaryFg", th.Colors.PrimaryFg.Value)
	validate("Colors.SurfaceSoft", th.Colors.SurfaceSoft.Value)
	for k, v := range th.DarkColors {
		validate("DarkColors["+k+"]", v)
	}
}

// Pin why the theme uses hex values where the plan wanted system
// keywords: the token grammar refuses them. Canvas is not a CSS named
// color in the allowlist, and a fallback chain is not one function
// call. The day the grammar learns system keywords, switch
// Background/Text/Accent to canvas/canvastext/AccentColor.
func TestColorGrammarRefusesSystemKeywords(t *testing.T) {
	for _, v := range []string{"canvas", "canvastext", "accentcolor", "AccentColor, -webkit-focus-ring-color"} {
		if err := style.ValidateColorValue(v); err == nil {
			t.Errorf("grammar accepted %q; the desktop theme's hex stand-ins rely on it being refused", v)
		}
	}
}

// The sourced values land verbatim in the emitted custom properties:
// fonts (the SF stack from the plan), the HIG type scale in px, and the
// system-blue accent pair.
func TestThemeEmitsSourcedTokens(t *testing.T) {
	css := desktopui.Theme().CSSCustomProperties()
	wants := []string{
		"--font-body: -apple-system, system-ui, ui-sans-serif, sans-serif;",
		"--font-heading: -apple-system, system-ui, ui-sans-serif, sans-serif;",
		"--font-mono: ui-monospace, 'SF Mono', Menlo, monospace;",
		// HIG type scale, 1 CSS px = 1 pt in a WKWebView.
		"--text-xs: 10px;",   // Caption 1/2 and Footnote
		"--text-sm: 11px;",   // Subheadline
		"--text-base: 13px;", // Body / Headline
		"--text-lg: 15px;",   // Title 3
		"--text-xl: 17px;",   // Title 2
		"--text-2xl: 22px;",  // Title 1
		"--text-3xl: 26px;",  // Large Title
		// Concentric radii: see the arithmetic in theme.go.
		"--radii-none: 0px;", "--radii-sm: 4px;", "--radii-md: 8px;",
		"--radii-lg: 12px;", "--radii-xl: 16px;", "--radii-full: 9999px;",
		// Canvas/CanvasText stand-ins (grammar refuses the keywords).
		"--color-background: #FFFFFF;", "--color-text: #000000;",
		// AccentColor stand-in: the macOS system blue.
		"--color-accent: #007AFF;", "--color-primary: #007AFF;",
		"--color-primary-fg: #FFFFFF;",
	}
	for _, w := range wants {
		if !strings.Contains(css, w) {
			t.Errorf("CSSCustomProperties missing %q\n--- CSS ---\n%s", w, css)
		}
	}
}

// The dark palette is matched to macOS dark surfaces and re-declares
// the accent pair so a scheme flip restyles the whole page.
func TestThemeDarkPalette(t *testing.T) {
	th := desktopui.Theme()
	if len(th.DarkColors) == 0 {
		t.Fatal("desktop theme declares no dark palette")
	}
	css := th.CSSCustomProperties()
	if !strings.Contains(css, `:root[data-color-scheme="dark"]`) {
		t.Errorf("no dark-scheme block in emitted CSS:\n%s", css)
	}
	wants := []string{
		"--color-background: #1E1E1E;",
		"--color-accent: #0A84FF;",
		"--color-primary: #0A84FF;",
	}
	for _, w := range wants {
		if !strings.Contains(css, w) {
			t.Errorf("dark palette missing %q\n--- CSS ---\n%s", w, css)
		}
	}
}

// NewStyleSheet binds the theme so component rules resolve {tokens.*}
// references to the desktop tokens, the same contract every framework
func TestStyleSheetResolvesThemeTokens(t *testing.T) {
	ss := style.NewStyleSheet(desktopui.Theme())
	css := ss.Rule(".probe").Set("font-family", "{fonts.body}", "border-radius", "{radii.md}").End().CSS()
	for _, w := range []string{
		"font-family: var(--font-body)",
		"border-radius: var(--radii-md)",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("stylesheet did not resolve token: %q in\n%s", w, css)
		}
	}
}
