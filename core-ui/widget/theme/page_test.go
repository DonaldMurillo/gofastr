package theme_test

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core-ui/widget/theme"
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
	// Apps override by directly assigning typed values — no
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
