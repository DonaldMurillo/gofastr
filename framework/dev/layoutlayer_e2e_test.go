package dev

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

const layoutPage = `<!doctype html><html><head>
<meta name="viewport" content="width=device-width">
<link rel="stylesheet" href="/page.css"></head><body>
<div data-cui-layout="app" data-cui-layout-key="l:app">
<main data-cui-layout-slot="l:app">
<div class="wide"><p class="wide-child">too wide</p></div>
<div class="scroller"><div class="track">scrolls</div></div>
<div class="clip"><div class="track">clipped on purpose</div></div>
</main></div>
<aside class="drawer">off-canvas drawer</aside>
</body></html>`

const layoutPageCSS = `body { margin: 0 }
.wide { width: 700px } .wide-child { width: 900px }
.scroller { overflow-x: auto; width: 200px } .track { width: 800px }
.clip { overflow: hidden; width: 200px }
.drawer { position: fixed; top: 0; left: 100vw; width: 300px }`

// The layer draws under a strict CSP, flags only the outermost element
// that widens the page (not a scroll box's or a clip's content, not an
// off-canvas fixed drawer), and keeps its on/off across a reload.
func TestLayoutLayerDrawsAndFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e: -short")
	}
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("needs Chrome, Chromium, or Edge")
	}
	r := router.New()
	RegisterLayoutLayer(r)
	r.Get("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(layoutPage))
	}))
	r.Get("/page.css", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(layoutPageCSS))
	}))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t,
		chromedptest.WindowSize(400, 800),
		chromedptest.AllocatorOptions(chromedp.ExecPath(path)))
	press := chromedp.ActionFunc(func(ctx context.Context) error {
		mods := input.ModifierAlt | input.ModifierShift
		if err := input.DispatchKeyEvent(input.KeyRawDown).WithCode("KeyL").WithKey("L").WithModifiers(mods).Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).WithCode("KeyL").WithKey("L").WithModifiers(mods).Do(ctx)
	})
	const layer = `document.querySelector('fui-dev-layout')`
	var on bool
	var labels, findings []string
	var border string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady(`.drawer`, chromedp.ByQuery),
		chromedp.Poll(`!!window.__gofastrDevLayout`, nil),
		press,
		chromedp.Poll(`!!`+layer, nil),
		chromedp.Evaluate(`window.__gofastrDevLayout.on`, &on),
		chromedp.Evaluate(`[...`+layer+`.shadowRoot.children].map(e => e.textContent).filter(Boolean)`, &labels),
		chromedp.Evaluate(`window.__gofastrDevLayout.findings`, &findings),
		chromedp.Evaluate(`getComputedStyle(`+layer+`.shadowRoot.firstElementChild).borderTopColor`, &border),
	); err != nil {
		t.Fatalf("show the layer: %v", err)
	}
	if !on {
		t.Fatal("Alt+Shift+L did not show the layer")
	}
	for _, want := range []string{"layout app · l:app", "slot l:app", "scrolls x"} {
		if !slices.Contains(labels, want) {
			t.Errorf("no %q label in %q", want, labels)
		}
	}
	if len(findings) != 1 || !strings.HasPrefix(findings[0], "overflows +") {
		t.Errorf("findings = %q, want the one .wide overflow", findings)
	}
	if !slices.ContainsFunc(labels, func(s string) bool { return strings.Contains(s, "page scrolls sideways") }) {
		t.Errorf("legend does not report the page's sideways scroll: %q", labels)
	}
	if border != "rgb(139, 92, 246)" {
		t.Errorf("layout box border = %q: the CSP blocked the layer's styles", border)
	}

	var afterReload, afterHide, afterSecondReload bool
	if err := chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.Poll(`!!window.__gofastrDevLayout`, nil),
		chromedp.Evaluate(`window.__gofastrDevLayout.on`, &afterReload),
		press,
		chromedp.Evaluate(`!!`+layer, &afterHide),
		chromedp.Reload(),
		chromedp.Poll(`!!window.__gofastrDevLayout`, nil),
		chromedp.Evaluate(`window.__gofastrDevLayout.on`, &afterSecondReload),
	); err != nil {
		t.Fatalf("reload and hide: %v", err)
	}
	if !afterReload {
		t.Error("the layer did not come back after a reload")
	}
	if afterHide {
		t.Error("a second Alt+Shift+L left the layer on the page")
	}
	if afterSecondReload {
		t.Error("the layer came back after it was hidden")
	}
}

const topLayerPage = `<!doctype html><html><head>
<meta name="viewport" content="width=device-width">
<link rel="stylesheet" href="/page.css"></head><body>
<div id="sheet" popover="auto"><div data-island="in-sheet" class="box">sheet</div></div>
<dialog id="modal"><div data-island="in-modal" class="box">modal</div></dialog>
</body></html>`

const topLayerCSS = `body { margin: 0 }
#sheet, #modal { margin: 0; padding: 0; border: 0; inset: 0; width: 100vw; height: 100vh;
  max-width: none; max-height: none; background: rgb(255, 255, 255) }
.box { position: absolute; left: 50px; top: 100px; width: 200px; height: 100px }`

// A phone sheet is a popover and a confirm is a modal dialog; both paint
// in the top layer, above any z-index. The layer has to be read on top of
// them, since they are what it is most often opened to inspect.
func TestLayoutLayerPaintsOverTopLayer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e: -short")
	}
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("needs Chrome, Chromium, or Edge")
	}
	r := router.New()
	RegisterLayoutLayer(r)
	r.Get("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(topLayerPage))
	}))
	r.Get("/page.css", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(topLayerCSS))
	}))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t,
		chromedptest.WindowSize(400, 800),
		chromedptest.AllocatorOptions(chromedp.ExecPath(path)))
	// The island's label is a solid #16a34a tab at the box's top-left
	// corner (50,100); its left padding holds no glyph.
	edge := func(name string) {
		t.Helper()
		var shot []byte
		var labelled bool
		var vw float64
		if err := chromedp.Run(ctx,
			chromedp.Poll(`[...document.querySelector('fui-dev-layout').shadowRoot.children].some(e => e.textContent === 'island `+name+`')`, &labelled),
			chromedp.Sleep(100*time.Millisecond),
			chromedp.Evaluate(`innerWidth`, &vw),
			chromedp.CaptureScreenshot(&shot),
		); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatalf("%s: decode screenshot: %v", name, err)
		}
		scale := float64(img.Bounds().Dx()) / vw
		cr, cg, cb, _ := img.At(int(52*scale), int(103*scale)).RGBA()
		if got := [3]uint32{cr >> 8, cg >> 8, cb >> 8}; got[1] < 120 || got[0] > 80 || got[2] > 120 {
			t.Errorf("%s: pixel on the island label is rgb%v, want the layer's green: the layer paints under it", name, got)
		}
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastrDevLayout`, nil),
		chromedp.Evaluate(`window.__gofastrDevLayout.show()`, nil),
		chromedp.Evaluate(`document.getElementById('sheet').showPopover()`, nil),
	); err != nil {
		t.Fatalf("open the sheet: %v", err)
	}
	edge("in-sheet")
	var sheetOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('sheet').matches(':popover-open')`, &sheetOpen),
		chromedp.Evaluate(`document.getElementById('sheet').hidePopover()`, nil),
		chromedp.Evaluate(`document.getElementById('modal').showModal()`, nil),
	); err != nil {
		t.Fatalf("open the dialog: %v", err)
	}
	if !sheetOpen {
		t.Error("raising the layer closed the sheet")
	}
	edge("in-modal")

	// A browser that fires no toggle event for showModal() still flips the
	// dialog's open attribute; the layer has to come back on top from that.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('modal').close()`, nil),
		chromedp.Poll(`![...document.querySelector('fui-dev-layout').shadowRoot.children].some(e => e.textContent === 'island in-modal')`, nil),
		chromedp.Evaluate(`addEventListener('toggle', (e) => { if (e.target.localName === 'dialog') e.stopImmediatePropagation(); }, true)`, nil),
		chromedp.Evaluate(`document.getElementById('modal').showModal()`, nil),
	); err != nil {
		t.Fatalf("reopen the dialog: %v", err)
	}
	edge("in-modal")
}
