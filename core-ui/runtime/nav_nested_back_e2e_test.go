package runtime_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// nestedBackServer: "/g/a" and "/g/b" sit on l:site then l:g, with no
// outlets, so the plain navigator runs. A move between them swaps the
// l:g slot; the boot entry for "/g/a" is keyed at <main>'s l:site slot.
// The l:g layer holds a text input and an element the test marks. Each
// page owns a description meta and a page-scoped quantity seed (A: 1,
// B: 10); B's partial carries them as a page-head carrier and an island.
func nestedBackServer(t *testing.T) *httptest.Server {
	t.Helper()
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	screen := func(name string) string {
		return `<p id="screen">` + name + `</p>`
	}
	qty := map[string]string{"A": "1", "B": "10"}
	page := func(name string) string {
		return `<!doctype html><html lang="en"><head><title>` + name + `</title>` +
			`<meta name="description" content="` + name + ` desc" data-cui-page-meta>` +
			`<script type="application/json" id="gofastr-signals">{"quantity":` + qty[name] + `}</script>` +
			`<script type="application/json" id="gofastr-signals-page">["quantity"]</script>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/g/a","layouts":["l:site","l:g"]},{"path":"/g/b","layouts":["l:site","l:g"]}]</script>` +
			`</head><body><div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<nav><a id="goA" href="/g/a">A</a><a id="goB" href="/g/b">B</a></nav>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main">` +
			`<div data-cui-layout="g" data-cui-layout-key="l:g" id="gwrap"><input id="gq" type="text">` +
			`<div data-cui-layout-slot="l:g" id="gslot">` + screen(name) + `</div></div>` +
			`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(coreJS))
	})
	mux.HandleFunc("/g/", func(w http.ResponseWriter, req *http.Request) {
		name := map[string]string{"/g/a": "A", "/g/b": "B"}[req.URL.Path]
		if name == "" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if req.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Swap", "l:g")
			w.Header().Set("X-Gofastr-Title", name)
			fmt.Fprint(w, `<template data-cui-page-head="&lt;meta name=&quot;description&quot; content=&quot;`+name+` desc&quot; data-cui-page-meta&gt;"></template>`+
				`<script type="application/json" id="gofastr-signals-partial">{"p":{"quantity":`+qty[name]+`}}</script>`+screen(name))
			return
		}
		fmt.Fprint(w, page(name))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Back to the first page of a visit keeps a shared nested layout's DOM
// (2026-10-27 bug bash R17). The boot entry is keyed at <main>'s slot,
// one layer above the l:g slot the move swapped, and the plain navigator
// replayed it there: the l:g layer was rebuilt, its listeners, typed
// input and element identity gone. The cell it replays keeps the
// entry's page head and seed: Back restores A's description and quantity.
func TestPlainBackKeepsSharedNestedLayout(t *testing.T) {
	srv := nestedBackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var screen, typed, desc, qtyB, qtyBack string
	var same, soft bool
	var layers int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/g/a"),
		chromedp.WaitVisible(`#gq`, chromedp.ByID),
		chromedp.Evaluate(`window.__softMark = 1; document.getElementById('gwrap').__mark = 1; document.getElementById('gq').value = 'kept'; true`, nil),
		chromedp.Click(`#goB`, chromedp.ByID),
		chromedp.WaitVisible(`//p[@id="screen" and text()="B"]`),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &qtyB),
		chromedp.Evaluate(`history.back(); true`, nil),
		chromedp.WaitVisible(`//p[@id="screen" and text()="A"]`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('screen').textContent`, &screen),
		chromedp.Evaluate(`document.getElementById('gwrap').__mark === 1`, &same),
		chromedp.Evaluate(`document.querySelectorAll('[data-cui-layout-key="l:g"]').length`, &layers),
		chromedp.Evaluate(`document.getElementById('gq').value`, &typed),
		chromedp.Evaluate(`window.__softMark === 1`, &soft),
		chromedp.Evaluate(`document.querySelector('meta[name="description"]').content`, &desc),
		chromedp.Evaluate(`String(__gofastr.getSignal('quantity'))`, &qtyBack),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !soft {
		t.Fatal("Back reloaded the document")
	}
	if screen != "A" {
		t.Fatalf("Back showed %q, want A", screen)
	}
	if qtyB != "10" {
		t.Fatalf("on B quantity = %s, want B's seed 10 (the fixture never moved it)", qtyB)
	}
	if desc != "A desc" || qtyBack != "1" {
		t.Errorf("Back kept B's page state: description %q, quantity %s (want \"A desc\", 1)", desc, qtyBack)
	}
	if layers != 1 {
		t.Errorf("Back left %d l:g layers, want 1: the entry's own copy of the layer landed inside its cell", layers)
	}
	if !same || typed != "kept" {
		t.Fatalf("Back rebuilt the shared l:g layer: same element %v, input %q (want true, \"kept\")", same, typed)
	}
}
