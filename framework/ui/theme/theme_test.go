package theme

import (
	"strings"
	"testing"
)

func TestDefaultHasCanonicalTokens(t *testing.T) {
	th := Default()
	// Every canonical typed color token must have a non-empty Name.
	colors := []struct {
		name string
		got  string
	}{
		{"Primary", th.Colors.Primary.Name},
		{"Background", th.Colors.Background.Name},
		{"Surface", th.Colors.Surface.Name},
		{"Border", th.Colors.Border.Name},
		{"Text", th.Colors.Text.Name},
		{"Danger", th.Colors.Danger.Name},
		{"Success", th.Colors.Success.Name},
		{"Warning", th.Colors.Warning.Name},
		{"Info", th.Colors.Info.Name},
		{"Accent", th.Colors.Accent.Name},
	}
	for _, c := range colors {
		if c.got == "" {
			t.Errorf("Default theme Color %s missing Name", c.name)
		}
	}
	if th.Spacing.MD.Name == "" || th.Spacing.XL.Name == "" {
		t.Errorf("Default theme missing canonical spacing tokens")
	}
}

func TestSinglePrimaryOverrideSwapsToken(t *testing.T) {
	indigo := Default().Colors.Primary.Value
	teal := Default(Overrides{Primary: "#14B8A6"}).Colors.Primary.Value
	if teal != "#14B8A6" {
		t.Errorf("expected primary value=#14B8A6, got %q", teal)
	}
	if indigo == teal {
		t.Errorf("override did not change value")
	}
}

func TestEmptyOverridesUnchanged(t *testing.T) {
	a := Default()
	b := Default(Overrides{})
	if a.Colors.Primary.Value != b.Colors.Primary.Value {
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
