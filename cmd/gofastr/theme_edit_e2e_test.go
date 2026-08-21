package main

// Browser-level (chromedp) e2e for `gofastr theme edit`.
//
// The theme_edit_test.go suite asserts on the Go STRING that contains the
// editor's ~360 lines of bootstrap JS. It pins characters, not behaviour,
// and every one of those tests passes even when the function holding the
// asserted line is never called. Three real defects survived exactly that
// gap: a contrast check that could not fail, a Write that emitted the file
// without the operator's last keystroke, and a preview that reverted on
// iframe navigation. These tests close it by driving the real tool through
// a real headless Chrome.
//
// Gated by -short (slow, needs Chrome). The suite shares ONE browser across
// its tests; each test gets a fresh tab and its own httptest server on a
// unique port, so it never runs concurrently with another browser suite.

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ---------------------------------------------------------------------------
// Shared headless browser (one process, one tab per test).
// ---------------------------------------------------------------------------

var (
	themeBrowserOnce sync.Once
	themeBrowserRoot context.Context
	themeBrowserErr  error
	themeBrowserKill context.CancelFunc
	themeAllocKill   context.CancelFunc
)

// themeBrowserCtx returns a fresh foreground tab against the shared browser.
// A shared process avoids chromedp's cold-launch flake (its 20s websocket-URL
// deadline is intermittently exceeded across many sequential launches); a
// fresh tab keeps DOM/JS state isolated per test. Mirrors examples/site.
func themeBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	themeBrowserOnce.Do(func() {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.WindowSize(1280, 800),
		)
		allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		if err := chromedp.Run(browserCtx); err != nil {
			themeBrowserErr = err
			browserCancel()
			allocCancel()
			return
		}
		themeBrowserRoot = browserCtx
		themeBrowserKill = browserCancel
		themeAllocKill = allocCancel
	})
	if themeBrowserErr != nil {
		t.Fatalf("shared browser failed to start: %v", themeBrowserErr)
	}
	tabCtx, tabCancel := chromedp.NewContext(themeBrowserRoot)
	t.Cleanup(tabCancel) // closes the tab, not the browser
	ctx, cancel := context.WithTimeout(tabCtx, 60*time.Second)
	t.Cleanup(cancel)
	// Bring the tab forward: Chrome throttles rAF / timers on background
	// tabs, and chromedp.Poll's default "raf" mode would stall on a tab
	// left behind about:blank.
	if err := chromedp.Run(ctx, page.BringToFront()); err != nil {
		t.Fatalf("bring tab to front: %v", err)
	}
	return ctx
}

func TestMain(m *testing.M) {
	code := m.Run()
	if themeBrowserKill != nil {
		themeBrowserKill()
	}
	if themeAllocKill != nil {
		themeAllocKill()
	}
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Server + page wiring.
// ---------------------------------------------------------------------------

// newThemeEditE2EServer stands the themeEditServer up behind an httptest
// listener, wired EXACTLY as runThemeEdit wires it: the UIHost carries the
// gallery base CSS plus the contrast-probe CSS the /preview page needs for
// the check to measure real colours (newTestServer omits these because its
// suite never renders). hosts/origins are pinned to the ACTUAL bound port:
// a browser sends the real port in Host/Origin, and the DNS-rebinding guard
// would 403 every request against newTestServer's hardcoded "127.0.0.1:0".
func newThemeEditE2EServer(t *testing.T) (srv *themeEditServer, base string) {
	t.Helper()
	core := uitheme.Default()
	a := app.NewApp("theme-edit-e2e").WithTheme(core)
	a.Register("/preview", &galleryPreviewScreen{}, nil)
	host := uihost.New(a, uihost.WithCustomCSS(
		gallery.BaseCSS(core)+previewChromeCSS+contrastProbeCSS()))
	srv = &themeEditServer{
		host:    host,
		base:    core,
		working: core,
		outPath: filepath.Join(t.TempDir(), "theme.go"),
		hosts:   []string{"127.0.0.1:0"},
		origins: []string{"http://127.0.0.1:0"},
		token:   "e2e-token", // not-a-secret: fixture bearer for the in-process test server
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	srv.hosts, srv.origins = loopbackGuards(ts.Listener.Addr().String())
	return srv, ts.URL
}

// navigateEditor loads the controls page and waits for the preview iframe's
// app.css to apply, proven by the readiness sentinel probe reading back as
// literal black-on-white. Before the sheet lands the sentinel inherits the
// page background and the contrast check would publish a wrong reading, so
// every test waits here first.
func navigateEditor(t *testing.T, ctx context.Context, base string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Poll(previewReadyJS, nil),
	); err != nil {
		t.Fatalf("navigate editor: %v", err)
	}
}

// ---------------------------------------------------------------------------
// JS probes (run in the TOP frame; same-origin, they reach into the iframe).
// ---------------------------------------------------------------------------

// previewReadyJS is truthy once the iframe's readiness sentinel measures as
// literal black-on-white, the signal that the generated app.css has applied.
const previewReadyJS = `(function(){
  var f = document.getElementById('te-frame');
  if (!f || !f.contentDocument) return false;
  var s = f.contentDocument.querySelector('[data-cp="ready|sentinel"]');
  if (!s) return false;
  var cs = f.contentWindow.getComputedStyle(s);
  return cs.color === 'rgb(0, 0, 0)' && cs.backgroundColor === 'rgb(255, 255, 255)';
})()`

// setTokenInputJS sets a token control's text input and fires a real 'input'
// event, the exact path a keystroke takes through the debounce handler.
func setTokenInputJS(key, value string) string {
	return fmt.Sprintf(`(function(){
  var input = document.querySelector('[data-token=%[1]q]:not([data-type="color-swatch"])');
  if (!input) return 'no input for %[1]s';
  input.value = %[2]q;
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return true;
})()`, key, value)
}

// primaryColorIsJS is truthy once the preview's primary-fg|primary probe,
// whose background is var(--color-primary), resolves to the expected rgb.
// Reaching this state proves the edit round-tripped through applyToken →
// RegisterThemeVariant → the swapped app.css → a re-resolved var().
func primaryColorIsJS(wantRGB string) string {
	return fmt.Sprintf(`(function(){
  var f = document.getElementById('te-frame');
  if (!f || !f.contentDocument) return false;
  var p = f.contentDocument.querySelector('[data-cp="primary-fg|primary"]');
  if (!p) return false;
  return f.contentWindow.getComputedStyle(p).backgroundColor === %q;
})()`, wantRGB)
}

// primaryColorJS returns the preview's resolved --color-primary as rgb(...).
const primaryColorJS = `(function(){
  var f = document.getElementById('te-frame');
  if (!f || !f.contentDocument) return null;
  var p = f.contentDocument.querySelector('[data-cp="primary-fg|primary"]');
  if (!p) return null;
  return f.contentWindow.getComputedStyle(p).backgroundColor;
})()`

// contrastPanelJS returns the contrast panel's visibility + text.
const contrastPanelJS = `(function(){
  var el = document.getElementById('te-contrast');
  if (!el) return { hidden: true, text: '' };
  return { hidden: el.hidden, text: el.textContent };
})()`

type contrastPanelState struct {
	Hidden bool   `json:"hidden"`
	Text   string `json:"text"`
}

// contrastPanelMentionsJS is truthy once the visible panel names the pair.
func contrastPanelMentionsJS(pair string) string {
	return fmt.Sprintf(`(function(){
  var el = document.getElementById('te-contrast');
  if (!el || el.hidden) return false;
  return el.textContent.indexOf(%q) !== -1;
})()`, pair)
}

// contrastPanelLacksJS is truthy once the panel no longer mentions the pair.
func contrastPanelLacksJS(pair string) string {
	return fmt.Sprintf(`(function(){
  var el = document.getElementById('te-contrast');
  if (!el) return true;
  return el.textContent.indexOf(%q) === -1;
})()`, pair)
}

// writeDoneJS is truthy once a Write round-trip completes (button re-enabled
// and a status line posted), whether it succeeded or errored.
const writeDoneJS = `(function(){
  var b = document.getElementById('te-write');
  var s = document.getElementById('te-status');
  return !!(b && !b.disabled && s.textContent.length > 0);
})()`

// statusTextJS returns the status line's text.
const statusTextJS = `(function(){
  var el = document.getElementById('te-status');
  return el ? el.textContent : '';
})()`

// evalString runs a JS expression in the top frame and returns its result
// as a string. chromedp actions must execute via Run; a bare Evaluate
// returns an Action (not an error), so wrapping is what actually sends it.
func evalString(t *testing.T, ctx context.Context, expr string) string {
	t.Helper()
	var s string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &s)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return s
}

// evalPanel reads the contrast panel's visibility + text.
func evalPanel(t *testing.T, ctx context.Context) contrastPanelState {
	t.Helper()
	var p contrastPanelState
	if err := chromedp.Run(ctx, chromedp.Evaluate(contrastPanelJS, &p)); err != nil {
		t.Fatalf("evaluate panel: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Tests.
// ---------------------------------------------------------------------------

// 1. An edit reaches the rendered preview. Set color-primary to a distinctive
// value and assert the preview iframe's RESOLVED --color-primary actually
// changed: the core round trip, untested before.
func TestThemeEditPreviewReceivesEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	_, base := newThemeEditE2EServer(t)
	ctx := themeBrowserCtx(t)
	navigateEditor(t, ctx, base)

	before := evalString(t, ctx, primaryColorJS)

	// #01FF70 = rgb(1, 255, 112); the default primary is indigo (#4F46E5),
	// so a change here is unambiguous.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(setTokenInputJS("color-primary", "#01FF70"), nil),
		chromedp.Poll(primaryColorIsJS("rgb(1, 255, 112)"), nil),
	); err != nil {
		t.Fatalf("edit never reached the rendered preview: %v", err)
	}

	after := evalString(t, ctx, primaryColorJS)
	if after == before {
		t.Fatalf("preview primary did not change (before=%s after=%s) — the edit did not reach the rendered iframe", before, after)
	}
	if after != "rgb(1, 255, 112)" {
		t.Errorf("preview primary = %s, want rgb(1, 255, 112)", after)
	}
}

// 2. The contrast panel can report a failure. Set color-text to a poor-
// contrast value and assert the panel names the failing pair; then restore a
// passing value and assert the panel drops it. A check that cannot fail is
// the defect class this guards.
//
// The default theme already trips four DARK-mode findings (white text on the
// light dark-mode status tones); those are real, but out of scope here. This
// test isolates the LIGHT text|surface pair: it is clean on the default, so a
// failing edit is the only thing that can introduce it, and restoring the
// value is the only thing that can remove it.
func TestThemeEditContrastPanelReportsFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	_, base := newThemeEditE2EServer(t)
	ctx := themeBrowserCtx(t)
	navigateEditor(t, ctx, base)

	// text|surface passes in both schemes on the default theme (light text
	// #18181B on #FFFFFF; dark text #FAFAFA on #18181B), so it is absent.
	panel := evalPanel(t, ctx)
	if strings.Contains(panel.Text, "text|surface") {
		t.Fatalf("text|surface should pass on the default theme, but the panel already names it: %q", panel.Text)
	}

	// #999999 on the default light surface (#FFFFFF) is ~2.85:1, below the
	// 4.5 threshold the checker enforces.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(setTokenInputJS("color-text", "#999999"), nil),
		chromedp.Poll(contrastPanelMentionsJS("text|surface"), nil),
	); err != nil {
		t.Fatalf("contrast panel never reported the failing pair: %v", err)
	}
	panel = evalPanel(t, ctx)
	if panel.Hidden {
		t.Fatal("contrast panel is hidden after a failing edit — a check that cannot fail is the defect class this test exists for")
	}
	if !strings.Contains(panel.Text, "text|surface") {
		t.Errorf("panel does not name the failing pair text|surface: %q", panel.Text)
	}

	// Restore a passing value; the panel must drop the pair it reported.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(setTokenInputJS("color-text", "#000000"), nil),
		chromedp.Poll(contrastPanelLacksJS("text|surface"), nil),
	); err != nil {
		t.Fatalf("contrast panel never dropped the pair after restoring a passing value: %v", err)
	}
	panel = evalPanel(t, ctx)
	if strings.Contains(panel.Text, "text|surface") {
		t.Errorf("contrast panel still names text|surface after restoring a passing value: %q", panel.Text)
	}
}

// 3. Write includes the operator's last edit. Type into a token and click
// Write quickly enough to race the 300 ms debounce, then read the file from
// disk and assert it carries the typed value. The Go writeBack reads only
// s.working; without flushing the pending debounced edit, a Write that beat
// the timer emitted the theme WITHOUT the last keystroke.
func TestThemeEditWriteIncludesLastEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	srv, base := newThemeEditE2EServer(t)
	ctx := themeBrowserCtx(t)
	navigateEditor(t, ctx, base)

	// Set the value AND click Write in one JS turn so the click lands well
	// inside the 300 ms debounce, the exact race that loses the edit.
	const raceJS = `(function(){
  var input = document.querySelector('[data-token="color-primary"]:not([data-type="color-swatch"])'); // not-a-secret: a DOM attribute selector
  input.value = "#AB12CD";
  input.dispatchEvent(new Event('input', { bubbles: true }));
  document.getElementById('te-write').click();
  return true;
})()`
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(raceJS, nil),
		chromedp.Poll(writeDoneJS, nil),
	); err != nil {
		t.Fatalf("write did not complete: %v", err)
	}

	status := evalString(t, ctx, statusTextJS)
	src, err := os.ReadFile(srv.outPath)
	if err != nil {
		var detail string
		if status != "" {
			detail = " (status: " + status + ")"
		}
		t.Fatalf("read written file: %v%s", err, detail)
	}
	// The emitter writes the value as a %q double-quoted literal.
	if !strings.Contains(string(src), `"#AB12CD"`) {
		t.Errorf("written file does not carry the last edit (#AB12CD); status=%q\n--- file ---\n%s", status, truncate(string(src), 400))
	}
}

// 4. Write twice in one session. Edit, Write, edit again, Write again: both
// must succeed. The first Write creates the file; without the session-owned-
// file digest the second Write refused, citing a file the tool itself had
// just produced.
func TestThemeEditWriteTwiceInOneSession(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	srv, base := newThemeEditE2EServer(t)
	ctx := themeBrowserCtx(t)
	navigateEditor(t, ctx, base)

	// First edit + Write.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(setTokenInputJS("color-primary", "#111111"), nil),
		chromedp.Poll(primaryColorIsJS("rgb(17, 17, 17)"), nil),
		chromedp.Click("#te-write", chromedp.ByID),
		chromedp.Poll(writeDoneJS, nil),
	); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	status1 := evalString(t, ctx, statusTextJS)
	if strings.Contains(status1, "already exists") {
		t.Fatalf("first write was refused: %q", status1)
	}

	// Second edit + Write.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(setTokenInputJS("color-primary", "#222222"), nil),
		chromedp.Poll(primaryColorIsJS("rgb(34, 34, 34)"), nil),
		chromedp.Click("#te-write", chromedp.ByID),
		chromedp.Poll(writeDoneJS, nil),
	); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	status2 := evalString(t, ctx, statusTextJS)

	src, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatalf("read written file: %v (status1=%q status2=%q)", err, status1, status2)
	}
	if strings.Contains(status2, "already exists") {
		t.Fatalf("second write was refused — the session cannot rewrite its own file: %q", status2)
	}
	if !strings.Contains(string(src), `"#222222"`) {
		t.Errorf("written file does not carry the second edit (#222222); status1=%q status2=%q\n--- file ---\n%s", status1, status2, truncate(string(src), 400))
	}
}
