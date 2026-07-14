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

func TestDarkColorOverridesKeepBrandingAdaptive(t *testing.T) {
	th := Default(Overrides{
		Primary:    "#0F766E",
		DarkColors: map[string]string{"primary": "#5EEAD4", "surface": "#10201E"},
	})
	if th.Colors.Primary.Value != "#0F766E" {
		t.Fatalf("light primary override missing: %q", th.Colors.Primary.Value)
	}
	if th.DarkColors["primary"] != "#5EEAD4" || th.DarkColors["surface"] != "#10201E" {
		t.Fatalf("dark overrides missing: %#v", th.DarkColors)
	}
	if Default(Overrides{Primary: "#0F766E"}).DarkColors["primary"] != Default().DarkColors["primary"] {
		t.Fatal("light override must not be copied into dark mode without an explicit contrast-safe value")
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

func TestDefaultShipsAdaptiveDarkPalette(t *testing.T) {
	th := Default()
	for _, token := range []string{
		"background", "surface", "surface-soft", "border", "border-strong",
		"text", "text-muted", "text-subtle", "primary", "primary-fg",
		"secondary", "secondary-fg", "accent", "success", "warning", "danger", "info",
	} {
		if strings.TrimSpace(th.DarkColors[token]) == "" {
			t.Errorf("Default theme missing dark token %q", token)
		}
	}
	css := th.CSSCustomProperties()
	for _, want := range []string{
		`:root[data-color-scheme="dark"]`,
		`--color-background: #111113;`,
		`--color-primary: #A5B4FC;`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("adaptive theme CSS missing %q\n%s", want, css)
		}
	}
}
