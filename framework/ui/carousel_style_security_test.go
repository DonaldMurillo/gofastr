package ui

// Pins: VirtualPlaceholderHeight renders as one plain CSS length — a
// number + unit or a var() token — inside the placeholder slide's
// inline style attribute, and the style attribute is dropped when the
// value fails the grammar.
// Property: VirtualPlaceholderHeight is concatenated into a placeholder
// slide's inline style attribute, so it must be a plain CSS length — the
// field's own doc comment promises "an optional CSS length", and the UI
// package otherwise renders no inline styles (see image_placeholder.go's
// rationale; the default CSP blocks them), so an arbitrary
// declaration list is a new escape hatch into page CSS.
// Surfaces: framework/ui/carousel.go::Carousel —
// slideAttrs["style"] = "min-block-size:" + cfg.VirtualPlaceholderHeight + ";"
// unvalidated for placeholder slides under VirtualScroll.
// Finding: any `;`-separated CSS declaration list in
// VirtualPlaceholderHeight ships verbatim inside style="…" (render.Attr
// HTML-escapes the value, but CSS metacharacters need no HTML escape),
// so config derived from request input injects live declarations.
// Fix direction: validate the value against a CSS-length grammar
// (number + unit) and drop the style attribute when it fails.

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestCarouselStyleInjectionDropped(t *testing.T) {
	malicious := "0;background:url(//evil.example/x);position:fixed;inset:0"
	h := string(Carousel(CarouselConfig{
		Label:                    "L",
		VirtualScroll:            true,
		VirtualWindow:            1,
		VirtualPlaceholderHeight: malicious,
		Slides: []CarouselSlide{
			{Content: render.Text("one")},
			{Content: render.Text("two")},
		},
	}))
	if strings.Contains(h, "background:url(") || strings.Contains(h, "position:fixed") {
		t.Errorf("SECURITY: [carousel-style-injection] VirtualPlaceholderHeight %q reached the page as live CSS: the placeholder's style attribute carries the injected declarations (background:url/position:fixed present). Attack: any host that feeds request-derived config into CarouselConfig — e.g. persisting a per-viewer placeholder height from a query param — lets the sender pin a full-viewport overlay or load attacker CSS on every visitor. Output:\n%s", malicious, h)
	}
	// Degrade, not breakage: the placeholder must still defer without
	// its style once the value is dropped.
	if !strings.Contains(h, `data-fui-carousel-defer="1"`) {
		t.Errorf("placeholder slide 1 must still carry data-fui-carousel-defer when its style is dropped:\n%s", h)
	}
	// Happy path: a plain length must keep rendering.
	ok := string(Carousel(CarouselConfig{
		Label:                    "L",
		VirtualScroll:            true,
		VirtualWindow:            1,
		VirtualPlaceholderHeight: "240px",
		Slides: []CarouselSlide{
			{Content: render.Text("one")},
			{Content: render.Text("two")},
		},
	}))
	if !strings.Contains(ok, "min-block-size:240px;") {
		t.Errorf("plain CSS length 240px must still render min-block-size:240px; on placeholders:\n%s", ok)
	}
}
