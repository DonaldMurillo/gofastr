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
// while nothing is. A list swapped for markup that already carries the
// mark (an island or a poll) moves the pill too.
func TestPhoneSheetPillFollowsCurrent(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	const label = `document.querySelector('.cui-sheet-pill__label').textContent`
	var before, marked, cleared, swapped string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sheetPage(t)),
		chromedp.Evaluate(label, &before),
		chromedp.Evaluate(`document.querySelector('#list a[href="#s3"]').setAttribute('aria-current','true')`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(label, &marked),
		chromedp.Evaluate(`document.querySelector('#list a[href="#s3"]').removeAttribute('aria-current')`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(label, &cleared),
		chromedp.Evaluate(`document.querySelector('#list ol').innerHTML = '<li><a href="#s4" aria-current="true">Section 4</a></li>'`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(label, &swapped),
	); err != nil {
		t.Fatal(err)
	}
	if before != "On this page" || marked != "Section 3" || cleared != "On this page" || swapped != "Section 4" {
		t.Errorf("label before|marked|cleared|swapped = %q|%q|%q|%q", before, marked, cleared, swapped)
	}
}

// The sheet is a dialog: opening it moves focus to the current entry,
// or to the close button while nothing is current, and closing it
// hands focus back to the pill.
func TestPhoneSheetMovesFocusIn(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	const focused = `(() => { const a = document.activeElement;
		return a.getAttribute('href') || a.className; })()`
	var first, current, back string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sheetPage(t)),
		chromedp.Focus(`.cui-sheet-pill`, chromedp.ByQuery),
		chromedp.KeyEvent("\r"),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(focused, &first),
		chromedp.KeyEvent("\x1b"),
		chromedp.Evaluate(`document.querySelector('#list a[href="#s3"]').setAttribute('aria-current','true')`, nil),
		chromedp.Focus(`.cui-sheet-pill`, chromedp.ByQuery),
		chromedp.KeyEvent("\r"),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(focused, &current),
		chromedp.KeyEvent("\x1b"),
		chromedp.Evaluate(focused, &back),
	); err != nil {
		t.Fatal(err)
	}
	if first != "cui-sheet-head__close" || current != "#s3" || !strings.HasPrefix(back, "cui-sheet-pill") {
		t.Errorf("focus on open|open with a current entry|after close = %q|%q|%q", first, current, back)
	}
}

// The page scroll lock is the phone sheet's: a sheet open above the
// swap (no script to close it on a resize) leaves the page scrolling.
func TestPhoneSheetScrollLockBelowSwap(t *testing.T) {
	const lock = `getComputedStyle(document.documentElement).overflowY`
	for _, c := range []struct {
		w    int
		want string
	}{{390, "hidden"}, {1280, "visible"}} {
		ctx := chromedptest.Context(t, chromedptest.WindowSize(c.w, 800))
		var got string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(sheetPage(t)),
			chromedp.Evaluate(`document.getElementById('toc-sheet').showPopover()`, nil),
			chromedp.Evaluate(lock, &got),
		); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("at %dpx the open sheet sets the page overflow %q; want %q", c.w, got, c.want)
		}
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

// The pill names a marked rail link by the link's own text: the number
// chip before it and the count chip after it are not the entry's name.
func TestPhoneSheetPillSkipsLinkChips(t *testing.T) {
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(sheetPage(t)),
		chromedp.Evaluate(`document.querySelector('#list ol').innerHTML =
			'<li><a href="#s2" aria-current="true"><span>02</span>Serving HTTP<span>13</span></a></li>'`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.cui-sheet-pill__label').textContent`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got != "Serving HTTP" {
		t.Errorf("pill label = %q; want %q", got, "Serving HTTP")
	}
}

// Top pins the pill as a bar under a 56px sticky header, its name
// visible; the page publishes the bar's height as
// --ui-sheet-bar-offset below the swap only; what the header drops (a
// menu) covers the bar. The sheet hangs from the bar's bottom edge with
// the bar left lit above the dim, its chevron turned and no title row
// repeating the bar; focus lands on the current entry, and a second tap
// on the bar closes the sheet. Opening and closing, the sheet rolls
// from the bar in place: no frame draws it above the bar, over the
// header (an open popover paints over everything).
func TestPhoneSheetTopBarPinsUnderHeader(t *testing.T) {
	sheet := PhoneSheet(PhoneSheetConfig{ID: "toc-sheet", Title: "On this page", SwapAt: SwapMD, Top: true},
		render.HTML(`<nav aria-label="On this page"><ol><li><a href="#s0" aria-current="true">Overview</a></li></ol></nav>`))
	css := kitCSS(phoneSheetStyle)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width">
<style>:root{--ui-sticky-top:56px}body{margin:0;font-family:sans-serif}ol{margin:0;padding:0;list-style:none}header{position:sticky;top:0;height:56px;z-index:200;background:#eee}#drop{position:absolute;top:56px;left:0;right:0;height:300px;display:none}main{height:3000px}</style>
<style>%s</style><header><div id=drop></div></header><div class=page style="padding-inline:24px;--ui-sheet-bar-bleed:24px"><p style="height:200px">intro</p>%s<main></main></div><script>%s</script>`, css, sheet, phoneSheetJS)
	}))
	t.Cleanup(srv.Close)
	const offset = `getComputedStyle(document.documentElement).getPropertyValue('--ui-sheet-bar-offset').trim()`
	const bar = `(() => {
		const b = document.querySelector('.cui-sheet-pill--bar');
		const r = b.getBoundingClientRect();
		const sr = b.querySelector('.cui-sheet-pill__sr').getBoundingClientRect();
		return [Math.round(r.top), Math.round(r.height), r.left === 0 && r.right === innerWidth, sr.width > 1].join('|');
	})()`
	ctx := chromedptest.Context(t, chromedptest.WindowSize(390, 800))
	var scrolled, off, open, wide, covered, frames string
	var closed bool
	var still string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`scrollTo(0, 1000)`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(bar, &scrolled),
		chromedp.Evaluate(offset, &off),
		chromedp.Evaluate(`(() => { const d = document.getElementById('drop'); d.style.display = 'block';
			const at = document.elementFromPoint(innerWidth / 2, 78).id; d.style.display = 'none'; return at; })()`, &covered),
		chromedp.Evaluate(`(() => {
			const sheet = document.getElementById('toc-sheet'), bar = document.querySelector('.cui-sheet-pill--bar');
			window.__frames = { above: 0, moved: 0, clips: { open: new Set(), closed: new Set() }, n: 0 };
			sheet.addEventListener('beforetoggle', (e) => {
				const clips = window.__frames.clips[e.newState];
				const t0 = performance.now();
				const tick = () => {
					const f = window.__frames, cs = getComputedStyle(sheet);
					f.n++;
					if (sheet.getClientRects().length && sheet.getBoundingClientRect().top < bar.getBoundingClientRect().bottom - 0.5) f.above++;
					if (cs.translate !== 'none') f.moved++;
					clips.add(cs.clipPath);
					if (performance.now() - t0 < 400) requestAnimationFrame(tick);
				};
				requestAnimationFrame(tick);
			});
		})()`, nil),
		chromedp.Click(`.cui-sheet-pill--bar`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => { const r = document.getElementById('toc-sheet').getBoundingClientRect();
			const bar = document.querySelector('.cui-sheet-pill--bar'), b = bar.getBoundingClientRect();
			const lit = Math.round(parseFloat(getComputedStyle(document.getElementById('toc-sheet'), '::backdrop').top)) === Math.round(b.bottom);
			const turned = getComputedStyle(bar.querySelector('.cui-sheet-pill__chevron')).rotate;
			const head = document.querySelector('#toc-sheet .cui-sheet-head').getClientRects().length;
			const aligned = Math.round(document.querySelector('#toc-sheet a').getBoundingClientRect().left) ===
				Math.round(bar.querySelector('.cui-sheet-pill__sr').getBoundingClientRect().left);
			return [`+sheetOpen+`, Math.round(r.top) === Math.round(b.bottom), r.bottom < innerHeight, lit, turned, head, aligned,
				document.activeElement.getAttribute('href')].join('|'); })()`, &open),
		chromedp.Click(`.cui-sheet-pill--bar`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!(`+sheetOpen+`)`, &closed),
		chromedp.Evaluate(`(() => { const f = window.__frames;
			return 'above=' + f.above + ' moved=' + f.moved + ' animates=' + (f.clips.open.size > 2 && f.clips.closed.size > 2) + ' sampled=' + (f.n > 5); })()`, &frames),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('toc-sheet')).transitionDuration`, &still),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{}),
		emulation.SetDeviceMetricsOverride(1280, 800, 1, false),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(offset, &wide),
	); err != nil {
		t.Fatal(err)
	}
	if scrolled != "56|44|true|true" {
		t.Errorf("scrolled bar top|height|full width|name shown = %s; want 56|44|true|true", scrolled)
	}
	if covered != "drop" {
		t.Errorf("the header's drop-down at the bar shows %q; want it over the bar", covered)
	}
	if off != "2.75rem" || wide != "" {
		t.Errorf("--ui-sheet-bar-offset = %q at 390px, %q at 1280px; want 2.75rem, empty", off, wide)
	}
	if open != "true|true|true|true|none|0|true|#s0" {
		t.Errorf("sheet open|hangs from bar|short of the bottom|bar lit|chevron|head boxes|aligned|focus = %s; want true|true|true|true|none|0|true|#s0", open)
	}
	if frames != "above=0 moved=0 animates=true sampled=true" {
		t.Errorf("open and close frames: %s; want the sheet rolled in place below the bar", frames)
	}
	if still != "0s" {
		t.Errorf("with reduced motion the sheet transitions over %q; want 0s", still)
	}
	if !closed {
		t.Error("a second tap on the bar left the sheet open")
	}
}
