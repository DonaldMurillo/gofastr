package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The kit's trail draws its separators as generated content with empty
// alternative text, so the markup holds only the steps.
func TestBreadcrumbsSeparatorsAreGeneratedContent(t *testing.T) {
	h := string(Breadcrumbs(BreadcrumbsConfig{}, Crumb{Text: "Home", Href: "/"}, Crumb{Text: "Docs", Href: "/docs/"}, Crumb{Text: "Here"}))
	if strings.Contains(h, ">/<") || strings.Contains(h, "fui-breadcrumbs__sep") {
		t.Errorf("a separator element rendered:\n%s", h)
	}
	css := breadcrumbsCSS(style.Theme{})
	for _, want := range []string{
		`.fui-breadcrumbs__item + .fui-breadcrumbs__item::before {`,
		`content: "/" / "";`,
		`.fui-breadcrumbs__item:nth-last-child(2)::before { content: none; }`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("breadcrumbs sheet missing %q", want)
		}
	}
}
