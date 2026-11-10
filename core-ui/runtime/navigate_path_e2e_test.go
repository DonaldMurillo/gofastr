package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The router keys on path+search: its current path, the screen cache
// and gofastr:navigate's detail.path. A link click has always resolved
// its href to that form and pushed the #fragment separately. These tests
// hold the other ways in to the same rule: __gofastr.navigate(), a
// data-cui-spa GET form (which hands navigate() its action's fragment)
// and the X-Gofastr-Location redirect leg, in core's navigator and the
// envelope module's. Each used to hand loadPage the string as written,
// so a relative "?p=2" was one cache key on every page and a fragment
// made the page read as another one.

// TestNavigateRelativeQueryKeysByPage: navigate('?p=2') is this page's
// second page. Made on /a and later on /b, the second call must fetch
// and show /b?p=2; the raw "?p=2" key replayed /a's screen under it.
func TestNavigateRelativeQueryKeysByPage(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	routes := `<script type="application/json" id="gofastr-routes">[{"path":"/a"},{"path":"/b"}]</script>`
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := "a"
		if r.URL.Path == "/b" {
			name = "b"
		}
		body := `<h1 id="screen" data-page="` + name + `" data-q="` + r.URL.RawQuery + `">` + name + ` ` + r.URL.RawQuery + `</h1><a id="to-b" href="/b">B</a>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
			`<main role="main" tabindex="-1">`+body+`</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got, current string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen[data-page="a"]`, chromedp.ByQuery),
		// On /a: the page's own second page.
		chromedp.Evaluate(`window.__gofastr.navigate('?p=2'); true`, nil),
		chromedp.WaitVisible(`#screen[data-page="a"][data-q="p=2"]`, chromedp.ByQuery),
		chromedp.Click(`#to-b`, chromedp.ByID),
		chromedp.WaitVisible(`#screen[data-page="b"]`, chromedp.ByQuery),
		// On /b: its own second page, by the same relative spelling.
		chromedp.Evaluate(`window.__gofastr.navigate('?p=2'); true`, nil),
		chromedp.Poll(`location.pathname + location.search === '/b?p=2'`, nil),
		chromedp.WaitVisible(`#screen[data-q="p=2"]`, chromedp.ByQuery),
		chromedp.Evaluate(`location.pathname + location.search + ' shows page ' + document.getElementById('screen').getAttribute('data-page')`, &got),
		chromedp.Evaluate(`window.__gofastr.currentPath`, &current),
	); err != nil {
		t.Fatal(err)
	}
	if got != "/b?p=2 shows page b" {
		t.Fatalf("navigate('?p=2') on /b after the same call on /a: %s (want /b?p=2 shows page b)", got)
	}
	if current != "/b?p=2" {
		t.Errorf("after navigate('?p=2') on /b the router's current path is %q, want /b?p=2", current)
	}
}

// TestNavigateFragmentKeepsPageScopedWidgets: navigate('/b#frag') lands
// on the page /b. gofastr:navigate names /b, so the catalog re-fetch asks
// for page=/b and a widget scoped to /b is offered there. With the
// fragment left in the path the catalog was asked for "/b#frag" and the
// widget was missing.
func TestNavigateFragmentKeepsPageScopedWidgets(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var pages []string
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	// The server side of .Pages("/b"): the widget is offered on /b only.
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		mu.Lock()
		pages = append(pages, page)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if page != "/b" {
			fmt.Fprint(w, `[]`)
			return
		}
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"scoped","chromePath":"/chrome/scoped"}}]`)
	})
	serveTwoScreens(mux, "/b")
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var synced, navPath, hash string
	var inCatalog bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.__synced = new Promise((done) => addEventListener('gofastr:widgets-synced', (e) => { if (e.detail.path !== '/a') done(e.detail.path); }));
			addEventListener('gofastr:navigate', (e) => { window.__navPath = e.detail.path; }); true`, nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/b#frag'); true`, nil),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`window.__synced`, &synced, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		chromedp.Evaluate(`!!(window.__gofastr._widgetCatalog && window.__gofastr._widgetCatalog.scoped)`, &inCatalog),
		chromedp.Evaluate(`window.__navPath`, &navPath),
		chromedp.Evaluate(`location.hash`, &hash),
	); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if synced != "/b" || !inCatalog {
		t.Fatalf("after navigate('/b#frag'): widgets-synced path %q (want /b), scoped widget in catalog %v (want true); catalog pages asked %q", synced, inCatalog, pages)
	}
	if navPath != "/b" {
		t.Errorf("gofastr:navigate detail.path = %q after navigate('/b#frag'), want /b", navPath)
	}
	// The fragment is not dropped: it rides the URL, where the post-swap
	// scroll reads it.
	if hash != "#frag" {
		t.Errorf("location.hash = %q after navigate('/b#frag'), want #frag", hash)
	}
}

// fragmentSite serves /a and /b, counting the requests for /b. /a
// reaches /b#frag three ways: a link to /old, whose partial answers with
// X-Gofastr-Location: /b#frag (uihost's shape for a policy redirect: 200
// plus the header), a data-cui-spa GET form whose action is /b#frag, and
// whatever script the test runs. /b holds a field and an in-page #other
// link. With envelope set the layout holds an outlet, so the envelope
// module's navigator runs instead of core's.
func fragmentSite(t *testing.T, envelope bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var fetchesB atomic.Int32
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	outlet := ""
	if envelope {
		outlet = `<div data-cui-outlet="l:site#side">SIDE</div>`
	}
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/b" {
				fetchesB.Add(1)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Title", "t")
				w.Header().Set("X-Gofastr-Swap", "l:site")
				if envelope {
					w.Header().Set("X-Gofastr-Envelope", "2")
					fmt.Fprint(w, `<template data-cui-fill="l:site">`+body+`</template>`)
					return
				}
				fmt.Fprint(w, body)
				return
			}
			fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
				`<script type="application/json" id="gofastr-routes">`+
				`[{"path":"/a","layouts":["l:site"]},{"path":"/old","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}]</script>`+
				`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">`+outlet+
				`<main role="main" tabindex="-1" data-cui-layout-slot="l:site">`+body+
				`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
		}
	}
	mux.HandleFunc("/a", page(`<h1 id="screen-a">A</h1><a id="to-old" href="/old">old</a>`+
		`<form id="f" data-cui-spa method="get" action="/b#frag"><input name="q" value="x"><button id="go" type="submit">Go</button></form>`))
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gofastr-Location", "/b#frag")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	})
	mux.HandleFunc("/b", page(`<h1 id="screen-b">B</h1><input id="typed"><a id="jump" href="#other">jump</a>`+
		`<div style="height:1500px"></div><h2 id="frag">frag</h2><div style="height:1500px"></div><h2 id="other">other</h2><div style="height:1500px"></div>`))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &fetchesB
}

// TestFragmentArrivalThenAnchorJumpKeepsPage: the reader lands on
// /b#frag, types into a field, and follows an in-page "#other" link.
// That is the browser's own jump on the page already shown: /b is not
// requested again and the field keeps its text. The router's popstate
// compares location.pathname+search with its current path; a fragment
// left in that path made the jump re-fetch /b and swap the screen out
// from under the reader. The arrival also scrolls to its fragment.
func TestFragmentArrivalThenAnchorJumpKeepsPage(t *testing.T) {
	arrivals := []struct {
		name   string
		arrive chromedp.Action
		url    string // after the jump
	}{
		{"navigate", chromedp.Evaluate(`window.__gofastr.navigate('/b#frag'); true`, nil), "/b#other"},
		{"spa-get-form", chromedp.Click(`#go`, chromedp.ByID), "/b?q=x#other"},
		{"redirect-location", chromedp.Click(`#to-old`, chromedp.ByID), "/b#other"},
	}
	for _, nav := range []struct {
		name     string
		envelope bool
	}{{"core", false}, {"envelope", true}} {
		for _, a := range arrivals {
			t.Run(nav.name+"/"+a.name, func(t *testing.T) {
				srv, fetchesB := fragmentSite(t, nav.envelope)
				ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
				var typed, href, current, arrived string
				var loaded bool
				var arrivedY, fragY float64
				if err := chromedp.Run(ctx,
					chromedp.Navigate(srv.URL+"/a"),
					chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
					a.arrive,
					chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
					// The arrival's own scroll to #frag settles first.
					chromedp.Poll(`Math.abs(document.getElementById('frag').getBoundingClientRect().top) < 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
					chromedp.Evaluate(`window.scrollY`, &arrivedY),
					chromedp.Evaluate(`document.getElementById('frag').getBoundingClientRect().top + window.scrollY`, &fragY),
					chromedp.Evaluate(`location.pathname + location.search + location.hash`, &arrived),
					chromedp.Evaluate(`window.__gofastr.currentPath`, &current),
					chromedp.Evaluate(`!!window.__gofastr._navHooks.envelope`, &loaded),
					chromedp.Evaluate(`document.getElementById('typed').value = 'draft'; true`, nil),
					chromedp.Evaluate(`document.getElementById('jump').click(); true`, nil),
					chromedp.Poll(`location.hash === '#other'`, nil),
					// Room for a (wrong) re-fetch and swap to land.
					chromedp.Sleep(700*time.Millisecond),
					chromedp.Evaluate(`(document.getElementById('typed') || {}).value || ''`, &typed),
					chromedp.Evaluate(`location.pathname + location.search + location.hash`, &href),
				); err != nil {
					t.Fatalf("chromedp: %v (arrived at %q, current path %q)", err, arrived, current)
				}
				if loaded != nav.envelope {
					t.Fatalf("envelope module loaded = %v, want %v", loaded, nav.envelope)
				}
				if got := fetchesB.Load(); got != 1 || typed != "draft" {
					t.Fatalf("an in-page #other jump on /b: /b fetched %d times (want 1), typed value %q (want \"draft\"), url %s, router path on arrival %q", got, typed, href, current)
				}
				if href != a.url {
					t.Errorf("after the jump the URL is %s, want %s", href, a.url)
				}
				if d := arrivedY - fragY; arrivedY < 1000 || d < -2 || d > 2 {
					t.Errorf("the arrival at %s left scrollY = %v, want #frag's offset %v", arrived, arrivedY, fragY)
				}
			})
		}
	}
}

// TestRedirectLocationOffOriginLoadsNothing: the redirect leg resolves a
// same-origin target to its path, and only that. A target on another
// origin stays as sent, so the navigator's origin gate still refuses it;
// resolving it first would have fetched this origin's page at the same
// path (/b here) and shown it under the requested URL.
func TestRedirectLocationOffOriginLoadsNothing(t *testing.T) {
	for _, nav := range []struct {
		name     string
		envelope bool
	}{{"core", false}, {"envelope", true}} {
		t.Run(nav.name, func(t *testing.T) {
			js, err := RuntimeJS()
			if err != nil {
				t.Fatal(err)
			}
			var fetchesB atomic.Int32
			mux := http.NewServeMux()
			handleRuntimeModules(t, mux)
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, js)
			})
			outlet := ""
			if nav.envelope {
				outlet = `<div data-cui-outlet="l:site#side">SIDE</div>`
			}
			mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
					`<script type="application/json" id="gofastr-routes">`+
					`[{"path":"/a","layouts":["l:site"]},{"path":"/old","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}]</script>`+
					`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">`+outlet+
					`<main role="main" tabindex="-1" data-cui-layout-slot="l:site"><h1 id="screen-a">A</h1><a id="to-old" href="/old">old</a>`+
					`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`)
			})
			var asked atomic.Int32
			mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
				asked.Add(1)
				// A closed port on another origin: nothing answers there.
				w.Header().Set("X-Gofastr-Location", "http://127.0.0.1:1/b#frag")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			})
			mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
				fetchesB.Add(1)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("X-Gofastr-Partial", "true")
				w.Header().Set("X-Gofastr-Swap", "l:site")
				fmt.Fprint(w, `<h1 id="screen-b">B</h1>`)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var shownA bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/a"),
				chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
				chromedp.Click(`#to-old`, chromedp.ByID),
				chromedp.ActionFunc(func(context.Context) error {
					for i := 0; i < 100 && asked.Load() == 0; i++ {
						time.Sleep(50 * time.Millisecond)
					}
					return nil
				}),
				// Room for a (wrong) load of this origin's /b to land.
				chromedp.Sleep(700*time.Millisecond),
				chromedp.Evaluate(`!!document.getElementById('screen-a') && !document.getElementById('screen-b')`, &shownA),
			); err != nil {
				t.Fatal(err)
			}
			if asked.Load() == 0 {
				t.Fatal("setup: the link never requested /old")
			}
			if got := fetchesB.Load(); got != 0 || !shownA {
				t.Fatalf("an off-origin X-Gofastr-Location: this origin's /b fetched %d times (want 0), page A still shown = %v (want true)", got, shownA)
			}
		})
	}
}

// TestNavigateRelativeQueryStaysSoftUnderLocalePrefix: navigate('?p=2')
// on /es/list is /es/list?p=2, the same locale and the same document.
// The document-boundary test reads the destination's locale off its
// path; handed the string as written it read "?p=2" as the default
// locale and loaded a whole new document.
func TestNavigateRelativeQueryStaysSoftUnderLocalePrefix(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/es/list", func(w http.ResponseWriter, r *http.Request) {
		body := `<h1 id="screen" data-q="` + r.URL.RawQuery + `">lista ` + r.URL.RawQuery + `</h1>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, `<!doctype html><html lang="es"><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/list"}]</script>`+
			`<script type="application/json" id="gofastr-locales">{"prefixes":["es"]}</script></head><body>`+
			`<main role="main" tabindex="-1">`+body+`</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var sameDoc bool
	var at string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/list"),
		chromedp.WaitVisible(`#screen`, chromedp.ByID),
		chromedp.Evaluate(`window.__stamp = 1; window.__gofastr.navigate('?p=2'); true`, nil),
		chromedp.WaitVisible(`#screen[data-q="p=2"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__stamp === 1`, &sameDoc),
		chromedp.Evaluate(`location.pathname + location.search`, &at),
	); err != nil {
		t.Fatal(err)
	}
	if at != "/es/list?p=2" || !sameDoc {
		t.Fatalf("navigate('?p=2') on /es/list: at %s (want /es/list?p=2), same document = %v (want true)", at, sameDoc)
	}
}

// TestNavigateRelativePathResolvesOnce: navigate('sub/b') on /dir/a is
// /dir/sub/b. navigate() resolves its argument once, before it writes
// history; resolved again after the write (against the URL the write
// just made) the same string named /dir/sub/sub/b.
func TestNavigateRelativePathResolvesOnce(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var asked []string
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/dir/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if r.URL.Path != "/dir/a" && r.URL.Path != "/dir/sub/b" {
			http.NotFound(w, r)
			return
		}
		body := `<h1 id="screen" data-path="` + r.URL.Path + `">` + r.URL.Path + `</h1>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/dir/a"},{"path":"/dir/sub/b"}]</script></head><body>`+
			`<main role="main" tabindex="-1">`+body+`</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var at, current string
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/dir/a"),
		chromedp.WaitVisible(`#screen`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.navigate('sub/b'); true`, nil),
		chromedp.Poll(`document.querySelector('#screen[data-path="/dir/sub/b"]') !== null`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`location.pathname`, &at),
		chromedp.Evaluate(`window.__gofastr.currentPath`, &current),
	)
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		t.Fatalf("navigate('sub/b') on /dir/a never showed /dir/sub/b: %v; paths requested: %q", err, asked)
	}
	if at != "/dir/sub/b" || current != "/dir/sub/b" {
		t.Fatalf("navigate('sub/b') on /dir/a: URL %s, router path %s (want /dir/sub/b for both); paths requested: %q", at, current, asked)
	}
}
