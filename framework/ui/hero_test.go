package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The split hero's phone column must be minmax(0, 1fr): a bare 1fr
// track grows to its content, so an unbreakable line in Media widens
// the page past a 390px viewport.
func TestHeroSplitMobileTrackCanShrink(t *testing.T) {
	css := heroCSS(style.Theme{})
	if !strings.Contains(css, "fui-hero--split { grid-template-columns: minmax(0, 1fr);") {
		t.Errorf("hero split mobile track must be minmax(0, 1fr):\n%s", css)
	}
}

func TestHeroExtraAttrsOnRoot(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"single": Hero(HeroConfig{Title: "T", ExtraAttrs: extra}),
		"split":  Hero(HeroConfig{Title: "T", Media: render.Raw("<img src=x>"), ExtraAttrs: extra}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s hero root missing data-test:\n%s", name, root)
		}
	}
}

// Lede carries inline markup in place of the plain Subtitle, and
// Footer renders under the actions. Both are caller slots, so neither
// sits inside a node marked data-cui-internal.
func TestHeroLedeAndFooterSlots(t *testing.T) {
	h := string(Hero(HeroConfig{
		Title:    "T",
		Subtitle: "plain lede",
		Lede:     render.HTML(`<strong>rich</strong> lede`),
		Actions:  []render.HTML{render.HTML(`<a href="/go">Go</a>`)},
		Footer:   render.HTML(`<p>install line</p>`),
	}))
	if strings.Contains(h, "plain lede") {
		t.Errorf("Lede must replace Subtitle:\n%s", h)
	}
	if !strings.Contains(h, `<p class="fui-hero__lede"><strong>rich</strong> lede</p>`) {
		t.Errorf("rich lede must render unmarked in the lede paragraph:\n%s", h)
	}
	actions := strings.Index(h, "fui-hero__actions")
	footer := strings.Index(h, `<div class="fui-hero__footer"><p>install line</p></div>`)
	if footer < 0 || actions < 0 || footer < actions {
		t.Errorf("footer must render, unmarked, after the actions:\n%s", h)
	}
	// With no Actions, the slots alone must keep the copy column
	// unmarked: it holds caller markup.
	bare := string(Hero(HeroConfig{Title: "T", Footer: render.HTML(`<p>x</p>`)}))
	i := strings.Index(bare, "fui-hero__copy")
	if i < 0 || strings.Contains(bare[i:i+strings.Index(bare[i:], ">")], "data-cui-internal") {
		t.Errorf("a copy column holding a caller slot must not be marked internal:\n%s", bare)
	}
}
