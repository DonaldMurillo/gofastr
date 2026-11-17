package axetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/axecov"
)

func TestSweepAxeRulesJSKeepsDefaultsAndForcesOverrides(t *testing.T) {
	if got := axeRulesJS(scanConfig{}); got != "{}" {
		t.Errorf("no options = %s, want {}", got)
	}
	got := axeRulesJS(scanConfig{enabledRules: []string{"target-size"}, disabledRules: []string{"region"}})
	want := `{ "target-size": { enabled: true }, "region": { enabled: false } }`
	if got != want {
		t.Errorf("rules = %s, want %s", got, want)
	}
}

func TestSweepFilteredErrorfDropsNodeEventNoise(t *testing.T) {
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(prev); log.SetFlags(flags) }()
	filteredErrorf("unhandled node event %s", "*dom.EventAdoptedStyleSheetsModified")
	filteredErrorf("could not %s", "dial")
	if got := buf.String(); got != "ERROR: could not dial\n" {
		t.Errorf("logged %q, want only the real error", got)
	}
}

func TestSweepNewBrowserContextStartsTheBrowserEagerly(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	if _, done, err := NewBrowserContext(parent); err == nil {
		done()
		t.Fatal("NewBrowserContext on a cancelled context returned no error; the browser was not started")
	}
}

func sweepServe(t *testing.T, csp string, pages map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Content-Type", "text/css")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		if csp != "" {
			w.Header().Set("Content-Security-Policy", csp)
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func sweepLoad(t *testing.T, url string) (ctxDone func(), run func(...chromedp.Action) error) {
	t.Helper()
	ctx, cancel := NewTab(t, NewBrowser(t))
	if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady()); err != nil {
		cancel()
		t.Fatalf("load %s: %v", url, err)
	}
	return cancel, func(a ...chromedp.Action) error { return chromedp.Run(ctx, a...) }
}

func TestSweepScanRefusesABlankPage(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	base := sweepServe(t, "", map[string]string{"/": `<!doctype html><html lang="en"><head><title>blank</title></head><body><p>x</p></body></html>`})
	ctx, cancel := NewTab(t, NewBrowser(t))
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), WaitReady()); err != nil {
		t.Fatal(err)
	}
	if vs, err := Scan(ctx, "light", nil); err == nil || !strings.Contains(err.Error(), "vacuous") {
		t.Fatalf("Scan of a one-element page = %v, %v; want the vacuous-pass refusal", vs, err)
	}
}

func TestSweepScanDropsAllowlistedViolations(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	url, _ := schemeFixture(t, "dark", "")
	ctx, cancel := NewTab(t, NewBrowser(t))
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady()); err != nil {
		t.Fatal(err)
	}
	vs, err := ScanSchemes(ctx, Schemes, map[string]string{"color-contrast": "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.ID == "color-contrast" {
			t.Errorf("allowlisted color-contrast still reported under %s", v.Scheme)
		}
	}
}

func TestSweepScanRecordsCoverageUnlessDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	url, _ := schemeFixture(t, "dark", "")
	browser := NewBrowser(t)
	scan := func(t *testing.T) string {
		dir := t.TempDir()
		t.Setenv("GOFASTR_AXE_COVERAGE_DIR", dir)
		ctx, cancel := NewTab(t, browser)
		defer cancel()
		if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady(), Prepare("light")); err != nil {
			t.Fatal(err)
		}
		if _, err := Scan(ctx, "light", nil); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	t.Run("recorded", func(t *testing.T) {
		m, err := axecov.Read(scan(t))
		if err != nil {
			t.Fatal(err)
		}
		if got := m.Pages["/"]; len(got) != 1 || got[0] != "light" {
			t.Errorf("manifest pages = %v, want / scanned under light", m.Pages)
		}
	})
	t.Run("unwritable", func(t *testing.T) {
		ctx, cancel := NewTab(t, browser)
		defer cancel()
		t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "/dev/null/axe")
		if err := chromedp.Run(ctx, chromedp.Navigate(url), WaitReady(), Prepare("light")); err != nil {
			t.Fatal(err)
		}
		if _, err := Scan(ctx, "light", nil); err != nil {
			t.Errorf("a coverage directory that cannot be written failed the scan: %v", err)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		t.Setenv("GOFASTR_AXE_COVERAGE", "0")
		if _, err := axecov.Read(scan(t)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("GOFASTR_AXE_COVERAGE=0 still wrote a manifest (err %v)", err)
		}
	})
}

func TestSweepPrepareFreezesTransitionsUnderCSP(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	base := sweepServe(t, "default-src 'self'", map[string]string{
		"/":      `<!doctype html><html lang="en"><head><title>csp</title><link rel="stylesheet" href="/s.css"></head><body><main><p>CSP fixture</p></main></body></html>`,
		"/s.css": `body { transition: color 5s; }`,
	})
	done, run := sweepLoad(t, base+"/")
	defer done()
	var got struct {
		Duration string `json:"duration"`
		Meta     string `json:"meta"`
	}
	if err := run(Prepare("dark"), chromedp.Evaluate(`({
		duration: getComputedStyle(document.body).transitionDuration,
		meta: (document.querySelector('meta[name="color-scheme"]') || {content: ''}).content
	})`, &got)); err != nil {
		t.Fatal(err)
	}
	if got.Duration != "0s" {
		t.Errorf("transition-duration under a default-src 'self' CSP = %q, want 0s", got.Duration)
	}
	if got.Meta != "dark" {
		t.Errorf("<meta name=color-scheme> = %q, want the fallback to create it with dark", got.Meta)
	}
}

func TestSweepPreparePrefersTheSchemeBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: -short")
	}
	base := sweepServe(t, "", map[string]string{"/": `<!doctype html><html lang="en"><head><title>boot</title>
<meta name="color-scheme" content="light">
<script>window.__gofastr_colorScheme = {set(m) {
  window.__setCalls = (window.__setCalls || 0) + 1;
  document.documentElement.setAttribute('data-color-scheme', m);
  document.querySelector('meta[name="color-scheme"]').setAttribute('content', m);
}};</script></head><body><main><p>bootstrap fixture</p></main></body></html>`})
	done, run := sweepLoad(t, base+"/")
	defer done()
	var calls int
	if err := run(Prepare("dark"), chromedp.Evaluate(`window.__setCalls || 0`, &calls)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("__gofastr_colorScheme.set called %d times, want 1", calls)
	}
}
