package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestPaneDeclinedLinkKeepsForwardTransition(t *testing.T) {
	const first = "transition"
	kernel, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(ready) }) }
	t.Cleanup(unblock)
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, kernel)
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		if name != first && (name == "transition" || name == "intercept") {
			select {
			case <-ready:
			case <-req.Context().Done():
				return
			}
		}
		if source, ok := Module(name); ok {
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, source)
			return
		}
		http.NotFound(w, req)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if req.Header.Get("X-Gofastr-Intercept") != "" {
			w.Header().Set("X-Gofastr-Overlay", "drawer")
			fmt.Fprint(w, `<p id="record">Record</p><a id="to-plain" href="/plain">Plain</a>`)
			return
		}
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Transition", map[string]string{"/list": "slide", "/plain": "fade"}[req.URL.Path])
			fmt.Fprintf(w, `<p id="page">%s</p><a id="to-record" href="/rec/a">Record</a>`, req.URL.Path)
			return
		}
		fmt.Fprint(w, `<!doctype html><html data-cui-vt-kinds="slide fade"><head><script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/list"},{"path":"/plain"},{"path":"/rec/:id","intercept":{"from":"/list","as":"drawer"}}]</script></head><body><main>Home<a id="to-list" href="/list">List</a></main><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(server.URL).Do(ctx)
		return err
	}), chromedp.WaitReady("main", chromedp.ByQuery), chromedp.Poll(fmt.Sprintf(`!!window.__gofastr?.loadedModules?.[%q]`, first), nil)); err != nil {
		unblock()
		t.Fatal(err)
	}
	unblock()
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!window.__gofastr.loadedModules.intercept && !!window.__gofastr.loadedModules.transition`, nil),
		chromedp.Click("#to-list", chromedp.ByID),
		chromedp.Poll(`document.getElementById('page')?.textContent === '/list' && !document.documentElement.hasAttribute('aria-busy')`, nil),
		chromedp.Click("#to-record", chromedp.ByID),
		chromedp.WaitVisible("#record", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	var underPath string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr._pushURL('?p=2');window.__gofastr.currentPath`, &underPath)); err != nil {
		t.Fatal(err)
	}
	if underPath != "/list" {
		t.Fatalf("pane URL write changed under-page path to %q", underPath)
	}
	readTypes := captureTypes(ctx)
	if err := chromedp.Run(ctx, chromedp.Click("#to-plain", chromedp.ByID), chromedp.Poll(`document.getElementById('page')?.textContent === '/plain' && !document.documentElement.hasAttribute('aria-busy')`, nil)); err != nil {
		t.Fatal(err)
	}
	if types := readTypes(); len(types) != 1 || strings.Join(types[0], ",") != "forward,fade" {
		t.Fatalf("ordinary link transition = %v, want forward,fade", types)
	}
}
