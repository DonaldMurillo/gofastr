package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestE2EPrintRendersChromeFree drives a real browser to a public print
// document and asserts it renders the body content WITHOUT any host chrome:
// no nav, no runtime.js, body.print-doc, and an @page rule present. This is
// the whole point of the battery — a clean, inert, print-friendly page.
func TestE2EPrintRendersChromeFree(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var h1, bodyClass, atPageCSS string
	var hasRuntime, navCount bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/print/invoice/42"),
		chromedp.WaitVisible("body.print-doc", chromedp.ByQuery),
		chromedp.Text("h1", &h1, chromedp.ByQuery),
		chromedp.Evaluate(`document.body.className`, &bodyClass),
		chromedp.Evaluate(`!!document.querySelector('script[src*="runtime"]')`, &hasRuntime),
		chromedp.Evaluate(`document.querySelectorAll('nav').length > 0`, &navCount),
		chromedp.Evaluate(`([...document.querySelectorAll('style')].find(s => s.textContent.includes('@page'))?.textContent) || ''`, &atPageCSS),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(h1, "Invoice 42") {
		t.Errorf("h1 = %q, want to contain %q", h1, "Invoice 42")
	}
	if bodyClass != "print-doc" {
		t.Errorf("body class = %q, want print-doc", bodyClass)
	}
	if hasRuntime {
		t.Errorf("print page must not load runtime.js")
	}
	if navCount {
		t.Errorf("print page must not render host nav chrome")
	}
	if !strings.Contains(atPageCSS, "@page") || !strings.Contains(atPageCSS, "A4") {
		t.Errorf("@page rule missing or wrong size (want A4): %q", atPageCSS)
	}
}

// TestE2EPrintNoConsoleErrors asserts a print page produces no JS console
// errors (it has no runtime, so it should be silent).
func TestE2EPrintNoConsoleErrors(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	sink := &consoleSink{}
	listenConsoleErrors(ctx, sink)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/print/receipt"),
		chromedp.WaitVisible("body.print-doc", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if errs := sink.hasErrors(); len(errs) > 0 {
		t.Errorf("console errors fired on print page: %v", errs)
	}
}

// TestE2EPrintRouteStatuses checks the route contract over plain HTTP:
// public doc 200, PDF-not-configured 501, RequireAuth doc 401 anonymous.
func TestE2EPrintRouteStatuses(t *testing.T) {
	base := startE2EServer(t)
	cases := []struct {
		path string
		want int
	}{
		{"/print/invoice/42", http.StatusOK},
		{"/print/receipt", http.StatusOK},
		{"/print/invoice/42/pdf", http.StatusNotImplemented}, // no PDFRenderer wired
		{"/print/statement/42", http.StatusUnauthorized},     // RequireAuth, no session
	}
	for _, c := range cases {
		resp, err := http.Get(base + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("GET %s = %d, want %d", c.path, resp.StatusCode, c.want)
		}
	}
}
