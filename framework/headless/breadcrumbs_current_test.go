package headless

import (
	"strings"
	"testing"
)

// Any step with no Href was taken for the current page, so a middle
// crumb with no page of its own (a grouping level) was a second
// aria-current="page".
func TestBreadcrumbsOneCurrentStep(t *testing.T) {
	h := string(Breadcrumbs(BreadcrumbsProps{Items: []Breadcrumb{
		{Text: "Home", Href: "/"},
		{Text: "Settings"},
		{Text: "Profile"},
	}}, nil))
	if n := strings.Count(h, `aria-current="page"`); n != 1 {
		t.Fatalf("%d steps say aria-current=page:\n%s", n, h)
	}
	if !strings.Contains(h, `<span aria-current="page">Profile</span>`) {
		t.Fatalf("the last step is not the current page:\n%s", h)
	}
}
