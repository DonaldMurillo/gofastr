package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// With End the root is a row holding the <nav> and the end control, and
// the control sits outside the navigation landmark.
func TestTabNavEndSitsBesideTheNav(t *testing.T) {
	h := string(TabNav(TabNavConfig{
		Label: "Views",
		Items: []TabNavItem{{Text: "All", Href: "/orders", Current: true}},
		End:   render.HTML(`<form id="save"></form>`),
	}))
	if !strings.HasPrefix(h, `<div class="fui-tab-nav-row" data-cui-comp="ui-tab-nav"`) {
		t.Fatalf("the root is not the tab row:\n%s", h)
	}
	navEnd := strings.Index(h, "</nav>")
	form := strings.Index(h, `<form id="save">`)
	if navEnd < 0 || form < navEnd {
		t.Errorf("the End control is inside the <nav>:\n%s", h)
	}
	for _, want := range []string{
		`<nav aria-label="Views" class="fui-tab-nav" role="navigation" data-cui-internal="">`,
		`<div class="fui-tab-nav__end"><form id="save">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("tab row missing %q:\n%s", want, h)
		}
	}
}

// Without End the <nav> stays the root, as before.
func TestTabNavWithoutEndIsTheNav(t *testing.T) {
	h := string(TabNav(TabNavConfig{Label: "Views", Items: []TabNavItem{{Text: "All", Href: "/"}}}))
	if !strings.HasPrefix(h, `<nav`) || strings.Contains(h, "fui-tab-nav-row") {
		t.Errorf("a strip with no End grew a row:\n%s", h)
	}
}
