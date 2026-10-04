package app

import (
	"context"
	"strings"
	"testing"
)

// The embed layout must size to its CONTENT, not to the viewport.
//
// An embedded surface lives in an iframe the host page resizes to the height
// the frame reports. A viewport-tall shell (the old shared .layout-body rule,
// min-height: 100vh) made that reported height partly the frame's own height,
// so each report grew the frame, which grew 100vh, which grew the next report:
// the panel ratcheted open with a band of empty space below the content. The
// layout's body is the primary slot alone — no shell, no body row — so nothing
// in the emitted markup carries (or can carry) a viewport-height min. Every
// element is laid out correctly, so only a screenshot ever showed the ratchet;
// this pins the structural fact the screenshot depended on.
func TestEmbedLayoutIsNotViewportTall(t *testing.T) {
	out := string(EmbedLayout().WrapCtx(context.Background(), "<p>body</p>"))
	for _, cls := range []string{`class="layout-body"`, `data-cui-comp="ui-shell"`} {
		if strings.Contains(out, cls) {
			t.Fatalf("EmbedLayout emitted a structural shell (%s) a stylesheet could size to the viewport — the frame would ratchet its own height open:\n%s", cls, out)
		}
	}
	if !strings.Contains(out, "<p>body</p>") {
		t.Fatalf("EmbedLayout lost the body:\n%s", out)
	}
}

// The embed layout's CSS contract must key on the SAME name EmbedLayout
// emits. Asserting against a hard-coded "embed" literal is a tautology
// against NewLayout(EmbedLayoutName): it reads EmbedLayoutName into both
// the contract lookup and the constant, so renaming the constant silently
// orphans every rule a host app wrote against .layout-embed, all while
// the test reports success. This pins the emitted name, class, and the
// data-cui-layout marker to the constant.
func TestEmbedLayoutCSSMatchesTheEmittedLayoutName(t *testing.T) {
	l := EmbedLayout()
	if got := l.Name; got != EmbedLayoutName {
		t.Fatalf("EmbedLayout().Name = %q, want %q", got, EmbedLayoutName)
	}
	out := string(l.WrapCtx(context.Background(), "<p>body</p>"))
	wantClass := `class="layout-` + EmbedLayoutName + `"`
	wantAttr := `data-cui-layout="` + EmbedLayoutName + `"`
	if !strings.Contains(out, wantClass) || !strings.Contains(out, wantAttr) {
		t.Fatalf("EmbedLayout's wrapper must carry the CSS contract %q and %q:\n%s", wantClass, wantAttr, out)
	}
}

func TestEmbedLayoutHasNoChrome(t *testing.T) {
	out := string(EmbedLayout().WrapCtx(context.Background(), "<p>body</p>"))
	if strings.Count(out, "<main") != 1 {
		t.Fatalf("EmbedLayout must emit exactly one <main> landmark:\n%s", out)
	}
	for _, tag := range []string{"<header", "<footer", "<nav"} {
		if strings.Contains(out, tag) {
			t.Errorf("EmbedLayout emitted %s:\n%s", tag, out)
		}
	}
}
