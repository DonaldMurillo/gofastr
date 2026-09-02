package render

import (
	"strings"
	"testing"
)

func TestAttr_RejectsWhitespaceInAttributeName(t *testing.T) {
	got := Attr(`src onerror`, `alert(1)`)

	if strings.Contains(got, ` onerror=`) || strings.HasPrefix(got, `src onerror=`) {
		t.Fatalf("SECURITY: [render-attrs] Attr rendered a whitespace-bearing attribute name verbatim: %q. Attack: attribute-name breakout creates a second executable attribute.", got)
	}
}

func TestAttr_RejectsEventHandlerAttributeName(t *testing.T) {
	got := Attr(`onload`, `alert(1)`)

	if strings.HasPrefix(got, `onload=`) {
		t.Fatalf("SECURITY: [render-attrs] Attr rendered event-handler attribute name verbatim: %q. Attack: HTML builder allows direct script gadget creation via attribute keys.", got)
	}
}

func TestTag_DropsWhitespaceBearingAttributeName(t *testing.T) {
	got := string(Tag("img", map[string]string{
		"src":         "/ok.png",
		"src onerror": "alert(1)",
	}))

	if strings.Contains(got, ` onerror=`) || strings.Contains(got, `src onerror=`) {
		t.Fatalf("SECURITY: [render-attrs] Tag rendered whitespace-bearing attribute key into HTML: %q", got)
	}
}

func TestTag_DropsEventHandlerAttributeName(t *testing.T) {
	got := string(Tag("div", map[string]string{
		"onmouseover": "alert(1)",
	}, Text("safe")))

	if strings.Contains(got, `onmouseover=`) {
		t.Fatalf("SECURITY: [render-attrs] Tag rendered event-handler attribute key into HTML: %q", got)
	}
}

func TestVoidTag_DropsEventHandlerAttributeName(t *testing.T) {
	got := string(VoidTag("img", map[string]string{
		"src":     "/ok.png",
		"onerror": "alert(1)",
	}))

	if strings.Contains(got, `onerror=`) {
		t.Fatalf("SECURITY: [render-attrs] VoidTag rendered event-handler attribute key into HTML: %q", got)
	}
}

// Property: an attribute name that could smuggle a second attribute or
// an event handler is dropped at every builder surface — Attr, Tag, and
// VoidTag share one allow-list, so a shape rejected by one must be
// rejected by all. The shapes below are breakout classes the existing
// tests (whitespace, lowercase on*) do not cover: a case-variant event
// handler, a quote, a slash, a NUL, and an angle-bracket pair.
func TestAttrKeyDropsBreakoutShapes(t *testing.T) {
	shapes := map[string]string{
		"case-variant handler": `ONERROR`,
		"quote in name":        `src"x=1`,
		"slash in name":        `src/onerror`,
		"nul in name":          "src\x00onerror",
		"angle brackets":       `a><script`,
	}
	for name, key := range shapes {
		t.Run(name, func(t *testing.T) {
			if got := Attr(key, "alert(1)"); got != "" {
				t.Errorf("SECURITY: [render-attrs] Attr(%q, …) = %q, want dropped (empty). Attack: attribute-name breakout.", key, got)
			}

			tag := string(Tag("img", map[string]string{
				"src": "/ok.png",
				key:   "alert(1)",
			}))
			if strings.Contains(tag, "alert(1)") || !strings.Contains(tag, `src="/ok.png"`) {
				t.Errorf("SECURITY: [render-attrs] Tag leaked attribute %q or dropped the safe sibling: %q. Attack: attribute-name breakout inside Tag.", key, tag)
			}

			void := string(VoidTag("img", map[string]string{
				"src": "/ok.png",
				key:   "alert(1)",
			}))
			if strings.Contains(void, "alert(1)") || !strings.Contains(void, `src="/ok.png"`) {
				t.Errorf("SECURITY: [render-attrs] VoidTag leaked attribute %q or dropped the safe sibling: %q. Attack: attribute-name breakout inside VoidTag.", key, void)
			}
		})
	}
}

// Guard against overreach: the deny logic must keep admitting the
// legitimate namespaced / data-* / aria-* shapes the framework's UI
// code actually uses, or callers will route around Attr with Raw.
func TestAttrKeyAllowsLegitimateNames(t *testing.T) {
	for _, key := range []string{"data-config", "aria-label", "xml:lang", "data_x", "colspan2", "HX-Push"} {
		if got := Attr(key, "v"); got == "" {
			t.Errorf("Attr(%q, …) dropped a legitimate attribute name", key)
		}
	}
}
