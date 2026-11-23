package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HeadingHref links the heading and marks the card stretched; the sheet
// spreads that link over the card and lifts the card's own controls
// above it, and Article makes the root an <article>.
func TestCardHeadingHrefStretchesTheHeadingLink(t *testing.T) {
	h := string(Card(CardConfig{
		Heading: "Release", HeadingHref: "/blog/r", Article: true,
		Footer: render.HTML(`<button type="button">Share</button>`),
	}, render.Text("body")))
	for _, want := range []string{
		`<article class="fui-card fui-card--interactive fui-card--stretched"`,
		`<a class="fui-card__heading-link" href="/blog/r">Release</a>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("card missing %q:\n%s", want, h)
		}
	}
	css := cardCSS(style.DefaultTheme())
	for _, want := range []string{
		`.fui-card--stretched { position: relative; }`,
		`.fui-card--stretched .fui-card__heading-link::after {
  content: "";
  position: absolute;
  inset: 0;`,
		`.fui-card--stretched :is(.fui-card__action, .fui-card__footer),`,
		`z-index: 1;`,
		`.fui-card--stretched:has(.fui-card__heading-link:focus-visible) {`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("card sheet missing %q", want)
		}
	}
}
