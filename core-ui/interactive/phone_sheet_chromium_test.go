//go:build chromium

package interactive

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// kitCSS is a component's sheet plus the pill's and the sheet head's,
// which it composes.
func kitCSS(own *registry.Style) string {
	var b strings.Builder
	for _, s := range []*registry.Style{own, sheetPillStyle, sheetHeadStyle} {
		b.WriteString(s.Entry().CSSFor(style.Theme{}))
		b.WriteString("\n")
	}
	return b.String()
}

// sheetPage serves a PhoneSheet holding a list in a 240px column beside
// a tall main, with the phonesheet behaviour.
func sheetPage(t *testing.T) string {
	t.Helper()
	var items strings.Builder
	for i := range 6 {
		fmt.Fprintf(&items, `<li><a href="#s%d">Section %d</a></li>`, i, i)
	}
	sheet := PhoneSheet(PhoneSheetConfig{ID: "toc-sheet", Title: "On this page", SwapAt: SwapMD},
		render.HTML(`<nav aria-label="On this page" id="list"><ol>`+items.String()+`</ol></nav>`))
	css := kitCSS(phoneSheetStyle)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width">
<style>body{margin:0;font-family:sans-serif}.row{display:flex}.row>main{flex:1;height:3000px}#col{width:240px}</style>
<style>%s</style><div class=row><main><h2 id=s0>S0</h2></main><div id=col>%s</div></div><script>%s</script>`, css, sheet, phoneSheetJS)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const sheetOpen = `document.getElementById('toc-sheet').matches(':popover-open')`

// At and above the swap the list sits in the column as if no sheet
// were around it, and the pill is gone.
func TestPhoneSheetFlowsAboveSwap(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sheetPage(t)),
		chromedp.Evaluate(`(() => {
			const col = document.getElementById('col').getBoundingClientRect();
			const list = document.getElementById('list').getBoundingClientRect();
			const pill = getComputedStyle(document.querySelector('.cui-sheet-pill')).display;
			const head = document.querySelector('.cui-sheet-head').getClientRects().length;
			return [Math.round(list.left - col.left), list.height > 0, pill, head].join('|');
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got != "0|true|none|0" {
		t.Errorf("list offset|shown|pill display|head boxes = %s; want 0|true|none|0", got)
	}
}

// Below the swap the list hides behind the pill; the pill opens it as a
// sheet against the viewport's bottom edge, and the close button, a
// followed link and Escape each close it.
func TestPhoneSheetOpensAndCloses(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	url := sheetPage(t)
	var hidden bool
	var at string
	var afterClose, afterLink, afterEsc bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(`document.getElementById('list').getClientRects().length === 0`, &hidden),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		// The sheet rises over the theme's normal duration.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const r = document.getElementById('toc-sheet').getBoundingClientRect();
			return [`+sheetOpen+`, Math.round(r.bottom) === innerHeight, r.left === 0].join('|');
		})()`, &at),
		chromedp.Click(`.cui-sheet-head__close`, chromedp.ByQuery),
		chromedp.Evaluate(sheetOpen, &afterClose),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		chromedp.Click(`#list a[href="#s0"]`, chromedp.ByQuery),
		chromedp.Evaluate(sheetOpen, &afterLink),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		chromedp.KeyEvent("\x1b"),
		chromedp.Evaluate(sheetOpen, &afterEsc),
	); err != nil {
		t.Fatal(err)
	}
	if !hidden {
		t.Error("below the swap the list should be hidden until the pill opens it")
	}
	if at != "true|true|true" {
		t.Errorf("open|at bottom|full width = %s; want true|true|true", at)
	}
	if afterClose || afterLink || afterEsc {
		t.Errorf("still open after close=%v link=%v escape=%v", afterClose, afterLink, afterEsc)
	}
}

// The pill names the entry the list marks current, and what it opens
// while nothing is.
func TestPhoneSheetPillFollowsCurrent(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	const label = `document.querySelector('.cui-sheet-pill__label').textContent`
	var before, marked, cleared string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sheetPage(t)),
		chromedp.Evaluate(label, &before),
		chromedp.Evaluate(`document.querySelector('#list a[href="#s3"]').setAttribute('aria-current','true')`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(label, &marked),
		chromedp.Evaluate(`document.querySelector('#list a[href="#s3"]').removeAttribute('aria-current')`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(label, &cleared),
	); err != nil {
		t.Fatal(err)
	}
	if before != "On this page" || marked != "Section 3" || cleared != "On this page" {
		t.Errorf("label before|marked|cleared = %q|%q|%q", before, marked, cleared)
	}
}

// A sheet left open when the viewport grows past the swap closes: its
// body is back in the page there.
func TestPhoneSheetClosesPastSwap(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	var open, after bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sheetPage(t)),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		chromedp.Evaluate(sheetOpen, &open),
		emulation.SetDeviceMetricsOverride(1280, 800, 1, false),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(sheetOpen, &after),
	); err != nil {
		t.Fatal(err)
	}
	if !open || after {
		t.Errorf("open before resize=%v, after=%v; want true, false", open, after)
	}
}
