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

// A page that carries the kit's <template data-cui-confirm-dialog> asks
// in that dialog, never in window.confirm: the confirm module clones
// it, words it from the gated control and answers with its buttons.
// Every fixture stubs window.confirm to say yes, so a fallback to the
// native prompt would let the request through and fail the hit count.

// confirmTemplate is a hook-only dialog: what the kit renders, minus
// its classes.
const confirmTemplate = `<template data-cui-confirm-dialog><dialog>
<h2 data-cui-confirm-part="title">Are you sure?</h2>
<p data-cui-confirm-part="message"></p>
<button type="button" data-cui-confirm-part="cancel">Cancel</button>
<button type="button" data-cui-confirm-part="accept">Confirm</button>
<button type="button" data-cui-confirm-part="accept-danger">Confirm</button>
</dialog></template>`

// confirmDialogState reads the open dialog's words and buttons.
const confirmDialogState = `(() => {
	const d = document.querySelector('body > dialog');
	if (!d) return {open: false};
	const p = (n) => d.querySelector('[data-cui-confirm-part="' + n + '"]');
	return {
		open: d.open,
		title: p('title').textContent,
		message: p('message').textContent,
		accept: !!p('accept'),
		danger: p('accept-danger') ? p('accept-danger').textContent : null,
		focused: document.activeElement === p('cancel'),
	};
})()`

func TestConfirmDialogGatesRPCButton(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<button id="b" data-cui-rpc="/act" data-cui-rpc-method="POST"
 data-cui-confirm="It cannot be undone." data-cui-confirm-title="Delete this invoice?"
 data-cui-confirm-accept="Delete" data-cui-confirm-tone="danger">Delete</button>
`+confirmTemplate+`
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		},
	})

	ctx := chromedptest.Context(t)
	var opened map[string]any
	var afterCancel, afterAccept bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(true), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Evaluate(confirmDialogState, &opened),
		chromedp.Click(`body > dialog [data-cui-confirm-part="cancel"]`),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('body > dialog')`, &afterCancel),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if opened["open"] != true || opened["title"] != "Delete this invoice?" || opened["message"] != "It cannot be undone." {
		t.Errorf("the dialog is not worded from the button: %v", opened)
	}
	if opened["accept"] != false || opened["danger"] != "Delete" {
		t.Errorf("a danger confirm must keep only the danger button, labelled Delete: %v", opened)
	}
	if opened["focused"] != true {
		t.Errorf("cancel must take focus when the dialog opens: %v", opened)
	}
	if !afterCancel || hits.Load() != 0 {
		t.Fatalf("cancel: dialog removed %v, POST /act hit %d time(s), want removed and 0", afterCancel, hits.Load())
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Click(`body > dialog [data-cui-confirm-part="accept-danger"]`),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('body > dialog')`, &afterAccept),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !afterAccept || hits.Load() != 1 {
		t.Errorf("accept: dialog removed %v, POST /act hit %d time(s), want removed and 1", afterAccept, hits.Load())
	}
	var native int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__confirmCalls`, &native)); err != nil {
		t.Fatal(err)
	}
	if native != 0 {
		t.Errorf("window.confirm ran %d time(s) on a page with a confirm dialog, want 0", native)
	}
}

func TestConfirmDialogEscapeDeclines(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<button id="b" data-cui-rpc="/act" data-cui-rpc-method="POST" data-cui-confirm="Sure?">Go</button>
`+confirmTemplate+`
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			fmt.Fprint(w, `{}`)
		},
	})

	ctx := chromedptest.Context(t)
	var opened map[string]any
	var gone bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(true), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Evaluate(confirmDialogState, &opened),
		chromedp.KeyEvent("\x1b"),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('body > dialog')`, &gone),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// No title on the control keeps the template's; no tone keeps the
	// primary accept.
	if opened["title"] != "Are you sure?" || opened["accept"] != true || opened["danger"] != nil {
		t.Errorf("an unworded confirm must keep the template's title and the primary accept: %v", opened)
	}
	if !gone || hits.Load() != 0 {
		t.Errorf("Escape: dialog removed %v, POST /act hit %d time(s), want removed and 0", gone, hits.Load())
	}
}

// The entity screens put the gate on an RPC form: accept must send the
// form's request once, through the submit bridge's second pass.
func TestConfirmDialogGatesRPCForm(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/act" data-cui-rpc="/act" data-cui-rpc-method="POST" data-cui-confirm="Purge it?">
<input type="hidden" name="back" value="/list">
<button id="b" type="submit">Purge</button>
</form>
`+confirmTemplate+`
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		},
	})

	ctx := chromedptest.Context(t)
	var url string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(true), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h := hits.Load(); h != 0 {
		t.Fatalf("the form sent %d request(s) before the dialog answered, want 0", h)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`body > dialog [data-cui-confirm-part="accept"]`),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &url),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h := hits.Load(); h != 1 {
		t.Errorf("accept sent %d request(s), want 1", h)
	}
	if url != "/" {
		t.Errorf("an RPC form navigated the document to %s; the request must go through the runtime", url)
	}
}

func TestConfirmDialogGatesNativeForm(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<form id="f" method="post" action="/act">
<button id="b" type="submit" name="op" value="disable" data-cui-confirm="Disable billing?">Disable</button>
</form>
`+confirmTemplate+`
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/act": func(w http.ResponseWriter, r *http.Request) {
			if r.FormValue("op") == "disable" {
				hits.Add(1)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span id="done">hit</span>`)
		},
	})

	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#b`, chromedp.ByID),
		chromedp.Evaluate(confirmStubJS(true), nil),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil),
		chromedp.Click(`body > dialog [data-cui-confirm-part="accept"]`),
		chromedp.WaitVisible(`#done`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The submitter's name and value ride the resubmit: an admin form
	// with two buttons must post the one pressed.
	if h := hits.Load(); h != 1 {
		t.Errorf("POST /act with op=disable hit %d time(s), want 1", h)
	}
}
