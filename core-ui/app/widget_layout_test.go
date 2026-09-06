package app

import (
	"strings"
	"testing"
)

// A floating widget window is transparent and small: the page behind
// the screen's surface must paint nothing, and the shell must not
// force a viewport-tall body or a padded column. The first widget
// rendered a white slab with its button cut off; a screenshot caught
// it, no DOM assertion could.
func TestWidgetLayoutIsTransparentAndCompact(t *testing.T) {
	css := LayoutBaseCSS()
	for _, want := range []string{
		"html:has(.layout-widget), body:has(.layout-widget) { background-color: transparent; }",
		".layout-widget .layout-body { min-height: 0; }",
		"--ui-layout-widget-padding",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("LayoutBaseCSS lacks %q", want)
		}
	}
	if got := WidgetLayout().Name; got != WidgetLayoutName {
		t.Fatalf("WidgetLayout().Name = %q, want %q", got, WidgetLayoutName)
	}
}

func TestWidgetLayoutHasNoChrome(t *testing.T) {
	l := WidgetLayout()
	if l.Header != nil || l.Sidebar != nil || l.Footer != nil || l.Container {
		t.Fatalf("WidgetLayout carries chrome: %+v", l)
	}
	out := string(l.Wrap("<p>body</p>"))
	if strings.Count(out, "<main") != 1 {
		t.Fatalf("WidgetLayout must emit exactly one <main> landmark:\n%s", out)
	}
	for _, tag := range []string{"<header", "<footer", "<nav"} {
		if strings.Contains(out, tag) {
			t.Errorf("WidgetLayout emitted %s:\n%s", tag, out)
		}
	}
	if !strings.Contains(out, `data-fui-layout="widget"`) {
		t.Fatalf("wrapper is not marked as the widget layout:\n%s", out)
	}
}
