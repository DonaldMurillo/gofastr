package app

import (
	"strings"
	"testing"
)

// The embed layout's CSS rule must key on the SAME name EmbedLayout emits, so
// the shared min-height: 100vh is actually neutralised for the frame's body.
//
// Asserting against a hard-coded ".layout-embed" literal (as the sibling
// TestEmbedLayoutIsNotViewportTall does on lines 18 and 22-24) is a tautology
// against NewLayout(EmbedLayoutName): it reads EmbedLayoutName into both the
// rule lookup and the const, so renaming the constant silently orphans the CSS
// rule, layout.go emits class layout-<newname> while layout_css.go still says
// .layout-embed, and the frame ratchets its own height open under a band of
// empty space, all while the test reports success. This pins the two together.
func TestEmbedLayoutCSSMatchesTheEmittedLayoutName(t *testing.T) {
	want := ".layout-" + EmbedLayout().Name + " .layout-body { min-height: 0; }"
	if !strings.Contains(LayoutBaseCSS(), want) {
		t.Fatalf("LayoutBaseCSS has no %q rule — the embed frame's body is not neutralised against min-height: 100vh, so it ratchets open",
			want)
	}
}
