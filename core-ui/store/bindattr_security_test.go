package store

import (
	"context"
	"strings"
	"testing"
)

// Property: a slice value stamped into a URL-bearing HTML attribute at
// SSR must never carry a dangerous scheme (javascript:/vbscript:/non-image
// data:). The runtime guards signal-driven *updates* of these attrs via
// _isUnsafeSignalUrl; the SSR initial paint (BindAttr) must apply the same
// guard for defense-in-depth parity, because a producer can Seed a
// request-influenced URL into a URL-bound slice.
//
// Surfaces: every URL-bearing HTML attribute BindAttr can target —
// href, src, action, xlink:href, formaction.
func TestBindAttrBlocksDangerousSchemeAtSSR(t *testing.T) {
	urlAttrs := []string{"href", "src", "action", "xlink:href", "formaction"}
	dangerous := []string{
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"  javascript:alert(1)",          // leading whitespace
		"java\tscript:alert(1)",          // interior control byte
		"JavaScript:alert(1)",            // case-folded scheme
	}
	for _, attr := range urlAttrs {
		for _, val := range dangerous {
			resetForTest()
			s := New("t").String("u", val)
			tag := "a"
			if attr == "src" {
				tag = "img"
			}
			html := string(s.BindAttr(context.Background(), tag, attr, nil))
			low := strings.ToLower(html)
			if strings.Contains(strings.ReplaceAll(low, " ", ""), "javascript:") ||
				strings.Contains(strings.ReplaceAll(low, " ", ""), "vbscript:") ||
				strings.Contains(strings.ReplaceAll(low, " ", ""), "data:text/html") {
				t.Errorf("SECURITY: [ssr-scheme] BindAttr stamped dangerous scheme into %s at SSR: value=%q html=%s", attr, val, html)
			}
		}
	}
}

// A safe scheme / relative URL / data:image must still pass through
// unchanged — the guard must not break legitimate URL-bound slices.
func TestBindAttrAllowsSafeURLAtSSR(t *testing.T) {
	cases := []string{"/logo.png", "https://example.com/x", "#frag", "data:image/png;base64,AAAA"}
	for _, val := range cases {
		resetForTest()
		s := New("t").String("u", val)
		html := string(s.BindAttr(context.Background(), "a", "href", nil))
		if !strings.Contains(html, "href=") {
			t.Fatalf("expected href attr in output: %s", html)
		}
		// The safe value must survive (attribute-escaped is fine).
		if !strings.Contains(html, val) && !strings.Contains(html, strings.ReplaceAll(val, "&", "&amp;")) {
			t.Errorf("SECURITY: [ssr-scheme] BindAttr dropped a safe URL %q: %s", val, html)
		}
	}
}

// Non-URL attributes (alt, title, aria-*) are not scheme-guarded — a
// value that merely looks like a scheme must pass through unchanged so
// the guard stays scoped to URL sinks only.
func TestBindAttrLeavesNonURLAttrsAlone(t *testing.T) {
	resetForTest()
	s := New("t").String("u", "javascript:not-a-url-here")
	html := string(s.BindAttr(context.Background(), "img", "alt", map[string]string{"src": "/x.png"}))
	if !strings.Contains(html, "javascript:not-a-url-here") {
		t.Errorf("non-URL attr alt should keep its literal value: %s", html)
	}
}
