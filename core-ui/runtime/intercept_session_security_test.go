package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestOverlayRollsSessionAndCache(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			testOverlaySession(t, status)
		})
	}
}

func testOverlaySession(t *testing.T, status int) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var pages atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(writer, js)
	})
	mux.HandleFunc("/__gofastr/runtime/", func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/__gofastr/runtime/"), ".js")
		writer.Header().Set("Content-Type", "application/javascript")
		module, _ := Module(name)
		fmt.Fprint(writer, module)
	})
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		if request.Header.Get("X-Gofastr-Intercept") == "1" {
			writer.Header().Set("X-Gofastr-Overlay", "drawer")
			writer.Header().Set("X-Gofastr-Session", "new-token")
			writer.WriteHeader(status)
			fmt.Fprint(writer, `<button id="pane">Pane</button>`)
			return
		}
		if request.URL.Path == "/plain" {
			pages.Add(1)
		}
		fmt.Fprintf(writer, `<!doctype html><html><head><title>Session</title>
			<script type="application/json" id="gofastr-routes">[{"path":"/list"},{"path":"/plain"},{"path":"/rec/:id","intercept":{"from":"/list","as":"drawer"}}]</script>
			</head><body><main tabindex="-1"><span id="path">%s</span><a id="open" href="/rec/a">Open</a></main>
			<script src="/__gofastr/runtime.js"></script></body></html>`, request.URL.Path)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	settled := `document.getElementById('pane') !== null`
	if status >= http.StatusBadRequest {
		settled = `document.getElementById('path').textContent === '/rec/a'`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/list"),
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.intercept`, nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/plain')`, nil),
		chromedp.Poll(`document.getElementById('path').textContent === '/plain'`, nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/list')`, nil),
		chromedp.Poll(`document.getElementById('path').textContent === '/list'`, nil),
		chromedp.Evaluate(`(() => {
			const meta = document.createElement('meta');
			meta.name = 'gofastr-sse'; meta.content = '/__gofastr/sse?session=dead-token&lane=events';
			document.head.appendChild(meta);
		})()`, nil),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Poll(settled, nil),
	); err != nil {
		t.Fatal(err)
	}
	var session string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]').content`, &session)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(session, "session=new-token&lane=events") {
		t.Errorf("overlay retained stale session: %s", session)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.navigate('/plain')`, nil),
		chromedp.Poll(`document.getElementById('path').textContent === '/plain'`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if got := pages.Load(); got != 2 {
		t.Errorf("canonical screen fetched %d times, want 2 after session rollover", got)
	}
}
