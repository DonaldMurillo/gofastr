package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Back to the first page of a visit restores its page-scoped signals,
// not just its markup. The first page's seed rides in <head>, outside
// the boot cache entry, so the replay kept page B's quantity=10 under
// page A's quantity=1 markup and the next increment showed 11. The
// host names the seed's page-scoped keys in #gofastr-signals-page; a
// global (cart) keeps the value the user gave it.
func TestBackRestoresFirstPageSignals(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Gofastr-Partial", "true")
		fmt.Fprint(w, `<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":10},"g":{"cart":0}}</script>`+
			`<h1 id="pageB">Page B</h1>`+
			`<span id="bqty" data-cui-signal="quantity">10</span>`+
			`<a id="toa" href="/">to A</a>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/b"}]</script>
  <script type="application/json" id="gofastr-signals">{"quantity":1,"cart":0,"note":"\u003c/script\u003e\u003cb id=pwn\u003ex\u003c/b\u003e"}</script>
  <script type="application/json" id="gofastr-signals-page">["quantity","note"]</script>
</head><body>
  <main role="main" tabindex="-1">
    <h1 id="pageA">Page A</h1>
    <span id="qty" data-cui-signal="quantity">1</span>
    <button id="inc" data-cui-signal-inc="quantity">+</button>
    <button id="cart" data-cui-signal-inc="cart">add</button>
    <a id="tob" href="/b">to B</a>
  </main>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var qtyBack, shownAfterInc, cart, note string
	var pwn bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// A global the user changed on A must survive the round trip.
		chromedp.Click(`#cart`, chromedp.ByID),
		chromedp.Click(`#tob`, chromedp.ByID),
		chromedp.WaitVisible(`#pageB`, chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#pageA`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &qtyBack),
		chromedp.Evaluate(`String(__gofastr.getSignal('note'))`, &note),
		chromedp.Evaluate(`!!document.getElementById('pwn')`, &pwn),
		chromedp.Click(`#inc`, chromedp.ByID),
		chromedp.Text(`#qty`, &shownAfterInc, chromedp.ByID),
		chromedp.Evaluate(`String(__gofastr.getSignal('cart'))`, &cart),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if qtyBack != "1" {
		t.Errorf("after Back to A, quantity = %s, want A's seed 1", qtyBack)
	}
	if shownAfterInc != "2" {
		t.Errorf("increment after Back shows %q, want 2", shownAfterInc)
	}
	if cart != "1" {
		t.Errorf("global cart = %s after the round trip, want the user's 1", cart)
	}
	// The replayed island is parsed from a string: a value holding
	// "</script>" must stay a value, never end the island into markup.
	if pwn {
		t.Error("a seed value's </script> ended the replayed island and its tail became markup")
	}
	if want := `</script><b id=pwn>x</b>`; note != want {
		t.Errorf("note = %q after Back, want %q", note, want)
	}
}

// A navigation that swaps in a fetched WHOLE document applies that
// document's page-scoped seed, as a partial's island would: the head
// seed was read once at boot, so the store kept the previous page's
// quantity under the new page's markup. Two branches fetch documents:
// the cross-chain swap (/c brings a layout chain A lacks) and a full
// answer to a partial request (/d, a host that ignores the header).
func TestFullDocNavAppliesPageSignals(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	doc := func(seed, body string) string {
		return `<!doctype html><html><head><title>t</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/c","layouts":["l:site"]},{"path":"/d"}]</script>
  <script type="application/json" id="gofastr-signals">` + seed + `</script>
  <script type="application/json" id="gofastr-signals-page">["quantity"]</script>
</head><body>` + body + `<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`{"quantity":7,"cart":0}`,
			`<div data-cui-layout="site" data-cui-layout-key="l:site">`+
				`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">`+
				`<h1 id="pageC">Page C</h1><span data-cui-signal="quantity">7</span></main></div>`))
	})
	mux.HandleFunc("/d", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`{"quantity":5,"cart":0}`,
			`<main role="main" tabindex="-1"><h1 id="pageD">Page D</h1><span data-cui-signal="quantity">5</span></main>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, doc(`{"quantity":1,"cart":0}`,
			`<main role="main" tabindex="-1"><h1 id="pageA">Page A</h1>`+
				`<span data-cui-signal="quantity">1</span>`+
				`<button id="cart" data-cui-signal-inc="cart">add</button>`+
				`<a id="toc" href="/c">to C</a><a id="tod" href="/d">to D</a></main>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, tc := range []struct{ link, page, want string }{
		{"#toc", "#pageC", "7"},
		{"#tod", "#pageD", "5"},
	} {
		t.Run(tc.page, func(t *testing.T) {
			ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
			var qty, cart string
			var soft bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Evaluate(`window.__softMark = 1`, nil),
				chromedp.Click(`#cart`, chromedp.ByID),
				chromedp.Click(tc.link, chromedp.ByQuery),
				chromedp.WaitVisible(tc.page, chromedp.ByQuery),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`window.__softMark === 1`, &soft),
				chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &qty),
				chromedp.Evaluate(`String(__gofastr.getSignal('cart'))`, &cart),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if !soft {
				t.Fatal("the navigation reloaded the document; the test needs a soft swap")
			}
			if qty != tc.want {
				t.Errorf("quantity = %s after the swap, want the document's %s", qty, tc.want)
			}
			if cart != "1" {
				t.Errorf("global cart = %s, want the user's 1", cart)
			}
		})
	}
}
