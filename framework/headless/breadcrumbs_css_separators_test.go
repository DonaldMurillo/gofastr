package headless

import (
	"strings"
	"testing"
)

// With SeparatorsInCSS the trail renders the steps and nothing between
// them, so the styling layer's generated separators are the only ones.
func TestBreadcrumbsSeparatorsInCSSRenderNone(t *testing.T) {
	items := []Breadcrumb{{Text: "Home", Href: "/"}, {Text: "Docs", Href: "/docs/"}, {Text: "Entities"}}
	h := renderCrumbs(BreadcrumbsProps{Items: items, SeparatorsInCSS: true})
	if strings.Contains(h, `aria-hidden="true"`) || strings.Contains(h, ">/<") {
		t.Errorf("a separator element rendered:\n%s", h)
	}
	if n := strings.Count(h, "<li"); n != 3 {
		t.Errorf("trail rendered %d steps, want 3:\n%s", n, h)
	}
	if def := renderCrumbs(BreadcrumbsProps{Items: items}); strings.Count(def, `aria-hidden="true"`) != 2 {
		t.Errorf("the default trail lost its separators:\n%s", def)
	}
}
