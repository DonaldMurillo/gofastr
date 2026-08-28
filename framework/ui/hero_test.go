package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

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
