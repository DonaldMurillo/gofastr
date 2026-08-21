package main

// Real-browser (chromedp) tests for the theme editor's inline JS. Every
// other test in theme_edit_test.go asserts on the Go string that HOLDS the
// JavaScript; those pin characters, not behaviour, and pass even when the
// function holding the asserted line is never called. These four drive the
// actual tool through a headless Chrome and assert what the operator sees.
//
// Gated by -short, matching every other browser test in this package
// (generate_sdkjs_browser_test.go, dev_e2e_test.go): `go test
// ./cmd/gofastr/ -count=1` runs them; `-short` skips them. They never run
// concurrently with another browser suite: Go runs a package's tests
// sequentially unless t.Parallel is called, and these never call it.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/chromedp/chromedp"
)

// newBrowserThemeServer stands up a themeEditServer behind an httptest.Server
// whose Host/Origin guards are pinned to the real ephemeral authority the
// browser will send, exactly what runThemeEdit does after net.Listen.
//
// It mirrors runThemeEdit's host wiring, which newTestServer does NOT: the
// contrast probes are only measurable once contrastProbeCSS is injected, and
// the gallery demos need gallery.BaseCSS. A browser test that drives the
// preview iframe must build the host the way the real tool does.
func newBrowserThemeServer(t *testing.T) (*themeEditServer, *httptest.Server) {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "theme")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := uitheme.Default()
	a := app.NewApp("theme-edit-browser").WithTheme(base)
	a.Register("/preview", &galleryPreviewScreen{}, nil)
	host := uihost.New(a, uihost.WithCustomCSS(gallery.BaseCSS(base)+previewChromeCSS+contrastProbeCSS()))
	srv := &themeEditServer{
		host:    host,
		base:    base,
		working: base,
		outPath: filepath.Join(outDir, "theme.go"),
		hosts:   []string{"127.0.0.1:0"},
		origins: []string{"http://127.0.0.1:0"},
		token:   "browser-test-token", // not-a-secret: fixture bearer for the in-process test server
	}
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)
	u, err := url.Parse(httpSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	srv.hosts = []string{u.Host}
	srv.origins = []string{httpSrv.URL}
	return srv, httpSrv
}

// newBrowserThemeServerWithDelayedVariantCSS stands up the same server as
// newBrowserThemeServer, but wraps the host so every /__gofastr/app.css?t=<key>
// response (the preview iframe's variant stylesheet) sleeps for `delay`
// before serving. Used to reproduce the slow-network race where the 1.5s
// fallback in swapPreviewCSS certifies the OLD theme while the NEW sheet is
// still in flight.
func newBrowserThemeServerWithDelayedVariantCSS(t *testing.T, delay time.Duration) (*themeEditServer, *httptest.Server) {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "theme")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := uitheme.Default()
	a := app.NewApp("theme-edit-browser-delayed").WithTheme(base)
	a.Register("/preview", &galleryPreviewScreen{}, nil)
	host := uihost.New(a, uihost.WithCustomCSS(gallery.BaseCSS(base)+previewChromeCSS+contrastProbeCSS()))
	srv := &themeEditServer{
		host:    host,
		base:    base,
		working: base,
		outPath: filepath.Join(outDir, "theme.go"),
		hosts:   []string{"127.0.0.1:0"},
		origins: []string{"http://127.0.0.1:0"},
		token:   "browser-test-token", // not-a-secret: fixture bearer for the in-process test server
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the iframe's variant stylesheet is slow. Editor API endpoints
		// (/__theme/*) and the unvariant'd base app.css must respond at full
		// speed or the test would time out on navigation.
		if strings.HasPrefix(r.URL.Path, "/__gofastr/app.css") && r.URL.Query().Get("t") != "" {
			time.Sleep(delay)
		}
		srv.ServeHTTP(w, r)
	})
	httpSrv := httptest.NewServer(handler)
	t.Cleanup(httpSrv.Close)
	u, err := url.Parse(httpSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	srv.hosts = []string{u.Host}
	srv.origins = []string{httpSrv.URL}
	return srv, httpSrv
}

// themeEditBrowserCtx boots a fresh headless Chrome for one test. Per-test
// allocation is the established shape in this package
// (generate_sdkjs_browser_test.go, blueprint_test.go).
func themeEditBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	// chromedp starts Chrome lazily on the first Run: allocate against the
	// browser context so the browser's lifetime is the browser context's;
	// passing a timeout context here would make the browser die when that
	// deadline passed. The watchdog bounds only the startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}

	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ─── shared page-JS probes (evaluated inside the controls page) ────────────

// tePreviewReadyJS is true once the preview iframe has loaded AND its
// readiness sentinel probe reads literal black-on-white: the signal the
// generated stylesheet has applied. Measuring before this point reads an
// unstyled document and reports whatever the inheritance happened to leave.
const tePreviewReadyJS = `(function () {
  var f = document.getElementById('te-frame');
  if (!f || !f.contentDocument) return false;
  var s = f.contentDocument.querySelector('[data-cp="ready|sentinel"]');
  if (!s) return false;
  var cs = f.contentWindow.getComputedStyle(s);
  return cs.color === 'rgb(0, 0, 0)' && cs.backgroundColor === 'rgb(255, 255, 255)';
})()`

// teSetControlJS returns a JS expression that sets a token's text input,
// dispatches the 'input' event (arming the 300ms debounce the same way a
// real keystroke does), and returns the value the input now holds.
func teSetControlJS(key, value string) string {
	return fmt.Sprintf(`(function () {
  var el = document.querySelector('[data-token=%[1]q]:not([data-type="color-swatch"])');
  if (!el) throw new Error('no text input for ' + %[1]q);
  el.focus();
  el.value = %[2]q;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  return el.value;
})()`, key, value)
}

// teReadControlJS returns a JS expression yielding a token input's current
// value (the source of truth, not the colour-picker swatch beside it).
func teReadControlJS(key string) string {
	return fmt.Sprintf(`(function () {
  var el = document.querySelector('[data-token=%q]:not([data-type="color-swatch"])');
  return el ? el.value : '';
})()`, key)
}

// tePreviewTokenJS returns a JS expression yielding the preview iframe's
// computed value for a CSS custom property, read off :root. This is the
// rendered round trip: edit → ApplyTokens → variant app.css → browser.
func tePreviewTokenJS(cssVar string) string {
	return fmt.Sprintf(`(function () {
  var f = document.getElementById('te-frame');
  if (!f || !f.contentDocument) return '';
  var v = f.contentWindow.getComputedStyle(f.contentDocument.documentElement).getPropertyValue(%q);
  return v ? String(v).trim() : '';
})()`, cssVar)
}

// teTypeAndWriteJS types a value into a token AND clicks Write inside the
// same round-trip, so the click lands inside the 300ms debounce window,
// the exact race in which an unflushed Write emits a stale theme.
func teTypeAndWriteJS(key, value string) string {
	return fmt.Sprintf(`(function () {
  var el = document.querySelector('[data-token=%[1]q]:not([data-type="color-swatch"])');
  if (!el) throw new Error('no input for ' + %[1]q);
  el.focus();
  el.value = %[2]q;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  document.getElementById('te-write').click();
})()`, key, value)
}

const (
	teContrastVisibleJS = `(function () {
  var el = document.getElementById('te-contrast');
  return el ? !el.hidden : false;
})()`
	teContrastTextJS = `(function () {
  var el = document.getElementById('te-contrast');
  return el ? (el.textContent || '') : '';
})()`
	teStatusTextJS = `(document.getElementById('te-status').textContent) || ''`
)

// ─── shared Go waiters ─────────────────────────────────────────────────────

func waitPreviewReady(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(tePreviewReadyJS, &ready)); err == nil && ready {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("preview iframe never became ready — sentinel probe did not read black-on-white")
}

func waitStatusContains(t *testing.T, ctx context.Context, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		var status string
		if err := chromedp.Run(ctx, chromedp.Evaluate(teStatusTextJS, &status)); err == nil {
			last = status
			if strings.Contains(status, want) {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("status never reported %q (last=%q)", want, last)
}

func contrastVisible(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var visible bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(teContrastVisibleJS, &visible)); err != nil {
		t.Fatalf("read contrast visibility: %v", err)
	}
	return visible
}

func contrastText(t *testing.T, ctx context.Context) string {
	t.Helper()
	var text string
	if err := chromedp.Run(ctx, chromedp.Evaluate(teContrastTextJS, &text)); err != nil {
		t.Fatalf("read contrast panel: %v", err)
	}
	return text
}

func navigateToEditor(t *testing.T, ctx context.Context, httpSrv *httptest.Server) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(httpSrv.URL+"/"),
		chromedp.WaitVisible(`#te-write`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate to editor: %v", err)
	}
	waitPreviewReady(t, ctx)
}

// ─── tests ─────────────────────────────────────────────────────────────────

// TestEditReachesPreview is the core round trip nothing else exercises: set a
// token through the controls page and assert the RENDERED preview iframe's
// computed --color-primary actually changed. A string-contains test on the JS
// cannot tell whether applyEdit's swapPreviewCSS ever ran.
func TestEditReachesPreview(t *testing.T) {
	_, httpSrv := newBrowserThemeServer(t)
	ctx := themeEditBrowserCtx(t)
	navigateToEditor(t, ctx, httpSrv)

	// Sanity: capture a non-default baseline so "changed" is unambiguous.
	var before string
	_ = chromedp.Run(ctx, chromedp.Evaluate(tePreviewTokenJS("--color-primary"), &before))

	const want = "#FF00FF" // distinct from the default #4F46E5
	if err := chromedp.Run(ctx, chromedp.Evaluate(teSetControlJS("color-primary", want), nil)); err != nil {
		t.Fatalf("set color-primary: %v", err)
	}

	deadline := time.Now().Add(12 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		_ = chromedp.Run(ctx, chromedp.Evaluate(tePreviewTokenJS("--color-primary"), &got))
		if strings.EqualFold(strings.TrimSpace(got), want) {
			return // pass
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("preview never reflected the edit: --color-primary was %q before, %q after; want %q.\n"+
		"The apply→RegisterThemeVariant→app.css swap path did not land in the rendered iframe.",
		before, got, want)
}

// TestContrastPanelReportsFailure pins that the checker CAN fail. Set
// color-text to a low-contrast value against the default light surface and
// assert the panel becomes visible and names the failing pair; then restore a
// passing value and assert it clears. A check that cannot fail is the exact
// defect class this surface shipped with: inline-styled probes that CSP
// dropped, every ratio ~20:1, "no issues" for every theme.
func TestContrastPanelReportsFailure(t *testing.T) {
	_, httpSrv := newBrowserThemeServer(t)
	ctx := themeEditBrowserCtx(t)
	navigateToEditor(t, ctx, httpSrv)

	// The shipped default theme must pass its own contrast check, in both
	// schemes, with nothing edited. That is a real invariant and worth pinning:
	// the probe used to hardcode #ffffff as the status fills' foreground where
	// the design system paints var(--color-primary-fg), which invented four
	// dark-scheme failures for text nothing renders. A checker that cries wolf
	// gets ignored exactly like one that never fires.
	time.Sleep(500 * time.Millisecond)
	if txt := contrastText(t, ctx); strings.TrimSpace(txt) != "" {
		t.Fatalf("the default theme fails its own contrast check before any edit: %q", txt)
	}

	// #999999 on the default #FFFFFF surface is ~2.7:1, below the 4.5 bar.
	var original string
	_ = chromedp.Run(ctx, chromedp.Evaluate(teReadControlJS("color-text"), &original))
	if err := chromedp.Run(ctx, chromedp.Evaluate(teSetControlJS("color-text", "#999999"), nil)); err != nil {
		t.Fatalf("set color-text to low-contrast value: %v", err)
	}

	// The contrast check retries until the swapped sheet is measurable
	// (sentinel-gated), then renders. Wait for it to name OUR pair.
	deadline := time.Now().Add(12 * time.Second)
	var reported string
	for time.Now().Before(deadline) {
		reported = contrastText(t, ctx)
		if contrastVisible(t, ctx) && strings.Contains(reported, "text|surface") && strings.Contains(reported, "light") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !contrastVisible(t, ctx) || !strings.Contains(reported, "text|surface") {
		t.Fatalf("contrast panel never reported the introduced text|surface failure.\n"+
			"A checker that cannot fail is the defect class — panel visible=%v, text=%q",
			contrastVisible(t, ctx), reported)
	}

	// Restore a passing value; the text|surface finding must clear (the
	// panel must not stay frozen on the old reading, the separate "panel
	// kept whatever it last showed" bug). Pre-existing dark findings may
	// remain; only OUR finding is required to disappear.
	if err := chromedp.Run(ctx, chromedp.Evaluate(teSetControlJS("color-text", original), nil)); err != nil {
		t.Fatalf("restore color-text: %v", err)
	}
	deadline = time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if !strings.Contains(contrastText(t, ctx), "text|surface") {
			return // pass
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("text|surface finding never cleared after restoring a passing value; text=%q", contrastText(t, ctx))
}

// TestWriteIncludesLastEdit is the known race: type into a token and click
// Write inside the 300ms debounce, then read the file from disk and assert it
// carries the typed value. The Write handler used to POST immediately,
// consulting neither pendingTimers nor applyQueue, so the file was emitted
// without the operator's last edit. Written first; watched fail; then fixed.
func TestWriteIncludesLastEdit(t *testing.T) {
	srv, httpSrv := newBrowserThemeServer(t)
	ctx := themeEditBrowserCtx(t)
	navigateToEditor(t, ctx, httpSrv)

	const want = "#ABCDEF"
	// Type + click in ONE round-trip: the click beats the 300ms timer.
	raceStart := time.Now()
	if err := chromedp.Run(ctx, chromedp.Evaluate(teTypeAndWriteJS("color-primary", want), nil)); err != nil {
		t.Fatalf("type-and-write race: %v", err)
	}
	waitStatusContains(t, ctx, "wrote", 12*time.Second)
	t.Logf("'wrote' status observed after %v (debounce window is 300ms)", time.Since(raceStart))

	got, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatalf("read written file %s: %v", srv.outPath, err)
	}
	if !strings.Contains(string(got), want) {
		t.Fatalf("written file does not contain the operator's last edit %q — "+
			"Write raced the debounce and emitted a stale theme.\nfile:\n%s",
			want, truncate(string(got), 600))
	}
}

// TestWriteBlocksOnInvalidPendingEdit pins: typing an INVALID value into a
// token and clicking Write inside the 300ms debounce must NOT overwrite the
// theme file. The apply endpoint returns 422 {"error":"invalid color"};
// flushPendingEdits must propagate that failure and the Write handler must
// refuse to POST /__theme/writeback, telling the operator which token failed.
//
// Before the fix applyEdit resolved normally on a {error:...} response, so
// flushPendingEdits resolved its queue regardless, and the Write handler
// immediately POSTed /__theme/writeback, overwriting the file with the
// PREVIOUS theme while the control still showed the typed-but-rejected
// value. Observed request order: ['/__theme/apply', '/__theme/writeback'].
func TestWriteBlocksOnInvalidPendingEdit(t *testing.T) {
	srv, httpSrv := newBrowserThemeServer(t)
	ctx := themeEditBrowserCtx(t)
	navigateToEditor(t, ctx, httpSrv)

	// Type an invalid value AND click Write in ONE round-trip: the click
	// lands inside the 300ms debounce window, exactly the race in which the
	// old code certified the previous theme.
	if err := chromedp.Run(ctx, chromedp.Evaluate(teTypeAndWriteJS("color-primary", "red;}"), nil)); err != nil {
		t.Fatalf("type-and-write race: %v", err)
	}

	// Wait for a terminal status: with the bug this becomes "wrote …"
	// (writeback proceeded); with the fix it names the invalid token.
	deadline := time.Now().Add(12 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(teStatusTextJS, &status)); err == nil {
			status = strings.TrimSpace(status)
			// Terminal: status mentions the token (blocked) OR "wrote" (the bug).
			if strings.Contains(status, "color-primary") || strings.Contains(status, "wrote") {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The status must NOT report a successful write; the apply was rejected
	// at the ApplyTokens boundary, so the write must be blocked.
	if strings.Contains(status, "wrote") {
		t.Fatalf("writeback succeeded despite a pending INVALID edit.\n"+
			"applyEdit returned {error:...}; flushPendingEdits should have "+
			"rejected, blocking the write. Status: %q", status)
	}
	// And the operator must be told WHICH token is invalid: a bare
	// "validation failed" forces them to scan every control.
	if !strings.Contains(status, "color-primary") {
		t.Fatalf("status does not name the invalid token; the operator cannot "+
			"tell which edit to fix. Status: %q", status)
	}

	// The write must be blocked entirely: no file on disk. With the bug the
	// previous theme was written here; with the fix nothing reaches disk.
	if _, err := os.Stat(srv.outPath); err == nil {
		written, _ := os.ReadFile(srv.outPath)
		t.Fatalf("writeback reached disk despite the invalid pending edit.\n"+
			"The apply failure must block the write entirely.\n"+
			"Status: %q\nFile:\n%s", status, truncate(string(written), 400))
	}
}

// TestContrastCheckRunsAfterLateStylesheetLoad pins: when the preview
// iframe's variant CSS takes longer than the 1.5s fallback to land, the
// contrast check must STILL re-run after the late load and measure the NEW
// theme. The previous swapPreviewCSS set done=true after the fallback fired
// and ignored the late load event; the readiness sentinel couldn't tell the
// generations apart (both sheets paint literal black-on-white), so the panel
// measured and passed the OLD theme and never re-checked.
func TestContrastCheckRunsAfterLateStylesheetLoad(t *testing.T) {
	// Delay the variant CSS past the 1.5s fallback so the fallback MUST fire
	// first. The late load then lands at ~2s, the only path that lets the
	// panel measure the new theme.
	srv, httpSrv := newBrowserThemeServerWithDelayedVariantCSS(t, 2*time.Second)
	_ = srv
	ctx := themeEditBrowserCtx(t)
	navigateToEditor(t, ctx, httpSrv)

	// Sanity: the default theme passes its own contrast check before any
	// edit. (Pre-existing dark-scheme findings may exist; what matters is
	// that light-scheme text|surface is NOT among them.)
	time.Sleep(500 * time.Millisecond)
	if txt := contrastText(t, ctx); strings.Contains(txt, "text|surface") && strings.Contains(txt, "light") {
		t.Fatalf("default theme already trips a light-scheme text|surface failure before any edit: %q", txt)
	}

	// Edit color-text to a low-contrast value (#999999 on #FFFFFF ≈ 2.7:1).
	if err := chromedp.Run(ctx, chromedp.Evaluate(teSetControlJS("color-text", "#999999"), nil)); err != nil {
		t.Fatalf("set color-text to low-contrast value: %v", err)
	}

	// The 1.5s fallback fires before the 2s-delayed variant CSS lands. With
	// the bug the panel locks onto the OLD theme (which passes the check)
	// and never re-runs. With the fix the late load re-fires the callback,
	// the check verifies the generation before publishing, and the panel
	// names the new failure.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		reported := contrastText(t, ctx)
		if contrastVisible(t, ctx) && strings.Contains(reported, "text|surface") && strings.Contains(reported, "light") {
			return // pass: the late load re-ran the check and it measured the NEW theme
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("contrast panel never named the light-scheme text|surface failure after the late stylesheet load.\n"+
		"One of two regressions is back: (a) the 1.5s fallback certified the OLD theme and the late load was ignored, or\n"+
		"(b) the late load re-fired but the check couldn't tell which generation it measured.\n"+
		"Last reported: %q", contrastText(t, ctx))
}

// TestWriteTwiceInSession pins the session-owned-file digest: after the first
// Write creates the file, a second Write must succeed (not refuse the file the
// tool itself just wrote) and reflect the second edit. Both must return ok.
func TestWriteTwiceInSession(t *testing.T) {
	srv, httpSrv := newBrowserThemeServer(t)
	ctx := themeEditBrowserCtx(t)
	navigateToEditor(t, ctx, httpSrv)

	// edit → let the debounce apply land → write
	writeTwiceStep(t, ctx, "color-primary", "#101010", "wrote")
	// edit again → write again; must not be refused as "already exists".
	writeTwiceStep(t, ctx, "color-primary", "#202020", "wrote")

	got, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatalf("read written file %s: %v", srv.outPath, err)
	}
	if !strings.Contains(string(got), "#202020") {
		t.Fatalf("second write did not land in the file; content:\n%s", truncate(string(got), 600))
	}
}

// writeTwiceStep applies an edit, waits for the debounce to settle it on the
// server, then clicks Write and waits for the status. Waiting for the apply
// isolates "write twice" from the debounce race covered by TestWriteIncludesLastEdit.
func writeTwiceStep(t *testing.T, ctx context.Context, key, value, statusSub string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(teSetControlJS(key, value), nil)); err != nil {
		t.Fatalf("set %s=%s: %v", key, value, err)
	}
	waitStatusContains(t, ctx, "updated", 8*time.Second)
	if err := chromedp.Run(ctx, chromedp.Click(`#te-write`, chromedp.ByID)); err != nil {
		t.Fatalf("click Write: %v", err)
	}
	waitStatusContains(t, ctx, statusSub, 12*time.Second)
}
