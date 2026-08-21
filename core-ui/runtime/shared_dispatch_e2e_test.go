package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// This file pins the shared-dispatch extraction (fix #2): the
// widget-scoped RPC path must honor every primitive the global
// dispatchRPC does, because both now call ONE implementation exposed
// on window.__gofastr.dispatchRPC. The widget path historically
// forked and drifted, data-fui-confirm was silently ignored (a
// destructive delete in a drawer fired unconfirmed) and a GET-method
// form serialized a JSON body, which fetch(GET, body) rejects.

// widgetConfirmerCatalog is a non-hidden widget whose chrome carries a
// data-fui-confirm RPC button.
func widgetConfirmerCatalog() string {
	b, _ := json.Marshal([]map[string]any{{
		"hidden": false,
		"cfg": map[string]any{
			"name": "confirmer", "position": "bottom-right",
			"backdrop": false, "closeOnEscape": false, "closeOnClick": false,
			"stylePath":  "/core-ui/widget/confirmer/style.css",
			"chromePath": "/core-ui/widget/confirmer/chrome",
		},
	}})
	return string(b)
}

// TestWidgetRPC_ConfirmHonored: inside a widget, a data-fui-confirm
// button MUST call window.confirm and ABORT on cancel. Before the
// shared-dispatch fix the widget's local dispatchRPC ignored the
// attribute entirely, so a destructive RPC fired unconfirmed.
func TestWidgetRPC_ConfirmHonored(t *testing.T) {
	var mu sync.Mutex
	delHits := 0

	base := startPollServer(t, `<!doctype html><html><head></head><body>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(widgetConfirmerCatalog()))
		},
		"/core-ui/widget/confirmer/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="confirmer"><button id="del" data-fui-rpc="/rpc/del" data-fui-confirm="Delete this?">Delete</button></div>`)
		},
		"/core-ui/widget/confirmer/style.css": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
		"/rpc/del": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			delHits++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		},
	})

	ctx := newPollBrowserCtx(t)
	var confirmCalls int

	// --- cancel: confirm returns false → RPC MUST NOT fire ---
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#del`, chromedp.ByID),
		chromedp.Evaluate(`window.__confirmCalls=0; window.confirm=function(){window.__confirmCalls++;return false;};`, nil),
		chromedp.Click(`#del`, chromedp.ByID),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
	); err != nil {
		t.Fatalf("chromedp (cancel): %v", err)
	}
	mu.Lock()
	h := delHits
	mu.Unlock()
	if confirmCalls != 1 {
		t.Errorf("cancel: window.confirm called %d time(s), want 1 — data-fui-confirm not consulted inside widget", confirmCalls)
	}
	if h != 0 {
		t.Errorf("cancel: destructive /rpc/del fired (%d hit(s)) despite confirm returning false — confirm ignored inside widget", h)
	}

	// --- accept: confirm returns true → RPC fires ---
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.confirm=function(){window.__confirmCalls++;return true;};`, nil),
		chromedp.Click(`#del`, chromedp.ByID),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
	); err != nil {
		t.Fatalf("chromedp (accept): %v", err)
	}
	mu.Lock()
	h = delHits
	mu.Unlock()
	if h != 1 {
		t.Errorf("accept: /rpc/del hit %d time(s), want 1 after confirm=true", h)
	}
}

// TestWidgetRPC_GetFormEncodesToQuery: inside a widget, a GET-method
// form RPC MUST encode its fields as the query string. Before the
// shared-dispatch fix the widget path serialized a JSON body for every
// method, and fetch(GET, body) throws "Request with GET/HEAD method
// cannot have body", so the server was never reached.
func TestWidgetRPC_GetFormEncodesToQuery(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotQuery string
	var gotBodyLen int
	getHits := 0

	cat, _ := json.Marshal([]map[string]any{{
		"hidden": false,
		"cfg": map[string]any{
			"name": "getter", "position": "bottom-right",
			"backdrop": false, "closeOnEscape": false, "closeOnClick": false,
			"stylePath":  "/core-ui/widget/getter/style.css",
			"chromePath": "/core-ui/widget/getter/chrome",
		},
	}})

	base := startPollServer(t, `<!doctype html><html><head></head><body>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(cat)
		},
		"/core-ui/widget/getter/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="getter"><form id="gf" data-fui-rpc="/rpc/get" data-fui-rpc-method="GET"><input name="q" value="hello"><button type="submit" id="go">Go</button></form></div>`)
		},
		"/core-ui/widget/getter/style.css": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
		"/rpc/get": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			getHits++
			gotMethod = r.Method
			gotQuery = r.URL.RawQuery
			gotBodyLen = int(r.ContentLength)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		},
	})

	ctx := newPollBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	h := getHits
	m := gotMethod
	q := gotQuery
	bl := gotBodyLen
	mu.Unlock()

	if h != 1 {
		t.Fatalf("GET-form-in-widget: /rpc/get hit %d time(s), want 1 — fetch(GET, body) threw and the server was never reached", h)
	}
	if m != http.MethodGet {
		t.Errorf("GET-form-in-widget: method=%q, want GET", m)
	}
	if q != "q=hello" {
		t.Errorf("GET-form-in-widget: query=%q, want q=hello (form fields must encode to the query string)", q)
	}
	if bl > 0 {
		t.Errorf("GET-form-in-widget: request carried a body of %d bytes — a GET RPC must have no body", bl)
	}
}
