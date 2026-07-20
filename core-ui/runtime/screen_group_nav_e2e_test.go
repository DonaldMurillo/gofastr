package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestGroupSiblingNavPreservesShell reproduces issue #89: a ScreenGroup
// registered under a default layout carries the INNER group layout name in
// the route manifest ("studio") but the OUTERMOST default layout name in the
// [data-fui-layout] shell marker ("main"). layoutWillChange compares the two,
// so sibling nav inside the group always took the cross-layout branch and
// replaced the whole shell — rebuilding the group's persistent chrome (the
// tab strip) on every click. The fix: a shared [data-fui-screen-group]
// between the two paths proves the shell is shared, so it's an in-shell swap.
func TestGroupSiblingNavPreservesShell(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	// shell renders the default-layout shell (data-fui-layout="main") wrapping
	// the /studio/ group (data-fui-screen-group="/studio/") whose persistent
	// chrome is the tab strip; only .layout-content differs between screens.
	shell := func(content string) string {
		return `<!doctype html><html><head>` +
			`<script type="application/json" id="gofastr-routes">` +
			`[{"path":"/studio/","layout":"studio"},{"path":"/studio/read","layout":"studio"}]` +
			`</script></head><body>` +
			`<div data-fui-layout="main">` +
			`<header id="siteheader">site</header>` +
			`<div data-fui-screen-group="/studio/">` +
			`<nav id="grouptabs"><a id="tab-create" href="/studio/">Create</a>` +
			`<a id="tab-read" href="/studio/read">Read</a></nav>` +
			`<main role="main" tabindex="-1" class="layout-content">` + content + `</main>` +
			`</div></div>` +
			`<span id="ready">ready</span>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/studio/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, shell(`<h1 id="create-screen">Create</h1>`))
	})
	mux.HandleFunc("/studio/read", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "Read")
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
	var readVisible bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/studio/"),
		chromedp.WaitVisible(`#create-screen`, chromedp.ByID),
		// Stamp the persistent tab strip; a shell swap replaces the node
		// (an imported copy) and loses the stamp, an in-shell swap keeps it.
		chromedp.Evaluate(`document.getElementById('grouptabs').dataset.stamp = 'kept'`, nil),
		chromedp.Click(`#tab-read`, chromedp.ByID),
		chromedp.WaitVisible(`#read-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('grouptabs').dataset.stamp === 'kept'`, &tabsSurvived),
		chromedp.Evaluate(`!!document.getElementById('read-screen')`, &readVisible),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if !readVisible {
		t.Fatal("read screen did not render after sibling nav")
	}
	if !tabsSurvived {
		t.Error("group chrome (tab strip) was rebuilt on sibling nav — shell swapped instead of in-shell content swap")
	}
}

// TestGroupSlashlessIndexNavPreservesShell covers the trailing-slash matching
// fix: a group index registered at the slashless path (/studio) must still be
// recognized as inside the group (prefix "/studio/") so its first sibling nav
// gets the in-shell swap, not a full shell rebuild (#89, minor follow-on).
func TestGroupSlashlessIndexNavPreservesShell(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	shell := func(content string) string {
		return `<!doctype html><html><head>` +
			`<script type="application/json" id="gofastr-routes">` +
			`[{"path":"/studio","layout":"studio"},{"path":"/studio/read","layout":"studio"}]` +
			`</script></head><body>` +
			`<div data-fui-layout="main">` +
			`<header id="siteheader">site</header>` +
			`<div data-fui-screen-group="/studio/">` +
			`<nav id="grouptabs"><a id="tab-create" href="/studio">Create</a>` +
			`<a id="tab-read" href="/studio/read">Read</a></nav>` +
			`<main role="main" tabindex="-1" class="layout-content">` + content + `</main>` +
			`</div></div>` +
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
		t.Error("slashless group index nav rebuilt the shell — trailing-slash prefix match failed")
	}
}

// TestCrossLayoutNavCopiesSSEMeta pins the #112 rollover-recovery half
// that lives on the cross-layout branch: a layout-changing navigation
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
		`[{"path":"/","layout":"marketing"},{"path":"/app","layout":"app"}]` +
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
			`</head><body><div data-fui-layout="marketing">`+
			`<main role="main"><h1 id="home">Home</h1><a id="to-app" href="/app">App</a></main>`+
			`</div><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, _ *http.Request) {
		// The full fetch's head carries the CURRENT (re-minted) session.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>`+routes+
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-NEW">`+
			`</head><body><div data-fui-layout="app">`+
			`<main role="main"><h1 id="app-screen">App</h1></main>`+
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
		t.Errorf("live SSE meta = %q after cross-layout nav, want the fetched head's sess-NEW — rollover recovery lost on the full-fetch branch", meta)
	}
}
