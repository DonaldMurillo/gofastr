package nestedlist

import (
	"strings"
	"testing"
)

// TestHrefSchemeAllowlist asserts that script-executing URL schemes on
// a leaf Item.Href are neutralised: the dangerous value never reaches
// an anchor href, and the node falls back to a non-link span instead.
func TestHrefSchemeAllowlist(t *testing.T) {
	cases := []struct {
		name string
		href string
		safe bool // true => should render as an anchor with that href
	}{
		{"relative path", "/docs", true},
		{"javascript scheme", "javascript:alert(document.cookie)", false},
		{"vbscript scheme", "vbscript:msgbox(1)", false},
		{"data scheme", "data:text/html,<script>alert(1)</script>", false},
		{"protocol relative", "//evil.example/x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := string(Render(Config{Items: []Item{{Label: "X", Href: tc.href}}}))
			if tc.safe {
				if !strings.Contains(out, `href="`+tc.href+`"`) {
					t.Fatalf("safe href %q should be preserved, got: %s", tc.href, out)
				}
				return
			}
			// Dangerous: the raw scheme must not appear inside an href.
			if strings.Contains(out, `href="`+tc.href+`"`) {
				t.Fatalf("dangerous href %q leaked into anchor: %s", tc.href, out)
			}
			// Defense in depth: the scheme token itself must not survive
			// as a clickable href anywhere.
			lower := strings.ToLower(out)
			for _, scheme := range []string{"javascript:", "vbscript:", "data:"} {
				if strings.Contains(tc.href, scheme) && strings.Contains(lower, `href="`+scheme) {
					t.Fatalf("scheme %q still present in an href: %s", scheme, out)
				}
			}
			// Must fall back to a non-link: no empty-href anchor either.
			if strings.Contains(out, `href=""`) {
				t.Fatalf("dropped href should yield a span, not an empty anchor: %s", out)
			}
		})
	}
}
