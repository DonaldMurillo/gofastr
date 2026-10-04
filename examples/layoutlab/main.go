// Package main is layoutlab, the demo app for the tree-layout outlets
// prototype. One tree layout "shell" carries a
// static header with nav links, a "crumbs" route area rendering the
// path, a "toolbar" outlet (FallbackNothing) and an "aside" outlet
// (Default: HelpPanel). Screens and the /inbox group fill the outlets;
// every fill renders a visibly distinct marker so browser tests can
// assert exactly which fill is on screen:
//
//	TOOLBAR-INBOX / TOOLBAR-MESSAGE / TOOLBAR-GROUP   (toolbar outlet)
//	ASIDE-HELP (default) / ASIDE-SENDER-<id> / ASIDE-SLOW
//	crumbs:<path>                                      (route area)
//
// Run with:
//
//	go run ./examples/layoutlab        # :8095, or $PORT
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/static"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// defaultAddr is the fallback when $PORT is unset; `gofastr dev` and
// PaaS runtimes inject PORT and isolation.ListenAddr honours it.
const defaultAddr = ":8095"

// labStats counts SPA navigation requests so the e2e suite can prove a
// Back replay came from the client cache (count does not grow).
var labStats struct {
	navigations atomic.Int64
}

func main() {
	fwApp := buildApp()
	// `layoutlab --export <dir>` renders
	// the lab to static HTML + assets, like examples/site. The lab's
	// export set deliberately omits the routes that fail on purpose:
	// the builder now REFUSES a page whose fill failed and was
	// contained (a live server degrades the outlet, an export would
	// bake the degraded bytes in), so /broken/*, /mixfail and
	// /slowfail stay out unless a test includes them to prove that.
	if dir := exportDir(os.Args[1:]); dir != "" {
		var host *uihost.UIHost
		for _, m := range fwApp.Mountables() {
			if h, ok := m.(*uihost.UIHost); ok {
				host = h
				break
			}
		}
		if host == nil {
			log.Fatal("export: no uihost.UIHost mounted")
		}
		if _, err := (&static.Builder{
			Host:          host,
			OutDir:        dir,
			ExcludeRoutes: labExportExcludes,
		}).Build(context.Background()); err != nil {
			log.Fatalf("export: %v", err)
		}
		if err := writeLabScripts(dir); err != nil {
			log.Fatalf("export: %v", err)
		}
		log.Printf("layoutlab exported to %s", dir)
		return
	}
	listenAddr, err := isolation.ListenAddr(".", defaultAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("layoutlab listening on http://localhost%s", listenAddr)
	if err := fwApp.Start(listenAddr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// writeLabScripts writes the lab's own scripts (served by plain
// handlers, which the route export does not reach) beside the export,
// so the reducers and the timeline run on a static host too.
func writeLabScripts(dir string) error {
	// PUBLIC static-export web assets, served verbatim by whatever host
	// serves the export — the same class as the static builder's own
	// 0644 page/asset writes; owner-only would break serving under a
	// different user.
	// gofastr:allow(worldreadable) public export asset dir (see above)
	if err := os.MkdirAll(filepath.Join(dir, "__lab"), 0o755); err != nil {
		return err
	}
	for name, body := range map[string]string{
		"route-reducers.js": labReducersJS,
		"timeline.js":       labTimelineJS,
	} {
		// gofastr:allow(worldreadable) public export asset (see the dir note above)
		if err := os.WriteFile(filepath.Join(dir, "__lab", name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// exportDir scans args for `--export <dir>` or `--export=<dir>`, the
// examples/site spelling: the target directory, or "" to serve live.
func exportDir(args []string) string {
	for i := range args {
		switch {
		case args[i] == "--export" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(args[i], "--export="):
			return strings.TrimPrefix(args[i], "--export=")
		}
	}
	return ""
}

// labExportExcludes is the lab's static export set: the routes whose
// fills fail ON PURPOSE stay out (the builder refuses a contained fill
// failure; a live server degrades the outlet instead).
var labExportExcludes = []string{"/broken", "/mixfail", "/slowfail", "/partsfail"}

// buildApp wires the site into a framework app without binding a port,
// so tests drive the app in-process.
func buildApp() *framework.App {
	site := buildSite()
	hostOpts := []uihost.Option{
		//: the computed reducers ride an external script after
		// runtime.js (CSP-clean; the namespace assignment can't clobber
		// them). Served from the route below.
		uihost.WithExtraScripts("/__lab/route-reducers.js", "/__lab/timeline.js"),
		// The typed transitions (LayoutSpec.Primary.Transition =
		// app.Slide(...)) need no wiring: the host collects every
		// layout's generated CSS into app.css. This is only the
		// diagnostic region outlines and timeline panel.
		uihost.WithCustomCSS(labDemoCSS),
	}
	// The page-wide loading component is a fixture option (env-gated)
	// so the default-strip baseline tests and the WithPageLoading tests
	// share one buildApp.
	if os.Getenv("LAYOUTLAB_PAGE_LOADING") == "1" {
		hostOpts = append(hostOpts, uihost.WithPageLoading(uiapp.LoadingComponent(pageBar())))
	}
	host := uihost.New(site, hostOpts...)
	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "layoutlab"}),
	)
	fwApp.Use(host.RouteMatchMiddleware())
	// Count SPA navigations for the e2e cache-replay assertion.
	fwApp.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				labStats.navigations.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	fwApp.Mount(host)
	fwApp.Router().Get("/__lab/stats", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int64{"navigations": labStats.navigations.Load()})
	}))
	// The computed reducers (): external script, CSP-clean, loaded
	// after runtime.js via WithExtraScripts. The bodies mirror
	// crumbsFor/mixFor so the SSR first paint matches the recomputed
	// value (no flash on hydration).
	//gofastr:allow(GOFASTR1003) TestTreeLayoutFillsE2E exercises the route-bound crumb reducer through the loaded script.
	fwApp.Router().Get("/__lab/route-reducers.js", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(labReducersJS))
	}))
	// The timeline panel (the lab's self-explaining layer): an external
	// script, same CSP-clean pattern. It only READS the runtime's
	// events and regions — no framework behaviour depends on it.
	//gofastr:allow(GOFASTR1003) TestTreeLayoutFillsE2E checks the loaded timeline reports the changed message region.
	fwApp.Router().Get("/__lab/timeline.js", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(labTimelineJS))
	}))
	// fixture: a route answering text/plain 404 — the non-HTML
	// error a plain endpoint or a broken proxy gives — so the e2e can
	// prove the nav toast names the status, not "check your connection".
	//gofastr:allow(GOFASTR1003) TestP14NonHTML404ToastsStatus navigates here through a JavaScript expression.
	fwApp.Router().Get("/nope-plain", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such page", http.StatusNotFound)
	}))

	return fwApp
}

// labItemsLayout is the /items group's tree layout; buildApp feeds its
// generated view-transition CSS () to the host. A package var so
// buildSite's construction and buildApp's CSS pick-up share one value.
var labItemsLayout *uiapp.Layout

// labPartsLayout is the parallel-parts fixture's layout; a package
// var for the same reason as labItemsLayout.
var labPartsLayout *uiapp.Layout

// pageBar is the page-wide loading component (a fixed top bar
// labelled for tests). Its placement/animation CSS is the framework's
// ([data-fui-page-loading] visibility rules); the label carries the
// test hook.
func pageBar() render.HTML {
	return html.Div(html.DivConfig{
		ID:         "lab-page-bar",
		ExtraAttrs: html.Attrs{"role": "status"},
	}, render.Text("PAGE-BAR"))
}

const labReducersJS = `(function () {
	var G = window.__gofastr = window.__gofastr || {};
	G._reducers = G._reducers || {};
	G._reducers['lab.crumbs'] = function (deps) {
		var p = (deps['route.path'] || '/').split('?')[0];
		var parts = ['Home'];
		p.split('/').forEach(function (s) {
			if (!s) return;
			parts.push(s.charAt(0).toUpperCase() + s.slice(1));
		});
		return parts.join(' / ');
	};
	G._reducers['lab.mix'] = function (deps) {
		return (deps['route.path'] || '') + '|' + (deps['route.params.id'] || '');
	};
})();`

// labTimelineJS renders the timeline panel: for every navigation it
// lists the runtime's events (beforenavigate, transition, navigate)
// and each region the DOM actually changed (a MutationObserver over
// outlets, areas and slots), with millisecond offsets from the click.
// Changed regions get a 600 ms border flash and a "+N ms" tag via the
// data-lab-flash attribute (the CSS owns the look). The panel is a
// direct child of body — never inside a region, never a div[hidden]
// (a test counts those as parked nodes) — and it only READS the
// runtime: no framework behaviour depends on it.
const labTimelineJS = `(function () {
	'use strict';
	if (window.__labTimeline) return;
	var api = window.__labTimeline = {};

	// Region addresses -> short names (mirrors the CSS tags).
	var NAMES = {
		'l:shell#toolbar': 'toolbar outlet',
		'l:shell#aside': 'aside outlet',
		'l:shell#rail': 'rail outlet',
		'l:shell~crumbs': 'crumbs area',
		'l:shell': 'main slot',
		'l:bare': 'main slot (bare page)',
		'g:/items/:items': 'item detail slot',
	};
	function regionName(el) {
		var a = el.getAttribute('data-cui-outlet') || el.getAttribute('data-cui-area') ||
			el.getAttribute('data-cui-layout-slot') || '';
		if (a) return NAMES[a] || a;
		var k = el.getAttribute('data-cui-layout-key') || '';
		if (k === 'g:/items/:items') return 'kept layer (items list)';
		return k ? 'layer ' + k : 'region';
	}

	// --- panel -----------------------------------------------------------
	var panel = document.createElement('div');
	panel.id = 'lab-timeline';
	var bar = document.createElement('div');
	bar.className = 'lab-tl-bar';
	var title = document.createElement('strong');
	title.textContent = 'Timeline';
	var toggle = document.createElement('button');
	toggle.type = 'button';
	toggle.textContent = 'collapse';
	toggle.setAttribute('aria-expanded', 'true');
	var clear = document.createElement('button');
	clear.type = 'button';
	clear.textContent = 'clear';
	bar.appendChild(title);
	bar.appendChild(toggle);
	bar.appendChild(clear);
	var logBox = document.createElement('div');
	logBox.className = 'lab-tl-log';
	var hint = document.createElement('div');
	hint.className = 'lab-tl-hint';
	hint.textContent = 'Click any link: each navigation lists what happened, per region, with ms offsets. Changed boxes flash and carry +ms.';
	logBox.appendChild(hint);
	panel.appendChild(bar);
	panel.appendChild(logBox);
	document.body.appendChild(panel);

	toggle.addEventListener('click', function () {
	var min = panel.classList.toggle('lab-tl-min');
		toggle.textContent = min ? 'expand' : 'collapse';
		toggle.setAttribute('aria-expanded', min ? 'false' : 'true');
	});
	clear.addEventListener('click', function () {
		logBox.textContent = '';
		session = null;
	});

	// --- one block per navigation -----------------------------------------
	var session = null;
	var lastTypes = [];
	var sawPop = false;

	function startSession(t0, label, target) {
		if (session) session = null;
		while (logBox.children.length > 10) logBox.removeChild(logBox.firstChild);
		var block = document.createElement('div');
		block.className = 'lab-tl-nav';
		var path = document.createElement('div');
		path.className = 'lab-tl-path';
		path.textContent = label;
		block.appendChild(path);
		logBox.appendChild(block);
		session = { t0: t0, target: target || '', seen: {}, lines: 0, block: block };
	}
	function line(kind, text) {
		if (!session) return;
		if (session.lines++ > 60) return;
		var row = document.createElement('div');
		row.className = 'lab-tl-line';
		var t = document.createElement('span');
		t.className = 'lab-tl-ms';
		t.textContent = '+' + Math.round(performance.now() - session.t0) + ' ms';
		var body = document.createElement('span');
		body.className = 'lab-tl-kind-' + kind;
		body.textContent = text;
		row.appendChild(t);
		row.appendChild(body);
		session.block.appendChild(row);
		logBox.scrollTop = logBox.scrollHeight;
	}

	// 600 ms flash on the region itself; the tag is CSS (attr()).
	function flash(el, offsetMs) {
		el.removeAttribute('data-lab-flash');
		void el.offsetWidth; /* restart the animation */
		el.setAttribute('data-lab-flash', '+' + offsetMs + ' ms');
		clearTimeout(el.__labFlashT);
		el.__labFlashT = setTimeout(function () {
			el.removeAttribute('data-lab-flash');
		}, 600);
	}

	function regionChanged(el) {
		if (!session) return;
		var name = regionName(el);
		var text = (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 48);
		if (!session.seen[name]) {
			session.seen[name] = true;
			line('change', name + ' changed \u2192 "' + text + '"');
		}
		flash(el, Math.round(performance.now() - session.t0));
		// Contained error: the failing aside fill leaves the outlet on a
		// fallback marker instead of failing the page.
		if (el.getAttribute('data-cui-outlet') === 'l:shell#aside' &&
			/^(\/broken\/|\/mixfail|\/slowfail)/.test(session.target) &&
			/ASIDE-(HELP|BOUNDARY-ERROR)/.test(text)) {
			line('error', 'contained error \u2014 the failing aside fill fell back (' + text.slice(0, 24) + ')');
		}
	}

	// --- observers ---------------------------------------------------------
	var mo = new MutationObserver(function (recs) {
		for (var i = 0; i < recs.length; i++) {
			var r = recs[i];
			if (r.type === 'childList') {
				if (r.addedNodes.length || r.removedNodes.length) regionChanged(r.target);
			} else if (r.type === 'attributes') {
				var v = r.target.getAttribute(r.attributeName);
				line('state', regionName(r.target) + ' \u00b7 ' +
					(v === null ? r.attributeName + ' cleared' : r.attributeName + '="' + v + '"'));
			}
		}
	});
	function watchAll() {
		var els = document.querySelectorAll(
			'[data-cui-outlet],[data-cui-area],[data-cui-layout-slot],[data-cui-layout-key]:not([data-cui-lang])');
		for (var i = 0; i < els.length; i++) {
			if (els[i].__labWatched) continue;
			els[i].__labWatched = true;
			mo.observe(els[i], {
				childList: true,
				attributes: true,
				attributeFilter: ['aria-busy', 'data-cui-loadstate'],
			});
		}
	}

	// --- the runtime's own events -------------------------------------------
	document.addEventListener('gofastr:beforenavigate', function (e) {
		var d = e.detail || {};
		startSession(performance.now(), 'click \u2192 ' + (d.path || ''), d.path || '');
		line('nav', 'navigation starts');
	});
	window.addEventListener('popstate', function () {
		sawPop = true;
		if (session) return;
		var p = location.pathname + location.search;
		startSession(performance.now(), 'history \u2192 ' + p, p);
		line('nav', 'history move (Back or Forward)');
	});
	document.addEventListener('gofastr:transition', function (e) {
		var d = e.detail || {};
		lastTypes = d.types || [];
		if (!session) startSession(performance.now(), 'navigate \u2192 ' + (d.to || ''), d.to || '');
		line('motion', 'view transition [' + lastTypes.join(', ') + '] ' +
			(d.from || '?') + ' \u2192 ' + (d.to || '?'));
	});
	window.addEventListener('gofastr:navigate', function (e) {
		var d = e.detail || {};
		if (!session) startSession(performance.now(), 'navigate \u2192 ' + (d.path || ''), d.path || '');
		line('nav', 'navigated ' + (d.cached ? '(cache replay) ' : '') +
			(d.prevPath || '?') + ' \u2192 ' + (d.path || '?'));
		watchAll(); /* new layers bring new regions to observe */
		var isBack = lastTypes.indexOf('back') >= 0 || (sawPop && !lastTypes.length);
		if (isBack) {
			setTimeout(function () {
				line('scroll', 'after Back \u00b7 window scrollY = ' + Math.round(window.scrollY) + ' px');
			}, 350);
		}
		lastTypes = [];
		sawPop = false;
	});

	watchAll();
	api.watchAll = watchAll;
})();`

// The route-bound header family and the computed slices the lab
// demonstrates : a title binding, a param
// binding, a breadcrumb computed from route.path, and a mix computed
// over path AND params.id whose every evaluation the e2e records to
// prove atomic publication.
var (
	labStore  = store.New("lab")
	labCrumbs = store.Computed[string](labStore, "crumbs", "lab.crumbs", "route.path")
	labMix    = store.Computed[string](labStore, "mix", "lab.mix", "route.path", "route.params.id")
)

// crumbsFor is the server-side first paint of the breadcrumb; the
// lab.crumbs reducer mirrors it client-side.
func crumbsFor(path string) string {
	parts := []string{"Home"}
	segs := strings.Split(strings.Split(path, "?")[0], "/")
	for _, s := range segs {
		if s == "" {
			continue
		}
		if s[0] >= 'a' && s[0] <= 'z' {
			s = string(s[0]-'a'+'A') + s[1:]
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " / ")
}

// mixFor is the first paint of lab.mix: path|id.
func mixFor(path, id string) string { return path + "|" + id }

func buildSite() *uiapp.App {
	site := uiapp.NewApp("Layout Lab")
	site.WithTheme(theme.Default())

	// The shell: one tree layout with three outlets and one route area.
	// It is the app's DEFAULT layout, so every screen's chain is
	// ["l:shell"] and every navigation is an in-chain partial.
	//: transitions are ON by default (the runtime wraps every
	// swap in one; a click landing during the animation is skipped and
	// re-delivered). Naming the shell's primary (raw name, no
	// generated anims) keeps the browser's default crossfade look for
	// the main region while exercising the named-cell mirror.
	shellToolbar := uiapp.NewOutlet("toolbar", uiapp.OutletOptions{
		// the toolbar shows a Spinner
		// (a ready-made piece) after 150ms of wait, held at least
		// 300ms once shown; aside and rail keep the dim only, the
		// contrast case. the same cell
		// carries a spec-declared Crossfade transition.
		Loading: &uiapp.Loading{
			Show:  uiapp.LoadingComponent(ui.Spinner(ui.SpinnerConfig{Label: "Loading"})),
			After: 150 * time.Millisecond,
			Min:   300 * time.Millisecond,
		},
		Transition: uiapp.Crossfade(150 * time.Millisecond),
	})
	shellAside := uiapp.NewOutlet("aside", uiapp.OutletOptions{Default: &HelpPanel{}}) // Default when unfilled
	shellRail := uiapp.NewOutlet("rail")                                               // FallbackNothing (slow3's group fill)
	shell := uiapp.NewLayout("shell", uiapp.LayoutSpec{
		Primary: uiapp.PrimaryConfig{Transition: uiapp.Transition{Name: "shell"}},
		Outlets: []*uiapp.Outlet{shellToolbar, shellAside, shellRail},
		// Areas take loading content (2026-09-26): the crumbs area —
		// re-rendered by the server on every navigation the shell
		// survives — carries the same inert-template contract an
		// outlet does, marker("CRUMBS-LOADING") past 150ms of wait.
		Areas: []uiapp.AreaSpec{{Name: "crumbs", Loading: &uiapp.Loading{
			Show:  uiapp.LoadingComponent(marker("CRUMBS-LOADING")),
			After: 150 * time.Millisecond,
		}}},
	}, func(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
		// The frame composed from the pieces (the lab's outline skin
		// keys on the .shell class carried by the stack root): a
		// page-tall stack, the header band, the content row (toolbar
		// above main, context aside after it), the footer band.
		header := ui.Box(ui.BoxConfig{Pad: ui.BoxPadSM}, ui.Stack(ui.StackConfig{ID: "shell-header"},
			html.Nav(html.NavConfig{Label: "Main"}, ui.Cluster(ui.ClusterConfig{},
				navGroup("Basics",
					navLink("/", "Home"),
					navLink("/inbox", "Inbox"),
					navLink("/inbox/1", "Message 1"),
					navLink("/settings", "Settings"),
					navLink("/bare", "Bare"),
				),
				navGroup("Errors",
					navLink("/broken/load", "Broken load"),
					navLink("/broken/panic", "Broken panic"),
					navLink("/broken/boundary", "Broken boundary"),
					navLink("/nope", "Missing page"),
				),
				navGroup("Loading",
					navLink("/slow/300", "Slow"),
					navLink("/slow3/200", "Slow3"),
				),
				navGroup("Items",
					navLink("/items", "Items"),
					navLink("/items/1", "Item 1"),
					navLink("/items/2", "Item 2"),
					navLink("/items/3", "Item 3"),
					navLink("/items/30#lab-detail-p2", "Item 30 §2"),
				),
				navGroup("Motion",
					navLink("/wobble", "Wobble"),
				),
			)),
			//: the crumbs area also owns the computed slices'
			// server-side first paint. Derived route values must not
			// read the match from the build (gofastr's layoutfunc
			// lint: the build's markup is static chrome a partial
			// never refreshes) — the area fn receives the match as
			// its parameter and re-runs on every render the shell
			// survives, so partials refresh the seeds too. Placed
			// BEFORE the binds below so the SSR stamps seed values,
			// not the empty first paint.
			l.RouteArea("crumbs", func(ctx context.Context, m uiapp.Match) render.HTML {
				labCrumbs.Seed(ctx, crumbsFor(m.Path()))
				labMix.Seed(ctx, mixFor(m.Path(), m.Param("id")))
				return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "lab-crumbs"}},
					render.Text("crumbs:"+m.Path()))
			}),
			func() render.HTML {
				return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS},
					uiapp.Route.Title.Bind(ctx, "span", map[string]string{"id": "lab-route-title", "class": "lab-route"}),
					uiapp.Route.Param("id").Bind(ctx, "span", map[string]string{"id": "lab-route-param", "class": "lab-route"}),
					labCrumbs.Bind(ctx, "span", map[string]string{"id": "lab-crumbs-bind"}),
					labMix.Bind(ctx, "span", map[string]string{"id": "lab-mix-bind"}),
				)
			}(),
		))
		return ui.Box(ui.BoxConfig{Pad: ui.BoxPadMD}, ui.Stack(ui.StackConfig{Class: "shell", Screen: true, Gap: ui.GapNone},
			html.Header(html.HeaderConfig{Banner: true}, header),
			ui.ContentRow(ui.ContentRowConfig{
				Toolbar: ui.Box(ui.BoxConfig{Pad: ui.BoxPadSM}, l.Place(shellToolbar)),
				Aside:   l.Place(shellAside),
			}, ui.Box(ui.BoxConfig{Pad: ui.BoxPadSM}, l.Primary())),
			html.Footer(html.FooterConfig{ContentInfo: true}, ui.Box(ui.BoxConfig{Pad: ui.BoxPadSM}, l.Place(shellRail))),
		))
	})
	site.SetDefaultLayout(shell)

	// The /inbox group "uses the shell" (its screens render under the
	// default shell) and carries a GROUP fill: any /inbox/... screen
	// that does not fill the toolbar itself gets the GroupToolbar.
	inbox := uiapp.NewScreenGroup("/inbox", nil)
	inbox.Fill(shellToolbar, &GroupToolbar{})
	inbox.Screen(uiapp.NewScreen("/inbox", &InboxScreen{}).
		WithTitle("Inbox").
		Fill(shellToolbar, &InboxToolbar{}), nil)
	inbox.Screen(uiapp.NewScreen("{id}", &MessageScreen{}).
		WithTitle("Message").
		Fill(shellToolbar, &MessageToolbar{}).
		Fill(shellAside, &SenderCard{}), nil)
	site.Router.ScreenGroup(inbox)

	site.RegisterScreen(uiapp.NewScreen("/slow/{ms}", &SlowScreen{}).
		WithTitle("Slow").
		// PER-ROUTE loading for the swap
		// slot — this screen's declaration rides the route manifest
		// (the shell declares no primary template, so the manifest
		// entry owns the slot; /items' inner layout template wins for
		// its own slot).
		WithLoading(&uiapp.Loading{
			Show:  uiapp.LoadingComponent(ui.SkeletonCard(ui.SkeletonCardConfig{Label: "Loading page"})),
			After: 150 * time.Millisecond,
			Min:   250 * time.Millisecond,
		}).
		Fill(shellAside, &SlowPanel{}), nil)

	// the measurement route — a fast
	// primary, a toolbar fill that sleeps 50ms and an aside fill that
	// sleeps 800ms. Under no streaming the whole response waits for the
	// slowest fill; under streaming the toolbar's chunk arrives first.
	site.RegisterScreen(uiapp.NewScreen("/mix", &MixScreen{}).
		WithTitle("Mix").
		Fill(shellToolbar, &SleepFill{label: "TOOLBAR-MIX", ms: 50}).
		Fill(shellAside, &SleepFill{label: "ASIDE-MIX", ms: 800}), nil)
	// The same shape with a FAILING late fill, for the streaming
	// containment check (page error policy would 404 before the first
	// chunk under no streaming; under streaming the late error must
	// degrade the outlet only).
	site.RegisterScreen(uiapp.NewScreen("/mixfail", &BrokenScreen{marker: "SCREEN-MIXFAIL"}).
		WithTitle("Mixfail").
		Fill(shellToolbar, &SleepFill{label: "TOOLBAR-MIXFAIL", ms: 50}).
		Fill(shellAside, &BrokenLoad{}), nil)

	// A navigation that fails only after the loading content is up
	// (the toolbar fill sleeps past its After, then the aside fails):
	// under the page error policy the runtime must restore the parked
	// nodes. /broken/load fails before After, so nothing is parked there.
	site.RegisterScreen(uiapp.NewScreen("/slowfail", &BrokenScreen{marker: "SCREEN-SLOWFAIL"}).
		WithTitle("Slowfail").
		Fill(shellToolbar, &SleepFill{label: "TOOLBAR-SLOWFAIL", ms: 500}).
		Fill(shellAside, &BrokenLoad{}), nil)

	// variant B the list-and-detail case
	// WITHOUT hashes. The list pane is not an outlet: it is a STATIC
	// part of a NESTED tree layout the /items group owns, so the layer
	// chain keeps it across /items/:id navigation (the swap boundary is
	// the inner layer's primary). The group still fills the shell's
	// TOOLBAR with the same component for index and detail — the
	// identical-fill case, which B re-applies on every navigation.
	//
	// the detail slides in from the right
	// going forward and from the left going back, configured by ONE
	// typed value; Layout.TransitionCSS generates every rule (name
	// assignment, in/out keyframes, the back-direction variants).
	// the same slot shows a SkeletonRow
	// while it loads — the list-and-detail case, both declarations on
	// the merged PrimaryConfig.
	labItemsLayout = uiapp.NewLayout("items", uiapp.LayoutSpec{
		Primary: uiapp.PrimaryConfig{
			Loading: &uiapp.Loading{
				Show:  uiapp.LoadingComponent(ui.SkeletonRow(ui.SkeletonRowConfig{Label: "Loading item"})),
				After: 150 * time.Millisecond,
				Min:   250 * time.Millisecond,
			},
			Transition: uiapp.Slide(uiapp.Right, 220*time.Millisecond),
		},
	}, func(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
		return ui.ListDetail(ui.ListDetailConfig{
			List: listMarkup(), ListLabel: "Items", Detail: l.Primary(),
		})
	})
	items := uiapp.NewScreenGroup("/items", labItemsLayout)
	items.Fill(shellToolbar, &ItemsToolbar{})
	items.Screen(uiapp.NewScreen("/items", &ItemsIndexScreen{}).WithTitle("Items"), nil)
	// fixture: the detail screen fills the shell's aside with a
	// per-id panel, so a whole-document (static host) navigation must
	// visibly carry the destination's fills into the outlets outside
	// the swapped slot. The toolbar stays the GROUP's identical fill
	// (the skip-keeps-input case is pinned on it).
	items.Screen(uiapp.NewScreen("{id}", &ItemDetailScreen{}).WithTitle("Item").
		Fill(shellAside, &ItemAside{}), nil)
	site.Router.ScreenGroup(items)

	// The first-declaration proof. FirstScreen binds
	// route.params.tag from inside its OWN render — no earlier lab
	// route declares it, so the /first/:tag render that introduces the
	// slice is also the first render that carries a value for it. Under
	// that render stamped '' (the seeding had already run against
	// a name list that did not know "tag" yet); the raw bag write
	// makes it stamp the value.
	site.RegisterScreen(uiapp.NewScreen("/first/{tag}", &FirstScreen{}).WithTitle("First"), nil)

	//: the cross-chain route. BareScreen owns its OWN layout (not
	// the shell), so its chain is [l:bare]: reaching it from any shell
	// page is a cross-chain navigation (full-document swap). The layout
	// re-uses the shell's route bindings and the lab.mix computed so
	// the recorder keeps evaluating across the swap.
	bareLayout := uiapp.NewLayout("bare", uiapp.LayoutSpec{}, func(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
		// The computed slices seed from BareScreen's render (below): a
		// build must not read the match (gofastr's layoutfunc lint —
		// the markup is static chrome a partial never refreshes), and
		// the screen renders before the layout builds, so the binds
		// below stamp seeded values, not an empty first paint.
		return html.Div(html.DivConfig{Class: "bare"},
			html.Header(html.HeaderConfig{Banner: true, ExtraAttrs: html.Attrs{"id": "bare-header"}},
				uiapp.Route.Title.Bind(ctx, "span", map[string]string{"id": "lab-route-title", "class": "lab-route"}),
				uiapp.Route.Param("id").Bind(ctx, "span", map[string]string{"id": "lab-route-param", "class": "lab-route"}),
				labCrumbs.Bind(ctx, "span", map[string]string{"id": "lab-crumbs-bind"}),
				labMix.Bind(ctx, "span", map[string]string{"id": "lab-mix-bind"}),
			),
			l.Primary(),
		)
	})
	bareScreen := uiapp.NewScreen("/bare", &BareScreen{})
	bareScreen.Layout = bareLayout
	site.RegisterScreen(bareScreen.WithTitle("Bare"), nil)

	site.RegisterScreen(uiapp.NewScreen("/", &HomeScreen{}).WithTitle("Home"), nil)
	site.RegisterScreen(uiapp.NewScreen("/settings", &SettingsScreen{}).WithTitle("Settings"), nil)

	// The /broken/* routes carry an aside fill that fails one way each,
	// for the variant measurements (page error vs outlet
	// containment). .
	site.RegisterScreen(uiapp.NewScreen("/broken/load", &BrokenScreen{marker: "SCREEN-BROKEN-LOAD"}).
		WithTitle("Broken load").
		Fill(shellAside, &BrokenLoad{}), nil)
	site.RegisterScreen(uiapp.NewScreen("/broken/panic", &BrokenScreen{marker: "SCREEN-BROKEN-PANIC"}).
		WithTitle("Broken panic").
		Fill(shellAside, &BrokenPanic{}), nil)
	site.RegisterScreen(uiapp.NewScreen("/broken/boundary", &BrokenScreen{marker: "SCREEN-BROKEN-BOUNDARY"}).
		WithTitle("Broken boundary").
		Fill(shellAside, &BrokenBoundary{}), nil)

	// The /slow3 group carries a GROUP fill on the rail outlet; its
	// screen fills toolbar and aside itself. Under the concurrent load
	// strategy all three sleep in parallel — the benchmark route.
	slow3 := uiapp.NewScreenGroup("/slow3", nil)
	slow3.Fill(shellRail, &SlowRail{})
	slow3.Screen(uiapp.NewScreen("{ms}", &Slow3Screen{}).
		WithTitle("Slow3").
		Fill(shellToolbar, &SlowToolbar{}).
		Fill(shellAside, &SlowPanel{}), nil)
	site.Router.ScreenGroup(slow3)

	//: the height-changed-on-Back fixture. The toolbar fill renders
	// a different number of lines each SPA navigation (wobbleLines).

	// : the parallel-parts fixture. One tree layout
	// of its own ("pshell", so navigating to it from the shell pages is
	// a cross-chain full load, and every /parts* click afterwards is an
	// in-chain partial) carrying two DEFERRED outlets (aside, rail) and
	// one ordinary one (toolbar). A click to a /parts* route sends the
	// page request with X-Gofastr-Defer (the deferred loaders are
	// skipped, their loading content travels in place) plus one part
	partsToolbar := uiapp.NewOutlet("toolbar")
	partsAside := uiapp.NewOutlet("aside", uiapp.OutletOptions{
		Default:  &HelpPanel{},
		Deferred: true,
		Loading: &uiapp.Loading{
			Show:  uiapp.LoadingComponent(marker("ASIDE-LOADING")),
			After: 150 * time.Millisecond,
			Min:   250 * time.Millisecond,
		},
	})
	partsRail := uiapp.NewOutlet("rail", uiapp.OutletOptions{
		Deferred: true,
		Loading: &uiapp.Loading{
			Show:  uiapp.LoadingComponent(marker("RAIL-LOADING")),
			After: 150 * time.Millisecond,
			Min:   250 * time.Millisecond,
		},
	})
	labPartsLayout = uiapp.NewLayout("pshell", uiapp.LayoutSpec{
		// The primary names a transition (raw name, the shell's
		// spelling) so the /parts pages OPT IN to view transitions:
		// the machinery is a demand module since the opt-in pass 2,
		// and the parts e2e suite asserts transition behavior on these
		// pages (the page's commit rides one; a part never starts a
		// second).
		Primary: uiapp.PrimaryConfig{Transition: uiapp.Transition{Name: "pshell"}},
		Outlets: []*uiapp.Outlet{partsToolbar, partsAside, partsRail},
		Areas:   []uiapp.AreaSpec{{Name: "crumbs"}},
	}, func(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
		return html.Div(html.DivConfig{Class: "shell"},
			html.Header(html.HeaderConfig{Banner: true, ExtraAttrs: html.Attrs{"id": "pshell-header"}},
				html.Nav(html.NavConfig{Label: "Parts"},
					navLink("/parts", "Parts"),
					navLink("/parts2", "Parts 2"),
					navLink("/partsfail", "Parts fail"),
				),
				l.RouteArea("crumbs", func(ctx context.Context, m uiapp.Match) render.HTML {
					return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "lab-crumbs"}},
						render.Text("crumbs:"+m.Path()))
				}),
			),
			l.Place(partsToolbar),
			l.Primary(),
			l.Place(partsAside),
			l.Place(partsRail),
		)
	})
	partsScreen := uiapp.NewScreen("/parts", &PartsScreen{}).
		WithTitle("Parts").
		Fill(partsToolbar, &SleepFill{label: "TOOLBAR-PARTS", ms: 50}).
		Fill(partsAside, &SleepFill{label: "ASIDE-PARTS-SLOW", ms: 800}).
		Fill(partsRail, &SleepFill{label: "RAIL-PARTS", ms: 100})
	partsScreen.Layout = labPartsLayout
	site.RegisterScreen(partsScreen, nil)
	//gofastr:allow(GOFASTR1003) TestP10PartsE2E and TestP10PartsSupersededE2E navigate here through JavaScript expressions.
	parts2Screen := uiapp.NewScreen("/parts2", &PartsTwoScreen{}).
		WithTitle("Parts two").
		Fill(partsToolbar, &SleepFill{label: "TOOLBAR-PARTS2", ms: 50}).
		Fill(partsAside, &SleepFill{label: "ASIDE-PARTS2-SLOW", ms: 700}).
		Fill(partsRail, &SleepFill{label: "RAIL-PARTS2", ms: 100})
	parts2Screen.Layout = labPartsLayout
	site.RegisterScreen(parts2Screen, nil)
	// The contained-failure twin: the deferred aside's Load fails, so
	// the PART answers 200 with the region's fallback (ASIDE-HELP) —
	// the page's own containment, on the part transport.
	partsFailScreen := uiapp.NewScreen("/partsfail", &BrokenScreen{marker: "SCREEN-PARTSFAIL"}).
		WithTitle("Parts fail").
		Fill(partsToolbar, &SleepFill{label: "TOOLBAR-PARTSFAIL", ms: 50}).
		Fill(partsAside, &BrokenLoad{}).
		Fill(partsRail, &SleepFill{label: "RAIL-PARTSFAIL", ms: 100})
	partsFailScreen.Layout = labPartsLayout
	site.RegisterScreen(partsFailScreen, nil)

	// a different number of lines each SPA navigation (wobbleLines).
	site.RegisterScreen(uiapp.NewScreen("/wobble", &WobbleScreen{}).
		WithTitle("Wobble").
		Fill(shellToolbar, &WobbleToolbar{}), nil)

	return site
}

func navLink(href, text string) render.HTML {
	return html.Link(html.LinkConfig{
		Href:       href,
		Text:       text,
		ExtraAttrs: html.Attrs{"data-nav": href},
	})
}

// navGroup wraps related nav links with a small visible heading, so the
// sixteen demo routes read as five piles instead of one long run. Every
// link keeps its href and data-nav (browser tests click those).
func navGroup(name string, links ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXS},
		html.Strong(html.TextConfig{}, render.Text(name)),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapLG}, links...),
	)
}

// --- Screens ---------------------------------------------------------

// HomeScreen is the scenarios page: one card per proven behaviour, each
// saying what it shows, where to click, and what to watch for. The
// SCREEN-HOME marker stays first so tests can read the slot's text.
type HomeScreen struct{}

func (s *HomeScreen) Render() render.HTML {
	return ui.Stack(ui.StackConfig{ID: "lab-home"},
		ui.Cluster(ui.ClusterConfig{}, marker("SCREEN-HOME")),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Scenarios")),
		html.Paragraph(html.TextConfig{}, render.Text(
			"Each card names one behaviour of the layout runtime. Open the Timeline panel (bottom right) and click a card's link; the panel lists what happened, when, and which region changed.")),
		ui.Grid(ui.GridConfig{}, scenarioCards()...),
	)
}

// scenarioLink is a card's clickable link. Plain anchors — the runtime
// hijacks same-route clicks itself; no data-nav (tests click the
// header's copies).
func scenarioLink(href, text string) render.HTML {
	return html.Link(html.LinkConfig{Href: href, Text: text})
}

// scenarioCards returns one card per proven prototype (see
// docs/PLAN-2026-09-25-layout-prototypes.md, Results).
func scenarioCards() []render.HTML {
	cards := []struct {
		id, title, what string
		links           []render.HTML
		watch           string
	}{
		{
			id: "P1", title: "One URL, many regions",
			what:  "A route fills the toolbar and the aside beside the main slot, in one response.",
			links: []render.HTML{scenarioLink("/inbox", "Inbox"), scenarioLink("/inbox/1", "Message 1")},
			watch: "The toolbar and aside swap their content; the header never re-renders (type in nothing, watch the boxes' tags) and the page does not reload.",
		},
		{
			id: "P2", title: "A failing fill stays in its outlet",
			what:  "The aside fill fails (error, panic, panic-with-fallback) and only the aside degrades.",
			links: []render.HTML{scenarioLink("/broken/load", "Broken load"), scenarioLink("/broken/panic", "Broken panic"), scenarioLink("/broken/boundary", "Broken boundary")},
			watch: "The aside falls back (help panel or the fill's own error card) and says which fallback it shows; the main slot keeps the screen's marker and the status stays 200. The static copy leaves these routes out — the export refuses a page whose fill failed — so their links land on the exported 404 page instead.",
		},
		{
			id: "P3", title: "Fills load concurrently",
			what:  "Three fills that each sleep 200 ms resolve in parallel, not in sequence.",
			links: []render.HTML{scenarioLink("/slow3/200", "Slow3")},
			watch: "All three regions (toolbar, aside, rail) land around +200 ms in the Timeline, not +600 ms.",
		},
		{
			id: "P4", title: "Changing regions say they are busy",
			what:  "While a slow fetch is in flight, every region it will change is marked and dims after a delay.",
			links: []render.HTML{scenarioLink("/slow/900", "Slow 900 ms")},
			watch: "The toolbar, aside, crumbs and main slot grow a “waiting” tag and dim; a fast page (Settings) never dims.",
		},
		{
			id: "P7", title: "Route values reach the header",
			what:  "Title, :id param, breadcrumbs and a computed mix are bound to signals seeded from the route.",
			links: []render.HTML{scenarioLink("/inbox/2", "Message 2"), scenarioLink("/settings", "Settings")},
			watch: "The four header values change on every navigation without the header re-rendering; the mix (path|id) never shows a half-updated pair.",
		},
		{
			id: "P8", title: "The list layer is kept",
			what:  "The 200-row list lives in the /items group's own layer, so detail routes swap only the detail.",
			links: []render.HTML{scenarioLink("/items", "Items"), scenarioLink("/items/1", "Item 1")},
			watch: "Type in the filter, bump the counter, scroll the list, then click rows: the list keeps its DOM, its text and its scroll; the detail slot flashes “+N ms” instead.",
		},
		{
			id: "P9", title: "Loading content per region",
			what:  "A slow fill shows its declared loading piece after a delay, holds a minimum, then swaps in.",
			links: []render.HTML{scenarioLink("/slow/700", "Slow 700 ms")},
			watch: "The toolbar shows a spinner (~150 ms in, held ≥300 ms); the old content is parked and restored if the flight fails (see /slowfail's toolbar input).",
		},
		{
			id: "P10", title: "The fast fill paints first",
			what:  "The envelope streams: a 50 ms toolbar fill and an 800 ms aside fill arrive as they finish.",
			links: []render.HTML{scenarioLink("/mix", "Mix"), scenarioLink("/mixfail", "Mix, late failure")},
			watch: "The toolbar lands near +70 ms while the aside keeps its loading content until ~+800 ms; on /mixfail the late error is contained to the aside.",
		},
		{
			id: "P11", title: "Swaps animate as view transitions",
			what:  "Each commit rides the View Transitions API with a direction type; item details slide.",
			links: []render.HTML{scenarioLink("/settings", "Settings"), scenarioLink("/inbox", "Inbox"), scenarioLink("/items/1", "Item 1")},
			watch: "Settings → Inbox crossfades the main slot; Item 1 → Item 2 slides right; Back plays the mirrored direction. The Timeline logs each transition and its types.",
		},
		{
			id: "P12", title: "Back restores your place",
			what:  "Leaving a page records what was at the top of the viewport; Back puts it back.",
			links: []render.HTML{scenarioLink("/items/30#lab-detail-p2", "Item 30 §2"), scenarioLink("/wobble", "Wobble")},
			watch: "Scroll deep into Item 30, go elsewhere, come Back: the same paragraph sits at the same offset — even on /wobble, whose toolbar height changes between visits.",
		},
	}
	out := make([]render.HTML, 0, len(cards))
	for _, c := range cards {
		out = append(out, ui.Card(ui.CardConfig{
			Heading: c.id + " · " + c.title, Description: c.what,
			ExtraAttrs: html.Attrs{"data-lab-scenario": c.id},
		},
			ui.Cluster(ui.ClusterConfig{}, c.links...),
			html.Paragraph(html.TextConfig{},
				render.Join(html.Strong(html.TextConfig{}, render.Text("Watch for: ")), render.Text(c.watch))),
		))
	}
	return out
}

type SettingsScreen struct{}

func (s *SettingsScreen) Render() render.HTML { return marker("SCREEN-SETTINGS") }

// InboxScreen fills the toolbar itself; the group fill never shows for
// this screen.
type InboxScreen struct{}

func (s *InboxScreen) Render() render.HTML { return marker("SCREEN-INBOX") }

// MessageScreen fills both outlets. SenderCard declines (ErrNoFill) on
// the magic missing id, so /inbox/404 falls back to the HelpPanel.
type MessageScreen struct {
	id string
}

func (s *MessageScreen) SetParams(m map[string]string) { s.id = m["id"] }

// ScreenTitle makes the title dynamic (post-Load): the route.title
// binding and the snapshot island carry "Message <id>", which is what
// the e2e asserts updates on navigation.
func (s *MessageScreen) ScreenTitle() string { return "Message " + s.id }

func (s *MessageScreen) Render() render.HTML { return marker("SCREEN-MESSAGE-" + s.id) }

// StaticPaths exports the message pages the e2e suite navigates
// (): message 1, and 404 for the declined-fill (HelpPanel) case.
func (s *MessageScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"id": "1"}, {"id": "404"}}
}

// SlowScreen fills the aside with a component whose Load sleeps, for
// the later loading prototypes. Capped at 3s so nothing wedges.
type SlowScreen struct {
	ms string
}

func (s *SlowScreen) SetParams(m map[string]string) { s.ms = m["ms"] }

func (s *SlowScreen) Render() render.HTML { return marker("SCREEN-SLOW-" + s.ms) }

// ---: list-and-detail ----------------------------------------------

// ItemsIndexScreen is the /items index route's primary.
type ItemsIndexScreen struct{}

func (s *ItemsIndexScreen) Render() render.HTML { return marker("SCREEN-ITEMS") }

// ItemDetailScreen is the /items/:id detail route's primary: the
// marker plus one numbered, id-anchored paragraph per id unit (capped),
// so the page is TALL and every paragraph is a stable anchor for the
// scroll-restore measurements.
type ItemDetailScreen struct {
	id string
}

func (s *ItemDetailScreen) SetParams(m map[string]string) { s.id = m["id"] }

// StaticPaths exports the four detail pages the e2e suite navigates
// (): 1, 2, 3 (the clicks) and 30 (the §2 anchor case).
func (s *ItemDetailScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, 4)
	for _, id := range []string{"1", "2", "3", "30"} {
		out = append(out, map[string]string{"id": id})
	}
	return out
}

func (s *ItemDetailScreen) Render() render.HTML {
	n, err := strconv.Atoi(s.id)
	if err != nil || n < 1 {
		n = 3
	}
	if n > 40 {
		n = 40
	}
	ps := make([]render.HTML, 0, n)
	for i := 1; i <= n; i++ {
		ps = append(ps, html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{
			"id":       "lab-detail-p" + strconv.Itoa(i),
			"data-key": "detail-" + strconv.Itoa(i),
		}}, render.Text("Detail "+s.id+" paragraph "+strconv.Itoa(i))))
	}
	return html.Div(html.DivConfig{},
		marker("SCREEN-DETAIL-"+s.id),
		render.Join(ps...),
	)
}

// StaticPaths exports the slow page the nav links use ().
func (s *SlowScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"ms": "300"}, {"ms": "700"}, {"ms": "900"}}
}

// listMarkup is the list pane the nested /items layout owns: 200 rows,
// a filter input, and a signal-backed counter (deliberately UNDECLARED,
// so no seed ever rewrites its value — the DOM node's survival is the
// only thing that keeps the display).
func listMarkup() render.HTML {
	rows := make([]render.HTML, 0, 200)
	for i := 1; i <= 200; i++ {
		rows = append(rows, html.Link(html.LinkConfig{
			Href: "/items/" + strconv.Itoa(i),
			Text: "Item " + strconv.Itoa(i),
			ExtraAttrs: html.Attrs{
				"data-row": strconv.Itoa(i),
				//: the stable identity scroll anchors use.
				"data-key": "row-" + strconv.Itoa(i),
			},
		}))
	}
	return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "lab-list"}},
		html.Input(html.InputConfig{

			Type: "text", Name: "filter", ID: "lab-filter", Placeholder: "filter",
			ExtraAttrs: html.Attrs{"aria-label": "Filter items"},
		}),
		html.Button(html.ButtonConfig{
			Label:      "+1",
			ExtraAttrs: html.Attrs{"id": "lab-count-btn", "data-cui-signal-inc": "lab.count:1"},
		}),
		html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"id": "lab-count", "data-cui-signal": "lab.count"}}, render.Text("0")),
		ui.Stack(ui.StackConfig{Gap: ui.GapXS}, rows...),
	)
}

// ---: scroll-restore fixtures --------------

// WobbleScreen is the /wobble route: a tall primary of anchored
// paragraphs whose TOOLBAR fill (above it, in the shell) re-renders
// with a DIFFERENT number of lines each SPA navigation, so a Back into
// a refetched page shifts every offset below the toolbar — the
// height-changed case measures.
type WobbleScreen struct{}

func (s *WobbleScreen) Render() render.HTML {
	n := 20
	if o := wobbleParas.Load(); o > 0 {
		n = int(o)
	}
	ps := make([]render.HTML, 0, n)
	for i := 1; i <= n; i++ {
		ps = append(ps, html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{
			"id":       "lab-wobble-p" + strconv.Itoa(i),
			"data-key": "wobble-" + strconv.Itoa(i),
		}}, render.Text("Wobble paragraph "+strconv.Itoa(i))))
	}
	return html.Div(html.DivConfig{},
		marker("SCREEN-WOBBLE"),
		render.Join(ps...),
	)
}

// wobbleParas pins the /wobble paragraph count for tests (0 = the
// default 20); lets a test remove the scroll anchor's target on the
// refetched visit (the pixel-fallback case).
var wobbleParas atomic.Int64

// wobbleOverride pins wobbleLines for tests (-1 = derive from the SPA
// navigation counter, the demo's real behavior); the process-global
// counter is shared across every test in the binary, so a test that
// needs a specific height delta sets this instead.
var wobbleOverride atomic.Int64

// wobbleLines is 2..5, driven by the SPA navigation counter: two
// visits to /wobble with any navigation between them render different
// toolbar heights.
func wobbleLines() int {
	if o := wobbleOverride.Load(); o >= 0 {
		return int(2 + o)
	}
	return int(2 + labStats.navigations.Load()%4)
}

// WobbleToolbar is /wobble's toolbar fill: wobbleLines() stacked lines
// above the primary.
type WobbleToolbar struct{}

func (t *WobbleToolbar) Render() render.HTML {
	lines := make([]render.HTML, 0, 5)
	for i := 1; i <= wobbleLines(); i++ {
		lines = append(lines, ui.Box(ui.BoxConfig{Pad: ui.BoxPadSM, Surface: true}, render.Text("WOB-LINE-"+strconv.Itoa(i))))
	}
	return ui.Stack(ui.StackConfig{ID: "lab-wobble-toolbar", Gap: ui.GapXS}, lines...)
}

// ItemsToolbar is the /items group's TOOLBAR fill, identical for the
// index and every detail route: the identical-fill case where a skip
// keeps user input (a filter phrase) that a re-apply would lose.
type ItemsToolbar struct{}

func (t *ItemsToolbar) Render() render.HTML {
	return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "lab-items-toolbar"}},
		render.Text("TOOLBAR-ITEMS"),
		html.Input(html.InputConfig{
			Type: "text", Name: "note", ID: "lab-tool-input", Placeholder: "toolbar note",
			ExtraAttrs: html.Attrs{"aria-label": "Toolbar note"},
		}),
	)
}

// FirstScreen is the /first/:tag route: its render declares and binds
// route.params.tag in the same breath (the first-declaration proof).
type FirstScreen struct {
	tag string
}

func (s *FirstScreen) SetParams(m map[string]string) { s.tag = m["tag"] }

func (s *FirstScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Join(
		marker("SCREEN-FIRST-"+s.tag),
		uiapp.Route.Param("tag").Bind(ctx, "span", map[string]string{"id": "lab-first-tag", "class": "lab-route"}),
	)
}

func (s *FirstScreen) Render() render.HTML { return marker("SCREEN-FIRST-" + s.tag) }

// StaticPaths exports one tagged page ().
func (s *FirstScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"tag": "hello"}}
}

// BareScreen is the /bare route's primary (its layout is its own). Its
// render also seeds the computed slices the bare layout's header binds
// stamp (): the screen renders before the layout builds, and a
// build must not read the match (gofastr's layoutfunc lint).
type BareScreen struct{}

// ItemAside is the /items/:id detail screen's own aside fill (P13-A
// fixture): a per-id panel, so a whole-document navigation on a static
// host visibly carries the destination's fill into the outlet outside
// the swapped slot (ASIDE-ITEM-<id> vs the default ASIDE-HELP).
type ItemAside struct {
	id string
}

func (a *ItemAside) SetParams(m map[string]string) { a.id = m["id"] }

func (a *ItemAside) Render() render.HTML { return marker("ASIDE-ITEM-" + a.id) }

func (s *BareScreen) RenderCtx(ctx context.Context) render.HTML {
	// Seed the bare header's computed slices (see the type comment):
	// the screen renders before the layout builds, so the binds stamp
	// these values at first paint.
	m, _ := uiapp.MatchFromContext(ctx)
	labCrumbs.Seed(ctx, crumbsFor(m.Path()))
	labMix.Seed(ctx, mixFor(m.Path(), m.Param("id")))
	return s.Render()
}

func (s *BareScreen) Render() render.HTML { return marker("SCREEN-BARE") }

// --- Fill components -------------------------------------------------

type HelpPanel struct{}

func (h *HelpPanel) Render() render.HTML {
	//: the default carries a labelled fallback badge so a page that
	// never filled the aside (or whose fill failed and was contained)
	// reads as "this is the default", not as content.
	return render.Join(marker("ASIDE-HELP"),
		labBadge("default content: this page did not fill the aside, or its fill failed"))
}

type GroupToolbar struct{}

func (g *GroupToolbar) Render() render.HTML { return marker("TOOLBAR-GROUP") }

// InboxToolbar is /inbox's own toolbar fill.
type InboxToolbar struct{}

func (t *InboxToolbar) Render() render.HTML { return marker("TOOLBAR-INBOX") }

// MessageToolbar is /inbox/:id's own toolbar fill.
type MessageToolbar struct{}

func (t *MessageToolbar) Render() render.HTML { return marker("TOOLBAR-MESSAGE") }

// SenderCard is the /inbox/:id aside fill; its Load reads the id and
// declines the fill for the missing message.
type SenderCard struct {
	id string
}

func (c *SenderCard) SetParams(m map[string]string) { c.id = m["id"] }

func (c *SenderCard) Load(ctx context.Context) error {
	if c.id == "404" {
		return uiapp.ErrNoFill
	}
	return nil
}

func (c *SenderCard) Render() render.HTML { return marker("ASIDE-SENDER-" + c.id) }

// SlowPanel is the /slow/:ms aside fill; its Load sleeps ms millis.
type SlowPanel struct {
	ms string
}

func (p *SlowPanel) SetParams(m map[string]string) { p.ms = m["ms"] }

func (p *SlowPanel) Load(ctx context.Context) error {
	d, err := strconv.Atoi(p.ms)
	if err != nil {
		return nil
	}
	if d > 3000 {
		d = 3000
	}
	if d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	return nil
}

func (p *SlowPanel) Render() render.HTML { return marker("ASIDE-SLOW-" + p.ms) }

// --- broken fills ---------------------------

// BrokenScreen is the shared screen under the three /broken/* routes;
// its marker names the case.
type BrokenScreen struct {
	marker string
}

func (s *BrokenScreen) Render() render.HTML { return marker(s.marker) }

// BrokenLoad's Load returns an error carrying markup, a C1 CSI byte,
// and a bidi override: the security check asserts none of it reaches
// the HTML raw under either variant.
type BrokenLoad struct{}

func (b *BrokenLoad) Load(ctx context.Context) error {
	return errors.New("boom <img src=x onerror=alert(1)>\u009b[31m\u202eevil")
}

func (b *BrokenLoad) Render() render.HTML { return marker("ASIDE-BOOM-LOAD") }

// BrokenPanic's Render panics with the same hostile text.
type BrokenPanic struct{}

func (b *BrokenPanic) Render() render.HTML {
	panic("aside fill panicked: <img src=x onerror=alert(1)>\u009b[31m\u202eevil")
}

// BrokenBoundary panics on Render and carries its own error fallback.
type BrokenBoundary struct{}

func (b *BrokenBoundary) Render() render.HTML { panic("boundary fill panicked") }

func (b *BrokenBoundary) RenderError(err error) render.HTML {
	//: the fill's own fallback is labelled, same as the default.
	return render.Join(marker("ASIDE-BOUNDARY-ERROR"), labBadge("error boundary fallback"))
}

// PartsScreen is /parts' fast primary: the deferred fixtures' fills do
// the waiting.
type PartsScreen struct{}

func (s *PartsScreen) Render() render.HTML { return marker("SCREEN-PARTS") }

// PartsTwoScreen is the second /parts* page, so a parts navigation can
// be superseded by another parts navigation inside one layout.
type PartsTwoScreen struct{}

func (s *PartsTwoScreen) Render() render.HTML { return marker("SCREEN-PARTS2") }

// MixScreen is /mix's fast primary .
type MixScreen struct{}

func (s *MixScreen) Render() render.HTML { return marker("SCREEN-MIX") }

// SleepFill sleeps a fixed duration in Load then renders its marker
// (): the /mix routes' fast and slow fills.
type SleepFill struct {
	label string
	ms    int
}

func (f *SleepFill) Load(ctx context.Context) error {
	time.Sleep(time.Duration(f.ms) * time.Millisecond)
	return nil
}

func (f *SleepFill) Render() render.HTML { return marker(f.label) }

// --- slow3 fills ----------------------------

// Slow3Screen fills the toolbar and aside itself; the /slow3 group
// fills the rail, so the route resolves three sleeping fills at once.
type Slow3Screen struct {
	ms string
}

func (s *Slow3Screen) SetParams(m map[string]string) { s.ms = m["ms"] }

func (s *Slow3Screen) Render() render.HTML { return marker("SCREEN-SLOW3-" + s.ms) }

// StaticPaths exports the slow3 page the nav links use ().
func (s *Slow3Screen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"ms": "200"}}
}

// SlowToolbar is the slow3 toolbar fill; it sleeps ms millis.
type SlowToolbar struct {
	ms string
}

func (t *SlowToolbar) SetParams(m map[string]string) { t.ms = m["ms"] }

func (t *SlowToolbar) Load(ctx context.Context) error { return sleepMillis(t.ms) }

func (t *SlowToolbar) Render() render.HTML { return marker("TOOLBAR-SLOW3-" + t.ms) }

// SlowRail is the /slow3 GROUP fill on the rail outlet.
type SlowRail struct {
	ms string
}

func (r *SlowRail) SetParams(m map[string]string) { r.ms = m["ms"] }

func (r *SlowRail) Load(ctx context.Context) error { return sleepMillis(r.ms) }

func (r *SlowRail) Render() render.HTML { return marker("RAIL-SLOW3-" + r.ms) }

// sleepMillis sleeps ms milliseconds, capped at 3s so nothing wedges.
func sleepMillis(ms string) error {
	d, err := strconv.Atoi(ms)
	if err != nil {
		return nil
	}
	if d > 3000 {
		d = 3000
	}
	if d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	return nil
}

// marker renders one visibly distinct labelled box, the unit a browser
// test asserts on.
func marker(label string) render.HTML {
	return html.Div(html.DivConfig{
		ID:         "lab-" + label,
		ExtraAttrs: html.Attrs{"data-lab-marker": label},
	}, render.Text(label))
}

// labBadge renders a fallback badge: an empty element whose text
// the CSS draws from the attribute — the labelled-region trick the
// region tags use — so a region's textContent (the exact-text e2e
// surface) gains no text while the badge still reads on screen.
func labBadge(text string) render.HTML {
	return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-lab-badge": text}})
}
