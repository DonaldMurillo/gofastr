package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The action module's group registry (src/action.js) is a Map of Sets:
// the truth about what is bound. Members whose elements left the
// document are pruned when a sibling commits; a group navigated away
// from wholesale never sees that pass, so before the gofastr:navigate
// prune the Sets were the last strong reference to every detached
// button on the previous page — retained for the lifetime of the
// document. This test drives a REAL SPA navigation (the router
// intercepts the anchor, swaps the content cell, dispatches
// gofastr:navigate) and asserts the group set no longer holds the
// detached member, through the module's test-only _groupCount().
func TestActionPrunesGroupMembersOnNavigate(t *testing.T) {
	srv := startGroupRetentionSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__ready === true`) {
		t.Fatal("the action primitive never loaded")
	}
	if !pollTrue(ctx, `window.__gofastr.action._groupCount() === 1`) {
		t.Fatal("the grouped button never joined the group registry")
	}
	// Real SPA navigation: the runtime intercepts the anchor, swaps
	// main's content, THEN dispatches gofastr:navigate — by which time
	// the button is already detached, exactly the state the prune runs
	// against.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#nav`, chromedp.ByID),
		chromedp.WaitVisible(`#bmark`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !pollTrue(ctx, `window.__gofastr.action._groupCount() === 0`) {
		t.Fatal("the group registry still holds the detached button after gofastr:navigate")
	}
}

func startGroupRetentionSrv(t *testing.T) *httptest.Server {
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
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	page := func(body string) string {
		return `<!doctype html><html><head><title>group retention</title>` +
			// Inline route manifest: without it the router does not
			// intercept the anchor and the click becomes a full page load
			// (fresh document, window wiped). Same shape every SPA-nav
			// e2e test in this package uses.
			`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/b"}]</script>` +
			`</head><body>` + body +
			`<span id="ready">ready</span>` +
			`<script src="/__gofastr/runtime.js"></script>` +
			`<script>
(function () {
  window.__gofastr.loadModule('action').then(function () {
    var el = document.getElementById('g');
    if (el) window.__gofastr.action.bind(el, { endpoint: '/ok', group: 'plan',
      idle: document.getElementById('gi'), done: document.getElementById('gd') });
    window.__ready = true;
  });
})();
</script>` +
			`</body></html>`
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var body string
		if r.URL.Path == "/b" {
			body = `<main><p id="bmark">page B</p></main>`
		} else {
			body = `<main><button type="button" id="g" data-state="idle">
  <span id="gi">Follow</span><span id="gd" hidden>Following</span>
</button>
<a id="nav" href="/b">go to B</a></main>`
		}
		fmt.Fprint(w, page(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
