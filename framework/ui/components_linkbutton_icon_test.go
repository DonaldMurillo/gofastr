package ui

import (
	"strings"
	"testing"
)

// LinkButton with a registered Icon renders the SVG before the label;
// an unregistered icon name falls back to the plain text anchor.
func TestLinkButtonIcon(t *testing.T) {
	RegisterIcon("test-glyph", `<circle cx="12" cy="12" r="10"/>`)
	h := string(LinkButton(LinkButtonConfig{Label: "Repo", Href: "https://example.com", Icon: "test-glyph", External: true}))
	svg := strings.Index(h, "<svg")
	label := strings.Index(h, "Repo")
	if svg == -1 {
		t.Fatalf("Icon should render an inline SVG:\n%s", h)
	}
	if svg > label {
		t.Errorf("icon must precede the label:\n%s", h)
	}
	if !strings.Contains(h, `target="_blank"`) {
		t.Errorf("External must survive the icon path:\n%s", h)
	}

	plain := string(LinkButton(LinkButtonConfig{Label: "Repo", Href: "/x", Icon: "no-such-icon"}))
	if strings.Contains(plain, "<svg") {
		t.Errorf("unknown icon must fall back to label-only:\n%s", plain)
	}
}
