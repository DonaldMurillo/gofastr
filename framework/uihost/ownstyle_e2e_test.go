package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

const (
	ownGreen = "rgb(0, 128, 0)"
	ownRed   = "rgb(255, 0, 0)"
	ownBlue  = "rgb(0, 0, 255)"
	ownNone  = "rgba(0, 0, 0, 0)"
)

// ownedStyleServer serves a site built on owned styles. Every sheet
// registers inside an isolated registry, so the LoadAlways app sheet
// never reaches another test's head.
//
//   - /plain: no layout, no owned style — the soft-nav starting point.
//   - /styled/a: the "own-layout" layout; a screen with no style of
//     its own, holding every scope probe.
//   - /styled/b: the same layout; a screen with its own style.
//   - /styled/lazy: the same layout; no kit marker, so the kit sheet
//     can arrive after the owned one at runtime.
//
// Two kit-shaped sheets (unscoped, the way every framework/ui
// component registers) set the same properties as own-layout at equal
// specificity. "a-kit" sorts before "own-layout" in the head bundle
// and "tie-kit" after it, so one page load puts a kit sheet on both
// sides of the owned one.
func ownedStyleServer(t *testing.T) *httptest.Server {
	t.Helper()
	registry.IsolateForTest(t)
	registry.RegisterStyle("a-kit", func(style.Theme) string { return ".tie2 { color: rgb(255, 0, 0); }" })
	registry.RegisterStyle("tie-kit", func(style.Theme) string { return ".tie { color: rgb(255, 0, 0); }" })
	ownstyle.Must("app", ownstyle.KindApp, ".app-probe { background-color: rgb(0, 0, 255); }")
	layoutStyle := ownstyle.Must("own-layout", ownstyle.KindScoped, `
.owned { background-color: rgb(0, 128, 0); }
.tie, .tie2 { color: rgb(0, 128, 0); }
.nest { outline-color: rgb(255, 0, 0); }
.dark-probe { @media (--dark) { background-color: rgb(0, 128, 0); } }
`)
	nested := ownstyle.Must("own-nested", ownstyle.KindScoped, `:scope { outline-color: rgb(0, 128, 0); }`)
	screenB := ownstyle.Must("own-screen-b", ownstyle.KindScoped, `.owned-b { background-color: rgb(0, 128, 0); }`)

	otherStyle := ownstyle.Must("own-other", ownstyle.KindScoped, `.owned-o { background-color: rgb(0, 128, 0); }`)

	// The outlet makes the runtime load its envelope module on these
	// pages, which then drives every navigation away from them.
	aside := app.NewOutlet("aside", app.OutletOptions{Default: &rawHTMLComp{html: `<p>aside</p>`}})
	layout := app.NewLayout("owned", app.LayoutSpec{Style: layoutStyle, Outlets: []*app.Outlet{aside}}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Primary(), l.Place(aside))
	})
	other := app.NewLayout("other", app.LayoutSpec{Style: otherStyle}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	a := app.NewApp("owned")
	a.RegisterScreen(app.NewScreen("/plain", &rawHTMLComp{html: `<p id="plain">plain</p><a id="goA" href="/styled/a">a</a>`}).WithTitle("Plain"), nil)
	a.RegisterScreen(app.NewScreen("/styled/a", &rawHTMLComp{html: `<p class="owned" id="slot">slot</p>` +
		`<div data-fui-internal><p class="owned" id="internal-own">i</p><p class="app-probe" id="internal-app">i</p></div>` +
		`<p class="app-probe" id="app">app</p>` +
		`<p class="tie" id="tie-in">tie</p><p class="tie2" id="tie2-in">tie</p>` +
		// The kit markers sit inside the internal boundary, outside
		// own-layout's scope: they load the kit sheets, and the probes
		// beside them prove those sheets applied.
		`<div data-fui-internal data-fui-comp="tie-kit"><p class="tie" id="tie-out">k</p></div>` +
		`<div data-fui-internal data-fui-comp="a-kit"><p class="tie2" id="tie2-out">k</p></div>` +
		`<p class="dark-probe" id="dark">dark</p>` +
		string(nested.Scope(`<section class="nest owned" id="nested-root"><p class="owned" id="nested-child">n</p></section>`)) +
		`<a id="goB" href="/styled/b">b</a>`}).WithTitle("A"), layout)
	a.RegisterScreen(app.NewScreen("/styled/b", &rawHTMLComp{html: `<p class="owned-b" id="b">b</p><a id="goOther" href="/other">other</a>`}).WithStyle(screenB).WithTitle("B"), layout)
	a.RegisterScreen(app.NewScreen("/other", &rawHTMLComp{html: `<p class="owned-o" id="other">other</p>`}).WithTitle("Other"), other)
	a.RegisterScreen(app.NewScreen("/styled/lazy", &rawHTMLComp{html: `<p id="lazy">lazy</p>`}).WithTitle("Lazy"), layout)

	srv := httptest.NewServer(New(a))
	t.Cleanup(srv.Close)
	return srv
}

func ownedStyleContext(t *testing.T) context.Context {
	t.Helper()
	return chromedptest.Context(t,
		chromedptest.Timeout(60*time.Second),
		chromedptest.AllocatorOptions(chromedp.ExecPath(browserExecutable(t))))
}

// ownProp reads one computed property from each element id.
func ownProp(t *testing.T, ctx context.Context, prop string, ids ...string) map[string]string {
	t.Helper()
	var got map[string]string
	js := `Object.fromEntries(` + jsStringArray(ids) + `.map(id => [id, getComputedStyle(document.getElementById(id))['` + prop + `']]))`
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &got)); err != nil {
		t.Fatalf("read %s: %v", prop, err)
	}
	return got
}

func jsStringArray(ss []string) string {
	return `['` + strings.Join(ss, `','`) + `']`
}

func wantProps(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: #%s = %q, want %q", what, id, got[id], w)
		}
	}
}

// The app style loads on every page, rendered or exported: the live
// head bundles it, and the static-export path (one link per sheet)
// links it directly.
func TestAppStyleReachesEveryHead(t *testing.T) {
	registry.IsolateForTest(t)
	ownstyle.Must("app", ownstyle.KindApp, ".x { gap: 1px; }")
	a := app.NewApp("appstyle")
	a.RegisterScreen(app.NewScreen("/", &rawHTMLComp{html: `<p>no markers</p>`}).WithTitle("Home"), nil)
	ds := New(a)
	if body := pageBody(t, ds, "/"); !strings.Contains(body, `/__gofastr/comp/app.css`) {
		t.Errorf("a page with no markers must still link the app style:\n%s", truncate(body, 800))
	}
	if tags := ds.componentCSSTags(`<p>no markers</p>`, false); !strings.Contains(tags, `href="/__gofastr/comp/app.css?v=`) {
		t.Errorf("the export path must link the app style directly, got %q", tags)
	}
}

// TestOwnedStyleScopeInBrowser pins the compiled @scope's reach in
// Chromium: the owner styles its slot content and a nested owner's
// root, and stops at the nested owner's children and at a
// data-fui-internal subtree; the app style covers every page except
// internal subtrees; (--dark) applies only under a dark document.
func TestOwnedStyleScopeInBrowser(t *testing.T) {
	srv := ownedStyleServer(t)
	ctx := ownedStyleContext(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/styled/a"),
		chromedp.WaitVisible(`#slot`, chromedp.ByID),
		chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme','light')`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	wantProps(t, "background", ownProp(t, ctx, "backgroundColor",
		"slot", "nested-root", "nested-child", "internal-own", "app", "internal-app", "dark"), map[string]string{
		"slot":         ownGreen, // slot content is the layout's
		"nested-root":  ownGreen, // a nested owner's root is still the outer owner's
		"nested-child": ownNone,  // its children are not
		"internal-own": ownNone,  // framework internals are no owner's
		"app":          ownBlue,  // the app style reaches into a layout
		"internal-app": ownNone,  // but not into internals
		"dark":         ownNone,  // (--dark) is off in a light document
	})
	// Proximity: the nested owner's :scope beats the outer owner's
	// .nest at equal specificity, whichever sheet loaded last.
	wantProps(t, "outline", ownProp(t, ctx, "outlineColor", "nested-root"), map[string]string{"nested-root": ownGreen})

	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme','dark')`, nil)); err != nil {
		t.Fatal(err)
	}
	wantProps(t, "dark background", ownProp(t, ctx, "backgroundColor", "dark"), map[string]string{"dark": ownGreen})
}

// TestOwnedRuleWinsTieInEitherOrder pins the cascade claim the load
// path rests on: an owned rule is scoped and a kit rule is not, so at
// equal specificity the owned rule wins by scope proximity whatever
// order the sheets load in. Nothing in the host or the runtime orders
// sheets for it. Three orders: a kit sheet before the owned one in
// the SSR head, a kit sheet after it, and a kit sheet the runtime
// loads after the page has rendered. Each case also proves the kit
// sheet applied (its probe outside the scope reads red) and that the
// order is the one claimed.
func TestOwnedRuleWinsTieInEitherOrder(t *testing.T) {
	srv := ownedStyleServer(t)
	ctx := ownedStyleContext(t)

	t.Run("ssr-head", func(t *testing.T) {
		var href string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+"/styled/a"),
			chromedp.WaitVisible(`#slot`, chromedp.ByID),
			chromedp.Evaluate(`[...document.querySelectorAll('link[rel=stylesheet]')].map(l => l.href).find(h => h.includes('own-layout')) || ''`, &href),
		); err != nil {
			t.Fatal(err)
		}
		ak, own, tk := strings.Index(href, "a-kit"), strings.Index(href, "own-layout"), strings.Index(href, "tie-kit")
		if ak < 0 || tk < 0 || !(ak < own && own < tk) {
			t.Fatalf("want one head bundle loading a-kit, own-layout, tie-kit in that order, got %q", href)
		}
		wantProps(t, "tie", ownProp(t, ctx, "color", "tie-in", "tie2-in", "tie-out", "tie2-out"), map[string]string{
			"tie-in":   ownGreen, // kit sheet loaded after the owned one
			"tie2-in":  ownGreen, // kit sheet loaded before it
			"tie-out":  ownRed,
			"tie2-out": ownRed,
		})
	})

	t.Run("runtime-load-after", func(t *testing.T) {
		var loaded bool
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+"/styled/lazy"),
			chromedp.WaitVisible(`#lazy`, chromedp.ByID),
			chromedp.Evaluate(`(() => {
				if (document.querySelector('link[href*="tie-kit"]')) return 'kit already in head';
				const p = document.getElementById('lazy');
				p.insertAdjacentHTML('afterend',
					'<p class="tie" id="lazy-in">t</p>' +
					'<div data-fui-internal data-fui-comp="tie-kit"><p class="tie" id="lazy-out">k</p></div>');
				window.__gofastr.scanAndLoadCSS(p.parentNode);
				return 'ok';
			})()`, nil),
			chromedp.Poll(`getComputedStyle(document.getElementById('lazy-out')).color === '`+ownRed+`'`, &loaded, chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("lazy kit load: %v", err)
		}
		var order []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('link[rel=stylesheet]')].map(l => l.href).filter(h => h.includes('own-layout') || h.includes('tie-kit'))`, &order)); err != nil {
			t.Fatal(err)
		}
		if len(order) != 2 || !strings.Contains(order[0], "own-layout") || !strings.Contains(order[1], "tie-kit") {
			t.Fatalf("want the kit link after the owned one, got %q", order)
		}
		wantProps(t, "lazy tie", ownProp(t, ctx, "color", "lazy-in", "lazy-out"), map[string]string{
			"lazy-in":  ownGreen,
			"lazy-out": ownRed,
		})
	})
}

// TestOwnedStylesLoadOnSoftNav pins the loader half of data-fui-scope:
// each client navigation loads the sheets its new markup needs, with
// no reload. Three swaps, each a different runtime path:
//
//   - /plain → /styled/a: the core navigator's shell swap; the new
//     shell root itself carries the layout's scope.
//   - /styled/a → /styled/b: a slot swap in the same layout; the page
//     holds an outlet, so this click loads the envelope module.
//   - /styled/b → /other: the envelope module's own cross-layout
//     shell swap.
func TestOwnedStylesLoadOnSoftNav(t *testing.T) {
	srv := ownedStyleServer(t)
	ctx := ownedStyleContext(t)
	var ok bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/plain"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Evaluate(`window.__stay = 1; !document.querySelector('link[href*="own-"]')`, &ok),
	); err != nil {
		t.Fatalf("navigate plain: %v", err)
	}
	if !ok {
		t.Fatal("the plain page must not load an owned sheet (the test would prove nothing)")
	}
	steps := []struct {
		link, probe string
		envelope    bool // the envelope module must drive this click
	}{
		{"#goA", "slot", false},
		{"#goB", "b", false},
		{"#goOther", "other", true},
	}
	for _, s := range steps {
		if s.envelope {
			if err := chromedp.Run(ctx, chromedp.Poll(`!!window.__gofastr._navHooks?.envelope`, &ok, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
				t.Fatalf("before %s: the envelope module never loaded: %v", s.link, err)
			}
		}
		if err := chromedp.Run(ctx,
			chromedp.Click(s.link, chromedp.ByQuery),
			chromedp.WaitVisible(`#`+s.probe, chromedp.ByQuery),
			chromedp.Poll(`getComputedStyle(document.getElementById('`+s.probe+`')).backgroundColor === '`+ownGreen+`'`, &ok, chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			t.Fatalf("after %s, #%s never took its owned style: %v", s.link, s.probe, err)
		}
		var stay int
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__stay || 0`, &stay)); err != nil {
			t.Fatal(err)
		}
		if stay != 1 {
			t.Fatalf("%s reloaded the page; it must be a client navigation", s.link)
		}
	}
}
