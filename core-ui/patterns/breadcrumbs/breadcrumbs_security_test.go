package breadcrumbs

import (
	"strings"
	"testing"
)

// TestHrefSchemeAllowlist asserts dangerous Crumb.Href schemes never reach an
// <a href>: they degrade to a <span> label, while safe URLs render an anchor.
func TestHrefSchemeAllowlist(t *testing.T) {
	dangerous := []struct {
		name string
		href string
	}{
		{"javascript", "javascript:alert(document.cookie)"},
		{"vbscript", "vbscript:msgbox(1)"},
		{"data html", "data:text/html,<script>alert(1)</script>"},
		{"obfuscated js", "java\tscript:alert(1)"},
		{"protocol relative", "//evil.example.com/x"},
	}
	for _, tc := range dangerous {
		t.Run(tc.name, func(t *testing.T) {
			h := string(New(Config{},
				Crumb{Text: "Home", Href: "/"},
				Crumb{Text: "Bad", Href: tc.href},
			))
			if strings.Contains(h, "<a ") && strings.Contains(h, tc.href) {
				t.Fatalf("dangerous href reached an anchor: %s", h)
			}
			if !strings.Contains(h, "Bad</span>") {
				t.Fatalf("expected span fallback for dangerous href, got: %s", h)
			}
		})
	}

	safe := []string{"/docs/", "https://example.com", "#frag", "mailto:a@b.com"}
	for _, href := range safe {
		t.Run("safe "+href, func(t *testing.T) {
			h := string(New(Config{},
				Crumb{Text: "Home", Href: "/"},
				Crumb{Text: "Ok", Href: href},
			))
			if !strings.Contains(h, `href="`+href+`"`) {
				t.Fatalf("expected anchor for safe href %q, got: %s", href, h)
			}
		})
	}
}
