package runtime

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Issue #279: data-cui-confirm used to be read only inside dispatchRPC, so a
// plain native POST form carrying the attribute (no data-cui-rpc, no
// data-cui-spa, urlencoded enctype) submitted on the first click with no
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
// button carries data-cui-confirm must prompt, and a declined prompt must
// keep the form from reaching the server. The handler-hit count is the
// assertion that matters: counting confirm calls alone would pass even if
// the form submitted anyway.
func TestConfirmNativeFormCancelBlocks(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/act">
<button id="b" type="submit" data-cui-confirm="Really?">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := chromedptest.Context(t)
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
<button id="b" type="submit" data-cui-confirm="Really?">Go</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := chromedptest.Context(t)
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

// driveConfirmFixture loads a confirm fixture (form body only; the
// doctype/script wrapper is added) with the given accept/deny confirm
// stub, clicks #b, and returns the recorded confirm calls and prompted
// messages. The caller keeps its own POST-hit assertions via routes.
func driveConfirmFixture(t *testing.T, accept bool, formBody string, routes map[string]http.HandlerFunc) (int, []string) {
	t.Helper()
	base := startPollServer(t, `<!doctype html><html><head></head><body>
`+formBody+`
<script src="/__gofastr/runtime.js"></script></body></html>`, routes)

	ctx := chromedptest.Context(t)
	var confirmCalls int
	var confirmMsgs []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(accept), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`window.__confirmCalls`, &confirmCalls),
		chromedp.Evaluate(`window.__confirmMsgs`, &confirmMsgs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return confirmCalls, confirmMsgs
}

// TestConfirmGating sweeps the fixtures that pin which message one submit
// prompts with and how many handler hits follow. Every case loads its form
// through driveConfirmFixture (which clicks #b) and keeps its own POST-hit
// assertion: counting confirm calls alone would pass even if the form
// submitted anyway. The cases differ only in fixture, route and expected
// hits; each case's comment records the break it exists to catch.
func TestConfirmGating(t *testing.T) {
	cases := []struct {
		name     string
		gateWhy  string // tail of the prompted-message failure
		hitsWhy  string // tail of the handler-hit failure
		accept   bool
		formBody string
		route    string
		routeCT  string
		routeRes string
		wantMsg  string
		wantHits int32
	}{
		{
			// The attribute on the FORM, with a bare submit button, gates
			// too. Every other fixture here puts it on the button, and the
			// bridge passing opts.confirmed means dispatchRPC no longer
			// reads the form as a fallback — so without this case,
			// narrowing the gate to the submitter alone would pass the
			// suite and silently restore #279 for form-level placement.
			name:    "FormLevelMsgGates",
			gateWhy: "a form-level message must gate a bare submit button",
			hitsWhy: "a declined confirm must keep the form from reaching the server",
			accept:  false,
			formBody: `<form id="f" method="post" action="/act" data-cui-confirm="form-msg">
<button id="b" type="submit">Go</button>
</form>`,
			route:    "/act",
			routeCT:  "text/html; charset=utf-8",
			routeRes: `<span id="done">hit</span>`,
			wantMsg:  "form-msg",
			wantHits: 0,
		},
		{
			// When both the form and the submit button carry
			// data-cui-confirm, the button's message is the one prompted
			// with. A form can carry several submit buttons of different
			// destructive weight.
			name:    "SubmitterBeatsFormMsg",
			gateWhy: "the submitter's message must win over the form's",
			hitsWhy: "a declined confirm must keep the form from reaching the server",
			accept:  false,
			formBody: `<form id="f" method="post" action="/act" data-cui-confirm="form-msg">
<button id="b" type="submit" data-cui-confirm="button-msg">Go</button>
</form>`,
			route:    "/act",
			routeCT:  "text/html; charset=utf-8",
			routeRes: `<span id="done">hit</span>`,
			wantMsg:  "button-msg",
			wantHits: 0,
		},
		{
			// A data-cui-rpc form submit must prompt exactly once. The
			// bridge gates, then dispatches with opts.confirmed so
			// dispatchRPC does not prompt again from the form's own
			// attribute. The form carries data-cui-confirm too, so a
			// dropped opts would surface as a second prompt with the
			// form's message.
			name:    "RPCFormPromptsOnce",
			gateWhy: "the RPC submit must not double-prompt",
			hitsWhy: "an accepted confirm lets exactly one RPC through",
			accept:  true,
			formBody: `<form id="f" method="post" action="/rpc/echo" data-cui-rpc="/rpc/echo" data-cui-confirm="form-msg">
<button id="b" type="submit" data-cui-confirm="button-msg">Go</button>
</form>`,
			route:    "/rpc/echo",
			routeCT:  "application/json",
			routeRes: `{"ok":true}`,
			wantMsg:  "button-msg",
			wantHits: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			confirmCalls, confirmMsgs := driveConfirmFixture(t, tc.accept, tc.formBody,
				map[string]http.HandlerFunc{
					tc.route: func(w http.ResponseWriter, _ *http.Request) {
						hits.Add(1)
						w.Header().Set("Content-Type", tc.routeCT)
						fmt.Fprint(w, tc.routeRes)
					},
				})
			if confirmCalls != 1 || len(confirmMsgs) != 1 || confirmMsgs[0] != tc.wantMsg {
				t.Errorf("confirm prompted with %v (%d call(s)), want exactly [%s] — %s", confirmMsgs, confirmCalls, tc.wantMsg, tc.gateWhy)
			}
			if h := hits.Load(); h != tc.wantHits {
				t.Errorf("%s hit %d time(s), want %d — %s", tc.route, h, tc.wantHits, tc.hitsWhy)
			}
		})
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
			fmt.Fprint(w, `<div class="cui-widget cui-pos-bottom-right" data-cui-widget="boxer"><form id="f" method="post" action="/act"><button id="b" type="submit" data-cui-confirm="Really?">Go</button></form></div>`)
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

	ctx := chromedptest.Context(t)
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
