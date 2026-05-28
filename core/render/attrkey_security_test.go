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
