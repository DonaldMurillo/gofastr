package packcmd

import (
	"testing"
)

// TestToKebabCase pins the consolidated kebab-case converter (the pack
// side of the theme-token round trip: "PrimaryFg" ↔ "primary-fg").
func TestToKebabCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Primary", "primary"},
		{"PrimaryFg", "primary-fg"},
		{"SurfaceSoft", "surface-soft"},
		{"BorderStrong", "border-strong"},
		{"HTTPServer", "http-server"},
		{"APIKey", "api-key"},
		{"TextMuted", "text-muted"},
		{"primary", "primary"},
	}
	for _, c := range cases {
		if got := toKebabCase(c.in); got != c.want {
			t.Errorf("toKebabCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
