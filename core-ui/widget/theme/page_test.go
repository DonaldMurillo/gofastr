package theme_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/theme"
)

func TestPageThemeIncludesCanonicalDefaults(t *testing.T) {
	tt := theme.PageTheme()
	// PageTheme now layers on top of canonical style.DefaultTheme(),
	// so every typed color/spacing field must be populated.
	if tt.Colors.Background.Name == "" {
		t.Errorf("page theme missing canonical Background token")
	}
	if tt.Colors.Text.Name == "" {
		t.Errorf("page theme missing canonical Text token")
	}
	if tt.Colors.Primary.Name == "" {
		t.Errorf("page theme missing canonical Primary token")
	}
}

func TestPageThemeOverridesViaDirectAssignment(t *testing.T) {
	tt := theme.PageTheme()
	// Apps override by directly assigning typed values, no
	// MergeThemes helper.
	tt.Colors.Background = style.Color{Name: "background", Value: "#000000"}
	tt.Colors.Primary = style.Color{Name: "primary", Value: "#FF00FF"}
	if tt.Colors.Background.Value != "#000000" {
		t.Errorf("override Background.Value didn't take")
	}
	if tt.Colors.Primary.Value != "#FF00FF" {
		t.Errorf("override Primary.Value didn't take")
	}
}

func TestPageCSSEmitsRootVarsAndUtilities(t *testing.T) {
	css := theme.PageCSS(theme.PageTheme())
	for _, want := range []string{
		":root",
		"--color-background",
		"--color-primary",
		"--spacing-lg",
		"body.kiln-app",
		".kiln-section",
		".kiln-card",
		".kiln-button",
		".kiln-grid-3",
		".kiln-hero",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("PageCSS missing %q", want)
		}
	}
}

func TestPageCSSReflectsOverrideValues(t *testing.T) {
	tt := theme.PageTheme()
	tt.Colors.Background = style.Color{Name: "background", Value: "#123456"}
	css := theme.PageCSS(tt)
	if !strings.Contains(css, "--color-background: #123456") {
		t.Errorf(":root var didn't reflect override; css head:\n%s", head(css, 800))
	}
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// A font size set from a token must follow that token when it is swapped.
//
// The rules used to hardcode the values: `.kiln-eyebrow` said "0.75rem"
// where the theme declares XS as exactly that, `.kiln-h2` said "1.5rem"
// against XXL, and `body.kiln-app` said "16px" against Base. The colors in
// the same file already went through {colors.*}, so the mechanism was
// present and the sizes simply bypassed it — the package doc promises "a
// single token swap re-skins the whole app", and these rules did not move.
//
// This asserts the PROPERTY rather than the spelling: change the token's
// value and the emitted CSS must change with it. Asserting that
// "var(--text-xs)" appears somewhere would pass just as well against a
// rule that never reads it.
func TestPageCSSFontSizesFollowTypographyTokens(t *testing.T) {
	base := theme.PageCSS(theme.PageTheme())
	for _, lit := range []string{`font-size: 0.75rem`, `font-size: 1.5rem`, `font-size: 16px`} {
		if strings.Contains(base, lit) {
			t.Errorf("emitted CSS still hardcodes %q; it maps exactly onto a declared typography token", lit)
		}
	}

	swapped := theme.PageTheme()
	swapped.Typography.XS = style.FontSize{Name: "xs", Value: "0.1rem"}
	swapped.Typography.XXL = style.FontSize{Name: "2xl", Value: "9.9rem"}
	swapped.Typography.Base = style.FontSize{Name: "base", Value: "5.5rem"}
	got := theme.PageCSS(swapped)

	for _, want := range []string{"0.1rem", "9.9rem", "5.5rem"} {
		if !strings.Contains(got, want) {
			t.Errorf("swapping a typography token did not reach the emitted CSS: %q absent", want)
		}
	}
	// And the old values must be gone from the declarations they came from,
	// or the swap only added a variable nothing reads.
	for _, gone := range []string{"--text-xs: 0.75rem", "--text-2xl: 1.5rem"} {
		if strings.Contains(got, gone) {
			t.Errorf("token declaration %q survived the swap", gone)
		}
	}
}
