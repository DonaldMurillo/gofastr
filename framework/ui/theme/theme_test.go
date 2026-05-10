package theme

import (
	"strings"
	"testing"
)

func TestDefaultHasCanonicalTokens(t *testing.T) {
	th := Default()
	for _, k := range []string{
		"background", "surface", "surface-soft", "border", "border-strong",
		"text", "text-muted", "text-subtle", "primary", "primary-fg",
		"accent", "success", "warning", "danger", "info",
	} {
		if _, ok := th.Colors[k]; !ok {
			t.Errorf("Default theme missing color token %q", k)
		}
	}
	for _, k := range []string{"xs", "sm", "md", "lg", "xl", "2xl", "3xl"} {
		if _, ok := th.Spacing[k]; !ok {
			t.Errorf("Default theme missing spacing token %q", k)
		}
	}
}

func TestSinglePrimaryOverrideSwapsToken(t *testing.T) {
	indigo := Default().Colors["primary"]
	teal := Default(Overrides{Primary: "#14B8A6"}).Colors["primary"]
	if teal != "#14B8A6" {
		t.Errorf("expected primary=#14B8A6, got %q", teal)
	}
	if indigo == teal {
		t.Errorf("override did not change value")
	}
}

func TestEmptyOverridesUnchanged(t *testing.T) {
	a := Default()
	b := Default(Overrides{})
	if a.Colors["primary"] != b.Colors["primary"] {
		t.Errorf("empty overrides should not change tokens")
	}
}

func TestCSSCustomPropertiesEmitsTokens(t *testing.T) {
	css := Default().CSSCustomProperties()
	for _, want := range []string{
		"--color-primary", "--color-surface", "--color-danger",
		"--spacing-md", "--radii-md", "--font-body",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSSCustomProperties missing %q", want)
		}
	}
}
