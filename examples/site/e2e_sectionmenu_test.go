package main

// Browser-level e2e for the interactive SectionMenu, the unified docs +
// components navigation. The mobile sheet is the framework's preset.Drawer
// widget, so these assert the real out-of-the-box behaviours the user asked
// for: close-on-outside-click, scroll-lock, and NO layout shift on open.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// sheetOpen waits until the named modal widget is mounted and visible,
// its demand-loaded stylesheet has applied, the page is scroll-locked,
// and the entry animations on the sheet and its backdrop have finished:
// the settled open state the tests measure.
func sheetOpen(name string) chromedp.Action {
	return waitFor(`(()=>{const w=document.querySelector('[data-cui-widget="` + name + `"]');
		if(!w||!w.isConnected||w.hidden)return false;
		const l=document.querySelector('link[data-cui-style="` + name + `"]');
		if(l&&!l.sheet)return false;
		if(getComputedStyle(document.documentElement).overflow!=='hidden')return false;
		const b=document.querySelector('[data-cui-backdrop="` + name + `"]');
		return [w,b].every(e=>!e||e.getAnimations().every(a=>a.playState!=='running'))})()`)
}

// sheetClosed waits until the named modal widget has been dismissed:
// no backdrop left and the scroll lock released.
func sheetClosed(name string) chromedp.Action {
	return waitFor(`!document.querySelector('[data-cui-backdrop="` + name + `"]')&&getComputedStyle(document.documentElement).overflow!=='hidden'`)
}

// scrolledTo waits until a programmatic scroll to y (clamped to the
// page's scroll range) has landed, smooth scrolling included.
func scrolledTo(y string) chromedp.Action {
	return waitFor(`Math.abs(scrollY-Math.min(` + y + `,document.documentElement.scrollHeight-innerHeight))<1`)
}

// scrollSettled waits until the scroll offset js reports holds the same
// value across six consecutive polls (~150ms), for a synthesized touch
// gesture whose distance is unknown in advance: a browser that cannot
// synthesize touch scrolls moves nothing at all.
func scrollSettled(js string) chromedp.Action {
	return chromedp.Tasks{
		chromedp.Evaluate(`delete window.__scrollSettle`, nil),
		waitFor(`(()=>{const v=` + js + `,s=window.__scrollSettle||(window.__scrollSettle={v:NaN,n:0});if(s.v===v){s.n++}else{s.v=v;s.n=1}return s.n>=6})()`),
	}
}

// phoneSheetSettled is true once the phone sheet popover has no running
// animation, so a tap lands where its link has come to rest.
const phoneSheetSettled = `document.querySelector('.cui-phone-sheet__sheet').getAnimations({subtree:true}).every(a=>a.playState!=='running')`

// Desktop: the menu is a rail (trigger hidden, groups force-expanded) and the
// current item is highlighted with the primary-coloured left border.
func TestE2E_SectionMenu_DesktopRail(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t) // 1280×800 → ≥900px → rail mode

	var triggerDisplay, railDisplay, activeDot string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/docs/entity-declarations"),
		chromedp.WaitReady(`[data-cui-comp="cui-section-menu"]`, chromedp.ByQuery),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.cui-sheet-pill')).display`, &triggerDisplay),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.cui-section-menu__rail')).display`, &railDisplay),
		// The docs rail is static chrome in the docs layer: the runtime's
		// active-link sweep marks the current doc (aria-current), not the
		// server, so wait for the mark on the current page's link.
		chromedp.WaitReady(`.cui-section-menu__rail a.cui-section-menu__link[href="/docs/entity-declarations"][aria-current="page"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(()=>{const s=getComputedStyle(document.querySelector('.cui-section-menu__rail .cui-section-menu__link[aria-current="page"]'),'::before');return s.width+' '+s.backgroundColor})()`, &activeDot),
	); err != nil {
		t.Fatalf("desktop rail: %v", err)
	}
	if triggerDisplay != "none" {
		t.Errorf("desktop: the mobile trigger must be hidden, got display=%q", triggerDisplay)
	}
	if railDisplay == "none" {
		t.Errorf("desktop: the rail must be visible, got display=%q", railDisplay)
	}
	if !strings.HasPrefix(activeDot, "6px ") || strings.Contains(activeDot, ", 0)") {
		t.Errorf("active item should carry a 6px coloured dot, got %q", activeDot)
	}
}

// Mobile: the trigger opens the framework drawer. Asserts no layout shift, a
// scroll-locked backdrop, and close-on-outside-click (the two reported bugs).
func TestE2E_SectionMenu_MobileDrawer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	const drawer = "components-section-menu"
	var viewportW, triggerTopBefore, triggerTopAfter float64
	var overflowClosed, overflowOpen, overflowAfterBackdrop string
	var backdropPresent bool

	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, true),
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady(`.cui-sheet-pill`, chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`window.innerWidth`, &viewportW),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowClosed),
		chromedp.Evaluate(`document.querySelector('.cui-sheet-pill').getBoundingClientRect().top`, &triggerTopBefore),
		// Open the drawer.
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		sheetOpen(drawer), // lazy widget chrome + open
		chromedp.Evaluate(`document.querySelector('.cui-sheet-pill').getBoundingClientRect().top`, &triggerTopAfter),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowOpen),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-backdrop="`+drawer+`"]')`, &backdropPresent),
		// Close on OUTSIDE click: tap the dim strip above the bottom sheet
		// (at most 75vh tall). Clicking the sheet itself would (correctly)
		// NOT dismiss.
		chromedp.MouseClickXY(187, 60),
		sheetClosed(drawer),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowAfterBackdrop),
	); err != nil {
		t.Fatalf("mobile drawer: %v", err)
	}

	if viewportW > 900 {
		t.Fatalf("viewport emulation failed: innerWidth=%.0f", viewportW)
	}
	// Bug #2: the trigger must NOT move when the drawer opens (no layout shift).
	if d := triggerTopAfter - triggerTopBefore; d > 1 || d < -1 {
		t.Errorf("opening the drawer shifted the trigger by %.1fpx (layout shift)", d)
	}
	if overflowClosed == "hidden" {
		t.Errorf("background scroll should not be locked before opening, got %q", overflowClosed)
	}
	if overflowOpen != "hidden" {
		t.Errorf("opening the drawer should lock background scroll, got overflow=%q", overflowOpen)
	}
	if !backdropPresent {
		t.Error("drawer should render a backdrop")
	}
	// Bug #1: tapping the backdrop (outside) must close the drawer.
	if overflowAfterBackdrop == "hidden" {
		t.Errorf("clicking the backdrop must close the drawer (scroll-lock released); overflow still %q", overflowAfterBackdrop)
	}
}

// Mobile: a visible × button closes the drawer, and closing preserves the
// page's scroll position (no jump, the two reported bugs).
func TestE2E_SectionMenu_CloseButtonAndScrollPreserved(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const drawer = "components-section-menu"

	var closeVisible bool
	var scrollBefore, scrollAfterOpen, scrollAfterClose float64
	var overflowAfterClose string
	// Use JS .click() (not chromedp.Click, which auto-scrolls the target into
	// view) so we measure the widget's own scroll behaviour, not the harness's.
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, true),
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady(`.cui-sheet-pill`, chromedp.ByQuery),
		pageReady(),
		// Scroll the page down so an open/close scroll jump would be visible.
		chromedp.Evaluate(`window.scrollTo(0, 600)`, nil),
		scrolledTo("600"),
		chromedp.Evaluate(`window.scrollY`, &scrollBefore),
		chromedp.Evaluate(`document.querySelector('.cui-sheet-pill').click()`, nil),
		sheetOpen(drawer),
		chromedp.Evaluate(`window.scrollY`, &scrollAfterOpen),
		// The × close button must be visible inside the drawer.
		chromedp.Evaluate(`(()=>{const b=document.querySelector('[data-cui-widget="`+drawer+`"] .cui-sheet-head__close');return !!b && b.getBoundingClientRect().width>0})()`, &closeVisible),
		// Close via the × button (data-cui-action="close").
		chromedp.Evaluate(`document.querySelector('[data-cui-widget="`+drawer+`"] .cui-sheet-head__close').click()`, nil),
		sheetClosed(drawer),
		chromedp.Evaluate(`window.scrollY`, &scrollAfterClose),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowAfterClose),
	); err != nil {
		t.Fatalf("close button: %v", err)
	}
	if !closeVisible {
		t.Error("the drawer must show a visible × close button")
	}
	if overflowAfterClose == "hidden" {
		t.Errorf("the × button must close the drawer (scroll-lock released); overflow=%q", overflowAfterClose)
	}
	// Opening and closing must not move the page.
	if d := scrollAfterOpen - scrollBefore; d > 1 || d < -1 {
		t.Errorf("opening the drawer scrolled the page by %.0fpx", d)
	}
	if d := scrollAfterClose - scrollBefore; d > 1 || d < -1 {
		t.Errorf("closing the drawer scrolled the page by %.0fpx (the reported bug)", d)
	}
}

// Mobile: picking a link inside the drawer navigates AND auto-closes it.
func TestE2E_SectionMenu_DrawerClosesOnNav(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const drawer = "components-section-menu"

	var pathBefore, pathAfter, overflowAfter string
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(375, 812, 1, true),
		chromedp.Navigate(base+"/components/dropdown"),
		chromedp.WaitReady(`.cui-sheet-pill`, chromedp.ByQuery),
		pageReady(),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		sheetOpen(drawer),
		chromedp.Evaluate(`location.pathname`, &pathBefore),
		// Tap the "Overview" lead link inside the open drawer.
		chromedp.Click(`[data-cui-widget="`+drawer+`"] .cui-section-menu__lead`, chromedp.ByQuery),
		// The SPA navigation lands and the drawer auto-closes.
		waitFor(`location.pathname!=='/components/dropdown'&&getComputedStyle(document.documentElement).overflow!=='hidden'`),
		pageReady(),
		chromedp.Evaluate(`location.pathname`, &pathAfter),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &overflowAfter),
	); err != nil {
		t.Fatalf("drawer nav: %v", err)
	}
	if pathAfter == pathBefore {
		t.Errorf("tapping a drawer link should navigate (before=%s after=%s)", pathBefore, pathAfter)
	}
	if overflowAfter == "hidden" {
		t.Errorf("the drawer must auto-close after navigation (scroll-lock released); overflow=%q", overflowAfter)
	}
}

// Opening the ⌘K command palette on a scrolled docs page must NOT dislodge the
// sticky nav rail. The reported bug: the scroll-lock set overflow:hidden on
// <body>, which turns the body into a clipped scroll container and breaks the
// rail's position:sticky, the whole side menu scrolls off-screen. The lock
// must go on <html> so sticky descendants survive.
func TestE2E_PaletteKeepsDocsRailVisible(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t) // 1280×800 → rail mode

	var railTop, railBottom, innerH float64
	var railDisplay string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/docs/auth"),
		chromedp.WaitReady(`[data-cui-comp="cui-section-menu"] .cui-section-menu__rail`, chromedp.ByQuery),
		pageReady(),
		// Scroll well past the fold so the rail is sticky-pinned.
		chromedp.Evaluate(`window.scrollTo(0, 1500)`, nil),
		scrolledTo("1500"),
		// Open the palette (⌘K trigger in the header).
		chromedp.Click(`[data-cui-scope="docsite-header"] .cmd`, chromedp.ByQuery),
		sheetOpen("site-command-palette"),
		chromedp.Evaluate(`window.innerHeight`, &innerH),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.cui-section-menu__rail')).display`, &railDisplay),
		chromedp.Evaluate(`document.querySelector('.cui-section-menu__rail').getBoundingClientRect().top`, &railTop),
		chromedp.Evaluate(`document.querySelector('.cui-section-menu__rail').getBoundingClientRect().bottom`, &railBottom),
	); err != nil {
		t.Fatalf("palette + rail: %v", err)
	}
	if railDisplay == "none" {
		t.Fatalf("rail display went none on palette open")
	}
	// The rail must still intersect the viewport: top above the fold and
	// bottom below 0. The bug drove top to ≈ -1364 (fully off-screen above).
	if railTop > innerH || railBottom < 0 {
		t.Errorf("opening the palette dislodged the sticky rail off-screen: top=%.0f bottom=%.0f innerHeight=%.0f", railTop, railBottom, innerH)
	}
}

// Mobile: the pill names the current page and its group, the sheet opens
// on that page's group, and typing in the sheet's filter narrows the list.
func TestE2E_SectionMenu_PillAndSheetFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const drawer = "components-section-menu"
	sheet := `[data-cui-widget="` + drawer + `"]`

	var pill, openGroup, shown string
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(390, 844, 1, true),
		chromedp.Navigate(base+"/components/tabs"),
		chromedp.WaitReady(`.cui-sheet-pill`, chromedp.ByQuery),
		pageReady(),
		waitFor(`(()=>{const b=document.querySelector('.cui-sheet-pill');return b.querySelector('.cui-sheet-pill__label').textContent+'|'+b.querySelector('.cui-sheet-pill__detail').textContent})()==='Tabs|Navigation'`),
		chromedp.Evaluate(`(()=>{const b=document.querySelector('.cui-sheet-pill');return b.querySelector('.cui-sheet-pill__label').textContent+'|'+b.querySelector('.cui-sheet-pill__detail').textContent})()`, &pill),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		sheetOpen(drawer),
		waitFor(`[...document.querySelectorAll('`+sheet+` details[open] .cui-section-menu__group-label')].map(e=>e.textContent).join('|')==='Navigation'`),
		chromedp.Evaluate(`[...document.querySelectorAll('`+sheet+` details[open] .cui-section-menu__group-label')].map(e=>e.textContent).join('|')`, &openGroup),
		chromedp.SendKeys(sheet+` .cui-section-menu__filter-input`, "datefield", chromedp.ByQuery),
		waitFor(`[...document.querySelectorAll('`+sheet+` .cui-section-menu__link')].filter(a=>a.getClientRects().length).map(a=>a.textContent).join('|')==='DateField'`),
		chromedp.Evaluate(`[...document.querySelectorAll('`+sheet+` .cui-section-menu__link')].filter(a=>a.getClientRects().length).map(a=>a.textContent).join('|')`, &shown),
	); err != nil {
		t.Fatalf("pill + sheet filter: %v", err)
	}
	if pill != "Tabs|Navigation" {
		t.Errorf("pill = %q, want Tabs|Navigation", pill)
	}
	if openGroup != "Navigation" {
		t.Errorf("sheet opened groups %q, want only Navigation", openGroup)
	}
	if shown != "DateField" {
		t.Errorf(`filter "datefield" shows %q, want DateField`, shown)
	}
}

// Mobile: a finger swipe on the open sheet scrolls its list, and a drag
// down on the handle bar still closes it. The sheet root is its own
// scroller, so a touch-action: none on it leaves the browser no pan to
// perform and the list never moves; the handle keeps its own none so
// the drag reaches the runtime instead of becoming a scroll.
func TestE2E_SectionMenu_SheetTouchScrolls(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	sheet := `document.querySelector('[data-cui-widget="docs-section-menu"]')`

	var overflow, scrolled, control float64
	var handle []float64
	var lockAfterDrag, rootTouch, handleTouch string
	touch := func(typ input.TouchType, y float64) chromedp.Action {
		pts := []*input.TouchPoint{}
		if typ != input.TouchEnd {
			pts = append(pts, &input.TouchPoint{X: handle[0], Y: y})
		}
		return input.DispatchTouchEvent(typ, pts)
	}
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(390, 844, 1, true),
		emulation.SetTouchEmulationEnabled(true),
		chromedp.Navigate(base+"/docs"),
		chromedp.WaitReady(`.cui-sheet-pill`, chromedp.ByQuery),
		pageReady(),
		// Control: the same swipe on the page itself. A browser build that
		// cannot synthesize a touch scroll (CI's Linux Chrome moved neither
		// the page nor the sheet) leaves this at 0.
		input.SynthesizeScrollGesture(195, 500).
			WithYDistance(-200).
			WithGestureSourceType(input.GestureTouch),
		// The page may legitimately not move (a browser that cannot
		// synthesize touch scrolls), so wait for scrollY to settle.
		scrollSettled(`scrollY`),
		chromedp.Evaluate(`(()=>{const y=scrollY;scrollTo(0,0);return y})()`, &control),
		chromedp.Evaluate(`document.querySelector('.cui-sheet-pill').click()`, nil),
		sheetOpen("docs-section-menu"),
		chromedp.Evaluate(`(()=>{const s=`+sheet+`;return s.scrollHeight-s.clientHeight})()`, &overflow),
		chromedp.Evaluate(`getComputedStyle(`+sheet+`).touchAction`, &rootTouch),
		chromedp.Evaluate(`getComputedStyle(`+sheet+`.querySelector('.cui-widget-drag-handle')).touchAction`, &handleTouch),
		input.SynthesizeScrollGesture(195, 500).
			WithYDistance(-200).
			WithGestureSourceType(input.GestureTouch),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if control < 100 {
				return scrollSettled(sheet + `.scrollTop`).Do(ctx)
			}
			return waitFor(sheet + `.scrollTop>=100`).Do(ctx)
		}),
		chromedp.Evaluate(sheet+`.scrollTop`, &scrolled),
		chromedp.Evaluate(`(()=>{const s=`+sheet+`;s.scrollTop=0;const r=s.querySelector('.cui-widget-drag-handle').getBoundingClientRect();return [r.left+r.width/2,r.top+r.height/2]})()`, &handle),
	); err != nil {
		t.Fatalf("sheet touch scroll: %v", err)
	}
	if overflow < 200 {
		t.Fatalf("the docs sheet overflows by %.0fpx; the swipe needs a list taller than the sheet", overflow)
	}
	if rootTouch == "none" {
		t.Errorf("sheet root has touch-action: none; a finger cannot pan its list")
	}
	if handleTouch != "none" {
		t.Errorf("drag handle touch-action = %q, want none so a drag reaches the runtime", handleTouch)
	}
	switch {
	case control < 100:
		t.Logf("this browser does not synthesize touch scrolls (the page moved %.0fpx); the swipe check is skipped", control)
	case scrolled < 100:
		t.Errorf("a 200px swipe scrolled the sheet %.0fpx, want it to follow the finger", scrolled)
	}

	y := handle[1]
	steps := []chromedp.Action{touch(input.TouchStart, y)}
	for i := 1; i <= 6; i++ {
		steps = append(steps, touch(input.TouchMove, y+float64(i)*25), chromedp.Sleep(16*time.Millisecond)) // deliberate: paces the synthesized drag at one move per frame
	}
	steps = append(steps, touch(input.TouchEnd, 0), waitFor(`getComputedStyle(document.documentElement).overflow!=='hidden'`),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).overflow`, &lockAfterDrag))
	if err := chromedp.Run(ctx, steps...); err != nil {
		t.Fatalf("handle drag: %v", err)
	}
	if lockAfterDrag == "hidden" {
		t.Error("a 150px drag down on the handle left the sheet open (scroll lock still held)")
	}
}

// Hub pages on a phone: the on-this-page list folds into the same pill,
// which names the concept in view and opens the list as a sheet; a
// tapped entry closes the sheet and jumps to its section.
func TestE2E_HubTOC_PhonePill(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const label = `document.querySelector('.cui-sheet-pill__label').textContent`
	const open = `document.querySelector('.cui-phone-sheet__sheet').matches(':popover-open')`

	var aside, pill, opened, entries string
	var closed bool
	var jumped float64
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(390, 844, 1, true),
		chromedp.Navigate(base+"/framework"),
		chromedp.WaitReady(`.cui-sheet-pill`, chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`(()=>{const a=document.querySelector('.fui-content-row__aside');return Math.round(a.getBoundingClientRect().height)+'|'+getComputedStyle(a).borderTopWidth})()`, &aside),
		chromedp.Evaluate(`document.getElementById('migrations').scrollIntoView()`, nil),
		waitFor(label+`==='Migrations'`),
		chromedp.Evaluate(label, &pill),
		chromedp.Click(`.cui-sheet-pill`, chromedp.ByQuery),
		waitFor(open+`+'|'+document.querySelector('.cui-sheet-head__title').textContent==='true|On this page'&&`+phoneSheetSettled),
		chromedp.Evaluate(open+`+'|'+document.querySelector('.cui-sheet-head__title').textContent`, &opened),
		chromedp.Evaluate(`[...document.querySelectorAll('.cui-phone-sheet__sheet .fui-toc__link')].map(a=>a.textContent).slice(0,2).join('|')`, &entries),
		chromedp.Click(`.cui-phone-sheet__sheet a[href="#auth"]`, chromedp.ByQuery),
		waitFor(`!(`+open+`)&&Math.abs(document.getElementById('auth').getBoundingClientRect().top)<=200`),
		chromedp.Evaluate(`!(`+open+`)`, &closed),
		chromedp.Evaluate(`Math.abs(document.getElementById('auth').getBoundingClientRect().top)`, &jumped),
	); err != nil {
		t.Fatalf("hub toc pill: %v", err)
	}
	if aside != "0|0px" {
		t.Errorf("stacked aside height|rule = %s, want 0|0px", aside)
	}
	if pill != "Migrations" {
		t.Errorf("pill = %q with Migrations in view", pill)
	}
	if opened != "true|On this page" {
		t.Errorf("sheet open|title = %q", opened)
	}
	if entries != "Entities|Auth" {
		t.Errorf("sheet entries start %q, want Entities|Auth", entries)
	}
	if !closed || jumped > 200 {
		t.Errorf("after tapping Auth: closed=%v, Auth section %vpx from the top", closed, jumped)
	}
}

// The docs index on a phone keeps both navigations: the docs nav's pill
// floats alone at the bottom, and the on-this-page rail folds into a
// bar pinned under the site header. The bar names the intent in view,
// drops the rail from under the header, and a tapped entry closes it
// and lands below the bar.
func TestE2E_DocsIndex_OnThisPageBar(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	const bar = `document.querySelector('.cui-sheet-pill--bar')`
	const open = `document.querySelector('.cui-phone-sheet__sheet').matches(':popover-open')`

	var layout, pinned, opened, landed string
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(390, 844, 1, true),
		chromedp.Navigate(base+"/docs/"),
		chromedp.WaitReady(`.cui-sheet-pill--bar`, chromedp.ByQuery),
		pageReady(),
		chromedp.Evaluate(`(()=>{
			const floating=[...document.querySelectorAll('.cui-sheet-pill')].filter(p=>getComputedStyle(p).position==='fixed');
			const rail=document.querySelector('[data-cui-comp="ui-anchored-rail"]').getClientRects().length;
			return 'floating='+floating.length+' docs pill='+!!floating[0]?.closest('[data-cui-comp="cui-section-menu"]')+' rail boxes='+rail;
		})()`, &layout),
		chromedp.Evaluate(`document.getElementById('serving').scrollIntoView()`, nil),
		waitFor(`(()=>{const b=`+bar+`, h=document.querySelector('header').getBoundingClientRect();
			const top=b.getBoundingClientRect().top;
			return (Math.round(top)===parseFloat(getComputedStyle(b).top) && top<=h.bottom)+'|'+b.textContent.replace(/\s+/g,' ').trim()})()==='true|On this page: Serving HTTP'`),
		chromedp.Evaluate(`(()=>{const b=`+bar+`, h=document.querySelector('header').getBoundingClientRect();
			const top=b.getBoundingClientRect().top;
			return (Math.round(top)===parseFloat(getComputedStyle(b).top) && top<=h.bottom)+'|'+b.textContent.replace(/\s+/g,' ').trim()})()`, &pinned),
		chromedp.Evaluate(bar+`.click()`, nil),
		waitFor(`(`+open+`+'|'+document.querySelector('.cui-phone-sheet .cui-sheet-head__title').textContent+'|'+
			Math.round(document.querySelector('.cui-phone-sheet__sheet').getBoundingClientRect().top - `+bar+`.getBoundingClientRect().bottom))==='true|On this page|0'&&`+phoneSheetSettled),
		chromedp.Evaluate(open+`+'|'+document.querySelector('.cui-phone-sheet .cui-sheet-head__title').textContent+'|'+
			Math.round(document.querySelector('.cui-phone-sheet__sheet').getBoundingClientRect().top - `+bar+`.getBoundingClientRect().bottom)`, &opened),
		chromedp.Click(`.cui-phone-sheet__sheet a[href="#persist"]`, chromedp.ByQuery),
		waitFor(`(()=>{const b=`+bar+`.getBoundingClientRect(), s=document.getElementById('persist').getBoundingClientRect();
			return `+open+`+'|'+(s.top>=b.bottom)+'|'+`+bar+`.querySelector('.cui-sheet-pill__label').textContent})()==='false|true|Persisting & migrating'`),
		chromedp.Evaluate(`(()=>{const b=`+bar+`.getBoundingClientRect(), s=document.getElementById('persist').getBoundingClientRect();
			return `+open+`+'|'+(s.top>=b.bottom)+'|'+`+bar+`.querySelector('.cui-sheet-pill__label').textContent})()`, &landed),
	); err != nil {
		t.Fatalf("docs index bar: %v", err)
	}
	if layout != "floating=1 docs pill=true rail boxes=0" {
		t.Errorf("phone layout: %s", layout)
	}
	if pinned != "true|On this page: Serving HTTP" {
		t.Errorf("bar pinned under the header|text = %q", pinned)
	}
	if opened != "true|On this page|0" {
		t.Errorf("sheet open|title|top below the bar's bottom = %q", opened)
	}
	if landed != "false|true|Persisting & migrating" {
		t.Errorf("after tapping Persisting: open|below bar|label = %q", landed)
	}
}
