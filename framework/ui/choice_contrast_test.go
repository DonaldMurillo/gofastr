package ui

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// WCAG 1.4.11: an unchecked checkbox or radio is drawn by its border
// alone, so the border needs 3:1 against the surface. The reskin moved
// it onto the hairline border-strong token, 1.5:1 in light and 1.7:1 in
// dark (caught in review). The border's token clears 3:1 in both
// schemes of the default theme.
func TestChoiceBorderMeetsNonTextContrast(t *testing.T) {
	css := toggleStyle.Entry().CSSFor(theme.Default())
	i := strings.Index(css, ".fui-choice__input {")
	if i < 0 {
		t.Fatalf("no .fui-choice__input rule:\n%s", css)
	}
	rule := css[i : i+strings.Index(css[i:], "}")]
	m := regexp.MustCompile(`\n\s*border: 1px solid var\(--color-([a-z-]+)`).FindStringSubmatch(rule)
	if m == nil {
		t.Fatalf("the choice border is not a colour token:\n%s", rule)
	}
	th := theme.Default()
	props := th.CSSCustomProperties()
	light := func(name string) string {
		v := regexp.MustCompile(`--color-` + name + `:\s*(#[0-9A-Fa-f]{6})`).FindStringSubmatch(props)
		if v == nil {
			t.Fatalf("--color-%s is not a #RRGGBB custom property:\n%s", name, props)
		}
		return v[1]
	}
	for _, c := range []struct{ scheme, border, surface string }{
		{"light", light(m[1]), light("surface")},
		{"dark", th.DarkColors[m[1]], th.DarkColors["surface"]},
	} {
		if got := wcagContrast(t, c.border, c.surface); got < 3 {
			t.Errorf("%s: --color-%s %s on surface %s is %.2f:1, want >= 3:1", c.scheme, m[1], c.border, c.surface, got)
		}
	}
}

// wcagContrast is the WCAG 2.x ratio of two #RRGGBB colours.
func wcagContrast(t *testing.T, a, b string) float64 {
	t.Helper()
	lum := func(hex string) float64 {
		if len(hex) != 7 || hex[0] != '#' {
			t.Fatalf("not #RRGGBB: %q", hex)
		}
		var l [3]float64
		for i := range l {
			v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
			if err != nil {
				t.Fatalf("not #RRGGBB: %q", hex)
			}
			c := float64(v) / 255
			if c <= 0.04045 {
				l[i] = c / 12.92
			} else {
				l[i] = math.Pow((c+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
	}
	la, lb := lum(a), lum(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}
