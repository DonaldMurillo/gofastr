package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// carouselSlideHTML is one slide's inner markup; slides are numbered so a
// stray scroll can be told apart in debugging output.
func carouselSlideHTML(n int) string {
	return `<li class="ui-carousel__slide" data-fui-carousel-slide="` + strconv.Itoa(n) + `">S` + strconv.Itoa(n) + `</li>`
}

// carouselFixture renders the framework/ui.Carousel SSR shape (root marker,
// track with tabindex, slides, prev/next buttons) with autorotate=80ms so
// ~12 rotate ticks land per second — enough margin that counter deltas are
// unambiguous without making the test slow.
func carouselFixture(id string) string {
	return `<div class="ui-carousel" id="` + id + `" data-fui-carousel="true" data-fui-carousel-autorotate="80">` +
		`<ul class="ui-carousel__track" data-fui-carousel-track="true" tabindex="0" aria-label="slides">` +
		carouselSlideHTML(0) + carouselSlideHTML(1) + carouselSlideHTML(2) +
		`</ul>` +
		`<button type="button" class="ui-carousel__prev" data-fui-carousel-prev="true" aria-label="Previous">&#8249;</button>` +
		`<button type="button" class="ui-carousel__next" data-fui-carousel-next="true" aria-label="Next">&#8250;</button>` +
		`</div>`
}

// startCarouselNavServer serves two layout-less pages that share one
// document. Page A has an autorotate carousel INSIDE <main> (swapped out on
// SPA nav) and one OUTSIDE <main> (persists, the shell-owned shape). Page B
// is plain content. Every swap path (runtime.js loadPage) replaces the
// content cell BEFORE dispatching gofastr:navigate, which is the ordering
// this test pins the carousel module's teardown against.
func startCarouselNavServer(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	})
	page := func(body string) string {
		return `<!doctype html><html><head><title>carousel teardown</title>` +
			// Inline route manifest: without it the router does not
			// intercept the anchor and the click becomes a full page
			// load (fresh document, window wiped). Same shape every
			// SPA-nav e2e test in this package uses.
			`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/b"}]</script>` +
			`</head><body>` +
			body +
			`<span id="ready">ready</span>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var body string
		if r.URL.Path == "/b" {
			body = `<main><p id="bmark">page B</p></main>`
		} else {
			// c2 sits OUTSIDE <main>: it survives the /a -> /b swap the
			// way a carousel in a shared layout layer does in production.
			body = carouselFixture("c2") +
				`<main>` + carouselFixture("c1") + `<a id="nav" href="/b">go to B</a></main>`
		}
		fmt.Fprint(w, page(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestCarouselAutoRotateTeardownOnNav pins the autorotate interval's
// lifetime across an SPA navigation.
//
// attach() arms a setInterval per autorotate carousel and stores stop() on
// the element (_fuiCarouselStop). The gofastr:navigate handler is the only
// teardown path, and it looks for carousels via
// document.querySelectorAll — but every loadPage swap path replaces the
// content cell BEFORE finishNav dispatches gofastr:navigate
// (runtime.js: swapAtSlot -> finishNav). Two failures fall out:
//
//  1. LEAK: a carousel inside the swapped region is already detached when
//     the handler runs, document.querySelectorAll cannot see it, its
//     interval is never cleared. It keeps firing step() — querySelectorAll
//     + getComputedStyle + scrollTo over the detached subtree — for the
//     remaining lifetime of the page, and the closure pins the subtree in
//     memory. Every autorotate carousel on every SPA nav away leaks one.
//  2. FEATURE DEATH: a carousel that PERSISTS (outside the swapped cell,
//     e.g. in a shared layout layer) IS found by the handler and stopped —
//     and scan() then skips it (fuiCarouselBound guard), so its autorotate
//     never restarts. One SPA nav anywhere permanently kills rotation on
//     every surviving carousel.
//
// The liveness probe overrides querySelectorAll on each carousel element
// (an own property; slides() dispatches through it on every rotate tick)
// and counts calls. Instrumentation happens after wiring, so deltas are
// pure interval ticks.
func TestCarouselAutoRotateTeardownOnNav(t *testing.T) {
	srv := startCarouselNavServer(t)
	ctx := newSeedBrowserCtx(t)

	read := func(el, expr string, dst *string) chromedp.Action {
		return chromedp.Evaluate(`(function(){
			var el = window.__probe['`+el+`'];
			return String(`+expr+`);
		})()`, dst)
	}

	var c1A, c1B, c1C, c2A, c2B, c2C string
	var c1Conn, c2Conn, loc string

	// Two observation windows AFTER the navigation: the first proves the
	// present state, the second (a beat later) proves it is stable, not a
	// timing artifact of the swap itself.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#c1`, chromedp.ByID),
		// Let the marker scan idle-load carousel.js and wire both
		// carousels (same budget the other module e2e tests use).
		chromedp.Sleep(700*time.Millisecond),

		// Instrument both carousels AFTER wiring.
		chromedp.Evaluate(`(function(){
			window.__probe = {};
			['#c1','#c2'].forEach(function(sel){
				var el = document.querySelector(sel);
				var orig = el.querySelectorAll.bind(el);
				el.__qsa = 0;
				el.querySelectorAll = function(s){ el.__qsa++; return orig(s); };
				window.__probe[sel] = el;
			});
		})()`, nil),
		// Baseline: both intervals must be alive pre-nav, otherwise the
		// probe proves nothing. 80ms rotate -> ~4 ticks in 350ms.
		chromedp.Sleep(350*time.Millisecond),
		read("#c1", `window.__probe['#c1'].__qsa`, &c1A),
		read("#c2", `window.__probe['#c2'].__qsa`, &c2A),

		// Real SPA navigation: the runtime intercepts the anchor click,
		// swaps main's innerHTML, THEN dispatches gofastr:navigate.
		chromedp.Click(`#nav`, chromedp.ByID),
		chromedp.WaitVisible(`#bmark`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		read("#c1", `window.__probe['#c1'].__qsa`, &c1B),
		read("#c2", `window.__probe['#c2'].__qsa`, &c2B),
		chromedp.Sleep(350*time.Millisecond),
		read("#c1", `window.__probe['#c1'].__qsa`, &c1C),
		read("#c2", `window.__probe['#c2'].__qsa`, &c2C),

		read("#c1", `window.__probe['#c1'].isConnected`, &c1Conn),
		read("#c2", `window.__probe['#c2'].isConnected`, &c2Conn),
		chromedp.Evaluate(`location.pathname`, &loc),
	); err != nil {
		t.Fatal(err)
	}

	// Fixture sanity: the nav really happened and the two carousels sit on
	// the expected sides of the swap boundary. Without these, a pass could
	// be an artifact of the navigation not firing at all.
	if loc != "/b" {
		t.Fatalf("fixture: SPA nav did not happen, location = %q", loc)
	}
	if c1Conn != "false" {
		t.Fatalf("fixture: #c1 should be detached after the swap, isConnected = %s", c1Conn)
	}
	if c2Conn != "true" {
		t.Fatalf("fixture: #c2 should persist across the swap, isConnected = %s", c2Conn)
	}

	toInt := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("counter not numeric: %q", s)
		}
		return n
	}
	if toInt(c1A) < 2 {
		t.Fatalf("fixture: #c1 interval was not rotating pre-nav (%s ticks), probe invalid", c1A)
	}
	if toInt(c2A) < 2 {
		t.Fatalf("fixture: #c2 interval was not rotating pre-nav (%s ticks), probe invalid", c2A)
	}

	leak := toInt(c1C) - toInt(c1B)
	if leak != 0 {
		t.Errorf("LEAK: detached carousel's autorotate interval is still firing — %d rotate ticks against a detached subtree after SPA nav (counter %s -> %s -> %s)", leak, c1B, c1C, c1C)
	}
	persisted := toInt(c2C) - toInt(c2B)
	if persisted < 2 {
		t.Errorf("FEATURE DEATH: persisted carousel's autorotate stopped permanently after an unrelated SPA nav — %d ticks in the post-nav window (counter %s -> %s -> %s), want >= 2 (rotation must continue; the carousel is still connected and visible)", persisted, c2B, c2C, c2C)
	}
}

// TestCarouselInjectedAfterLoadGetsWired pins the other half of the
// scanner registration: a carousel arriving via an island/RPC swap (any
// innerHTML insertion after module load) must be wired by
// _moduleScanners.carousel — core's MutationObserver re-runs loaded
// modules' scanners over inserted subtrees. Before the registration the
// module only scanned at load and on gofastr:navigate, so a carousel
// inside a swapped island was dead DOM (no Prev/Next, no autorotate).
//
// Observable: the wiring flag flips AND an injected autorotate carousel
// actually starts rotating (QSA probe, same technique as the teardown
// test).
func TestCarouselInjectedAfterLoadGetsWired(t *testing.T) {
	srv := startCarouselNavServer(t)
	ctx := newSeedBrowserCtx(t)

	var bound, ticksA, ticksB string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#c1`, chromedp.ByID),
		// Module loaded + initial carousels wired.
		chromedp.Sleep(700*time.Millisecond),

		// Simulate an island swap: innerHTML insertion of a carousel
		// the module has never seen (fresh element, no bound flag).
		chromedp.Evaluate(`(function(){
			var host = document.querySelector('main');
			host.insertAdjacentHTML('beforeend', `+strconv.Quote(carouselFixture("inj"))+`);
			var el = document.getElementById('inj');
			var orig = el.querySelectorAll.bind(el);
			el.__qsa = 0;
			el.querySelectorAll = function(s){ el.__qsa++; return orig(s); };
		})()`, nil),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('inj').dataset.fuiCarouselBound || '')`, &bound),
		chromedp.Evaluate(`String(document.getElementById('inj').__qsa)`, &ticksA),
		chromedp.Sleep(350*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('inj').__qsa)`, &ticksB),
	); err != nil {
		t.Fatal(err)
	}

	if bound != "1" {
		t.Errorf("injected carousel was not wired — data-fui-carousel-bound = %q, want \"1\" (the _moduleScanners.carousel registration is the only wiring path for island-swapped carousels)", bound)
	}
	if ticksA == "0" || ticksB == ticksA {
		t.Errorf("injected autorotate carousel is not rotating — ticks %s -> %s, want growing (wired + interval armed)", ticksA, ticksB)
	}
}
