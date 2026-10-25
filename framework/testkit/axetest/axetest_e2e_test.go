package axetest

import (
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
// times the page itself was requested.
func schemeFixture(t *testing.T, only string) (url string, loads *atomic.Int32) {
	t.Helper()
	loads = new(atomic.Int32)
	page := fmt.Sprintf(`<!doctype html><html lang="en"><head><title>scheme fixture</title>
<meta name="color-scheme" content="light">
<style>
body { background: #fff; color: #000; }
[data-color-scheme="dark"] body { background: #000; color: #fff; }
[data-color-scheme=%q] .trap { color: %s; }
</style></head><body><main><h1>Fixture</h1>
<p class="trap">Readable in one scheme only.</p><p>One.</p><p>Two.</p><p>Three.</p>
</main></body></html>`, only, map[string]string{"dark": "#111", "light": "#eee"}[only])
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
			url, loads := schemeFixture(t, only)
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
