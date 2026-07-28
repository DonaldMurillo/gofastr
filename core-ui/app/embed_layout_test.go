package app

import (
	"strings"
	"testing"
)

// The embed layout must size to its CONTENT, not to the viewport.
//
// An embedded surface lives in an iframe the host page resizes to the height
// the frame reports. Under the shared .layout-body rule (min-height: 100vh)
// that reported height is partly the frame's own height, so each report grows
// the frame, which grows 100vh, which grows the next report — the panel
// ratchets open with a band of empty space below the content. Every element is
// laid out correctly, so only a screenshot shows it.
func TestEmbedLayoutIsNotViewportTall(t *testing.T) {
	css := LayoutBaseCSS()
	if !strings.Contains(css, ".layout-embed .layout-body { min-height: 0; }") {
		t.Fatal("LayoutBaseCSS does not neutralise min-height: 100vh for the embed layout — a frame using it would ratchet its own height")
	}
	// And the layout has to actually carry that class.
	if got := EmbedLayout().Name; got != EmbedLayoutName {
		t.Fatalf("EmbedLayout().Name = %q, want %q — the CSS rule keys on layout-%s", got, EmbedLayoutName, EmbedLayoutName)
	}
}

func TestEmbedLayoutHasNoChrome(t *testing.T) {
	l := EmbedLayout()
	if l.Header != nil || l.Sidebar != nil || l.Footer != nil || l.Container {
		t.Fatalf("EmbedLayout carries chrome: %+v — a customer's page already has a header", l)
	}
	out := string(l.Wrap("<p>body</p>"))
	if strings.Count(out, "<main") != 1 {
		t.Fatalf("EmbedLayout must emit exactly one <main> landmark:\n%s", out)
	}
	for _, tag := range []string{"<header", "<footer", "<nav"} {
		if strings.Contains(out, tag) {
			t.Errorf("EmbedLayout emitted %s:\n%s", tag, out)
		}
	}
}
