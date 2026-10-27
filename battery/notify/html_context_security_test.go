package notify

import (
	"context"
	"strings"
	"testing"
)

func templated(t *testing.T, body string, data map[string]any) string {
	t.Helper()
	tm := NewMapTemplater()
	tm.Set("t", "email", Template{HTMLBody: body})
	r, err := tm.Render(context.Background(), "t", "email", data)
	if err != nil {
		t.Fatalf("Render(%q): %v", body, err)
	}
	return r.HTMLBody
}

// Every placeholder was escaped for element text, so in an unquoted
// attribute a value with a space added an attribute of its own.
func TestUnquotedAttrValueStaysOneAttr(t *testing.T) {
	got := templated(t, `<img src={{avatar}} alt="a">`, map[string]any{"avatar": "x onerror=alert(1)"})
	if strings.Contains(got, " onerror=") {
		t.Errorf("value injected an attribute: %s", got)
	}
}

// A quoted href got no scheme filtering, so javascript: arrived live.
func TestHrefValueRefusesJavascriptURL(t *testing.T) {
	got := templated(t, `<a href="{{url}}">open</a>`, map[string]any{"url": "javascript:alert(1)"})
	if strings.Contains(got, "javascript:") {
		t.Errorf("javascript: URL arrived in the href: %s", got)
	}
	ok := templated(t, `<a href="{{url}}">open</a>`, map[string]any{"url": "https://shop.example/o/1?a=1&b=2"})
	if !strings.Contains(ok, `href="https://shop.example/o/1?a=1&amp;b=2"`) {
		t.Errorf("an https URL did not survive: %s", ok)
	}
}

// A huge value is cut off at the output cap, as in TextBody.
func TestHTMLBodyOutputIsCapped(t *testing.T) {
	big := strings.Repeat("a", 2*MaxInterpolatedOutputBytes)
	if got := templated(t, `<p>{{v}}</p>`, map[string]any{"v": big}); len(got) != MaxInterpolatedOutputBytes {
		t.Errorf("HTMLBody = %d bytes, want the %d cap", len(got), MaxInterpolatedOutputBytes)
	}
}

// Literal braces and a placeholder the data does not fill stay as the
// host wrote them, as in Subject and TextBody.
func TestHTMLBodyKeepsUnfilledPlaceholder(t *testing.T) {
	got := templated(t, `<p>{{name}} has {{missing}} {{ </p>`, map[string]any{"name": "Al & Bo"})
	if got != `<p>Al &amp; Bo has {{missing}} {{ </p>` {
		t.Errorf("rendered %q", got)
	}
}
