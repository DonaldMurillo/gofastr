package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// The site's browser gates: the marketing pages ship the core runtime
// only, the announcement lives on the changelog alone, the help
// center's TOC rail collapses when an article has no headings, and a
// direct article load works on a phone without JavaScript.

// acmeServe builds the app in-process and serves it.
func acmeServe(t *testing.T) string {
	t.Helper()
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

// acmeBrowser opens a browser with the viewport pinned to width x height.
func acmeBrowser(t *testing.T, width, height int) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(width, height),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	root, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	if err := chromedp.Run(root); err != nil {
		t.Fatalf("browser failed to start: %v", err)
	}
	if err := chromedp.Run(root, chromedp.EmulateViewport(int64(width), int64(height))); err != nil {
		t.Fatalf("viewport: %v", err)
	}
	return root
}

func TestChangelogReadingMeasure(t *testing.T) {
	base := acmeServe(t)
	ctx := acmeBrowser(t, 1280, 800)
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/changelog"),
		chromedp.WaitReady("main li", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	var result string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const sections = [...document.querySelectorAll('main section')];
		const items = sections.flatMap(s => [...s.querySelectorAll('li')]);
		if (!items.length) return 'no release items';
		for (const item of items) {
			const width = item.getBoundingClientRect().width;
			if (width > 700) return 'release item width ' + width + ' exceeds 700px';
		}
		for (let i = 1; i < sections.length; i++) {
			const gap = sections[i].getBoundingClientRect().top - sections[i-1].getBoundingClientRect().bottom;
			if (Math.abs(gap - 32) > 1) return 'release gap ' + gap + ' differs from 32px';
		}
		return '';
	})()`, &result)); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatal(result)
	}
}

// evalString evaluates expr and returns the string it produced.
func evalString(t *testing.T, ctx context.Context, expr string) string {
	t.Helper()
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out)); err != nil {
		t.Fatalf("evaluate %s: %v", expr, err)
	}
	return out
}

// waitUntil polls expr (a boolean JavaScript expression) until true.
func waitUntil(t *testing.T, ctx context.Context, expr string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Poll(expr, nil,
		chromedp.WithPollingTimeout(20*time.Second))); err != nil {
		t.Fatalf("wait: %s: %v", expr, err)
	}
}

// reqRecord is one observed network request.
type reqRecord struct {
	url     string
	fillsHd string
}

// listenRequests records every request the tab makes from now on.
func listenRequests(ctx context.Context) *[]reqRecord {
	reqs := &[]reqRecord{}
	chromedp.ListenTarget(ctx, func(ev any) {
		e, ok := ev.(*network.EventRequestWillBeSent)
		if !ok {
			return
		}
		hdr := ""
		if v, ok := e.Request.Headers["X-Gofastr-Fills"]; ok {
			hdr = fmt.Sprint(v)
		}
		*reqs = append(*reqs, reqRecord{url: e.Request.URL, fillsHd: hdr})
	})
	return reqs
}

// layoutModuleRe matches the four layout demand modules the marketing
// pages must never request (the ?v= hash suffix included).
var layoutModuleRe = regexp.MustCompile(`/__gofastr/runtime/(envelope|loading|parts|transition)\.js(\?|$)`)

// TestAcmeMarketingShipsCoreOnly: first paint of /, /pricing and
// /changelog requests NO layout demand module and no SSE bus — the
// opt-in contract's boot half. The announcement outlet is the one
// piece of layout machinery the marketing pages declare, and it costs
// nothing until the first navigation: the FIRST click starts the
// envelope module's load BESIDE the page fetch (the module request is
// in flight while the page answer is still unapplied — asserted below
// against a held answer), and from then on the announcement appears
// and disappears across navigations both ways through the fills
// envelope, one chain, one shell. Mutations it catches: a boot row for
// the outlet marker fails the first-paint count; a navigation-time
// load that serialized behind the fetch (await-then-fetch) never shows
// the module request before the held answer applies; an envelope that
// forgets the announcement fill leaves the bar stranded across a
// navigation away from the changelog.
func TestAcmeMarketingShipsCoreOnly(t *testing.T) {
	base, held := acmeServeHeld(t, "/changelog")
	browser := acmeBrowser(t, 1280, 860)
	ctx, cancel := context.WithTimeout(browser, 90*time.Second)
	defer cancel()

	reqs := listenRequests(ctx)

	nav := func(path, want string) {
		t.Helper()
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+path),
			chromedp.WaitReady("main h1", chromedp.ByQuery),
		); err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		waitUntil(t, ctx, jsContains("document.querySelector('main h1').textContent", want))
		// First paint settles: give any stray module request time to
		// show up before counting.
		if err := chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
			t.Fatalf("settle %s: %v", path, err)
		}
	}
	nav("/", "Issue tracking")
	nav("/pricing", "Pay for the team")
	nav("/changelog", "What shipped")

	for _, r := range *reqs {
		if layoutModuleRe.MatchString(r.url) {
			t.Errorf("first paint requested the layout demand module %s — an outlet marker alone must not boot-load it", r.url)
		}
		if strings.Contains(r.url, "/__gofastr/sse") {
			t.Errorf("a marketing page opened the SSE bus: %s", r.url)
		}
	}

	// Back to the landing page; the changelog carries the announcement
	// bar on first paint (a direct load renders the fill inline).
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, jsContains("document.querySelector('main h1').textContent", "Issue tracking"))
	if got := evalString(t, ctx, `String(!!document.querySelector('[data-acme-announcement]'))`); got != "false" {
		t.Errorf("the announcement bar is on the landing page; only the changelog may fill the outlet")
	}

	// The first click navigation: / → /changelog. Hold the page answer
	// so the ordering is observable — the envelope module's request
	// must be in flight while the page response is still unapplied.
	(*reqs) = nil
	if err := chromedp.Run(ctx, chromedp.Click(`nav[aria-label="Primary"] a[href="/changelog"]`, chromedp.NodeVisible)); err != nil {
		t.Fatal(err)
	}
	waitForModuleReq := func() *reqRecord {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			for i := range *reqs {
				if strings.Contains((*reqs)[i].url, "/__gofastr/runtime/envelope.js") {
					return &(*reqs)[i]
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		return nil
	}
	modReq := waitForModuleReq()
	if modReq == nil {
		held.release()
		t.Fatal("the first click navigation never requested the envelope module beside its page fetch")
	}
	// The held answer has not applied: the changelog's own content (and
	// its announcement) is not in the DOM yet, while the module request
	// is already in flight — the load runs in PARALLEL with the fetch.
	var appliedEarly string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`String(!!document.querySelector('main h1') && document.querySelector('main h1').textContent.includes('What shipped'))`, &appliedEarly))
	if appliedEarly == "true" {
		t.Error("the held page answer applied before the module request was observed — the module load is serialized behind the fetch, not beside it")
	}
	held.release()
	waitUntil(t, ctx, jsContains("document.querySelector('main h1').textContent", "What shipped"))
	waitUntil(t, ctx, `!!document.querySelector('[data-acme-announcement]')`)
	if n := countReqs(*reqs, "envelope.js"); n != 1 {
		t.Errorf("envelope module requested %d times on the first navigation, want exactly 1", n)
	}

	// Both ways across the announcement boundary, module already
	// loaded: /changelog → /pricing (bar disappears), /pricing →
	// /changelog (bar returns).
	clickNav := func(sel, want string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.NodeVisible)); err != nil {
			t.Fatalf("click %s: %v", sel, err)
		}
		waitUntil(t, ctx, jsContains("document.querySelector('main h1').textContent", want))
	}
	clickNav(`nav[aria-label="Primary"] a[href="/pricing"]`, "Pay for the team")
	waitUntil(t, ctx, `!document.querySelector('[data-acme-announcement]')`)
	clickNav(`nav[aria-label="Primary"] a[href="/changelog"]`, "What shipped")
	waitUntil(t, ctx, `!!document.querySelector('[data-acme-announcement]')`)
	if got := evalString(t, ctx, `document.querySelector('[data-acme-announcement]').textContent`); !strings.Contains(got, "2.4") {
		t.Errorf("announcement after client navigation = %q, want it back with 2.4", got)
	}
	// And away again: the envelope's empty fill takes the bar off.
	clickNav(`nav[aria-label="Primary"] a[href="/"]`, "Issue tracking")
	waitUntil(t, ctx, `!document.querySelector('[data-acme-announcement]')`)
}

// countReqs counts observed requests whose URL contains want.
func countReqs(reqs []reqRecord, want string) int {
	n := 0
	for _, r := range reqs {
		if strings.Contains(r.url, want) {
			n++
		}
	}
	return n
}

// heldNav releases a held page answer.
type heldNav struct{ release func() }

// acmeServeHeld serves the app through a wrapper that holds every
// X-Gofastr-Navigate request for one path until the handle's release,
// then answers normally; everything else passes straight through. The
// returned base URL replaces the plain server's.
func acmeServeHeld(t *testing.T, holdPath string) (string, *heldNav) {
	t.Helper()
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	inner := app.Router()
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == holdPath && r.Header.Get("X-Gofastr-Navigate") == "1" {
			<-gate
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &heldNav{release: func() { close(gate) }}
}

// jsString renders s as a double-quoted JavaScript string literal.
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsContains builds the boolean expression "<expr>.includes(<want>)".
func jsContains(expr, want string) string {
	return expr + ".includes(" + jsString(want) + ")"
}

// TestAcmeAnnouncementOnlyOnChangelog: the v2.4 bar is on the
// changelog — on first load, after a client navigation to it, and gone
// after a client navigation away — and nowhere else. Mutation it
// catches: rendering the bar unconditionally puts it on every page;
// losing the announced chain keeps it off the changelog after a client
// navigation.
func TestAcmeAnnouncementOnlyOnChangelog(t *testing.T) {
	base := acmeServe(t)
	browser := acmeBrowser(t, 1280, 860)
	ctx, cancel := context.WithTimeout(browser, 60*time.Second)
	defer cancel()

	absent := func(page string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Navigate(base+page)); err != nil {
			t.Fatalf("load %s: %v", page, err)
		}
		waitUntil(t, ctx, `!!document.querySelector('main h1')`)
		if got := evalString(t, ctx, `String(!!document.querySelector('[data-acme-announcement]'))`); got == "true" {
			t.Errorf("%s carries the announcement bar; only the changelog may", page)
		}
	}
	absent("/")
	absent("/pricing")

	// Direct load: the bar is there, says 2.4, and its anchor resolves.
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/changelog")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!!document.querySelector('[data-acme-announcement]')`)
	if got := evalString(t, ctx, `document.querySelector('[data-acme-announcement]').textContent`); !strings.Contains(got, "2.4") {
		t.Errorf("announcement text = %q, want it to name 2.4", got)
	}
	if got := evalString(t, ctx, `document.querySelector('[data-acme-announcement] a').getAttribute('href')`); got != "#v2-4" {
		t.Errorf("announcement link = %q, want #v2-4", got)
	}
	if got := evalString(t, ctx, `String(!!document.getElementById('v2-4'))`); got != "true" {
		t.Error("the announcement's anchor target #v2-4 does not exist on the changelog")
	}

	// Client navigation away takes the bar with the shell; back to the
	// changelog it returns (the cross-chain swap re-renders the shell).
	if err := chromedp.Run(ctx,
		chromedp.Click(`nav[aria-label="Primary"] a[href="/pricing"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!document.querySelector('[data-acme-announcement]')`)
	waitUntil(t, ctx, jsContains("document.querySelector('main h1').textContent", "Pay for the team"))
	if err := chromedp.Run(ctx,
		chromedp.Click(`nav[aria-label="Primary"] a[href="/changelog"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!!document.querySelector('[data-acme-announcement]')`)
	if got := evalString(t, ctx, `document.querySelector('[data-acme-announcement]').textContent`); !strings.Contains(got, "2.4") {
		t.Errorf("announcement after client navigation = %q, want it back with 2.4", got)
	}
}

// tocState reads the toc outlet cell's display and the doc layout's
// column count — the two observables of the rail's collapse.
func tocState(t *testing.T, ctx context.Context) (cellDisplay string, columns int) {
	t.Helper()
	// The cell is the helpdocs column holding the toc outlet.
	cellDisplay = evalString(t, ctx, `(() => {
		const el = document.querySelector('[data-cui-outlet$="#toc"]');
		return el ? getComputedStyle(el.parentElement).display : 'missing';
	})()`)
	var cols int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const el = document.querySelector('[data-cui-scope="helpdocs"]');
		return el ? getComputedStyle(el).gridTemplateColumns.split(' ').length : 0;
	})()`, &cols)); err != nil {
		t.Fatalf("read help page columns: %v", err)
	}
	return cellDisplay, cols
}

// TestHelpTocRailCollapsesWhenEmpty: an article with headings shows the
// TOC rail; the glossary (no headings) collapses its column — on a
// first load and after a client navigation between the two, which is
// the fill applying to a kept layer. Mutation it catches: keying the
// collapse on a class at first render (instead of the outlet's live
// emptiness) leaves the column standing after the client navigation.
func TestHelpTocRailCollapsesWhenEmpty(t *testing.T) {
	base := acmeServe(t)
	browser := acmeBrowser(t, 1280, 860)
	ctx, cancel := context.WithTimeout(browser, 60*time.Second)
	defer cancel()

	// First load, an article with headings: the rail is shown and the
	// grid carries three columns.
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/help/projects")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!!document.querySelector('[data-cui-outlet$="#toc"] .fui-toc')`)
	if disp, cols := tocState(t, ctx); disp == "none" || cols != 3 {
		t.Errorf("projects: toc cell %q, %d columns; want a shown rail and 3 columns", disp, cols)
	}

	// First load, the no-heading article: the outlet is empty and the
	// column collapses.
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/help/glossary")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!!document.querySelector('main h1')`)
	if disp, cols := tocState(t, ctx); disp != "none" || cols != 2 {
		t.Errorf("glossary: toc cell %q, %d columns; want the rail collapsed to 2 columns", disp, cols)
	}

	// Client navigation glossary -> keyboard: the fill arrives, the
	// rail comes back.
	if err := chromedp.Run(ctx,
		chromedp.Click(`.fui-sidebar__inline a[href="/help/keyboard"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!!document.querySelector('[data-cui-outlet$="#toc"] .fui-toc')`)
	if disp, cols := tocState(t, ctx); disp == "none" || cols != 3 {
		t.Errorf("keyboard after client nav: toc cell %q, %d columns; want the rail back", disp, cols)
	}

	// Client navigation keyboard -> glossary: the empty fill collapses
	// the column again.
	if err := chromedp.Run(ctx,
		chromedp.Click(`.fui-sidebar__inline a[href="/help/glossary"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, jsContains("document.querySelector('main h1').textContent", "Words we use"))
	if disp, cols := tocState(t, ctx); disp != "none" || cols != 2 {
		t.Errorf("glossary after client nav: toc cell %q, %d columns; want the rail collapsed", disp, cols)
	}
}

// TestHelpDirectArticleWithoutJS: a direct load of an article on a
// phone, with no script, shows the article and reaches the rest of the
// site through the native menu. Mutation it catches: a docs nav that
// only opens through JavaScript leaves the article page a dead end on
// a phone.
func TestHelpDirectArticleWithoutJS(t *testing.T) {
	base := acmeServe(t)
	root := acmeBrowser(t, 390, 844)
	if err := chromedp.Run(root, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable script execution: %v", err)
	}
	ctx, cancel := context.WithTimeout(root, 45*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/help/filters"),
		chromedp.WaitReady("article", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("direct load of an article: %v", err)
	}

	// The article rendered server-side; the TOC is hidden below lg even
	// though this article has headings.
	var (
		paraShown string
		tocShown  string
	)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`getComputedStyle(document.querySelector('article p')).display`, &paraShown),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-cui-outlet$="#toc"]').parentElement).display`, &tocShown),
	); err != nil {
		t.Fatalf("probe the article: %v", err)
	}
	if paraShown == "none" {
		t.Error("the article's body is hidden on a phone without script")
	}
	if tocShown != "none" {
		t.Errorf("the TOC rail shows on a phone (%q); below lg it must hide", tocShown)
	}

	// landed polls until the browser sits at path with want in main —
	// with script off every link is a full page load, and a full
	// navigation tears up the execution context mid-poll, so the poll
	// is a loop of FRESH evaluates, retrying the ones racing the
	// navigation.
	landed := func(path, want string) {
		t.Helper()
		expr := `location.pathname === ` + jsString(path) +
			` && (document.querySelector('main') || {}).textContent.includes(` + jsString(want) + `)`
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			var ok bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err == nil && ok {
				return
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatalf("never landed on %s ~ %q", path, want)
	}

	// The docs nav opens natively and one of its links moves between
	// articles.
	var opened bool
	if err := chromedp.Run(ctx,
		chromedp.Click(".fui-sidebar-native__mobile > details > summary", chromedp.NodeVisible),
		chromedp.Evaluate(`document.querySelector('.fui-sidebar-native__mobile > details').open`, &opened),
	); err != nil {
		t.Fatalf("open the docs menu: %v", err)
	}
	if !opened {
		t.Error("clicking the menu's summary did not open it")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`.fui-sidebar-native__mobile .fui-collapsible__content a[href="/help/keyboard"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatalf("follow the menu's Keyboard shortcuts link: %v", err)
	}
	landed("/help/keyboard", "Keyboard shortcuts")

	// The site header's own mobile menu is native too: it leaves the
	// help center for the marketing site, still without script.
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-cui-scope="siteheader"] summary`, chromedp.NodeVisible),
		chromedp.Evaluate(`document.querySelector('[data-cui-scope="siteheader"] details').open`, &opened),
	); err != nil {
		t.Fatalf("open the site menu: %v", err)
	}
	if !opened {
		t.Error("clicking the site menu's summary did not open it")
	}
	if err := chromedp.Run(ctx,
		// The menu unrolls (400ms at most) before its rows take taps, as
		// a reader sees it. Poll needs page script, which this test turns
		// off, so it waits past the motion instead.
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Click(`nav[aria-label="Mobile primary"] a[href="/pricing"]`, chromedp.NodeVisible),
	); err != nil {
		t.Fatalf("follow the site menu's Pricing link: %v", err)
	}
	landed("/pricing", "Pay for the team")
}

// mouseClickAt presses and releases the left button at viewport
// coordinates — the real-mouse shape (detail 1, true hit testing),
// never el.click().
func mouseClickAt(t *testing.T, ctx context.Context, x, y float64) {
	t.Helper()
	click := func(p *input.DispatchMouseEventParams) {
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			return p.Do(c)
		})); err != nil {
			t.Fatalf("dispatch mouse event: %v", err)
		}
	}
	click(input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1))
	click(input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(input.Left).WithClickCount(1))
}

// nodeCenter returns the viewport centre of the first element matching sel.
func nodeCenter(t *testing.T, ctx context.Context, sel string) (float64, float64) {
	t.Helper()
	var box struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"width"`
		H float64 `json:"height"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const r = document.querySelector(`+"`"+sel+"`"+`).getBoundingClientRect();`+
			`return {x:r.x, y:r.y, width:r.width, height:r.height}; })()`, &box)); err != nil {
		t.Fatalf("box %s: %v", sel, err)
	}
	if box.W == 0 || box.H == 0 {
		t.Fatalf("%s has no box — element not visible", sel)
	}
	return box.X + box.W/2, box.Y + box.H/2
}

// TestHelpNavCurrentFollowsNavigation: the help nav's current-article
// mark follows the URL across client navigations. The docs layer is
// kept chrome, so the nav is a ROUTE AREA (server re-derived on every
// navigation); before that, the build read the entering match once and
// the mark went stale after the first click. Mutation it catches: the
// area's fn ignoring the live match (a fixed path) leaves every
// navigation marking the first render's article.
func TestHelpNavCurrentFollowsNavigation(t *testing.T) {
	base := acmeServe(t)
	ctx, cancel := context.WithTimeout(acmeBrowser(t, 1280, 860), 90*time.Second)
	t.Cleanup(cancel)

	current := func() []string {
		var out []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('.fui-sidebar__inline a[aria-current="page"]')).map((a) => a.getAttribute('href'))`, &out)); err != nil {
			t.Fatal(err)
		}
		return out
	}
	waitCurrent := func(href string) {
		t.Helper()
		waitUntil(t, ctx, fmt.Sprintf(
			`(() => { const a = document.querySelector('.fui-sidebar__inline a[aria-current="page"]'); return a && a.getAttribute('href') === %q && location.pathname === %q; })()`,
			href, href))
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/help/filters"),
		chromedp.WaitVisible(`.fui-sidebar__inline`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate filters: %v", err)
	}
	waitCurrent("/help/filters")
	if got := current(); len(got) != 1 || got[0] != "/help/filters" {
		t.Errorf("current help link on direct load = %v, want exactly [/help/filters]", got)
	}

	// Real-mouse navigation moves the mark and clears the old link.
	x, y := nodeCenter(t, ctx, `.fui-sidebar__inline a[href="/help/keyboard"]`)
	mouseClickAt(t, ctx, x, y)
	waitCurrent("/help/keyboard")
	if got := current(); len(got) != 1 || got[0] != "/help/keyboard" {
		t.Errorf("current help link after click = %v, want exactly [/help/keyboard]", got)
	}

	// And on to a third: two navigations deep, still truthful.
	x, y = nodeCenter(t, ctx, `.fui-sidebar__inline a[href="/help/glossary"]`)
	mouseClickAt(t, ctx, x, y)
	waitCurrent("/help/glossary")
	if got := current(); len(got) != 1 || got[0] != "/help/glossary" {
		t.Errorf("current help link after second click = %v, want exactly [/help/glossary]", got)
	}
}
