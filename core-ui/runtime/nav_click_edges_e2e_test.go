package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// Click arms of the router that used to hand the click back to the
// browser, which then loaded a whole document (hard rule 4).

func clickEdgeVariants(t *testing.T, run func(t *testing.T, r *dcRig)) {
	for _, v := range []struct {
		name  string
		plain bool
	}{{"plain", true}, {"envelope", false}} {
		t.Run(v.name, func(t *testing.T) { run(t, newDoubleClickRig(t, false, v.plain)) })
	}
}

// TestRepeatClickOnInFlightLinkStaysSoft: the first click pushes /b and
// moves currentPath there; a second click on the same link while /b's
// answer is still held met `fullPath === currentPath` and returned
// before preventDefault, so the browser loaded /b as a whole document.
// The repeat click is a no-op: one request, the page stamp survives.
func TestRepeatClickOnInFlightLinkStaysSoft(t *testing.T) {
	clickEdgeVariants(t, func(t *testing.T, r *dcRig) {
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		r.hold("/b")
		if err := chromedp.Run(ctx,
			chromedp.Navigate(r.srv.URL+"/"),
			chromedp.WaitVisible(`#goB`, chromedp.ByID),
			chromedp.Evaluate(`window.__stamp = 'alive'`, nil),
			chromedp.Click(`#goB`, chromedp.ByID),
			chromedp.Poll(`location.pathname === '/b'`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Click(`#goB`, chromedp.ByID),
			chromedp.Sleep(400*time.Millisecond),
		); err != nil {
			t.Fatalf("chromedp: %v", err)
		}
		r.release("/b")
		var stamp, main string
		if err := chromedp.Run(ctx,
			chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-B')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
			chromedp.Evaluate(`window.__stamp || 'gone'`, &stamp),
			chromedp.Evaluate(`document.getElementById('main').textContent`, &main),
		); err != nil {
			t.Fatalf("after release: %v", err)
		}
		if stamp != "alive" {
			t.Errorf("page stamp = %q after a repeat click on the in-flight link: the browser loaded a whole document", stamp)
		}
		r.mu.Lock()
		hits := r.hits["/b"]
		r.mu.Unlock()
		if hits != 1 {
			t.Errorf("/b navigate requests = %d, want 1 (the repeat click is a no-op)", hits)
		}
	})
}

// TestLinkToCurrentPageStaysSoft: a link naming the page already shown
// (no fragment) was handed to the browser, which reloaded the document.
func TestLinkToCurrentPageStaysSoft(t *testing.T) {
	clickEdgeVariants(t, func(t *testing.T, r *dcRig) {
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		var stamp, path string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(r.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-A')`, new(bool), chromedp.WithPollingTimeout(10*time.Second)),
			chromedp.Evaluate(`window.__stamp = 'alive'`, nil),
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Sleep(600*time.Millisecond),
			chromedp.Evaluate(`window.__stamp || 'gone'`, &stamp),
			chromedp.Evaluate(`location.pathname`, &path),
		); err != nil {
			t.Fatalf("chromedp: %v", err)
		}
		if stamp != "alive" || path != "/a" {
			t.Errorf("stamp %q path %q after clicking a link to the current page, want a no-op on /a", stamp, path)
		}
	})
}

// TestPreventedLinkClickDoesNotNavigate: a page listener on the anchor
// itself runs before the router's document listener; when it calls
// preventDefault the router must stand down, as the browser would.
func TestPreventedLinkClickDoesNotNavigate(t *testing.T) {
	clickEdgeVariants(t, func(t *testing.T, r *dcRig) {
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		var path, main string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(r.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Evaluate(`document.getElementById('goA').addEventListener('click', (e) => e.preventDefault())`, nil),
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Sleep(600*time.Millisecond),
			chromedp.Evaluate(`location.pathname`, &path),
			chromedp.Evaluate(`document.getElementById('main').textContent`, &main),
		); err != nil {
			t.Fatalf("chromedp: %v", err)
		}
		r.mu.Lock()
		hits := r.hits["/a"]
		r.mu.Unlock()
		if path != "/" || hits != 0 || !strings.Contains(main, "HOME") {
			t.Errorf("path %q, /a requests %d, main %q: a prevented click still navigated", path, hits, main)
		}
	})
}

// TestSignalLinkStillNavigates: the runtime's own signal delegate
// prevents the click it serves (signals is composed before nav, so it
// runs first), and that prevent is not a page listener's. A link that
// also sets a signal (interactive.SetLocal on an <a href>) sets it and
// navigates, as it did before the router honoured page prevents.
func TestSignalLinkStillNavigates(t *testing.T) {
	clickEdgeVariants(t, func(t *testing.T, r *dcRig) {
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		var sig string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(r.srv.URL+"/"),
			chromedp.WaitVisible(`#goA`, chromedp.ByID),
			chromedp.Evaluate(`document.getElementById('goA').setAttribute('data-cui-signal-set', 'menu:closed')`, nil),
			chromedp.Click(`#goA`, chromedp.ByID),
			chromedp.Poll(`location.pathname === '/a'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`String(window.__gofastr.signal('menu'))`, &sig),
		); err != nil {
			t.Fatalf("a link carrying a signal mutation did not navigate: %v", err)
		}
		if sig != "closed" {
			t.Errorf("signal menu = %q, want closed", sig)
		}
	})
}

// TestBackAfterHashJumpRestoresScroll: manual scrollRestoration means
// the browser no longer restores the position a Back across an in-page
// #anchor jump returns to; the router's popstate saw the same path and
// did nothing, leaving the reader at the section.
func TestBackAfterHashJumpRestoresScroll(t *testing.T) {
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
	mux.HandleFunc("/doc", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/doc"}]</script>`+
			`</head><body><main role="main" tabindex="-1">`+
			`<a id="jump" style="position:fixed;top:4px;right:4px" href="#sec">jump</a>`+
			`<a id="jump-named" style="position:fixed;top:40px;right:4px" href="#named">named</a>`+
			`<div style="height:2500px"></div><h2 id="sec">section</h2><div style="height:1500px"></div>`+
			`<a name="named">named target</a><div style="height:4000px"></div>`+
			`</main><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800), chromedptest.Timeout(60*time.Second))
	var atSec, back, atNamed float64
	var hash string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/doc"),
		chromedp.WaitVisible(`#jump`, chromedp.ByID),
		chromedp.Evaluate(`window.scrollTo(0, 700)`, nil),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Click(`#jump`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &atSec),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Poll(`location.hash === ''`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &back),
		chromedp.Evaluate(`location.hash`, &hash),
		// The browser's own jump to a target the runtime cannot resolve
		// by id stays where the browser put it.
		chromedp.Click(`#jump-named`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`window.scrollY`, &atNamed),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if atNamed < 3500 {
		t.Errorf("scrollY after a jump to <a name=named> = %v, want the target (~4000): the popstate handler moved the browser's own fragment scroll", atNamed)
	}
	if atSec < 2000 {
		t.Fatalf("scrollY after the #sec jump = %v, want the section (~2500): the scenario is vacuous", atSec)
	}
	if back < 650 || back > 750 {
		t.Errorf("scrollY after Back = %v, want ~700 (the position the hash jump left)", back)
	}
}

// TestNavSeedPrototypeNamedKey: a seed key that names an
// Object.prototype member (valueOf) is an ordinary signal. The truthy
// store[k] test read the inherited function: a page seed wrote .value
// onto Object.prototype.valueOf, and a global seed was never created.
func TestNavSeedPrototypeNamedKey(t *testing.T) {
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
	routes := `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/p"},{"path":"/g"}]</script>`
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "seed")
			seed := `{"p":{"valueOf":"PAGE"}}`
			if r.URL.Path == "/g" {
				seed = `{"g":{"toString":"GLOBAL"}}`
			}
			fmt.Fprintf(w, `<p id="landed%s">landed</p><script type="application/json" id="gofastr-signals-partial">%s</script>`,
				strings.TrimPrefix(r.URL.Path, "/"), seed)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
			`<a id="toP" href="/p">p</a><a id="toG" href="/g">g</a><main>home</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var polluted bool
	var page, global string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#toP`, chromedp.ByID),
		chromedp.Click(`#toP`, chromedp.ByID),
		chromedp.WaitVisible(`#landedp`, chromedp.ByID),
		chromedp.Click(`#toG`, chromedp.ByID),
		chromedp.WaitVisible(`#landedg`, chromedp.ByID),
		chromedp.Evaluate(`'value' in Object.prototype.valueOf`, &polluted),
		chromedp.Evaluate(`String(window.__gofastr.signal('valueOf'))`, &page),
		chromedp.Evaluate(`String(window.__gofastr.signal('toString'))`, &global),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if polluted {
		t.Error("a page seed key valueOf wrote .value onto Object.prototype.valueOf")
	}
	if page != "PAGE" || global != "GLOBAL" {
		t.Errorf("signals valueOf=%q toString=%q, want PAGE and GLOBAL", page, global)
	}
}
