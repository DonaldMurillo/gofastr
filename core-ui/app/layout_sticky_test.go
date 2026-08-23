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

// A sticky header makes the shell's dead scroll visible: .layout-body is
// min-height: 100vh, so a header above it makes a short page scroll by
// exactly the header's height with nothing in the overflow. Under a
// pinned header that reads as broken. The sticky shell is a flex column
// so it fills the viewport instead of exceeding it.
//
// Measured in a browser at 900x800 with a 64px header and one paragraph:
// scrollHeight 864 before this rule, 800 after, while a long page still
// scrolls (3440) with the header at rect.top 0.
func TestStickyShellDoesNotOverflow(t *testing.T) {
	css := LayoutBaseCSS()
	_, after, found := strings.Cut(css, ".layout--sticky-header {")
	if !found {
		t.Fatal("LayoutBaseCSS has no .layout--sticky-header shell rule: a short page scrolls by the header's height")
	}
	shell, _, _ := strings.Cut(after, "}")
	for _, decl := range []string{"display: flex", "flex-direction: column", "min-height: 100vh"} {
		if !strings.Contains(shell, decl) {
			t.Errorf("sticky shell must set %s, got: %s", decl, shell)
		}
	}
	_, after, found = strings.Cut(css, ".layout--sticky-header > .layout-body {")
	if !found {
		t.Fatal("the sticky shell's body cell has no rule, so it keeps min-height: 100vh and the overflow stays")
	}
	body, _, _ := strings.Cut(after, "}")
	// flex-grow keeps a short page full-height; min-height: 0 drops the
	// 100vh floor that caused the overflow. Both are load-bearing.
	for _, decl := range []string{"flex: 1 0 auto", "min-height: 0"} {
		if !strings.Contains(body, decl) {
			t.Errorf("sticky shell body must set %s, got: %s", decl, body)
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
