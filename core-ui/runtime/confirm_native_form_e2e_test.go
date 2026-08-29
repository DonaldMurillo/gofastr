package runtime

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Issue #279: data-fui-confirm used to be read only inside dispatchRPC, so a
// plain native POST form carrying the attribute (no data-fui-rpc, no
// data-fui-spa, urlencoded enctype) submitted on the first click with no
// prompt — the admin battery's module-disable and capability-revoke forms
// were the reported cases. The document submit bridge now runs the gate on
// every submit it sees, before the RPC/SPA/native branching; the submitter
// button's message wins over the form's; dispatchRPC skips its own prompt
// when the bridge already gated (opts.confirmed).

// confirmStubJS installs a window.confirm override that counts calls and
// records every message, returning the given answer.
func confirmStubJS(answer bool) string {
	a := "false"
	if answer {
		a = "true"
	}
	return `window.__confirmCalls=0;window.__confirmMsgs=[];window.confirm=function(m){window.__confirmCalls++;window.__confirmMsgs.push(m);return ` + a + `;};`
}

// TestConfirmNativeFormCancelBlocks: a plain native POST form whose submit
// button carries data-fui-confirm must prompt, and a declined prompt must
// keep the form from reaching the server. The handler-hit count is the
// assertion that matters: counting confirm calls alone would pass even if
// the form submitted anyway.
func TestConfirmNativeFormCancelBlocks(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/act">
<button id="b" type="submit" data-fui-confirm="Really?">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := newPollBrowserCtx(t)
	var confirmCalls int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(false), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		// Give an ungated submit time to land before counting.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if confirmCalls != 1 {
		t.Errorf("window.confirm called %d time(s), want 1 — plain-form submit not gated", confirmCalls)
	}
	if h := hits.Load(); h != 0 {
		t.Errorf("POST /act hit %d time(s) after a declined confirm, want 0 — the native submit must be prevented", h)
	}
}

// TestConfirmNativeFormAcceptSubmits: an accepted prompt lets the native
// submit through, exactly once.
func TestConfirmNativeFormAcceptSubmits(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/act">
<button id="b" type="submit" data-fui-confirm="Really?">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := newPollBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(true), nil),
		// The native submit navigates; wait for the response page.
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.WaitVisible(`#done`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h := hits.Load(); h != 1 {
		t.Errorf("POST /act hit %d time(s) after an accepted confirm, want 1", h)
	}
}

// TestConfirmSubmitterBeatsFormMsg: when both the form and the submit button
// carry data-fui-confirm, the button's message is the one prompted with. A
// form can carry several submit buttons of different destructive weight.
func TestConfirmSubmitterBeatsFormMsg(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/act" data-fui-confirm="form-msg">
<button id="b" type="submit" data-fui-confirm="button-msg">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := newPollBrowserCtx(t)
	var confirmCalls int
	var confirmMsgs []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(false), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
		chromedp.Evaluate(`window.__confirmMsgs`, &confirmMsgs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if confirmCalls != 1 || len(confirmMsgs) != 1 || confirmMsgs[0] != "button-msg" {
		t.Errorf("confirm prompted with %v (%d call(s)), want exactly [button-msg] — the submitter's message must win over the form's", confirmMsgs, confirmCalls)
	}
	if h := hits.Load(); h != 0 {
		t.Errorf("POST /act hit %d time(s) after a declined confirm, want 0", h)
	}
}

// TestConfirmRPCFormPromptsOnce: a data-fui-rpc form submit must prompt
// exactly once. The bridge gates, then dispatches with opts.confirmed so
// dispatchRPC does not prompt again from the form's own attribute. The form
// carries data-fui-confirm too, so a dropped opts would surface as a second
// prompt with the form's message.
func TestConfirmRPCFormPromptsOnce(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/rpc/echo" data-fui-rpc="/rpc/echo" data-fui-confirm="form-msg">
<button id="b" type="submit" data-fui-confirm="button-msg">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/rpc/echo": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		},
	})

	ctx := newPollBrowserCtx(t)
	var confirmCalls int
	var confirmMsgs []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(true), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
		chromedp.Evaluate(`window.__confirmMsgs`, &confirmMsgs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if confirmCalls != 1 || len(confirmMsgs) != 1 || confirmMsgs[0] != "button-msg" {
		t.Errorf("confirm prompted with %v (%d call(s)), want exactly [button-msg] — the RPC submit must not double-prompt", confirmMsgs, confirmCalls)
	}
	if h := hits.Load(); h != 1 {
		t.Errorf("/rpc/echo hit %d time(s), want 1 after an accepted confirm", h)
	}
}

// widgetPlainFormCatalog is a non-hidden widget whose chrome carries a plain
// native POST form with a confirmed submit button. The document bridge skips
// widget-scoped forms, so the widget's own submit listener must gate them.
func widgetPlainFormCatalog() string {
	return `[{"hidden":false,"cfg":{"name":"boxer","position":"bottom-right","backdrop":false,"closeOnEscape":false,"closeOnClick":false,"stylePath":"/core-ui/widget/boxer/style.css","chromePath":"/core-ui/widget/boxer/chrome"}}]`
}

// TestConfirmWidgetPlainFormGated: a plain form inside a widget is gated by
// the widget-scoped submit listener; a declined prompt never reaches the
// server.
func TestConfirmWidgetPlainFormGated(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(widgetPlainFormCatalog()))
		},
		"/core-ui/widget/boxer/chrome": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="fui-widget fui-pos-bottom-right" data-fui-widget="boxer"><form id="f" method="post" action="/act"><button id="b" type="submit" data-fui-confirm="Really?">Go</button></form></div>`)
		},
		"/core-ui/widget/boxer/style.css": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := newPollBrowserCtx(t)
	var confirmCalls int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(false), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if confirmCalls != 1 {
		t.Errorf("window.confirm called %d time(s), want 1 — plain form inside a widget not gated", confirmCalls)
	}
	if h := hits.Load(); h != 0 {
		t.Errorf("POST /act hit %d time(s) after a declined confirm, want 0", h)
	}
}
