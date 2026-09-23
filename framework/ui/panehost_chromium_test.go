package ui

// Hard rule 9: the drawer is layout + a pseudo-element scrim + a focus
// trap, none of which a DOM dump can see. This suite renders the real
// component with its real stylesheet and the real headless-panehost
// module, at both sides of the 768px breakpoint, and measures what the
// browser actually computes: the scrim paints only while a pane is open
// under the breakpoint, the scroll lock is taken and released, Tab wraps
// inside the drawer, a backdrop click closes it, the grid columns follow
// a client-side open (the hook the module maintains, not just the
// first-paint classes), and Escape is light-dismiss only in overlay
// mode — an inline column is page content.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// paneHostTestPage serves the component, its stylesheet, the kernel and
// the module registry the way the host does, so the behaviour under
// test is the shipped one.
func paneHostTestPage(t *testing.T, body string) *httptest.Server {
	t.Helper()
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	block := runtime.BehaviorsJSON()
	if block == nil {
		t.Fatal("runtime.BehaviorsJSON returned nil")
	}
	css := paneHostStyle.Entry().CSSFor(theme.Default())
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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><style>%s</style>`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`%s<script src="/__gofastr/runtime.js"></script></body></html>`, css, block, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const paneHostDrawerLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-panehost'])`

func paneHostDrawerBody() render.HTML {
	return PaneHost(PaneHostConfig{
		Primary: render.Join(
			render.Tag("button", map[string]string{"id": "open", "type": "button", "data-hui-pane-open-control": "secondary"}, render.Text("Open")),
			render.Text("The list"),
		),
		Secondary: render.Join(
			render.Tag("a", map[string]string{"id": "p-first", "href": "#a"}, render.Text("first")),
			render.Tag("a", map[string]string{"id": "p-last", "href": "#b"}, render.Text("last")),
		),
	})
}

// The phone drawer: no scrim on a closed page, scrim + scroll lock +
// Tab trap + backdrop close while open, all released on close.
func TestPaneHostOverlayDrawerChromium(t *testing.T) {
	srv := paneHostTestPage(t, string(paneHostDrawerBody()))
	ctx := chromedptest.Context(t)
	var closedScrim, modeBefore, openAttr bool
	var overflowWhileOpen, overflowAfterClose string
	var scrimWhileOpen, activeAfterTab, paneHiddenAfterBackdrop, modeAfterClose bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Poll(paneHostDrawerLoaded, nil, chromedp.WithPollingTimeout(15*time.Second)),
		// Under the breakpoint, with no pane open.
		chromedp.EmulateViewport(390, 844),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('[data-hui-panehost]').hasAttribute('data-hui-pane-mode')`, &modeBefore),
		chromedp.Evaluate(`(function () { var c = getComputedStyle(document.querySelector('[data-hui-panehost]'), '::before').content; return c === 'none' || c === 'normal'; })()`, &closedScrim),
		// Open: overlay mode, scrim painted, scroll locked.
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-hui-panehost]').getAttribute('data-hui-pane-mode') === 'overlay'`, &openAttr),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowWhileOpen),
		chromedp.Evaluate(`(function () { var c = getComputedStyle(document.querySelector('[data-hui-panehost]'), '::before').content; return c !== 'none' && c !== 'normal'; })()`, &scrimWhileOpen),
		// Tab from the pane's LAST focusable lands on its first.
		chromedp.Evaluate(`document.getElementById('p-last').focus()`, nil),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true, cancelable: true}))`, nil),
		chromedp.Evaluate(`document.activeElement === document.getElementById('p-first')`, &activeAfterTab),
		// Escape in overlay mode closes.
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}))`, nil),
		chromedp.Sleep(150*time.Millisecond),
		// Re-open, then a click on the host itself (the scrim lands
		// there) closes the topmost pane and releases the lock.
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-hui-panehost]').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &paneHiddenAfterBackdrop),
		chromedp.Evaluate(`!document.querySelector('[data-hui-panehost]').hasAttribute('data-hui-pane-mode')`, &modeAfterClose),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowAfterClose),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !modeBefore {
		t.Error("closed phone-width host carries data-hui-pane-mode: the sheet would paint a scrim over a closed page")
	}
	if !closedScrim {
		t.Error("closed phone-width host paints a scrim (::before content is neither none nor normal)")
	}
	if !openAttr {
		t.Error("opening a pane under the breakpoint did not enter overlay mode")
	}
	if overflowWhileOpen != "hidden" {
		t.Errorf("overlay mode did not lock scroll: documentElement.overflow=%q", overflowWhileOpen)
	}
	if !scrimWhileOpen {
		t.Error("open drawer paints no scrim (::before content is none/normal while in overlay mode)")
	}
	if !activeAfterTab {
		t.Error("Tab from the pane's last focusable did not wrap to its first: the drawer has no focus trap")
	}
	if !paneHiddenAfterBackdrop {
		t.Error("a click on the host itself did not close the topmost pane")
	}
	if !modeAfterClose {
		t.Error("closing the drawer did not clear data-hui-pane-mode")
	}
	if overflowAfterClose != "visible" {
		t.Errorf("closing the drawer did not release the scroll lock: documentElement.overflow=%q", overflowAfterClose)
	}
}

// A client-side open changes the grid columns: the sheet keys off the
// data-hui-pane-open hook the module maintains, not a class only the
// server-rendered markup carries.
func TestPaneHostClientOpenChangesGridColumns(t *testing.T) {
	srv := paneHostTestPage(t, string(paneHostDrawerBody()))
	ctx := chromedptest.Context(t)
	var before, after string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Poll(paneHostDrawerLoaded, nil, chromedp.WithPollingTimeout(15*time.Second)),
		// Desktop width: the open pane is an inline column.
		chromedp.EmulateViewport(1280, 800),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-hui-panehost]')).gridTemplateColumns`, &before),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-hui-panehost]')).gridTemplateColumns`, &after),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(strings.Fields(before)) != 1 {
		t.Errorf("closed host should compute one grid column, got %q", before)
	}
	if len(strings.Fields(after)) != 2 {
		t.Errorf("a client-side open must add the secondary column, got %q (before %q)", after, before)
	}
	if before == after {
		t.Errorf("client-side open did not change the columns: %q", after)
	}
}

// Escape is light-dismiss only in overlay mode: at desktop width an
// open inline column is page content and Escape must leave it alone.
func TestPaneHostEscapeDoesNotCloseAtWideViewport(t *testing.T) {
	srv := paneHostTestPage(t, string(paneHostDrawerBody()))
	ctx := chromedptest.Context(t)
	var stillOpen, noMode bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Poll(paneHostDrawerLoaded, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.EmulateViewport(1280, 800),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}))`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &stillOpen),
		chromedp.Evaluate(`!document.querySelector('[data-hui-panehost]').hasAttribute('data-hui-pane-mode')`, &noMode),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !stillOpen {
		t.Error("Escape closed an inline column at desktop width: Escape is light-dismiss for the overlay drawer only")
	}
	if !noMode {
		t.Error("desktop-width host entered overlay mode: the breakpoint or the open-state gate is wrong")
	}
}
