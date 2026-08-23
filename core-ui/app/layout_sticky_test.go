package app

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A header component given `position: sticky; top: 0` only travels inside
// its parent's box, and the layout renders that component inside a <header>
// wrapper of its own whose box is exactly the header's height: it sticks for
// 65px and scrolls away. WithStickyHeader makes the LAYOUT'S wrapper the
// sticky element instead, so it travels down the whole page. (#216)
func TestStickyHeaderClassOnWrapper(t *testing.T) {
	l := NewLayout("site").
		WithHeader(&stubComponent{html: render.Raw("<h1>Header</h1>")}).
		WithStickyHeader()
	html := string(l.Wrap(render.Raw("<p>Content</p>")))

	if !strings.Contains(html, "layout--sticky-header") {
		t.Errorf("expected layout--sticky-header modifier on the wrapper, got: %s", html)
	}
}

// The modifier is opt-in: a plain layout must not carry it, or every header
// would stick without the app asking for it.
func TestNoStickyClassByDefault(t *testing.T) {
	l := NewLayout("site").
		WithHeader(&stubComponent{html: render.Raw("<h1>Header</h1>")})
	html := string(l.Wrap(render.Raw("<p>Content</p>")))

	if strings.Contains(html, "layout--sticky-header") {
		t.Errorf("layout without WithStickyHeader must not carry layout--sticky-header, got: %s", html)
	}
}

// The class alone does nothing; LayoutBaseCSS must style the wrapper's
// direct <header> child sticky with an offset from the top edge.
func TestStickyHeaderCSSRule(t *testing.T) {
	css := LayoutBaseCSS()
	_, after, found := strings.Cut(css, ".layout--sticky-header > header {")
	if !found {
		t.Fatal("LayoutBaseCSS has no .layout--sticky-header > header rule: the sticky modifier styles nothing")
	}
	// Assert inside the rule's own braces. A repo-wide Contains would pass
	// on any other rule that happened to set the same property.
	block, _, _ := strings.Cut(after, "}")
	for _, decl := range []string{"position: sticky", "top: 0", "z-index:", "background-color:"} {
		if !strings.Contains(block, decl) {
			t.Errorf("sticky header rule must set %s, got: %s", decl, block)
		}
	}
}

// Modifiers compose: a contained layout with a sticky header carries both
// classes on the same wrapper, the way layout--has-sidebar does today.
func TestStickyComposesWithContainer(t *testing.T) {
	l := NewLayout("site").
		WithHeader(&stubComponent{html: render.Raw("<h1>Header</h1>")}).
		WithContainer().
		WithStickyHeader()
	html := string(l.Wrap(render.Raw("<p>Content</p>")))

	if !strings.Contains(html, "layout--contained") {
		t.Errorf("expected layout--contained modifier, got: %s", html)
	}
	if !strings.Contains(html, "layout--sticky-header") {
		t.Errorf("expected layout--sticky-header alongside layout--contained, got: %s", html)
	}
}

// The sticky wrapper IS the banner landmark. Making the wrapper sticky must
// not drop the <header role="banner"> the layout emits around the header
// component; removing it would break the page's landmark structure.
func TestStickyKeepsBannerLandmark(t *testing.T) {
	l := NewLayout("site").
		WithHeader(&stubComponent{html: render.Raw("<h1>Header</h1>")}).
		WithStickyHeader()
	html := string(l.Wrap(render.Raw("<p>Content</p>")))

	if !strings.Contains(html, "<header") {
		t.Errorf("sticky layout must still wrap the header component in <header>, got: %s", html)
	}
	if !strings.Contains(html, `role="banner"`) {
		t.Errorf("sticky layout must keep the banner role on its header wrapper, got: %s", html)
	}
}
