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

// navRollbackRoutes: /p/* render under the plain navigator, /o/* hold
// an outlet, so the envelope module's navigator runs them.
const navRollbackRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/p/a","layouts":["l:p"]},{"path":"/p/b","layouts":["l:p"]},{"path":"/p/c","layouts":["l:p"]},{"path":"/p/d","layouts":["l:p"]},` +
	`{"path":"/o/a","layouts":["l:o"]},{"path":"/o/b","layouts":["l:o"]},{"path":"/o/c","layouts":["l:o"]},{"path":"/o/d","layouts":["l:o"]}` +
	`]</script>`

func navRollbackPage(base, id string) string {
	outlet := ""
	if base == "/o" {
		outlet = `<div data-cui-outlet="l:o#aside" id="aside">aside</div>`
	}
	key := "l:" + base[1:]
	return `<!doctype html><html lang="en"><head><title>` + id + `</title>` + navRollbackRoutes + `</head><body>` +
		`<div data-cui-layout="` + base[1:] + `" data-cui-layout-key="` + key + `">` + outlet +
		`<main role="main" tabindex="-1" data-cui-layout-slot="` + key + `">` +
		navRollbackContent(base, id) +
		`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
}

func navRollbackContent(base, id string) string {
	return `<h1 id="` + base[1:] + `-` + id + `">` + id + `</h1>`
}

// A navigation that fails puts the address bar back on the page the DOM
// still shows. Starting C while B was still loading made C's previous
// path B, so C's failure rolled the URL and the router back to B, a page
// that never rendered, while the DOM showed A (2026-10-27 R31). Then D
// loads and E fails: the rollback names D, so the chain's starting page
// is not kept past the navigation that ended it.
func TestFailedNavRollsBackToTheShownPage(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	for _, base := range []string{"/p", "/o"} {
		for _, id := range []string{"a", "d"} {
			mux.HandleFunc(base+"/"+id, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if r.Header.Get("X-Gofastr-Navigate") == "1" {
					w.Header().Set("X-Gofastr-Partial", "true")
					w.Header().Set("X-Gofastr-Title", id)
					w.Header().Set("X-Gofastr-Swap", "l:"+base[1:])
					fmt.Fprint(w, navRollbackContent(base, id))
					return
				}
				fmt.Fprint(w, navRollbackPage(base, id))
			})
		}
		// B never answers while the test runs: it is the navigation C
		// supersedes.
		mux.HandleFunc(base+"/b", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-hold:
			case <-r.Context().Done():
			}
		})
		// C and E fail: a non-HTML answer is a failed navigation.
		for _, id := range []string{"c", "e"} {
			mux.HandleFunc(base+"/"+id, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "boom")
			})
		}
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, base := range []string{"/p", "/o"} {
		ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
		id := base[1:]
		settled := func(path string) chromedp.Action {
			return chromedp.Poll(`location.pathname !== '`+base+path+`' && !document.documentElement.hasAttribute('aria-busy')`, nil)
		}
		shown := func(page string) string {
			return `location.pathname + ' ' + __gofastr.currentPath + ' ' + !!document.getElementById('` + id + `-` + page + `')`
		}
		var got, again string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+base+"/a"),
			chromedp.WaitVisible(`#`+id+`-a`, chromedp.ByID),
			chromedp.Evaluate(`__gofastr.navigate('`+base+`/b')`, nil),
			chromedp.Poll(`location.pathname === '`+base+`/b'`, nil),
			chromedp.Evaluate(`__gofastr.navigate('`+base+`/c')`, nil),
			// C's failure clears the busy flag once it has rolled back.
			settled("/c"),
			chromedp.Evaluate(shown("a"), &got),
			chromedp.Evaluate(`__gofastr.navigate('`+base+`/d')`, nil),
			chromedp.WaitVisible(`#`+id+`-d`, chromedp.ByID),
			chromedp.Evaluate(`__gofastr.navigate('`+base+`/e')`, nil),
			settled("/e"),
			chromedp.Evaluate(shown("d"), &again),
		); err != nil {
			t.Fatalf("%s: chromedp: %v", base, err)
		}
		if want := base + "/a " + base + "/a true"; got != want {
			t.Errorf("%s: after B pending then C failing, location, router path, A shown = %q, want %q", base, got, want)
		}
		if want := base + "/d " + base + "/d true"; again != want {
			t.Errorf("%s: after D loaded then E failing, location, router path, D shown = %q, want %q", base, again, want)
		}
	}
}
