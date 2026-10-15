package runtime

import (
	"fmt"
	"net/http"
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
