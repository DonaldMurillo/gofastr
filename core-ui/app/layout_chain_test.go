package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestDirectScreenLayoutReplacesDefault(t *testing.T) {
	a := app.NewApp("t")
	a.SetDefaultLayout(app.NewLayout("site").WithHeader(app.NewStaticComponent("SITE")))
	own := app.NewLayout("bare").WithHeader(app.NewStaticComponent("OWN"))
	a.Register("/x", &stubComp{html: "X"}, own)

	res, err := a.RenderPageResult(context.Background(), "/x")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if strings.Contains(s, "SITE") {
		t.Errorf("explicit screen layout must replace the default, got: %s", s)
	}
	if !strings.Contains(s, "OWN") || !strings.Contains(s, `data-fui-layout-key="l:bare"`) {
		t.Errorf("own layout with key missing: %s", s)
	}
}

func TestGroupNestsUnderDefaultWithOneMain(t *testing.T) {
	a := app.NewApp("t")
	a.SetDefaultLayout(app.NewLayout("site").WithHeader(app.NewStaticComponent("SITE")))
	g := app.NewScreenGroup("/docs", app.NewLayout("docs").WithSidebar(app.NewStaticComponent("DOCS_NAV")))
	g.Screen(app.NewScreen("intro", &stubComp{html: "INTRO"}), nil)
	a.Router.ScreenGroup(g)

	res, err := a.RenderPageResult(context.Background(), "/docs/intro")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if got := strings.Count(s, "<main"); got != 1 {
		t.Fatalf("want exactly 1 <main>, got %d: %s", got, s)
	}
	// Default layout is layer 0 (owns <main>); group layer nests inside.
	if !strings.Contains(s, `data-fui-layout-slot="l:site"`) {
		t.Errorf("default layer slot missing: %s", s)
	}
	if !strings.Contains(s, `data-fui-layout-key="g:/docs/:docs"`) {
		t.Errorf("group layer key missing: %s", s)
	}
	if strings.Index(s, `data-fui-layout-key="l:site"`) >= strings.Index(s, `data-fui-layout-key="g:/docs/:docs"`) {
		t.Errorf("default layer must wrap group layer: %s", s)
	}
}

func TestStandaloneGroupSkipsDefault(t *testing.T) {
	a := app.NewApp("t")
	a.SetDefaultLayout(app.NewLayout("site").WithHeader(app.NewStaticComponent("SITE")))
	g := app.NewScreenGroup("/admin", app.NewLayout("admin").WithSidebar(app.NewStaticComponent("ADMIN"))).Standalone()
	g.Screen(app.NewScreen("home", &stubComp{html: "H"}), nil)
	a.Router.ScreenGroup(g)

	res, err := a.RenderPageResult(context.Background(), "/admin/home")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if strings.Contains(s, "SITE") {
		t.Errorf("standalone group must not nest under default: %s", s)
	}
	if got := strings.Count(s, "<main"); got != 1 {
		t.Fatalf("want exactly 1 <main>, got %d: %s", got, s)
	}
	// The group layer is layer 0: it owns <main> and its slot.
	if !strings.Contains(s, `<main data-fui-layout-slot="g:/admin/:admin"`) {
		t.Errorf("standalone group layer must own the main slot: %s", s)
	}
}

func TestInheritedSubgroupLayerIsMarkerOnly(t *testing.T) {
	parent := app.NewScreenGroup("/settings", app.NewLayout("settings").WithSidebar(app.NewStaticComponent("NAV")))
	child := parent.SubGroup("advanced", nil) // inherits parent's *Layout
	child.Screen(app.NewScreen("security", &stubComp{html: "SEC"}), nil)

	r := app.NewRouter()
	r.ScreenGroup(parent)
	out, err := r.RenderRaw("/settings/advanced/security")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// The inherited layout renders ONCE (at the parent level), not twice.
	if got := strings.Count(s, "NAV"); got != 1 {
		t.Errorf("inherited layout must render once, got %d: %s", got, s)
	}
	// The child level still exists as an addressable marker-only layer.
	if !strings.Contains(s, `data-fui-layout-key="g:/settings/advanced/"`) ||
		!strings.Contains(s, `data-fui-layout-slot="g:/settings/advanced/"`) {
		t.Errorf("marker-only child layer missing: %s", s)
	}
	if !strings.Contains(s, `data-fui-layout-key="g:/settings/:settings"`) {
		t.Errorf("parent layer key missing: %s", s)
	}
}

func TestOverrideScreenKeyDiffersFromSibling(t *testing.T) {
	g := app.NewScreenGroup("/shop", app.NewLayout("shop").WithSidebar(app.NewStaticComponent("SHOP")))
	g.Screen(app.NewScreen("browse", &stubComp{html: "B"}), nil)
	g.Screen(app.NewScreen("checkout", &stubComp{html: "C"}), app.NewLayout("focus"))

	a := app.NewApp("t")
	a.Router.ScreenGroup(g)

	var browseKeys, checkoutKeys []string
	for _, e := range a.Routes() {
		switch e.Path {
		case "/shop/browse":
			browseKeys = e.Layouts
		case "/shop/checkout":
			checkoutKeys = e.Layouts
		}
	}
	if len(browseKeys) != 1 || browseKeys[0] != "g:/shop/:shop" {
		t.Errorf("browse chain = %v", browseKeys)
	}
	if len(checkoutKeys) != 1 || checkoutKeys[0] != "g:/shop/:focus" {
		t.Errorf("checkout chain = %v", checkoutKeys)
	}
}

func TestOverlayScreensHaveNoChain(t *testing.T) {
	a := app.NewApp("t")
	a.SetDefaultLayout(app.NewLayout("site"))
	a.RegisterScreen(app.NewDrawer("/cart", &stubComp{html: "CART"}), nil)

	for _, e := range a.Routes() {
		if e.Path == "/cart" && len(e.Layouts) != 0 {
			t.Errorf("drawer must have empty chain, got %v", e.Layouts)
		}
	}
}

func TestPartialFromSiblingIsBareWithSwapLayer(t *testing.T) {
	a := app.NewApp("t")
	g := app.NewScreenGroup("/docs", app.NewLayout("docs").WithSidebar(app.NewStaticComponent("NAV")))
	g.Screen(app.NewScreen("a", &stubComp{html: "PAGE_A"}), nil)
	g.Screen(app.NewScreen("b", &stubComp{html: "PAGE_B"}), nil)
	a.Router.ScreenGroup(g)

	res, err := a.RenderPartialFromResult(context.Background(), "/docs/b", "/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if res.SwapLayer != "g:/docs/:docs" {
		t.Errorf("SwapLayer = %q, want g:/docs/:docs", res.SwapLayer)
	}
	s := string(res.HTML)
	if !strings.Contains(s, "PAGE_B") {
		t.Errorf("content missing: %s", s)
	}
	// Fully shared chain → bare content, no re-rendered shell.
	if strings.Contains(s, "NAV") || strings.Contains(s, "data-fui-layout-key") {
		t.Errorf("sibling partial must not re-render the shared shell: %s", s)
	}
}

func TestPartialFromRendersOnlyDivergingLayers(t *testing.T) {
	a := app.NewApp("t")
	a.SetDefaultLayout(app.NewLayout("site").WithHeader(app.NewStaticComponent("SITE")))
	g := app.NewScreenGroup("/docs", app.NewLayout("docs").WithSidebar(app.NewStaticComponent("DOCS_NAV")))
	g.Screen(app.NewScreen("intro", &stubComp{html: "INTRO"}), nil)
	a.Router.ScreenGroup(g)
	a.Register("/about", &stubComp{html: "ABOUT"}, nil) // default layout only

	// /about → /docs/intro shares only the default root; the docs layer
	// must be re-rendered in the partial, the site chrome must not.
	res, err := a.RenderPartialFromResult(context.Background(), "/docs/intro", "/about")
	if err != nil {
		t.Fatal(err)
	}
	if res.SwapLayer != "l:site" {
		t.Errorf("SwapLayer = %q, want l:site", res.SwapLayer)
	}
	s := string(res.HTML)
	if strings.Contains(s, "SITE") {
		t.Errorf("shared root must not be re-rendered: %s", s)
	}
	if !strings.Contains(s, "DOCS_NAV") || !strings.Contains(s, "INTRO") {
		t.Errorf("diverging docs layer + content must render: %s", s)
	}
	// Re-rendered layers nest: no <main> in a partial.
	if strings.Contains(s, "<main") {
		t.Errorf("partial must not emit <main>: %s", s)
	}
	if !strings.Contains(s, `data-fui-layout-slot="g:/docs/:docs"`) {
		t.Errorf("re-rendered layer must carry its slot marker: %s", s)
	}
}

func TestPartialFromUnknownOriginIsBare(t *testing.T) {
	a := app.NewApp("t")
	a.SetDefaultLayout(app.NewLayout("site"))
	a.Register("/x", &stubComp{html: "X"}, nil)

	res, err := a.RenderPartialFromResult(context.Background(), "/x", "/nope")
	if err != nil {
		t.Fatal(err)
	}
	if res.SwapLayer != "" {
		t.Errorf("unknown origin must yield no SwapLayer, got %q", res.SwapLayer)
	}
	if !strings.Contains(string(res.HTML), "X") {
		t.Errorf("content missing: %s", res.HTML)
	}
}

func TestPartialFromDisjointChainsIsBare(t *testing.T) {
	a := app.NewApp("t")
	a.Register("/m", &stubComp{html: "M"}, app.NewLayout("marketing"))
	a.Register("/app", &stubComp{html: "A"}, app.NewLayout("app"))

	res, err := a.RenderPartialFromResult(context.Background(), "/app", "/m")
	if err != nil {
		t.Fatal(err)
	}
	if res.SwapLayer != "" {
		t.Errorf("disjoint chains must yield no SwapLayer, got %q", res.SwapLayer)
	}
	if strings.Contains(string(res.HTML), "data-fui-layout-key") {
		t.Errorf("bare partial must not carry layer markers: %s", res.HTML)
	}
}

func TestPreloadOptionValidatesMode(t *testing.T) {
	a := app.NewApp("t")
	a.Register("/p", &stubComp{html: "P"}, nil, app.Preload(app.PreloadHover))
	for _, e := range a.Routes() {
		if e.Path == "/p" && e.Preload != "hover" {
			t.Errorf("Preload = %q, want hover", e.Preload)
		}
	}

	defer func() {
		if recover() == nil {
			t.Error("bad preload mode must panic at registration")
		}
	}()
	app.Preload("sometimes")
}
