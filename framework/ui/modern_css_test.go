package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// No kit sheet mixes colours in sRGB: it muddies tints toward grey, and
// oklab keeps them even (#711).
func TestKitMixesColoursInOklab(t *testing.T) {
	th := theme.Default()
	for _, e := range registry.All() {
		if css := e.CSSFor(th); strings.Contains(css, "in srgb") {
			t.Errorf("style %q mixes colours in srgb", e.Name)
		}
	}
}

// Kit headings balance their lines, the hero's fluid size mixes rem with
// vw so it follows the reader's font size, and inline code is sized by
// x-height where the browser can (#711).
func TestKitTypography(t *testing.T) {
	th := theme.Default()
	for _, c := range []struct {
		name  string
		style *registry.Style
		want  []string
	}{
		{"hero", heroStyle, []string{"text-wrap: balance", "calc(1.5rem + 3vw)", "calc(0.875rem + 0.6vw)"}},
		{"page header", pageHeaderStyle, []string{"text-wrap:balance"}},
		{"card heading", cardStyle, []string{"text-wrap: balance"}},
		{"table header", dataTableStyle, []string{"text-wrap: balance"}},
		{"inline code", inlineCodeStyle, []string{"@supports (font-size-adjust: 0.5)", "font-size-adjust: 0.5"}},
	} {
		css := c.style.Entry().CSSFor(th)
		for _, w := range c.want {
			if !strings.Contains(strings.ReplaceAll(css, ": ", ":"), strings.ReplaceAll(w, ": ", ":")) {
				t.Errorf("%s: sheet lacks %q", c.name, w)
			}
		}
	}
	if css := heroStyle.Entry().CSSFor(th); strings.Contains(css, ", 6vw,") || strings.Contains(css, ", 2.2vw,") {
		t.Error("hero clamp still prefers a bare vw size")
	}
}
