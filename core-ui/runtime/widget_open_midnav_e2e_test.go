package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// The mirror of TestStaleChromeSkipsNextPage: a widget opened WHILE a
// navigation is already in flight belongs to the page that navigation
// lands on, so the navigation committing under the open must not drop
// it. The reader clicked /b, then clicked a shell trigger (the bell in
// a header both pages share) before /b painted; the commit fires
// gofastr:navigate, which swaps the chrome cache, and the open used to
// read that swap as "the reader left" and bail without a word. The
// flake it caused: TestTrackerBellPopoverE2E, whose bell click lands
// between the issue link's pushState and the envelope commit on a
// loaded CI runner. Each wait the open makes (the chrome fetch, the
// deep-link module load) is held across the commit in turn. The
// widgetlinks case also pins the catalog re-fetch the commit triggers:
// its _syncDeepLinks reads a URL the open has not written its key to
// yet, and closing the widget there is not the URL saying "closed".
// A modal opened mid-navigation whose chrome lands BEFORE the commit
// stays too: the commit closes the modals of the page being left, and
// this one was asked for on the page it lands on (whether it survived
// used to depend on which of the two answers came first).
func TestOpenDuringNavigationSurvivesCommit(t *testing.T) {
	t.Run("chrome held", func(t *testing.T) { midNavOpenCase(t, "/chrome/late", false) })
	t.Run("widgetlinks held", func(t *testing.T) { midNavOpenCase(t, "/__gofastr/runtime/widgetlinks.js", false) })
	t.Run("modal lands before commit", func(t *testing.T) { midNavOpenCase(t, "", true) })
}

// midNavOpenCase starts a navigation to /b and holds its answer, opens
// the widget with the request at held hanging (none when held is ""),
// lets /b commit, then lets held finish. modal gives the widget a
// backdrop.
func midNavOpenCase(t *testing.T, held string, modal bool) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	gate := func() (chan struct{}, func()) {
		ch := make(chan struct{})
		var once sync.Once
		free := func() { once.Do(func() { close(ch) }) }
		t.Cleanup(free)
		return ch, free
	}
	navGate, freeNav := gate()
	openGate, freeOpen := gate()
	hold := func(r *http.Request, ch chan struct{}) bool {
		select {
		case <-ch:
			return true
		case <-r.Context().Done():
			return false
		}
	}

	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		backdrop := ""
		if modal {
			backdrop = `"backdrop":true,"stylePath":"/style.css",`
		}
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"late",`+backdrop+`"chromePath":"/chrome/late","deepLinkKey":"panel","deepLinkValue":"late","deepLinkParams":["who"]}}]`)
	})
	mux.HandleFunc("/chrome/late", func(w http.ResponseWriter, r *http.Request) {
		if held == r.URL.Path && !hold(r, openGate) {
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="cui-widget" data-cui-widget="late" role="dialog" aria-label="late">late</div>`)
	})
	mux.HandleFunc("/__gofastr/runtime/widgetlinks.js", func(w http.ResponseWriter, r *http.Request) {
		if held == r.URL.Path && !hold(r, openGate) {
			return
		}
		src, _ := Module("widgetlinks")
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})
	routes := `<script type="application/json" id="gofastr-routes">[{"path":"/a"},{"path":"/b"}]</script>`
	for path, screen := range map[string]string{"/a": "screen-a", "/b": "screen-b"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			body := `<h1 id="` + screen + `">` + screen + `</h1><a id="to-b" href="/b">B</a>`
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Header.Get("X-Gofastr-Navigate") == "1" {
				if path == "/b" && !hold(r, navGate) {
					return
				}
				w.Header().Set("X-Gofastr-Partial", "true")
				fmt.Fprint(w, body)
				return
			}
			fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+routes+`</head><body>`+
				`<main role="main" tabindex="-1">`+body+`</main>`+
				`<script src="/__gofastr/runtime.js"></script></body></html>`)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var mounted bool
	var search, who string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr._widgetCatalog && window.__gofastr._widgetCatalog.late)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// A script click, not chromedp.Click: no hover, so no prefetch
		// answers /b around the held navigation request.
		chromedp.Evaluate(`document.getElementById('to-b').click(); true`, nil),
		// The navigation is in flight: the URL moved, the DOM did not.
		chromedp.Poll(`location.pathname === '/b' && !!document.getElementById('screen-a')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// Not awaited: the request at held hangs until freeOpen.
		chromedp.Evaluate(`window.__gofastr.openWidget('late', {pushUrl: true, params: {who: 'fresh'}}); true`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.ActionFunc(func(c context.Context) error {
			if !modal {
				return nil
			}
			// The modal's chrome and deep link land while /b is held.
			return chromedp.Poll(`!!window.__gofastr._widgets.late && location.search !== ''`, nil,
				chromedp.WithPollingTimeout(5*time.Second)).Do(c)
		}),
		chromedp.ActionFunc(func(context.Context) error { freeNav(); return nil }),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.ActionFunc(func(context.Context) error { freeOpen(); return nil }),
		chromedp.Poll(`!!window.__gofastr._widgets.late && String(window.__gofastr.getSignal('who') ?? '') === 'fresh'`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		// Let the commit's listeners and the catalog re-fetch's deep-link
		// sync run before reading the outcome.
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="late"]') && !!window.__gofastr._widgets.late`, &mounted),
			chromedp.Evaluate(`location.search`, &search),
			chromedp.Evaluate(`String(window.__gofastr.getSignal('who') ?? '')`, &who))
		t.Fatalf("an open issued during the navigation to /b was dropped when /b committed "+
			"(mounted=%v search=%q who=%q): %v", mounted, search, who, err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="late"]') && !!window.__gofastr._widgets.late`, &mounted),
		chromedp.Evaluate(`location.pathname + location.search`, &search),
	); err != nil {
		t.Fatal(err)
	}
	if !mounted {
		t.Errorf("the widget opened during the navigation to /b is not open on /b after it committed")
	}
	if search != "/b?panel=late&who=fresh" {
		t.Errorf("URL = %q, want the open's deep link on the page it opened on (/b?panel=late&who=fresh)", search)
	}
}
