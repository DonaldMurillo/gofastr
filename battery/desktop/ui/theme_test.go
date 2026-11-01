package desktopui_test

import (
	"os"
	"path/filepath"
	"regexp"
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
	th := desktopui.ThemeFor("darwin")
	if err := th.Validate(); err != nil {
		t.Fatalf("desktop theme does not validate: %v", err)
	}
	if th.Name != "desktop" {
		t.Errorf("Name = %q, want %q", th.Name, "desktop")
	}
}

// Every color token, light and dark, passes the bounded color grammar.
func TestThemeColorsPassGrammar(t *testing.T) {
	th := desktopui.ThemeFor("darwin")
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
	css := desktopui.ThemeFor("darwin").CSSCustomProperties()
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
		"--color-accent: #007AFF;", "--color-primary: #0071E3;",
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
	th := desktopui.ThemeFor("darwin")
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
		"--color-primary: #0071E3;",
	}
	for _, w := range wants {
		if !strings.Contains(css, w) {
			t.Errorf("dark palette missing %q\n--- CSS ---\n%s", w, css)
		}
	}
}

// The primary stays #0071E3, not the system blue #007AFF (#607). A filled
// button's label is 13px regular, which WCAG counts as normal text, so a
// large-text exception would not cover it: white on #007AFF is 4.0:1 and
// the theme would fail its own AA floor. The system blue lives on Accent
// (focus rings, selection, links), where no white text sits on it. This
// test pins both halves: the shipped pair validates on every platform,
// and swapping in the system blue is refused.
func TestPrimaryStaysAAUnderWhite(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		th := desktopui.ThemeFor(goos)
		if err := th.Validate(); err != nil {
			t.Fatalf("%s: shipped theme fails Validate: %v", goos, err)
		}
		th.Colors.Primary = style.Color{Name: "primary", Value: "#007AFF"}
		th.DarkColors["primary"] = "#007AFF"
		err := th.Validate()
		if err == nil || !strings.Contains(err.Error(), "primary") {
			t.Errorf("%s: white on #007AFF passed Validate (err=%v); the 4.5:1 floor no longer guards primary", goos, err)
		}
	}
}

// NewStyleSheet binds the theme so component rules resolve {tokens.*}
// references to the desktop tokens, the same contract every framework
func TestStyleSheetResolvesThemeTokens(t *testing.T) {
	ss := style.NewStyleSheet(desktopui.ThemeFor("darwin"))
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

// A desktop window is pointer-driven: the theme sizes controls to the
// WCAG 2.5.8 target floor (24), not the 44-point touch floor that made
// every button and field about 40 points tall.
func TestThemeTouchTargetIsPointerSized(t *testing.T) {
	if got := desktopui.ThemeFor("darwin").Layout.TouchTarget.Value; got != 24 {
		t.Errorf("TouchTarget = %d, want 24", got)
	}
}

// GOFASTR1808 skips this package's colour fallbacks, which follow the
// desktop theme rather than the framework's, so they are judged here:
// every var(--color-*, …) fallback, whatever its spelling, is a keyword,
// a nested var(), or a hex the desktop theme declares for some platform
// and scheme. An rgba() or named colour is reported, as GOFASTR1808
// reports it elsewhere.
func TestColourFallbacksFollowTheme(t *testing.T) {
	declared := map[string]map[string]bool{}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for k, v := range style.ThemeToTokens(desktopui.ThemeFor(goos)) {
			k = strings.TrimPrefix(k, "dark.")
			if declared[k] == nil {
				declared[k] = map[string]bool{}
			}
			declared[k][strings.ToUpper(v)] = true
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`var\(\s*--(color-[A-Za-z0-9_-]+)\s*,\s*([^()]*?(?:\([^()]*\))?)\s*\)`)
	keywords := map[string]bool{"currentcolor": true, "transparent": true, "inherit": true, "initial": true, "unset": true, "revert": true, "revert-layer": true}
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen++
			v := strings.ToUpper(m[2])
			if keywords[strings.ToLower(v)] || strings.HasPrefix(v, "VAR(") {
				continue
			}
			if len(v) == 4 && v[0] == '#' {
				v = "#" + strings.Repeat(v[1:2], 2) + strings.Repeat(v[2:3], 2) + strings.Repeat(v[3:4], 2)
			}
			if !declared[m[1]][v] {
				t.Errorf("%s: var(--%s, %s) names no value the desktop theme declares for --%s", f, m[1], m[2], m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no colour fallbacks: the scan is reading the wrong files")
	}
}
