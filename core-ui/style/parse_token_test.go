package style_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// ParseToken is the one place a CSS value becomes a typed token: the
// tokens.css generator and ApplyTokens judge a value by the same rule.

func TestParseTokenTypes(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		want       any
	}{
		{"color-highlight", "#0F766E", style.Color{Name: "highlight", Value: "#0F766E"}},
		{"size-page-width", "67.5rem", style.Size{Name: "page-width", Value: "67.5rem"}},
		{"spacing-gutter", "24px", style.Spacing{Name: "gutter", Value: 24}},
		{"radii-card", "12px", style.Radius{Name: "card", Value: 12}},
		{"font-display", "\"Fraunces\", serif", style.Font{Name: "display", Value: "\"Fraunces\", serif"}},
		{"font-weight-display", "800", style.FontWeight{Name: "display", Value: 800}},
		{"shadow-lift", "0 8px 24px rgb(0 0 0 / 12%)", style.Shadow{Name: "lift", Value: "0 8px 24px rgb(0 0 0 / 12%)"}},
		{"z-banner", "40", style.ZIndexValue{Name: "banner", Value: 40}},
		{"duration-unroll", "260ms", style.Duration{Name: "unroll", Value: 260 * time.Millisecond}},
		{"easing-unroll", "cubic-bezier(0.2, 0, 0, 1)", style.Easing{Name: "unroll", Value: "cubic-bezier(0.2, 0, 0, 1)"}},
		{"text-hero", "clamp(2.5rem, 6vw, 4.5rem)", style.FontSize{Name: "hero", Value: "clamp(2.5rem, 6vw, 4.5rem)"}},
	} {
		got, err := style.ParseToken(tc.key, tc.value)
		if err != nil {
			t.Errorf("%s: %v", tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %#v, want %#v", tc.key, got, tc.want)
		}
	}
}

func TestParseTokenRefusesBadValues(t *testing.T) {
	for key, value := range map[string]string{
		"color-x":       "red; --y: 0",
		"size-x":        "huge",
		"spacing-x":     "1.5rem",
		"font-weight-x": "1001",
		"duration-x":    "fast",
		"z-x":           "top",
		"shadow-x":      "0 0 1px red} body {",
	} {
		if _, err := style.ParseToken(key, value); err == nil {
			t.Errorf("%s accepted %q", key, value)
		}
	}
}

func TestParseTokenRefusesKeys(t *testing.T) {
	for _, key := range []string{"breakpoint-x", "tk-x", "brand-glow", "color-", "size-Page", "size-page_width", "size--x", "size-x-"} {
		if _, err := style.ParseToken(key, "1px"); err == nil {
			t.Errorf("ParseToken accepted the key %q", key)
		}
	}
}

// Dark values ride on the token set: a set with a DarkTokens method
// fills Theme.DarkColors when the theme has a dark palette, and leaves
// a light-only theme light-only.
type darkBrand struct {
	BrandGlow style.Color
}

func (darkBrand) DarkTokens() map[string]string {
	return map[string]string{"brand-glow": "#FFB066"}
}

func TestExtendTakesDarkValues(t *testing.T) {
	base := style.DefaultTheme()
	base.DarkColors = map[string]string{"primary": "#818CF8"}
	got := base.Extend(darkBrand{style.Color{Value: "#FF7A00"}})
	if got.DarkColors["brand-glow"] != "#FFB066" {
		t.Fatalf("dark value missing: %v", got.DarkColors)
	}
	if _, leaked := base.DarkColors["brand-glow"]; leaked {
		t.Error("Extend wrote the dark value into its receiver's map")
	}

	lightOnly := style.DefaultTheme().Extend(darkBrand{style.Color{Value: "#FF7A00"}})
	if len(lightOnly.DarkColors) != 0 {
		t.Errorf("a light-only theme gained a dark palette: %v", lightOnly.DarkColors)
	}
}

func TestExtendRefusesDarkValueForUnknownColor(t *testing.T) {
	base := style.DefaultTheme()
	base.DarkColors = map[string]string{"primary": "#818CF8"}
	msg := panicText(t, func() { base.Extend(strayDark{style.Color{Value: "#000"}}) })
	if !strings.Contains(msg, "nope") {
		t.Errorf("panic %q should name the stray dark key", msg)
	}
}

type strayDark struct {
	Ink style.Color
}

func (strayDark) DarkTokens() map[string]string { return map[string]string{"nope": "#fff"} }
