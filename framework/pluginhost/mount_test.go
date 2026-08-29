package pluginhost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Fallback is trusted host render.HTML placed INSIDE the marker, wrapped
// so the broker can find and toggle it — emitted verbatim (not escaped
// like the plugin-controlled string fields), since it is the host's own
// server-rendered node.
func TestMountMarker_FallbackInsideMarker(t *testing.T) {
	html := string(MountMarker(MountConfig{
		Plugin:   "chart",
		Fallback: render.HTML(`<svg class="chart"><rect/></svg>`),
	}))
	if !strings.Contains(html, `<div data-fui-plugin-fallback>`) {
		t.Fatalf("fallback wrapper missing:\n%s", html)
	}
	// The fallback wrapper must sit INSIDE the marker div, before its
	// closing tag, and the SVG must be emitted verbatim (trusted HTML).
	if !strings.Contains(html, `<div data-fui-plugin-fallback><svg class="chart"><rect/></svg></div></div>`) {
		t.Fatalf("fallback not nested verbatim inside the marker:\n%s", html)
	}
	if strings.Contains(html, "&lt;svg") {
		t.Fatalf("fallback HTML was escaped; it is trusted host content:\n%s", html)
	}
}

// No Fallback ⇒ no wrapper, so plugins without one keep the plain marker.
func TestMountMarker_NoFallbackNoWrapper(t *testing.T) {
	html := string(MountMarker(MountConfig{Plugin: "chart"}))
	if strings.Contains(html, "data-fui-plugin-fallback") {
		t.Fatalf("empty Fallback must emit no wrapper:\n%s", html)
	}
}

// Every plugin-controlled VALUE must be HTML-escaped so it can't break out of
// its attribute and inject markup/handlers.
func TestMountMarker_EscapesValues(t *testing.T) {
	payload := `"><script>alert(1)</script>`
	html := string(MountMarker(MountConfig{
		Plugin:       payload,
		DocID:        payload,
		Capabilities: payload,
		Doc:          payload,
		Attributes:   []Attribute{{Name: "data-x", Value: payload}},
		Fields:       []Field{{Name: payload, Value: payload}},
	}))
	if strings.Contains(html, "<script>") {
		t.Fatalf("unescaped payload broke out into markup:\n%s", html)
	}
	// The escaped form should appear instead.
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected escaped payload, got:\n%s", html)
	}
}

// An unsafe attribute NAME (which can't be escaped into a valid name) must be
// DROPPED, not emitted raw, otherwise it breaks out of the tag.
func TestMountMarker_DropsUnsafeAttributeName(t *testing.T) {
	html := string(MountMarker(MountConfig{
		Plugin: "wysiwyg",
		Attributes: []Attribute{
			{Name: `x" onload="alert(1)`, Value: "v"},         // breakout attempt
			{Name: "data-fui-plugin-for", Value: "body_html"}, // legitimate
		},
	}))
	if strings.Contains(html, "onload") {
		t.Fatalf("unsafe attribute name was emitted raw:\n%s", html)
	}
	// The legitimate attribute survives.
	if !strings.Contains(html, `data-fui-plugin-for="body_html"`) {
		t.Fatalf("legitimate attribute was dropped:\n%s", html)
	}
}

func TestValidAttributeName(t *testing.T) {
	ok := []string{"data-fui-plugin-for", "data-x", "aria-label", "id", "data:ns", "role"}
	bad := []string{"", "1abc", "data x", `x"y`, "x=y", "x>y", "x/y", "x<y", "-lead",
		"onclick", "onmouseover", "ONLOAD", "onError"} // event-handler names rejected
	for _, s := range ok {
		if !validAttributeName(s) {
			t.Errorf("validAttributeName(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validAttributeName(s) {
			t.Errorf("validAttributeName(%q) = true, want false", s)
		}
	}
}
