package store

import (
	"context"
	"os"
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

// TestSignalURLGuardMirrorsRuntimeAttrs pins the parity the slice.go
// comment promises: urlBearingAttrs "mirror[s] the runtime's
// _isUnsafeSignalUrl allow-list exactly". Both layers must name the
// same five attributes AND both must fold case before matching, because
// the runtime reads the attribute name back from the SSR-emitted
// data-fui-signal-attr value and HTML parsers treat attribute names
// case-insensitively: a case-sensitive list on either side turns
// `HREF="javascript:…"` into a one-sided gap (guarded at SSR, live on
// client updates, or the reverse).
//
// Surfaces: the SSR guard (BindAttr, behavioural) and the runtime guard
// (_isUnsafeSignalUrl in both shipped compositions, runtime.js and
// frag/kernel.js, source-asserted since no JS engine runs in-process).
func TestSignalURLGuardMirrorsRuntimeAttrs(t *testing.T) {
	mixed := []string{"href", "src", "action", "xlink:href", "formaction",
		"HREF", "Src", "ACTION", "XLink:Href", "FORMACTION"}

	// SSR side: every case variant of a URL-bearing attr must blank a
	// dangerous scheme.
	for _, attr := range mixed {
		resetForTest()
		s := New("t").String("u", "javascript:alert(1)")
		tag := "a"
		if strings.EqualFold(attr, "src") {
			tag = "img"
		}
		html := string(s.BindAttr(context.Background(), tag, attr, nil))
		if strings.Contains(strings.ReplaceAll(strings.ToLower(html), " ", ""), "javascript:") {
			t.Errorf("SECURITY: [signal-url-parity] BindAttr stamped a dangerous scheme through the case variant %q: %s", attr, html)
		}
		// The binding itself must survive: refusing the variant entirely
		// would strand every developer who spells an attr in caps.
		if !strings.Contains(html, `data-fui-signal-attr="`+attr+`"`) {
			t.Errorf("BindAttr refused the legitimate case variant %q: %s", attr, html)
		}
	}

	// Runtime side: the guard names exactly the same five attrs, and
	// lowercases the attribute name before comparing.
	for _, rel := range []string{"../runtime/runtime.js", "../runtime/frag/kernel.js"} {
		b, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		src := string(b)
		start := strings.Index(src, "_isUnsafeSignalUrl(attr, value)")
		if start < 0 {
			t.Fatalf("could not locate _isUnsafeSignalUrl in %s", rel)
		}
		body := src[start:]
		if end := strings.Index(body, "register(id"); end > 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "toLowerCase()") {
			t.Errorf("SECURITY: [signal-url-parity] %s: _isUnsafeSignalUrl no longer lowercases the attribute name — HREF/SRC escape the guard on client-side updates", rel)
		}
		for _, attr := range []string{"href", "src", "action", "xlink:href", "formaction"} {
			if !strings.Contains(body, "'"+attr+"'") {
				t.Errorf("SECURITY: [signal-url-parity] %s: _isUnsafeSignalUrl no longer guards %q — the SSR guard still does, the two lists have drifted", rel, attr)
			}
		}
	}
}

// TestBindAttrAttrNameStaysInert pins the attribute-NAME allow-list's
// other edge: SignalAttrAllowed accepts ANY `aria-*` name by prefix,
// and aria- names reach render.Tag as map keys. A name carrying a
// quote (`aria-label" onclick="alert(1)`) must not smuggle a second,
// live attribute into the tag: render.Attr drops keys outside the
// attribute-name grammar, and the data-fui-signal-attr value is
// entity-escaped. The runtime side is inert by construction,
// setAttribute with a quote-bearing name creates no handler.
func TestBindAttrAttrNameStaysInert(t *testing.T) {
	hostile := []string{
		`aria-label" onclick="alert(1)`,
		`aria-"><img src=x onerror=alert(1)>`,
		`aria-x onmouseover=alert(1)`,
	}
	for _, attr := range hostile {
		resetForTest()
		s := New("t").String("u", "PAYLOAD")
		html := string(s.BindAttr(context.Background(), "span", attr, nil))
		// A breakout manifests as a NEW attribute (handler name followed
		// by a RAW quote) or raw markup. The same names entity-escaped
		// inside the data-fui-signal-attr value are inert text.
		for _, live := range []string{`onclick="`, `onmouseover="`, `onerror="`, `<img`, `<svg`} {
			if strings.Contains(strings.ToLower(html), live) {
				t.Errorf("SECURITY: [signal-attr-name] BindAttr attr name %q smuggled live markup %q into the tag: %s", attr, live, html)
			}
		}
		// The binding marker must carry the name escaped, not dropped
		// silently (the developer still needs the failure to be visible
		// in the emitted markup).
		if !strings.Contains(html, `data-fui-signal-attr="`) {
			t.Errorf("BindAttr dropped the binding marker entirely for %q: %s", attr, html)
		}
	}
}

// TestValidateNameRejectsReservedKeys pins the declaration-side half of
// the runtime's prototype-pollution guard. The runtime kernel
// (frag/signals.js isReservedSignalKey) refuses __proto__ / constructor /
// prototype as signal names precisely because those keys re-parent the
// client signal store, and validateName's own doc says it rejects names
// that could break "the signal key". But its character class
// (letters/digits/./_/-) accepts all three, so a legal-looking declaration
// ships: SSR keeps stamping the slice's value while EVERY client-side
// write to it is silently refused — a permanently dead binding with no
// error anywhere. Property: the declaration validator rejects the same
// reserved keys the runtime has to defend against, at declaration time
// like its other rejections. Surfaces: the three reserved keys as bare
// slice names (a namespace prefix produces a non-reserved full key, so
// only bare names are in scope).
func TestValidateNameRejectsReservedKeys(t *testing.T) {
	for _, name := range []string{"__proto__", "constructor", "prototype"} {
		t.Run(name, func(t *testing.T) {
			resetForTest()
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("SECURITY: [store-reserved-name] declaring slice %q was accepted — the runtime's isReservedSignalKey guard silently refuses every client write to it while SSR keeps stamping its value (dead binding, no error anywhere)", name)
				}
			}()
			_ = New("").String(name, "v")
		})
	}
}
