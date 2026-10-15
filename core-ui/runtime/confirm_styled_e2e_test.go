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

// The confirm dialog is cloned out of its template on demand. It opens
// only once its sheet has loaded: showModal on an unstyled clone paints
// the browser's default dialog for a round trip. The host prefetches the
// sheet (framework/uihost TestTemplateSheetPrefetchIsReused), but a
// click right after boot can beat the prefetch; the sheet here answers
// slowly to stand for that.
func TestConfirmDialogOpensStyled(t *testing.T) {
	base := startPollServer(t, `<!doctype html><html><head>
<script>window.__gofastr_catalog={"confirm-probe":{stylePath:"/css/confirm-probe.css"}};</script>
</head><body>
<button id="b" data-cui-rpc="/act" data-cui-rpc-method="POST" data-cui-confirm="Sure?">Go</button>
<template data-cui-confirm-dialog><dialog data-cui-comp="confirm-probe">
<p data-cui-confirm-part="message"></p>
<button type="button" data-cui-confirm-part="cancel">Cancel</button>
<button type="button" data-cui-confirm-part="accept">Confirm</button>
</dialog></template>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/css/confirm-probe.css": func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(1500 * time.Millisecond)
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, `dialog[data-cui-comp="confirm-probe"]{outline:7px solid rgb(1, 2, 3)}`)
		},
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		},
	})
	// Record the dialog's outline at the moment it first shows open.
	const watch = `window.__atOpen = null;
new MutationObserver(() => {
	const d = document.querySelector('body > dialog');
	if (d && d.open && window.__atOpen === null) window.__atOpen = getComputedStyle(d).outlineWidth;
}).observe(document.body, {childList: true, subtree: true, attributes: true});`
	ctx := chromedptest.Context(t)
	var atOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(watch, nil),
		chromedp.Evaluate(confirmStubJS(true), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`window.__atOpen !== null`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__atOpen`, &atOpen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if atOpen != "7px" {
		t.Fatalf("the confirm dialog opened with outline %q before its sheet loaded, want 7px", atOpen)
	}
}

// confirmPage serves a page whose confirm template's root carries attrs,
// with each sheet in sheets answering after its delay.
func confirmPage(t *testing.T, catalog, attrs string, sheets map[string]time.Duration, act http.HandlerFunc) string {
	t.Helper()
	routes := map[string]http.HandlerFunc{"/act": act}
	for name, delay := range sheets {
		name, delay := name, delay
		routes["/css/"+name+".css"] = func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(delay)
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprintf(w, `dialog[data-probe="%s"]{outline:9px solid rgb(1, 2, 3)}`, name)
		}
	}
	return startPollServer(t, `<!doctype html><html><head>
<script>window.__gofastr_catalog=`+catalog+`;</script>
</head><body>
<button id="b" data-cui-rpc="/act" data-cui-rpc-method="POST" data-cui-confirm="Sure?">Go</button>
<template data-cui-confirm-dialog><dialog `+attrs+`>
<p data-cui-confirm-part="message"></p>
<button type="button" data-cui-confirm-part="cancel">Cancel</button>
<button type="button" data-cui-confirm-part="accept">Confirm</button>
</dialog></template>
<script src="/__gofastr/runtime.js"></script></body></html>`, routes)
}

func okJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{}`)
}

// atOpenJS records the dialog's outline the moment it first shows open.
const atOpenJS = `window.__atOpen = null;
new MutationObserver(() => {
	const d = document.querySelector('body > dialog');
	if (d && d.open && window.__atOpen === null) { window.__atOpen = getComputedStyle(d).outlineWidth; window.__openAt = performance.now(); }
}).observe(document.body, {childList: true, subtree: true, attributes: true});`

// A kit may style the dialog through an owned sheet on its root
// (data-cui-scope) rather than a component root; that sheet is loaded
// and waited for too.
func TestConfirmLoadsRootScopeSheet(t *testing.T) {
	base := confirmPage(t, `{"confirm-scope":{stylePath:"/css/confirm-scope.css"}}`,
		`data-cui-scope="confirm-scope" data-probe="confirm-scope"`,
		map[string]time.Duration{"confirm-scope": 800 * time.Millisecond}, okJSON)
	ctx := chromedptest.Context(t)
	var atOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(atOpenJS, nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`window.__atOpen !== null`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__atOpen`, &atOpen),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if atOpen != "9px" {
		t.Fatalf("dialog opened with outline %q, want 9px from its root's data-cui-scope sheet", atOpen)
	}
}

// Two asks while the first waits on its sheet make one dialog: the
// second is declined, so one accept sends one request.
func TestConfirmAsksOneAtATime(t *testing.T) {
	var hits atomic.Int32
	base := confirmPage(t, `{"confirm-probe":{stylePath:"/css/confirm-probe.css"}}`,
		`data-cui-comp="confirm-probe" data-probe="confirm-probe"`,
		map[string]time.Duration{"confirm-probe": 800 * time.Millisecond},
		func(w http.ResponseWriter, r *http.Request) { hits.Add(1); okJSON(w, r) })
	ctx := chromedptest.Context(t)
	var dialogs int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('body > dialog').length`, &dialogs),
		chromedp.Click(`body > dialog [data-cui-confirm-part="accept"]`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if dialogs != 1 {
		t.Errorf("dialogs open = %d, want 1", dialogs)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("requests after one accept = %d, want 1", n)
	}
}

// The dialog waits for its own sheets only: a slow idle prewarm of an
// unrelated component does not hold it closed.
func TestConfirmIgnoresUnrelatedPrewarm(t *testing.T) {
	base := confirmPage(t, `{"confirm-probe":{stylePath:"/css/confirm-probe.css"},"slow-prewarm":{stylePath:"/css/slow-prewarm.css",loadMode:"prewarm"}}`,
		`data-cui-comp="confirm-probe" data-probe="confirm-probe"`,
		map[string]time.Duration{"confirm-probe": 0, "slow-prewarm": 2500 * time.Millisecond}, okJSON)
	ctx := chromedptest.Context(t)
	var waited float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		// The prewarm link is in flight before the click.
		chromedp.Poll(`!!document.querySelector('link[data-cui-style="slow-prewarm"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(atOpenJS+`;window.__clickAt = performance.now();`, nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`window.__atOpen !== null`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`window.__openAt - window.__clickAt`, &waited),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if waited > 1500 {
		t.Fatalf("dialog opened %.0f ms after the click, held back by an unrelated prewarm", waited)
	}
}

// A swap that removes an open dialog fires no close event. The next
// question still opens: the one-at-a-time guard does not outlive the
// dialog it was holding for.
func TestConfirmRemovedDialogFreesNextAsk(t *testing.T) {
	base := confirmPage(t, `{"confirm-probe":{stylePath:"/css/confirm-probe.css"}}`,
		`data-cui-comp="confirm-probe" data-probe="confirm-probe"`,
		map[string]time.Duration{"confirm-probe": 0}, okJSON)
	ctx := chromedptest.Context(t)
	var reopened bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadComponentCSS)`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('body > dialog').remove()`, nil),
		chromedp.Evaluate(`window.__gofastr.askWords({message: 'again'}); null`, nil),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, &reopened, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("the next question never opened after the first dialog was removed: %v", err)
	}
}
