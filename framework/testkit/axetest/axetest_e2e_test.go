package axetest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// schemeFixture serves one page whose .trap paragraph fails color
// contrast only under the scheme named by `only`, and counts how many
// times the page itself was requested. head is extra markup for <head>.
func schemeFixture(t *testing.T, only, head string) (url string, loads *atomic.Int32) {
	t.Helper()
	loads = new(atomic.Int32)
	page := fmt.Sprintf(`<!doctype html><html lang="en"><head><title>scheme fixture</title>
<meta name="color-scheme" content="light">
<style>
body { background: #fff; color: #000; }
[data-color-scheme="dark"] body { background: #000; color: #fff; }
[data-color-scheme=%q] .trap { color: %s; }
</style>%s</head><body><main><h1>Fixture</h1>
<p class="trap">Readable in one scheme only.</p><p>One.</p><p>Two.</p><p>Three.</p>
</main></body></html>`, only, map[string]string{"dark": "#111", "light": "#eee"}[only], head)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		loads.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/", loads
}

// ScanSchemes loads a page once and still audits every scheme: a
// contrast failure that exists only in dark, or only in light, must
// surface tagged with that scheme and no other.
func TestScanSchemesCatchesSchemeOnlyViolations(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	browser := NewBrowser(t)
	for _, only := range Schemes {
		t.Run(only+"-only", func(t *testing.T) {
			url, loads := schemeFixture(t, only, "")
			ctx, cancel := NewTab(t, browser)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady()); err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			vs, err := ScanSchemes(ctx, Schemes, nil)
			if err != nil {
				t.Fatalf("ScanSchemes: %v", err)
			}
			var got []string
			for _, v := range vs {
				if v.ID == "color-contrast" {
					got = append(got, v.Scheme)
				}
			}
			if len(got) != 1 || got[0] != only {
				t.Errorf("color-contrast violations by scheme = %v, want exactly [%s]", got, only)
			}
			if n := loads.Load(); n != 1 {
				t.Errorf("fixture page requested %d times, want 1 (one load for every scheme)", n)
			}
		})
	}
}

// An empty scheme list scans nothing, so it is an error, never a pass.
func TestScanSchemesRefusesNoSchemes(t *testing.T) {
	vs, err := ScanSchemes(context.Background(), nil, nil)
	if err == nil {
		t.Fatalf("ScanSchemes with no schemes passed with %d violations; it scanned nothing", len(vs))
	}
}

// A window.axe the page defined itself is never the engine that audits
// it: a stub answering "no violations" must not turn a failing page into
// a pass.
func TestScanIgnoresAPageDefinedAxe(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	url, _ := schemeFixture(t, "dark",
		`<script>window.axe = {run: async () => ({violations: []})};</script>`)
	ctx, cancel := NewTab(t, NewBrowser(t))
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady()); err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	vs, err := ScanSchemes(ctx, Schemes, nil)
	if err != nil {
		t.Fatalf("ScanSchemes: %v", err)
	}
	var dark bool
	for _, v := range vs {
		dark = dark || v.ID == "color-contrast" && v.Scheme == "dark"
	}
	if !dark {
		t.Errorf("the page's own window.axe stub hid the dark color-contrast violation: got %v", vs)
	}
}

// A rule that throws checks nothing, so it is an error, never a pass.
// Mixing two achromatic colors in oklch leaves the hue powerless, and
// Chrome serialises the result as oklch(L C none), which axe-core cannot
// parse: color-contrast throws and lands in axe's incomplete list. The
// docs site's header carried exactly this background, which silenced
// color-contrast on every page it rendered on.
func TestScanFailsOnACrashedRule(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	url, _ := schemeFixture(t, "dark",
		`<style>main { background: color-mix(in oklch, #000 14%, #fff); }</style>`)
	ctx, cancel := NewTab(t, NewBrowser(t))
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady()); err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	vs, err := ScanSchemes(ctx, Schemes, nil)
	if err == nil {
		t.Fatalf("ScanSchemes passed with %d violations while color-contrast could not parse the page's colors", len(vs))
	}
	if !strings.Contains(err.Error(), "color-contrast crashed") {
		t.Errorf("error = %v, want it to name the crashed color-contrast rule", err)
	}

	// Allowlisting the rule is the caller saying it does not apply, so
	// its crash is not an error either.
	if _, err := ScanSchemes(ctx, Schemes, map[string]string{"color-contrast": "fixture"}); err != nil {
		t.Errorf("ScanSchemes with color-contrast allowlisted: %v", err)
	}
}

// WaitReady names the unmet condition instead of hanging: a runtime
// module script that never registers keeps the page unready until the
// bound, and the error says which module.
func TestWaitReadyNamesAnUnregisteredModule(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	defer func(d time.Duration) { readyTimeout = d }(readyTimeout)
	readyTimeout = 1500 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="en"><head><title>ready</title>
<script>window.__gofastr = {loadedModules: {kept: true}};</script>
<script src="/__gofastr/runtime/kept.js"></script>
<script src="/__gofastr/runtime/ghost.js"></script>
</head><body><main><p>ready fixture</p></main></body></html>`)
		case "/__gofastr/runtime/kept.js", "/__gofastr/runtime/ghost.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, "/* loads, registers nothing itself */")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := NewTab(t, NewBrowser(t))
	defer cancel()
	start := time.Now()
	err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/"), WaitReady())
	if err == nil {
		t.Fatal("WaitReady passed on a page whose ghost module never registered")
	}
	if !strings.Contains(err.Error(), "runtime module ghost has not registered") {
		t.Errorf("WaitReady error does not name the module: %v", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("WaitReady took %s to give up; the bound is %s", took, readyTimeout)
	}
}
