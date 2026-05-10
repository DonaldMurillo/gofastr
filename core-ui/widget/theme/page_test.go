package theme_test

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core-ui/widget/theme"
)

func TestPageThemeIncludesDefaults(t *testing.T) {
	tt := theme.PageTheme()
	wantColors := []string{"page-bg", "page-fg", "page-primary", "page-accent"}
	for _, k := range wantColors {
		if _, ok := tt.Colors[k]; !ok {
			t.Errorf("default page theme missing color %q", k)
		}
	}
}

func TestPageThemeMergesOverrides(t *testing.T) {
	override := style.Theme{Colors: style.Colors{"page-bg": "#000000", "page-primary": "#FF00FF"}}
	tt := theme.PageTheme(override)
	if got := tt.Colors["page-bg"]; got != "#000000" {
		t.Errorf("page-bg = %q, want #000000 after override", got)
	}
	if got := tt.Colors["page-primary"]; got != "#FF00FF" {
		t.Errorf("page-primary = %q, want #FF00FF after override", got)
	}
	// Untouched keys should keep defaults.
	if tt.Colors["page-fg"] == "" {
		t.Errorf("page-fg lost on override merge")
	}
}

func TestPageCSSEmitsRootVarsAndUtilities(t *testing.T) {
	css := theme.PageCSS(theme.PageTheme())
	for _, want := range []string{
		":root",
		"--color-page-bg",
		"--color-page-primary",
		"--spacing-page-lg",
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
	override := style.Theme{Colors: style.Colors{"page-bg": "#123456"}}
	css := theme.PageCSS(theme.PageTheme(override))
	if !strings.Contains(css, "--color-page-bg: #123456") {
		t.Errorf(":root var didn't reflect override; css head:\n%s", head(css, 800))
	}
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
