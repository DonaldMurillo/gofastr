package main

// PROTOTYPE (spike/layout-static): P13 browser e2e — do layout outlets
// survive a static export? A static host serves whole documents and
// ignores the runtime's request headers, so the non-envelope fetch path
// must READ a full document as an envelope (swap at the deepest shared
// layer, every outlet outside it as a fill), the boot capture must be
// keyed at the layer a later Back swaps at, and the exporter must
// refuse to bake a contained fill failure into a page.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	framework "github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// labHost extracts the lab's mounted UIHost (the same lookup
// ExportStatic does) so tests and the --export flag can drive the
// static builder directly.
func labHost(t *testing.T, fwApp *framework.App) *uihost.UIHost {
	t.Helper()
	for _, m := range fwApp.Mountables() {
		if host, ok := m.(*uihost.UIHost); ok {
			return host
		}
	}
	t.Fatal("layoutlab: no uihost.UIHost mounted")
	return nil
}

// labExportStatic exports the lab under the current env into a fresh
// temp dir using the lab's own export set (labExportExcludes, plus any
// per-test extras) and serves it with a plain http.FileServer — no
// GoFastr server, exactly what a static host does: whole documents,
// every request header ignored.
func labExportStatic(t *testing.T, exclude ...string) *httptest.Server {
	t.Helper()
	fwApp := buildApp()
	if err := fwApp.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	dir := t.TempDir()
	ex := append([]string{}, labExportExcludes...)
	ex = append(ex, exclude...)
	if _, err := (&static.Builder{
		Host:          labHost(t, fwApp),
		OutDir:        dir,
		ExcludeRoutes: ex,
	}).Build(context.Background()); err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := writeLabScripts(dir); err != nil {
		t.Fatalf("export scripts: %v", err)
	}
	// P14 (spike/layout-static): a miss serves the exported 404.html
	// with status 404 — the shape GitHub Pages, Netlify, Cloudflare
	// Pages and S3 website hosting all give. The file server runs
	// against a recorder so its own resolution (the trailing-slash 301s,
	// index.html for directories) is untouched and only its 404 answers
	// are replaced.
	files := http.FileServer(http.Dir(dir))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		files.ServeHTTP(rec, r)
		if rec.Code == http.StatusNotFound {
			if body, err := os.ReadFile(filepath.Join(dir, "404.html")); err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write(body)
				return
			}
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// labJSON evaluates expr (a JSON.stringify expression) and unmarshals
// it into out, the labListState pattern.
func labJSON(t *testing.T, ctx context.Context, expr string, out any) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &raw)); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("json %q: %v", raw, err)
	}
}

// TestP13LiveBackKeepsGroupLayer is bug 2: the boot capture was keyed
// at layer 0 (mainSlotKey), so Back to the FIRST page loaded replayed
// it at the shell slot and replaced the kept /items group layer — the
// typed filter died with it. The entry must instead be keyed at the
// layer a later Back actually swaps at.
func TestP13LiveBackKeepsGroupLayer(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var filter, inputs string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items/1"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.SetValue(`#lab-filter`, "typed on first page", chromedp.ByQuery),
		chromedp.Evaluate(`window.__gofastr.navigate('/items/2')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Value(`#lab-filter`, &filter, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('#lab-filter').length + ''`, &inputs),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if filter != "typed on first page" {
		t.Errorf("filter after Back = %q, want \"typed on first page\" (the boot entry replayed at the shell slot and replaced the group layer)", filter)
	}
	if inputs != "1" {
		t.Errorf("%s filter inputs after Back, want 1", inputs)
	}
}

// TestP13StaticNavKeepsLayerAndFills is the static-host repro: on a
// plain file server every navigation answer is a whole document, so
// the runtime must read it as an envelope — the swap at the deepest
// shared layer keeps the /items list layer (the typed filter), and the
// outlets OUTSIDE the swapped slot (toolbar, aside, crumbs) show the
// destination's fills. Back replays the captured entry the same way.
func TestP13StaticNavKeepsLayerAndFills(t *testing.T) {
	srv := labExportStatic(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var filter string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/items/1"),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.SetValue(`#lab-filter`, "static typed", chromedp.ByQuery),
		// Programmatic click: a coordinate click scrolls the pane first.
		chromedp.Evaluate(`document.querySelector('a[data-row="3"]').click(); true`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-3`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("nav to /items/3: %v", err)
	}
	if got := labRead(t, tctx, toolbarSel); got != "TOOLBAR-ITEMS" {
		t.Errorf("toolbar after /items/3 = %q, want TOOLBAR-ITEMS (the fetched document's fill)", got)
	}
	if got := labRead(t, tctx, asideSel); got != "ASIDE-ITEM-3" {
		t.Errorf("aside after /items/3 = %q, want ASIDE-ITEM-3 (outlets outside the swap must take the destination's fills)", got)
	}
	if got := labRead(t, tctx, crumbsSel); got != "crumbs:/items/3" {
		t.Errorf("crumbs after /items/3 = %q, want crumbs:/items/3", got)
	}
	// The route-bound chip recomputes client-side from route.path; its
	// reducer script is a lab file the export must carry.
	if got := labRead(t, tctx, "#lab-crumbs-bind"); !strings.HasSuffix(strings.TrimRight(got, "/ "), "3") {
		t.Errorf("crumbs chip after /items/3 = %q, want it to end in 3 (the reducer must run on a static host)", got)
	}
	if err := chromedp.Run(tctx, chromedp.Value(`#lab-filter`, &filter, chromedp.ByQuery)); err != nil {
		t.Fatalf("read filter: %v", err)
	}
	if filter != "static typed" {
		t.Errorf("filter after /items/3 = %q, want \"static typed\" (the group layer is kept)", filter)
	}

	// Back: the captured entry replays at the same layer — item 1's
	// fills return, the list layer (and the filter) never moves.
	if err := chromedp.Run(tctx,
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-1`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Value(`#lab-filter`, &filter, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("back: %v", err)
	}
	if got := labRead(t, tctx, asideSel); got != "ASIDE-ITEM-1" {
		t.Errorf("aside after Back = %q, want ASIDE-ITEM-1 (item 1's fill must return)", got)
	}
	if got := labRead(t, tctx, crumbsSel); got != "crumbs:/items/1" {
		t.Errorf("crumbs after Back = %q, want crumbs:/items/1", got)
	}
	if filter != "static typed" {
		t.Errorf("filter after Back = %q, want \"static typed\"", filter)
	}
}

// TestP13StaticCrossGroupSwapsAtSharedLayer: two routes in DIFFERENT
// groups share only the shell, so the swap boundary is the shell's
// slot — but the outlets outside it (toolbar, aside, crumbs) still
// belong to the destination and must show ITS fills, not the origin's.
func TestP13StaticCrossGroupSwapsAtSharedLayer(t *testing.T) {
	srv := labExportStatic(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox/1"),
		chromedp.WaitVisible(`#lab-SCREEN-MESSAGE-1`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.Evaluate(`window.__gofastr.navigate('/items/2')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-DETAIL-2`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("nav /inbox/1 → /items/2: %v", err)
	}
	if got := labRead(t, tctx, toolbarSel); got != "TOOLBAR-ITEMS" {
		t.Errorf("toolbar after cross-group nav = %q, want TOOLBAR-ITEMS (was TOOLBAR-MESSAGE)", got)
	}
	if got := labRead(t, tctx, asideSel); got != "ASIDE-ITEM-2" {
		t.Errorf("aside after cross-group nav = %q, want ASIDE-ITEM-2 (was ASIDE-SENDER-1)", got)
	}
	if got := labRead(t, tctx, crumbsSel); got != "crumbs:/items/2" {
		t.Errorf("crumbs after cross-group nav = %q, want crumbs:/items/2", got)
	}
}

// TestP13ExportRefusesContainedFill: a fill that fails at build time is
// contained to its outlet on a live server, but the export must refuse
// to bake the degraded outlet into the page — the error names the route
// and the outlet address. The route set excludes every failing route
// except /broken/load — route iteration order is the router map's, so
// the refusal is pinned to the one route that can fail.
func TestP13ExportRefusesContainedFill(t *testing.T) {
	fwApp := buildApp()
	if err := fwApp.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	_, err := (&static.Builder{
		Host:          labHost(t, fwApp),
		OutDir:        t.TempDir(),
		ExcludeRoutes: []string{"/mixfail", "/slowfail", "/partsfail", "/broken/panic", "/broken/boundary"},
	}).Build(context.Background())
	if err == nil {
		t.Fatal("export of a route set with /broken/load succeeded; a contained fill failure must refuse the export")
	}
	t.Logf("export error: %v", err)
	if !strings.Contains(err.Error(), "/broken/load") {
		t.Errorf("error does not name the route: %v", err)
	}
	if !strings.Contains(err.Error(), "l:shell#aside") {
		t.Errorf("error does not name the aside outlet address: %v", err)
	}
}

// TestP13LoadingShowsOutsideTransition: the loading content is NOT the
// swap — while it shows there is no active view transition, and the
// transition that wraps the swap starts only when the response lands.
func TestP13LoadingShowsOutsideTransition(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/settings"),
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.Evaluate(`(() => {
			window.__p13 = { events: 0, duringSwap: null };
			document.addEventListener('gofastr:transition', () => {
				window.__p13.events++;
				queueMicrotask(() => {
					window.__p13.duringSwap = !!document.activeViewTransition;
				});
			});
			return true;
		})()`, nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/slow/900')`, nil),
		// The slot's loading content (After=150ms) is up while the
		// 900ms aside fill still holds the response.
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("nav to /slow/900: %v", err)
	}
	var flight struct {
		Loading bool `json:"loading"`
		VT      bool `json:"vt"`
	}
	labJSON(t, tctx, `JSON.stringify({
		loading: !!document.querySelector('[data-fui-loadstate="shown"]'),
		vt: !!document.activeViewTransition,
	})`, &flight)
	if !flight.Loading {
		t.Error("loading content is not shown while the slow fill holds the response")
	}
	if flight.VT {
		t.Error("document.activeViewTransition is set while the loading content shows — the transition must wrap only the swap")
	}
	var settle struct {
		Events    int  `json:"events"`
		DuringSel bool `json:"duringSwap"`
	}
	if err := chromedp.Run(tctx,
		chromedp.WaitVisible(`#lab-SCREEN-SLOW-900`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatalf("settle: %v", err)
	}
	labJSON(t, tctx, `JSON.stringify(window.__p13)`, &settle)
	if settle.Events < 1 || !settle.DuringSel {
		t.Errorf("transition events = %d, activeViewTransition during swap = %v; want >=1 and true", settle.Events, settle.DuringSel)
	}
}

// TestP13PartLandingSkipsNoPageTransition (streaming's replacement,
// spike/layout-parts): the page's commit runs through _commitSwap (one
// gofastr:transition event) and every part that lands afterwards
// applies as a plain content update — a part never starts a view
// transition of its own.
func TestP13PartLandingSkipsNoPageTransition(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	var out string
	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/parts"),
		chromedp.WaitVisible(`#lab-SCREEN-PARTS`, chromedp.ByQuery),
		page.BringToFront(),
		chromedp.Evaluate(`(() => {
			window.__p13ev = 0;
			document.addEventListener('gofastr:transition', () => { window.__p13ev++; });
			return true;
		})()`, nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/parts2')`, nil),
		chromedp.WaitVisible(`#lab-ASIDE-PARTS2-SLOW`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__p13ev + ''`, &out),
	); err != nil {
		t.Fatalf("nav to /parts2: %v", err)
	}
	if out != "1" {
		t.Errorf("gofastr:transition events for a parts /parts2 nav = %s, want exactly 1 (the page's commit; a part never starts one)", out)
	}
}

// TestP13SupersededNavDoesNotPoisonCache: the leave-time re-capture
// (P13-B) must only key the page the DOM actually shows. A navigation
// superseded mid-flight (its fetch never landed) leaves the DOM on the
// ORIGIN, and the newer navigation's prevPath names the dropped
// destination — capturing that key over the origin's DOM would poison
// the entry, so Back to it would replay the ORIGIN's bytes under the
// destination's URL. The _liveDomPath guard drops the mismatch.
func TestP13SupersededNavDoesNotPoisonCache(t *testing.T) {
	srv := labServe(t)
	browser := labBrowserCtx(t)
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(tcancel)

	if err := chromedp.Run(tctx,
		chromedp.Navigate(srv.URL+"/inbox"),
		chromedp.WaitVisible(`#lab-SCREEN-INBOX`, chromedp.ByQuery),
		page.BringToFront(),
		// /mix's 800ms aside fill holds the first nav's response; the
		// second nav supersedes it before anything swaps.
		chromedp.Evaluate(`window.__gofastr.navigate('/mix')`, nil),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`window.__gofastr.navigate('/settings')`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-SETTINGS`, chromedp.ByQuery),
		// Back to the dropped destination: no poisoned entry may
		// replay — the page must be /mix's own content.
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#lab-SCREEN-MIX`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := labRead(t, tctx, `main[data-fui-layout-slot="l:shell"]`); !strings.Contains(got, "SCREEN-MIX") || strings.Contains(got, "SCREEN-INBOX") {
		t.Errorf("main slot after Back to /mix = %q; want SCREEN-MIX and NOT the origin's SCREEN-INBOX (the superseded nav poisoned the cache entry)", got)
	}
}
