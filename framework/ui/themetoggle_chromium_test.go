package ui_test

// The icon variant's cycle button must carry the marker the module
// loader scans for. The pill root has data-hui-theme-toggle, so a pill
// page loads headless-navigation and its document-level click logic
// works. The icon variant renders a single button that carried only
// data-hui-theme-cycle: no marker, no module, an inert button (spike
// finding F3). Same harness shape as panehost_chromium_test.go: the
// real runtime.js, the behaviors manifest, and modules served by name.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func themeToggleTestPage(t *testing.T, body string, extra ...func(mux *http.ServeMux)) *httptest.Server {
	t.Helper()
	return themeTestPageWithHead(t, "", body, extra...)
}

// themeTestPageWithHead is themeToggleTestPage with markup at the top
// of <head>, where uihost ships the stylesheet and the colour-scheme
// bootstrap.
func themeTestPageWithHead(t *testing.T, head, body string, extra ...func(mux *http.ServeMux)) *httptest.Server {
	t.Helper()
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	block := runtime.BehaviorsJSON()
	if block == nil {
		t.Fatal("runtime.BehaviorsJSON returned nil")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := runtime.Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	// An endpoint a test's island posts to.
	for _, add := range extra {
		add(mux)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head>%s`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`<main role="main"><span id="ready">ready</span>%s</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`, head, block, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Clicking the icon variant cycles the colour scheme exactly as the
// pill variant's options do: the html data-color-scheme attribute the
// headless-navigation module writes flips. A fresh profile starts at
// auto; the cycle order is dark → light → auto, so on a light OS (the
// emulated preference: the cycle skips a step that would not change
// what the page shows, so the host's own appearance would decide the
// answer) the first click lands on dark.
func TestThemeToggleIconVariantCyclesScheme(t *testing.T) {
	srv := themeToggleTestPage(t, string(ui.ThemeToggle(ui.ThemeToggleConfig{
		Variant: ui.ThemeToggleIcon,
		ID:      "tt-icon",
	})))
	ctx := chromedptest.Context(t)

	var before, after string
	if err := chromedp.Run(ctx,
		prefersScheme("light"),
		chromedp.Navigate(srv.URL),
		chromedp.WaitVisible(`#tt-icon`, chromedp.ByID),
		chromedp.Evaluate(`String(document.documentElement.getAttribute('data-color-scheme'))`, &before),
		chromedp.Click(`#tt-icon`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`String(document.documentElement.getAttribute('data-color-scheme'))`, &after),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if after != "dark" {
		t.Errorf("clicking the icon toggle set data-color-scheme=%q (was %q), want \"dark\": the cycle click did nothing — the button lacks the marker that loads the module which handles it", after, before)
	}
}
