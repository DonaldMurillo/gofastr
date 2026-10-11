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
