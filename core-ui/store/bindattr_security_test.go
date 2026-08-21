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
// Surfaces: every URL-bearing HTML attribute BindAttr can target,
// href, src, action, xlink:href, formaction.
func TestBindAttrBlocksDangerousSchemeAtSSR(t *testing.T) {
	urlAttrs := []string{"href", "src", "action", "xlink:href", "formaction"}
	dangerous := []string{
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"  javascript:alert(1)", // leading whitespace
		"java\tscript:alert(1)", // interior control byte
		"JavaScript:alert(1)",   // case-folded scheme
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
// unchanged, the guard must not break legitimate URL-bound slices.
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

// Non-URL attributes (alt, title, aria-*) are not scheme-guarded, a
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

// TestSignalAttrModeDeniesSrcdoc pins the attribute-NAME allow-list.
//
// The URL-scheme guard above only covers href/src/action/xlink:href/
// formaction, so it never sees the other attributes a signal binding can
// write, and several of them execute regardless of their value's
// scheme: `srcdoc` on an iframe is a whole document, `style` reaches CSS,
// `data-behavior` is the runtime's <script src> sink, and any `on*` is
// inline JS. A signal bound to one of those is a live-updating
// script-injection point.
//
// The allow-list lives here rather than in the browser because the
// attribute name is developer-supplied and server-rendered: refusing to
// emit the binding beats warning about it after it shipped, and it costs
// the runtime no bytes.
func TestSignalAttrModeDeniesSrcdoc(t *testing.T) {
	denied := []string{"srcdoc", "style", "data-behavior", "onclick", "OnClick", "data-fui-rpc", "sandbox"}
	for _, attr := range denied {
		resetForTest()
		s := New("t").String("u", "PAYLOAD")
		html := string(s.BindAttr(context.Background(), "iframe", attr, nil))
		if strings.Contains(strings.ToLower(html), strings.ToLower(attr)) {
			t.Errorf("SECURITY: [xss] BindAttr bound a signal to the executing attribute %q: %s", attr, html)
		}
		if strings.Contains(html, "PAYLOAD") {
			t.Errorf("SECURITY: [xss] BindAttr stamped a signal value into %q: %s", attr, html)
		}
	}

	// The attributes real bindings use must still work.
	for _, attr := range []string{"value", "href", "src", "alt", "class", "data-active", "aria-checked"} {
		resetForTest()
		s := New("t").String("u", "ok")
		html := string(s.BindAttr(context.Background(), "a", attr, nil))
		if !strings.Contains(html, `data-fui-signal-attr="`+attr+`"`) {
			t.Errorf("BindAttr refused the legitimate attribute %q: %s", attr, html)
		}
	}
}
