package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// <html lang> and the skip link live OUTSIDE the shell the SPA runtime
// swaps, so after a client-side navigation they still described the page
// that was loaded first (#408). The render context now carries both
// values to the outermost layer of every render (full page: layer 0;
// subtree partial: the first re-rendered layer; layout-less page: the
// <main>) as data-fui-doc-lang / data-fui-skip-label, and the runtime
// copies them onto the document after each swap.

// openingTag returns the full opening tag containing s at s[idx].
func openingTag(s string, idx int) string {
	if idx < 0 || idx >= len(s) {
		return ""
	}
	start := strings.LastIndex(s[:idx], "<")
	end := strings.Index(s[idx:], ">")
	if start < 0 || end < 0 {
		return ""
	}
	return s[start : idx+end]
}

func TestDocShellMarksOutermostLayerOnly(t *testing.T) {
	a := NewApp("t").
		WithLang("es").
		WithSkipLabel("Saltar al contenido principal")
	a.SetDefaultLayout(NewLayout("site").WithHeader(NewStaticComponent("SITE")))
	g := NewScreenGroup("/docs", NewLayout("docs").WithSidebar(NewStaticComponent("NAV")))
	g.Screen(NewScreen("a", &stubComponent{html: render.Raw("A")}), nil)
	a.Router.ScreenGroup(g)

	res, err := a.RenderPageResult(context.Background(), "/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	// Exactly one carrier per document: the outermost layer.
	if got := strings.Count(s, "data-fui-doc-lang"); got != 1 {
		t.Errorf("data-fui-doc-lang appears %d times, want 1 (outermost layer only):\n%s", got, s)
	}
	if got := strings.Count(s, "data-fui-skip-label"); got != 1 {
		t.Errorf("data-fui-skip-label appears %d times, want 1:\n%s", got, s)
	}
	root := openingTag(s, strings.Index(s, `data-fui-layout-key="l:site"`))
	if !strings.Contains(root, `data-fui-doc-lang="es"`) || !strings.Contains(root, "data-fui-skip-label") {
		t.Errorf("layer 0 must carry the doc markers, got tag %q", root)
	}
	inner := openingTag(s, strings.Index(s, `data-fui-layout-key="g:/docs/:docs"`))
	if strings.Contains(inner, "data-fui-doc-lang") {
		t.Errorf("nested layer must not carry the doc markers, got tag %q", inner)
	}
}

func TestPartialDocShellOnFirstRenderedLayer(t *testing.T) {
	a := NewApp("t").
		WithLang("es").
		WithSkipLabel("Saltar al contenido principal")
	a.SetDefaultLayout(NewLayout("site").WithHeader(NewStaticComponent("SITE")))
	g := NewScreenGroup("/docs", NewLayout("docs").WithSidebar(NewStaticComponent("NAV")))
	g.Screen(NewScreen("intro", &stubComponent{html: render.Raw("INTRO")}), nil)
	a.Router.ScreenGroup(g)
	a.Register("/about", &stubComponent{html: render.Raw("ABOUT")}, nil)

	// /about → /docs/intro shares the site root; the partial re-renders
	// the docs layer, so the markers must ride that layer: it is the
	// payload the client swaps in, the only place fresh values can arrive.
	res, err := a.RenderPartialFromResult(context.Background(), "/docs/intro", "/about")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if strings.Contains(s, `data-fui-layout-key="l:site"`) {
		t.Fatalf("shared root must not re-render:\n%s", s)
	}
	if got := strings.Count(s, "data-fui-doc-lang"); got != 1 {
		t.Errorf("data-fui-doc-lang appears %d times in partial, want 1:\n%s", got, s)
	}
	frag := openingTag(s, strings.Index(s, `data-fui-layout-key="g:/docs/:docs"`))
	if !strings.Contains(frag, `data-fui-doc-lang="es"`) || !strings.Contains(frag, "data-fui-skip-label") {
		t.Errorf("the partial's outermost layer must carry the doc markers, got tag %q", frag)
	}
}

func TestMarkerOnlyPartialCarriesDocShell(t *testing.T) {
	parent := NewScreenGroup("/settings", NewLayout("settings").WithSidebar(NewStaticComponent("NAV")))
	child := parent.SubGroup("advanced", nil) // inherits parent's *Layout
	child.Screen(NewScreen("security", &stubComponent{html: render.Raw("SEC")}), nil)
	parent.Screen(NewScreen("base", &stubComponent{html: render.Raw("BASE")}), nil)
	a := NewApp("t").WithLang("fr")
	a.Router.ScreenGroup(parent)

	// /settings/base → /settings/advanced/security shares the settings
	// layer; the advanced level is marker-only, so the partial's payload
	// root IS the marker wrapper div and it must carry the markers itself.
	res, err := a.RenderPartialFromResult(context.Background(), "/settings/advanced/security", "/settings/base")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if got := strings.Count(s, "data-fui-doc-lang"); got != 1 {
		t.Fatalf("data-fui-doc-lang appears %d times, want 1 on the marker wrapper:\n%s", got, s)
	}
	w := openingTag(s, strings.Index(s, `data-fui-layout-key="g:/settings/advanced/"`))
	if !strings.Contains(w, `data-fui-doc-lang="fr"`) {
		t.Errorf("marker-only wrapper must carry the doc markers, got tag %q", w)
	}
}

func TestLayoutlessDocShellOnMain(t *testing.T) {
	a := NewApp("t").
		WithLang("fr").
		WithSkipLabel("Aller au contenu principal")
	a.RegisterScreen(NewScreen("/x", &stubComponent{html: render.Raw("X")}), nil)

	res, err := a.RenderPageResult(context.Background(), "/x")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	main := openingTag(s, strings.Index(s, "<main"))
	if !strings.Contains(main, `data-fui-doc-lang="fr"`) || !strings.Contains(main, "data-fui-skip-label") {
		t.Errorf("a layout-less page must carry the doc markers on its <main> (the element swapShell targets):\n%s", s)
	}
}

func TestBarePartialHasNoDocShellMarkers(t *testing.T) {
	// A fully shared chain renders bare content: nothing re-renders, so
	// there is no element to carry fresh markers, and the client cannot
	// (and must not) change the document language. A site whose language
	// varies between routes keys its outer layer per language for exactly
	// this reason (WithKey); the keyed nav always delivers a carrier.
	g := NewScreenGroup("/docs", NewLayout("docs").WithSidebar(NewStaticComponent("NAV")))
	g.Screen(NewScreen("a", &stubComponent{html: render.Raw("A")}), nil)
	g.Screen(NewScreen("b", &stubComponent{html: render.Raw("B")}), nil)
	a := NewApp("t").WithLang("es")
	a.Router.ScreenGroup(g)

	res, err := a.RenderPartialFromResult(context.Background(), "/docs/b", "/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(res.HTML); strings.Contains(s, "data-fui-doc-lang") || strings.Contains(s, "data-fui-skip-label") {
		t.Errorf("bare partial must not carry doc markers:\n%s", s)
	}
}
