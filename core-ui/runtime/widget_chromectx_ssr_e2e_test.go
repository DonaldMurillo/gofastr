package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"
)

// This file pins the SSR-inline half of #321: a Hidden().DeepLink()
// widget IS SSR-inlined when the request URL matches its deep link (the
// gallery shape, framework/gallery/catalog.go "modal" snippet:
// preset.Modal("user-edit").Hidden().DeepLink("modal","user-edit").
// DeepLinkParam("user_id")), and that inlined chrome is rendered with
// NO trigger ctx — arrival by URL has no trigger. The runtime must not
// let a later data-fui-ctx trigger hydrate that ctx-less node: it has
// to drop it and take the (name, ctx)-keyed chrome fetch instead, or a
// per-entity dialog shows chrome for the wrong entity (or a form
// posting to a placeholder action).

// ssrCtxServer is chromeCtxServer's SSR-inline twin: the page is served
// at the deep-link URL with the widget chrome ALREADY inlined before
// </body> (byte-for-byte what injectWidgetSSR emits: a ctx-less
// RenderChromeCtx render), and the catalog marks the widget
// hidden + deep-linked so _syncDeepLinks opens it on arrival.
type ssrCtxServer struct {
	Srv *httptest.Server

	mu         sync.Mutex
	perCtx     map[string]int
	totalFetch int
}

func (s *ssrCtxServer) snapshot() (perCtx map[string]int, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := map[string]int{}
	for k, v := range s.perCtx {
		cp[k] = v
	}
	return cp, s.totalFetch
}

// ssrChromeBody is the widget chrome: a mark span showing what the
// server rendered, and a close button so tests can dismiss between
// opens. mark carries the ctx the render saw ("ctx=|user=alice" for the
// SSR inline, which renders with no ctx).
func ssrChromeBody(mark string) string {
	return `<div class="fui-widget fui-pos-center" data-fui-widget="user-edit" role="dialog">` +
		`<span id="ctxmark">` + mark + `</span>` +
		`<button type="button" id="closer" data-fui-action="close">Close</button>` +
		`</div>`
}

func startSSRChromeCtxServer(t *testing.T) *ssrCtxServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	s := &ssrCtxServer{perCtx: map[string]int{}}
	// The catalog entry a real host serves for
	// Hidden().DeepLink("modal","user-edit").DeepLinkParam("user_id").
	catalog := `[{"hidden":true,"cfg":{"name":"user-edit","position":"center","backdrop":false,` +
		`"closeOnEscape":false,"closeOnClick":false,"deepLinkKey":"modal",` +
		`"deepLinkValue":"user-edit","deepLinkParams":["user_id"],` +
		`"stylePath":"/core-ui/widget/user-edit/style.css","chromePath":"/chrome/user-edit"}}]`
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
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
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(catalog))
	})
	mux.HandleFunc("/core-ui/widget/user-edit/style.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, "")
	})
	mux.HandleFunc("/chrome/user-edit", func(w http.ResponseWriter, r *http.Request) {
		ctxVal := r.URL.Query().Get("ctx")
		s.mu.Lock()
		s.perCtx[ctxVal]++
		s.totalFetch++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, ssrChromeBody("ctx="+ctxVal+"|user=alice"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Exactly the page injectWidgetSSR produces for
		// GET /?modal=user-edit&user_id=42: the dialog chrome — rendered
		// ctx-less — sits just inside </body>, open at first paint.
		fmt.Fprintf(w, `<!doctype html><html><head><title>ssr-chromectx</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>
</head><body>
<main id="main-content">
<button id="open-a" data-fui-open="user-edit" data-fui-deeplink="user_id=42" data-fui-ctx="inv-42">Edit 42</button>
<button id="open-b" data-fui-open="user-edit" data-fui-ctx="inv-99">Edit 99</button>
<span id="ready">ready</span>
</main>
%s
<script src="/__gofastr/runtime.js"></script>
</body></html>`, ssrChromeBody("ctx=|user=alice"))
	})
	s.Srv = httptest.NewServer(mux)
	t.Cleanup(s.Srv.Close)
	return s
}

// TestWidgetChromeCtx_SSRInlinedDeepLinkOpensPerCtx is the release
// blocker: a Hidden().DeepLink() widget SSR-inlined on a deep-link URL
// match must still serve per-ctx chrome when a data-fui-ctx trigger
// opens it later. The inlined node is ctx-less by construction (no
// trigger exists at page load); hydrating it for a ctx-carrying
// trigger shows the wrong entity. Two triggers with different ctx must
// produce two distinct chromes, and the ctx-less inline must never be
// served for either.
func TestWidgetChromeCtx_SSRInlinedDeepLinkOpensPerCtx(t *testing.T) {
	s := startSSRChromeCtxServer(t)
	ctx := chromeCtxBrowser(t)

	var mark string
	mounted := `window.__gofastr && window.__gofastr._widgets && !!window.__gofastr._widgets['user-edit']`
	unmounted := `window.__gofastr && window.__gofastr._widgets && !window.__gofastr._widgets['user-edit']`

	// 1. Arrive on the deep-link URL. The inlined chrome paints with no
	// ctx and NO chrome fetch: _syncDeepLinks hydrates the node.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.Srv.URL+"/?modal=user-edit&user_id=42"),
		chromedp.Poll(mounted, nil, chromedp.WithPollingTimeout(10_000)),
		chromedp.Text(`#ctxmark`, &mark, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate/hydrate: %v", err)
	}
	if mark != "ctx=|user=alice" {
		t.Fatalf("SSR-inlined chrome mark = %q, want the ctx-less inline %q", mark, "ctx=|user=alice")
	}
	if perCtx, total := s.snapshot(); total != 0 {
		t.Fatalf("arrival hydration fetched chrome (%d fetches, perCtx=%v); the inline must hydrate with no fetch", total, perCtx)
	}

	// 2. Close it. A hydrated node stays in the DOM (hidden) after
	// dismiss — that residual node is exactly what used to be
	// short-circuited on below.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#closer`, chromedp.ByID),
		chromedp.Poll(unmounted+` && document.querySelector('[data-fui-widget="user-edit"]')`, nil, chromedp.WithPollingTimeout(10_000)),
	); err != nil {
		t.Fatalf("close hydrated dialog: %v", err)
	}

	// 3. THE BUG: open from a trigger carrying ctx. Must render chrome
	// for inv-42, not re-show the ctx-less inline.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#open-a`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByID),
		chromedp.Text(`#ctxmark`, &mark, chromedp.ByID),
	); err != nil {
		t.Fatalf("open with ctx inv-42: %v", err)
	}
	if mark != "ctx=inv-42|user=alice" {
		t.Fatalf("chrome after ctx=inv-42 trigger shows %q — the SSR-inlined ctx-less chrome leaked into a ctx-carrying open", mark)
	}

	// 4. Close (this chrome was fetched, so dismiss removes the node),
	// then a second ctx must get its own distinct chrome.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#closer`, chromedp.ByID),
		chromedp.Poll(unmounted, nil, chromedp.WithPollingTimeout(10_000)),
		chromedp.Click(`#open-b`, chromedp.ByID),
		chromedp.WaitVisible(`#ctxmark`, chromedp.ByID),
		chromedp.Text(`#ctxmark`, &mark, chromedp.ByID),
	); err != nil {
		t.Fatalf("close + reopen with ctx inv-99: %v", err)
	}
	if mark != "ctx=inv-99|user=alice" {
		t.Fatalf("chrome after ctx=inv-99 trigger shows %q, want ctx=inv-99|user=alice", mark)
	}

	// 5. Exactly one fetch per ctx, and the ctx-less chrome endpoint was
	// never hit: the inline was replaced, not re-fetched.
	perCtx, total := s.snapshot()
	if perCtx["inv-42"] != 1 || perCtx["inv-99"] != 1 || perCtx[""] != 0 || total != 2 {
		t.Fatalf("chrome fetches: perCtx=%v total=%d, want exactly inv-42:1, inv-99:1, no ctx-less fetch", perCtx, total)
	}
}
