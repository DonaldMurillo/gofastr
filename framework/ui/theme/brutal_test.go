package theme

import (
	"strings"
	"testing"
)

// Brutal re-skins the same tokens: square corners, 2px strokes, hard
// offset shadows drawn in the border colour (so they flip with the
// scheme), Archivo type, and a yellow primary with black ink, in light
// and dark. It validates like any theme.
func TestBrutalReskinsTheTokens(t *testing.T) {
	th := Brutal()
	if err := th.Validate(); err != nil {
		t.Fatalf("Brutal does not validate: %v", err)
	}
	for _, r := range []int{th.Radii.SM.Value, th.Radii.MD.Value, th.Radii.LG.Value, th.Radii.XL.Value} {
		if r != 0 {
			t.Errorf("a radius is %d, want 0", r)
		}
	}
	if th.Strokes.Thin.Value != "2px" {
		t.Errorf("thin stroke %q", th.Strokes.Thin.Value)
	}
	if !strings.Contains(th.Shadows.SM.Value, "var(--color-border") || strings.Contains(th.Shadows.SM.Value, "rgba") {
		t.Errorf("SM shadow %q is not a hard border-coloured offset", th.Shadows.SM.Value)
	}
	if !strings.Contains(th.Fonts.Heading.Value, "Archivo") {
		t.Errorf("heading font %q", th.Fonts.Heading.Value)
	}
	if th.Colors.Primary.Value != "#FFD60A" || th.DarkColors["primary"] != "#FFD60A" || th.DarkColors["background"] == "" {
		t.Errorf("primary %q, dark %v", th.Colors.Primary.Value, th.DarkColors)
	}
}
