package style

import (
	"math"
	"strings"
	"testing"
)

func TestParseColorReadsCSSColourFunctions(t *testing.T) {
	cases := []struct {
		in      string
		r, g, b float64
	}{
		{"#FFFFFF", 1, 1, 1},
		{"#ffffffff", 1, 1, 1},
		{"#000f", 0, 0, 0},
		{"rgb(255, 0, 0)", 1, 0, 0},
		{"rgb(255 0 0 / 100%)", 1, 0, 0},
		{"rgba(0, 0, 255, 1)", 0, 0, 1},
		{"rgb(100% 50% 0%)", 1, 0.5, 0},
		{"hsl(120 100% 50%)", 0, 1, 0},
		{"hsl(0deg, 0%, 100%)", 1, 1, 1},
		{"hsla(0.5turn 100% 50%)", 0, 1, 1},
		{"oklab(1 0 0)", 1, 1, 1},
		{"oklab(0% 0 0)", 0, 0, 0},
		{"oklch(0.6279 0.2577 29.23)", 1, 0, 0},
		{"OKLCH(100% 0 none)", 1, 1, 1},
	}
	for _, c := range cases {
		r, g, b, ok := parseColor(c.in)
		if !ok {
			t.Errorf("parseColor(%q) not ok", c.in)
			continue
		}
		if math.Abs(r-c.r) > 0.005 || math.Abs(g-c.g) > 0.005 || math.Abs(b-c.b) > 0.005 {
			t.Errorf("parseColor(%q) = %.3f %.3f %.3f, want %.3f %.3f %.3f", c.in, r, g, b, c.r, c.g, c.b)
		}
	}
}

func TestParseColorSkipsWhatItCannotResolve(t *testing.T) {
	for _, in := range []string{
		"var(--color-text)", "white", "color-mix(in oklab, red 50%, white)",
		"rgb(0 0 0 / 50%)", "rgba(0,0,0,0.2)", "#00000080", "oklch(var(--l) 0 0)",
		"rgb(1 2)", "lab(50 0 0)", "", "#12345",
	} {
		if _, _, _, ok := parseColor(in); ok {
			t.Errorf("parseColor(%q) ok, want skipped", in)
		}
	}
}

// An oklch ink pair below AA is not refused (themes written that way
// booted before the parser read oklch) but is reported as a warning.
func TestContrastWarnings(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = DefaultDarkColors()
	if w := ContrastWarnings(th); len(w) != 0 {
		t.Fatalf("default theme warnings = %v, want none", w)
	}
	th.Colors.Primary = Color{Name: "primary", Value: "oklch(0.9 0.05 90)"}
	th.Colors.TextSubtle.Value = "#D4D4D8"
	th.DarkColors["text-subtle"] = "oklch(0.3 0 0)"
	if err := th.Validate(); err != nil {
		t.Fatalf("an oklch pair must warn, not refuse: %v", err)
	}
	got := strings.Join(ContrastWarnings(th), "\n")
	for _, want := range []string{"light: primary-fg #FFFFFF on primary oklch", "light: text-subtle #D4D4D8", "dark: text-subtle oklch(0.3 0 0)"} {
		if !strings.Contains(got, want) {
			t.Errorf("warnings missing %q:\n%s", want, got)
		}
	}
	// A hex pair Validate refuses is not repeated as a warning.
	th = DefaultTheme()
	th.Colors.PrimaryFg.Value = "#EEEEEE"
	th.Colors.Primary.Value = "#FFFFFF"
	if w := ContrastWarnings(th); len(w) != 0 {
		t.Errorf("hex pair repeated as a warning: %v", w)
	}
	th = DefaultTheme()
	th.Colors.TextSubtle.Value = "var(--x)"
	if w := ContrastWarnings(th); len(w) != 0 {
		t.Errorf("an unresolvable pair must be skipped, got %v", w)
	}
}
