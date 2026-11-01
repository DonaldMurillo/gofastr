package runtime_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// envelopeSeedServer: "/" and "/a" share the l:site layer and an
// outlet, so the envelope navigator takes every navigation. Each page's
// quantity is page-scoped (#gofastr-signals-page names it); cart is a
// global. "/c" brings a layout chain the others lack, so reaching it is
// a cross-chain whole-document swap. "/s" answers a partial request
// with a whole document, the way a static host does; "/f" does too, with
// an outlet the live page lacks.
func envelopeSeedServer(t *testing.T) *httptest.Server {
	t.Helper()
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	routes := `[{"path":"/","layouts":["l:site"]},{"path":"/a","layouts":["l:site"]},{"path":"/s","layouts":["l:site"]},{"path":"/f","layouts":["l:site"]},{"path":"/c","layouts":["l:other"]},{"path":"/c2","layouts":["l:other"]}]`
	doc := func(seed, body string) string {
		return `<!doctype html><html lang="en"><head><title>seed</title>` +
			`<script type="application/json" id="gofastr-routes">` + routes + `</script>` +
			`<script type="application/json" id="gofastr-signals">` + seed + `</script>` +
			`<script type="application/json" id="gofastr-signals-page">["quantity"]</script>` +
			`</head><body>` + body + `<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	site := func(inner string) string {
		return `<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/a">A</a><a id="goS" href="/s">S</a><a id="goC" href="/c">C</a><a id="goF" href="/f">F</a></nav>` +
			`<div data-cui-outlet="l:site#side" id="side">SIDE</div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main></div>`
	}
	home := `<p id="screen-home"><span data-cui-signal="quantity">1</span></p>`
	screenA := `<p id="screen-a"><span data-cui-signal="quantity">10</span></p>`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(coreJS))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch req.URL.Path {
		case "/c", "/c2":
			other := func(qty, inner string) string {
				return doc(`{"quantity":`+qty+`,"cart":0}`,
					`<div data-cui-layout="other" data-cui-layout-key="l:other"><nav><a id="goC2" href="/c2">C2</a></nav>`+
						`<main role="main" tabindex="-1" data-cui-layout-slot="l:other">`+inner+`</main></div>`)
			}
			if req.URL.Path == "/c2" {
				fmt.Fprint(w, other("8", `<p id="screen-c2">C2</p>`))
			} else {
				fmt.Fprint(w, other("7", `<p id="screen-c">C</p>`))
			}
			return
		case "/s":
			fmt.Fprint(w, doc(`{"quantity":5,"cart":0}`, site(`<p id="screen-s">S</p>`)))
			return
		case "/f":
			// An outlet the live page lacks: the document cannot be read
			// as an envelope, so its main swaps in whole.
			fmt.Fprint(w, doc(`{"quantity":6,"cart":0}`, strings.Replace(site(`<p id="screen-f">F</p>`),
				`<main`, `<div data-cui-outlet="l:site#extra">X</div><main`, 1)))
			return
		}
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			w.Header().Set("X-Gofastr-Envelope", "2")
			if req.URL.Path == "/a" {
				fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":10},"g":{"cart":0}}</script>`+
					`<template data-cui-fill="l:site">`+screenA+`</template>`)
			} else {
				fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":1},"g":{"cart":0}}</script>`+
					`<template data-cui-fill="l:site">`+home+`</template>`)
			}
			return
		}
		if req.URL.Path == "/a" {
			fmt.Fprint(w, doc(`{"quantity":10,"cart":0}`, site(screenA)))
			return
		}
		fmt.Fprint(w, doc(`{"quantity":1,"cart":0}`, site(home)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A cache replay through the envelope navigator restores the page's
// page-scoped signals, not only its route.* keys (2026-10-27 bug bash
// R30). The boot entry's seed and every leave capture's seed came from
// the route-only fold of the head island, so Back to the first page
// kept the page left's quantity under the first page's markup, and a
// page re-captured on leave lost the seed its own envelope carried.
func TestEnvelopeReplayRestoresPageSignals(t *testing.T) {
	srv := envelopeSeedServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	qty := func(dst *string) chromedp.Action {
		return chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, dst)
	}
	var onA, back1, fwd, back2, cart string
	var soft bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond), // the envelope module's boot capture
		chromedp.Evaluate(`window.__softMark = 1; __gofastr.setSignal('cart', 3); true`, nil),
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		qty(&onA),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		qty(&back1),
		chromedp.Evaluate(`history.forward(); true`, nil),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		qty(&fwd),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		qty(&back2),
		chromedp.Evaluate(`String(__gofastr.getSignal('cart'))`, &cart),
		chromedp.Evaluate(`window.__softMark === 1`, &soft),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !soft {
		t.Fatal("a navigation reloaded the document; the test needs soft swaps")
	}
	if onA != "10" {
		t.Fatalf("quantity on /a = %s, want its envelope's 10", onA)
	}
	if back1 != "1" {
		t.Errorf("Back to the boot page: quantity = %s, want its seed 1", back1)
	}
	if fwd != "10" {
		t.Errorf("Forward to /a from the cache: quantity = %s, want its envelope's 10", fwd)
	}
	if back2 != "1" {
		t.Errorf("Back to the re-captured boot page: quantity = %s, want 1", back2)
	}
	if cart != "3" {
		t.Errorf("global cart = %s after the replays, want the user's 3", cart)
	}
}

// A page whose cache entry was invalidated while it was live still
// leaves with its own page-scoped seed. The leave capture had only the
// route.* fold to fall back on, so Forward replayed /a's markup with the
// quantity Back had restored (#614).
func TestInvalidatedPageKeepsSeed(t *testing.T) {
	srv := envelopeSeedServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var back, fwd string
	var soft bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__softMark = 1; true`, nil),
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		// An RPC write on /a drops its entry, as rpc.js does.
		chromedp.Evaluate(`__gofastr.invalidate('/a'); true`, nil),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &back),
		chromedp.Evaluate(`history.forward(); true`, nil),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &fwd),
		chromedp.Evaluate(`window.__softMark === 1`, &soft),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !soft {
		t.Fatal("a navigation reloaded the document; the test needs soft swaps")
	}
	if back != "1" {
		t.Fatalf("Back to the boot page: quantity = %s, want 1", back)
	}
	if fwd != "10" {
		t.Errorf("Forward to the invalidated /a: quantity = %s, want its envelope's 10", fwd)
	}
}

// A plain partial (no envelope) the envelope navigator applied keeps its
// seed when the page is re-captured on leave. Its cache entry carries the
// island inline in its bytes, and the live DOM has merged it away, so the
// leave capture took the route keys alone; and a replay of an entry with
// no fills swapped the bytes and never applied the seed field. The
// outlet sits inside the slot, which loads the envelope module and lets a
// plain partial apply.
func TestEnvelopePlainPartialKeepsSeed(t *testing.T) {
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	page := func(seed, inner string) string {
		return `<!doctype html><html lang="en"><head><title>plain</title>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:site"]},{"path":"/p","layouts":["l:site"]}]</script>` +
			`<script type="application/json" id="gofastr-signals">` + seed + `</script>` +
			`<script type="application/json" id="gofastr-signals-page">["quantity"]</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goP" href="/p">P</a><a id="goHome" href="/">home</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` + inner + `</main>` +
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	home := `<div data-cui-outlet="l:site#in" id="in">IN</div><p id="screen-home"><span data-cui-signal="quantity">1</span></p>`
	screenP := `<div data-cui-outlet="l:site#in" id="in">IN</div><p id="screen-p"><span data-cui-signal="quantity">4</span></p>`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(coreJS))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			if req.URL.Path == "/p" {
				fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":4},"g":{"cart":0}}</script>`+screenP)
			} else {
				fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":1},"g":{"cart":0}}</script>`+home)
			}
			return
		}
		if req.URL.Path == "/p" {
			fmt.Fprint(w, page(`{"quantity":4,"cart":0}`, screenP))
			return
		}
		fmt.Fprint(w, page(`{"quantity":1,"cart":0}`, home))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var onP, home1, replayP string
	var soft bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__softMark = 1; true`, nil),
		chromedp.Click(`#goP`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-p`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &onP),
		// Leaving /p re-captures it; the boot entry replays home.
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &home1),
		chromedp.Evaluate(`history.forward(); true`, nil),
		chromedp.WaitVisible(`#screen-p`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &replayP),
		// A page whose entry was invalidated still leaves softly.
		chromedp.Evaluate(`__gofastr.invalidate('*'); true`, nil),
		chromedp.Click(`#goHome`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`window.__softMark === 1`, &soft),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !soft {
		t.Fatal("a navigation reloaded the document; the test needs soft swaps")
	}
	if onP != "4" || home1 != "1" {
		t.Fatalf("quantity on /p = %s and back home = %s, want 4 and 1", onP, home1)
	}
	if replayP != "4" {
		t.Errorf("Forward to the re-captured /p: quantity = %s, want its partial's 4", replayP)
	}
}

// A whole document the envelope navigator swaps in applies its
// page-scoped seed: the cross-chain swap (/c) folded only the head's
// route.* keys, and a static host's document answer (/s, and /f through
// the whole-main fallback) read no seed at all, so the store kept the
// boot page's quantity.
func TestEnvelopeDocSwapAppliesPageSignals(t *testing.T) {
	srv := envelopeSeedServer(t)
	for _, tc := range []struct{ link, screen, want string }{
		{"#goC", "#screen-c", "7"},
		{"#goS", "#screen-s", "5"},
		{"#goF", "#screen-f", "6"},
	} {
		t.Run(tc.screen, func(t *testing.T) {
			ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
			var qty, back, cart string
			var soft bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
				chromedp.Sleep(500*time.Millisecond),
				chromedp.Evaluate(`window.__softMark = 1; __gofastr.setSignal('cart', 3); true`, nil),
				chromedp.Click(tc.link, chromedp.ByQuery),
				chromedp.WaitVisible(tc.screen, chromedp.ByQuery),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &qty),
				chromedp.Evaluate(`String(__gofastr.getSignal('cart'))`, &cart),
				chromedp.Evaluate(`window.__softMark === 1`, &soft),
				// The swapped document's cache entry replays its seed too.
				chromedp.Evaluate(`history.back(); true`, nil),
				chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
				chromedp.Evaluate(`history.forward(); true`, nil),
				chromedp.WaitVisible(tc.screen, chromedp.ByQuery),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &back),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if !soft {
				t.Fatal("the navigation reloaded the document; the test needs a soft swap")
			}
			if qty != tc.want {
				t.Errorf("quantity = %s after the swap, want the document's %s", qty, tc.want)
			}
			if cart != "3" {
				t.Errorf("global cart = %s, want the user's 3", cart)
			}
			if back != tc.want {
				t.Errorf("quantity = %s on the cached replay, want %s", back, tc.want)
			}
		})
	}
}

// The entry a cross-chain swap caches holds the document's seed, so a
// Back inside the new chain replays it: /c's entry replays only from a
// page that has its l:other layer, here /c2, and the leave capture
// carries /c's own seed forward.
func TestEnvelopeCrossChainEntryKeepsSeed(t *testing.T) {
	srv := envelopeSeedServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var onC2, back string
	var soft bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.__softMark = 1; true`, nil),
		chromedp.Click(`#goC`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-c`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`#goC2`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-c2`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &onC2),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`#screen-c`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &back),
		chromedp.Evaluate(`window.__softMark === 1`, &soft),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !soft {
		t.Fatal("a navigation reloaded the document; the test needs soft swaps")
	}
	if onC2 != "8" {
		t.Fatalf("quantity on /c2 = %s, want its document's 8", onC2)
	}
	if back != "7" {
		t.Errorf("Back to /c replayed quantity %s, want its document's 7", back)
	}
}
