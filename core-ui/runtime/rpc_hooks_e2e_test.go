package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Browser coverage for the request/response hook seam in src/rpc.js:
// data-fui-rpc-with loads the named module BEFORE the dispatch, a
// request hook registered on __gofastr._rpcHooks.request decorates the
// request the fetch is built from, and a response hook sees the
// response's headers on a 2xx. The seam is what framework/local's
// upload and download bridges ride on; this test proves the seam with
// a hook the page registers itself, and the embedded `local` primitive
// as the module to load, so the proof does not depend on any consumer.

type rpcHooksServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []map[string]any
	hdrs []string
}

func startRPCHooksServer(t *testing.T) *rpcHooksServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &rpcHooksServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := Module(name); ok {
			_, _ = w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		s.mu.Lock()
		s.seen = append(s.seen, body)
		s.hdrs = append(s.hdrs, r.Header.Get("X-Test-Hook"))
		s.mu.Unlock()
		w.Header().Set("X-Test-Echo", "from-server")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>hooks</title></head><body>
  <main role="main"><span id="ready">ready</span>
    <form id="f" data-fui-rpc="/echo" data-fui-rpc-signal="res" data-fui-rpc-with="local"><input name="note" value="hi"><button id="go" type="submit">go</button></form>
    <button id="bare" data-fui-rpc="/echo" data-fui-rpc-signal="res">bare</button>
    <span id="out" data-fui-signal="res"></span>
  </main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func TestRPCHooksDecorateTheRequestAndSeeTheResponse(t *testing.T) {
	s := startRPCHooksServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// The page registers a hook of its own, the way a module does.
		chromedp.Evaluate(`(() => {
            const NS = window.__gofastr;
            NS._rpcHooks = NS._rpcHooks || { request: [], response: [] };
            window.__reqs = []; window.__resps = [];
            NS._rpcHooks.request.push(async (node, req) => {
                window.__reqs.push({ id: node.id, method: req.method, hadLocal: !!(NS.loadedModules && NS.loadedModules.local) });
                if (typeof req.body === 'string' && req.body) {
                    const o = JSON.parse(req.body); o.__extra = 'from-hook'; req.body = JSON.stringify(o);
                } else if (!req.body) {
                    req.body = JSON.stringify({ __extra: 'fresh-body' }); req.headers['Content-Type'] = 'application/json';
                }
                req.headers['X-Test-Hook'] = 'yes';
            });
            NS._rpcHooks.response.push((node, r) => { window.__resps.push({ id: node.id, echo: r.headers.get('X-Test-Echo') }); });
        })()`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !localPollTrue(ctx, `Promise.resolve(window.__resps.length === 1)`) {
		t.Fatal("the response hook never ran for the form")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#bare`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !localPollTrue(ctx, `Promise.resolve(window.__resps.length === 2)`) {
		t.Fatal("the response hook never ran for the bare button")
	}
	var reqs []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__reqs`, &reqs)); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[0]["id"] != "f" || reqs[0]["method"] != "POST" {
		t.Fatalf("request hooks saw %v", reqs)
	}
	if reqs[0]["hadLocal"] != true {
		t.Fatal("data-fui-rpc-with=local must have the module registered before the hook runs")
	}
	var resps []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__resps`, &resps)); err != nil {
		t.Fatal(err)
	}
	if resps[0]["echo"] != "from-server" || resps[1]["id"] != "bare" {
		t.Fatalf("response hooks saw %v", resps)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) != 2 {
		t.Fatalf("server saw %d requests", len(s.seen))
	}
	if s.seen[0]["note"] != "hi" || s.seen[0]["__extra"] != "from-hook" || s.hdrs[0] != "yes" {
		t.Fatalf("the form's request reached the server as %v with hook header %q — the hook's body and header edits must be what is sent", s.seen[0], s.hdrs[0])
	}
	if s.seen[1]["__extra"] != "fresh-body" {
		t.Fatalf("the bare button's request reached the server as %v — a hook may give an empty request a JSON body", s.seen[1])
	}
}

// A trigger naming a module that does not exist does NOT dispatch.
//
// data-fui-rpc-with is a PRECONDITION, not a hint: the trigger is
// saying this request is not itself without that module's decoration.
// Dispatching anyway sent the server a request that looks complete and
// is not, a save with the draft missing, and the page had no way to
// know. The same holds for a request hook that throws, or one that
// marks the request fatal itself.
func TestRPCWithUnknownModuleDoesNotDispatch(t *testing.T) {
	s := startRPCHooksServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            window.__refused = [];
            window.addEventListener('gofastr:rpc-refused', (e) => window.__refused.push(e.detail));
            document.getElementById('bare').setAttribute('data-fui-rpc-with', 'no-such-module');
        })()`, nil),
		chromedp.Click(`#bare`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !localPollTrue(ctx, `Promise.resolve(window.__refused.length === 1)`) {
		t.Fatal("the RPC was dispatched without the module the trigger declared, and nothing said so")
	}
	var refused []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__refused`, &refused)); err != nil {
		t.Fatal(err)
	}
	if r, _ := refused[0]["reason"].(string); r != "module:no-such-module" {
		t.Fatalf("gofastr:rpc-refused = %v, want the module that would not load", refused[0])
	}
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('out').textContent || ''`, &out)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ok") {
		t.Fatalf("the endpoint answered %q — the request must not have been sent at all", out)
	}
}

// A request hook that throws cancels the dispatch too: a hook is how a
// module attaches what the markup promised, and a hook that failed
// halfway leaves a request that is missing it.
func TestRPCRequestHookThatThrowsCancelsTheDispatch(t *testing.T) {
	s := startRPCHooksServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            window.__refused = [];
            window.addEventListener('gofastr:rpc-refused', (e) => window.__refused.push(e.detail));
            const h = window.__gofastr._rpcHooks || (window.__gofastr._rpcHooks = { request: [], response: [] });
            h.request.push(() => { throw new Error('the records could not be read'); });
        })()`, nil),
		chromedp.Click(`#bare`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !localPollTrue(ctx, `Promise.resolve(window.__refused.length === 1)`) {
		t.Fatal("a request hook threw and the request was dispatched anyway")
	}
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('out').textContent || ''`, &out)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ok") {
		t.Fatalf("the endpoint answered %q — the request must not have been sent", out)
	}
}

// A response hook that rejects does not escape the dispatch.
//
// The response hooks ran without await, so a hook returning a rejected
// promise (every async hook that throws) left the try block before the
// rejection landed: the RPC completed and the page logged an unhandled
// rejection nobody could catch. The hook is awaited like the request
// side; the rejection is logged, and the response is still applied.
func TestRPCResponseHookThatRejectsIsCaught(t *testing.T) {
	s := startRPCHooksServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
            window.__unhandled = 0;
            window.addEventListener('unhandledrejection', () => { window.__unhandled++; });
            const h = window.__gofastr._rpcHooks || (window.__gofastr._rpcHooks = { request: [], response: [] });
            h.response.push(async () => { throw new Error('the download could not be applied'); });
            h.response.push(() => Promise.reject(new Error('and a bare rejected promise')));
        })()`, nil),
		chromedp.Click(`#bare`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !localPollTrue(ctx, `Promise.resolve((document.getElementById('out').textContent || '').indexOf('ok') >= 0)`) {
		t.Fatal("the RPC never completed: a response hook that rejects must not cancel the response")
	}
	// Let any escaped rejection reach the window before counting.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`new Promise((res) => setTimeout(res, 200))`, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatal(err)
	}
	var unhandled int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__unhandled`, &unhandled)); err != nil {
		t.Fatal(err)
	}
	if unhandled != 0 {
		t.Fatalf("%d unhandled rejection(s) escaped the response hook loop: the hook is awaited inside the try, like the request side", unhandled)
	}
}
