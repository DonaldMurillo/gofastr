package runtime

// The host transport slot (core-ui ARCHITECTURE.md "Host transport"): a
// native host whose WebView cannot carry what fetch sends installs a
// fetch-compatible window.__gofastr_transport, and every runtime request
// goes through it. The kernel reads the slot at call time, so a host may
// install it from a rail script that runs after the runtime booted.

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestHostTransportCarriesRequests: a transport installed after boot
// carries the client navigation and the RPC form submission (the server
// sees the header only the transport adds), and the page keeps its
// window state across the navigation.
func TestHostTransportCarriesRequests(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu   sync.Mutex
		seen []string
	)
	record := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, fmt.Sprintf("%s %s via=%s", r.Method, r.URL.Path, r.Header.Get("X-Via-Transport")))
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/rpc/save", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			record(r)
		}
		body := `<h1 id="screen-a">A</h1><a id="next" href="/b">Next</a>`
		if r.URL.Path == "/b" {
			body = `<h1 id="screen-b">B</h1>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST"><input name="title" value="x"><button type="submit">Save</button></form>`
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>T</title><script type="application/json" id="gofastr-routes">[{"path":"/a","layouts":["l:site"]},{"path":"/b","layouts":["l:site"]}]</script></head><body><div data-cui-layout="site" data-cui-layout-key="l:site"><main id="main-content" data-cui-layout-slot="l:site">`+body+`</main></div>
<script src="/__gofastr/runtime.js"></script>
<script>
window.__gofastr_transport = (input, init) => {
  const req = new Request(input, init);
  req.headers.set('X-Via-Transport', '1');
  return fetch(req);
};
</script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var ok bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/a"),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`window.stamp = 'kept'; window.__results = [];
document.addEventListener('gofastr:formresult', (e) => window.__results.push(e.detail.ok));`, nil),
		chromedp.Click(`#next`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-b`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('f').requestSubmit()`, nil),
		chromedp.Poll(`window.__results.length === 1`, nil),
		chromedp.Evaluate(`window.stamp === 'kept' && window.__results[0] === true && window.__gofastr.fetch !== undefined`, &ok),
	); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("the navigation reloaded the document, or the form did not report ok")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /b via=1", "POST /rpc/save via=1"}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests the server saw:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

// bareFetch matches a call of the global fetch: not _fetch, NS.fetch,
// prefetch or any other member or identifier ending in "fetch".
var bareFetch = regexp.MustCompile(`(^|[^\w.$])fetch\(`)

// TestRuntimeSourcesFetchThroughHost: every runtime request goes
// through the host transport, so a runtime source calling the global
// fetch is a request a native host never sees. The kernel's own
// fallback inside _fetch is the one call; boot-embed.js is exempt
// because it runs inside a third-party frame with no host and carries
// embed grants of its own.
func TestRuntimeSourcesFetchThroughHost(t *testing.T) {
	var bad []string
	for root, fsys := range map[string]fs.FS{"frag": fragFS, "src": modulesFS} {
		err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") || path == "frag/boot-embed.js" {
				return err
			}
			b, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(b), "\n") {
				trim := strings.TrimSpace(line)
				if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "*") {
					continue
				}
				if path == "frag/kernel.js" && strings.Contains(trim, "t(input, init) : fetch(input, init)") {
					continue
				}
				if bareFetch.MatchString(line) {
					bad = append(bad, fmt.Sprintf("%s:%d: %s", path, i+1, trim))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bad) > 0 {
		t.Errorf("global fetch in runtime sources; call _fetch (fragments) or NS.fetch (modules):\n%s", strings.Join(bad, "\n"))
	}
}
