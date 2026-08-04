package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestGroupSlashlessIndexNavPreservesShell covers the trailing-slash
// matching half of #89: a group index registered at the slashless path
// (/studio) must resolve to the same manifest entry — and therefore the
// same layout chain — as its slashed siblings, so its first sibling nav
// swaps at the shared group layer instead of rebuilding the shell.
func TestGroupSlashlessIndexNavPreservesShell(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	shell := func(content string) string {
		return `<!doctype html><html><head>` +
			`<script type="application/json" id="gofastr-routes">` +
			`[{"path":"/studio","layouts":["l:main","g:/studio/:studio"]},` +
			`{"path":"/studio/read","layouts":["l:main","g:/studio/:studio"]}]` +
			`</script></head><body>` +
			`<div data-fui-layout="main" data-fui-layout-key="l:main">` +
			`<header id="siteheader">site</header>` +
			`<main role="main" tabindex="-1" data-fui-layout-slot="l:main">` +
			`<div class="fui-screen-group" data-fui-screen-group="/studio/">` +
			`<div data-fui-layout="studio" data-fui-layout-key="g:/studio/:studio">` +
			`<nav id="grouptabs"><a id="tab-create" href="/studio">Create</a>` +
			`<a id="tab-read" href="/studio/read">Read</a></nav>` +
			`<div class="layout-content" tabindex="-1" data-fui-layout-slot="g:/studio/:studio">` + content + `</div>` +
			`</div></div></main></div>` +
			`<span id="ready">ready</span>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/studio", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, shell(`<h1 id="create-screen">Create</h1>`))
	})
	mux.HandleFunc("/studio/read", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "Read")
			w.Header().Set("X-Gofastr-Swap", "g:/studio/:studio")
			fmt.Fprint(w, `<h1 id="read-screen">Read</h1>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, shell(`<h1 id="read-screen">Read</h1>`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var tabsSurvived bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/studio"),
		chromedp.WaitVisible(`#create-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('grouptabs').dataset.stamp = 'kept'`, nil),
		chromedp.Click(`#tab-read`, chromedp.ByID),
		chromedp.WaitVisible(`#read-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('grouptabs').dataset.stamp === 'kept'`, &tabsSurvived),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if !tabsSurvived {
		t.Error("slashless group index nav rebuilt the shell — trailing-slash route lookup failed")
	}
}

// TestCrossLayoutNavCopiesSSEMeta pins the #112 rollover-recovery half
// that lives on the cross-chain branch: a chain-changing navigation
// full-fetches the destination, and the freshly fetched head's
// gofastr-sse meta (rendered under the CURRENT session — re-minted if
// the old token died) must be copied onto the live document's meta.
// Without the copy, a server restart leaves the SSE reconnect loop
// pinned to the dead stream id until a hard reload.
func TestCrossLayoutNavCopiesSSEMeta(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	routes := `<script type="application/json" id="gofastr-routes">` +
		`[{"path":"/","layouts":["l:marketing"]},{"path":"/app","layouts":["l:app"]}]` +
		`</script>`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+routes+
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-OLD">`+
			`</head><body><div data-fui-layout="marketing" data-fui-layout-key="l:marketing">`+
			`<main role="main" data-fui-layout-slot="l:marketing"><h1 id="home">Home</h1><a id="to-app" href="/app">App</a></main>`+
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, _ *http.Request) {
		// The full fetch's head carries the CURRENT (re-minted) session.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+routes+
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-NEW">`+
			`</head><body><div data-fui-layout="app" data-fui-layout-key="l:app">`+
			`<main role="main" data-fui-layout-slot="l:app"><h1 id="app-screen">App</h1></main>`+
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var meta string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#home`, chromedp.ByID),
		chromedp.Click(`#to-app`, chromedp.ByID),
		chromedp.WaitVisible(`#app-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content')`, &meta),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if meta != "/__gofastr/sse?session=sess-NEW" {
		t.Errorf("live SSE meta = %q after cross-chain nav, want the fetched head's sess-NEW — rollover recovery lost on the full-fetch branch", meta)
	}
}
