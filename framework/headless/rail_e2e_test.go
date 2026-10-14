package headless

// Browser coverage for headless-rail and headless-toc: the kernel
// loads each on its marker, the shared observer marks the anchor whose
// target holds the top of the view (aria-current and the class,
// together), a malformed selector is a safe no-op, the TOC arms
// through headless-rail's exported watcher, and markup that arrives
// after the module is armed without double-binding. Same harness as
// behavior_e2e_test.go.

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// jsString quotes a Go string as a JavaScript string literal.
func jsString(s string) string { return strconv.Quote(s) }

// railPage renders one rail (or TOC) with tall sections so the
// observer has real targets to report on.
func railPage(t *testing.T, nav render.HTML) string {
	t.Helper()
	sections := ""
	for _, id := range []string{"one", "two", "three"} {
		sections += `<section id="` + id + `" style="min-height:120vh">Section ` + id + `</section>`
	}
	return string(nav) + `<div id="region">` + sections + `</div>`
}

func railActiveExpr(sel, id string) string {
	return `(() => {
		const a = document.querySelector('` + sel + ` a[href="#` + id + `"]');
		return !!a && a.getAttribute('aria-current') === 'true' && a.classList.contains('is-active');
	})()`
}

// The observer marks the first section on load (the bootstrap pick),
// the section the reader scrolls to after, and only that one.
func TestE2E_RailMarksTheAnchorWhoseSectionIsInView(t *testing.T) {
	nav := Rail(RailProps{
		Label: "On this page", ObserveSelector: "#region",
		Items: []RailItem{
			{Anchor: "one", Text: "One"},
			{Anchor: "two", Text: "Two"},
			{Anchor: "three", Text: "Three"},
		},
	}, nil)
	b := startBehaviorServer(t, railPage(t, nav))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-rail'])`) {
		t.Fatal("the rail marker never loaded headless-rail")
	}
	if !pollTrue(ctx, railActiveExpr("[data-hui-rail]", "one")) {
		t.Fatal("the bootstrap pass did not mark the first section's anchor")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('two').scrollIntoView()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, railActiveExpr("[data-hui-rail]", "two")) {
		t.Fatal("scrolling to section two did not move the active anchor")
	}
	// Exactly one anchor carries the state: the mark moved, it did not
	// accumulate.
	var marked int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-hui-rail] a[aria-current]').length`, &marked)); err != nil {
		t.Fatal(err)
	}
	if marked != 1 {
		t.Fatalf("%d anchors carry aria-current, want exactly 1", marked)
	}
}

// A static rail (no observe selector) never loads the module, and a
// malformed observe selector is a safe no-op: the links stay links and
// nothing throws.
func TestE2E_RailStaticAndMalformedSelectorAreSafe(t *testing.T) {
	static := Rail(RailProps{Label: "Steps", Items: []RailItem{{Anchor: "one", Text: "One"}}}, nil)
	b := startBehaviorServer(t, railPage(t, static))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, bootSettledExpr) {
		t.Fatal("the page never finished booting")
	}
	for range 5 {
		if b.hits.Load() != 0 {
			t.Fatal("a rail with no observe selector loaded the module — the marker is matching markup it cannot bind")
		}
	}

	broken := Rail(RailProps{Label: "Steps", ObserveSelector: "#a:not(", Items: []RailItem{{Anchor: "one", Text: "One"}}}, nil)
	b2 := startBehaviorServer(t, railPage(t, broken))
	ctx2 := behaviorPage(t, b2)
	if !pollTrue(ctx2, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-rail'])`) {
		t.Fatal("the marker never loaded the module")
	}
	// The malformed selector threw inside the module's own try/catch:
	// the page still responds, the link still works, and no uncaught
	// error killed the scanner registration.
	if !pollTrue(ctx2, `!!document.querySelector('[data-hui-rail] a[href="#one"]')`) {
		t.Fatal("the rail's link was lost")
	}
}

// The TOC list is complete before any script runs, and the active
// entry moves with the reader through the observer headless-rail owns
// (the toc module declares Requires, so the observer is loaded first).
func TestE2E_TOCListIsServerRenderedAndActiveStateMoves(t *testing.T) {
	nav := TableOfContents(TableOfContentsProps{
		TargetSelector: "#region",
		Items: []TOCItem{
			{ID: "one", Label: "One"},
			{ID: "two", Label: "Two"},
		},
	}, nil)
	b := startBehaviorServer(t, railPage(t, nav))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-toc'])`) {
		t.Fatal("the toc marker never loaded headless-toc")
	}
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-rail'])`) {
		t.Fatal("headless-toc loaded without headless-rail — the Requires declaration is not honoured")
	}
	if !pollTrue(ctx, railActiveExpr("[data-hui-toc]", "one")) {
		t.Fatal("the toc's active entry never moved to the first heading")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('two').scrollIntoView()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, railActiveExpr("[data-hui-toc]", "two")) {
		t.Fatal("scrolling did not move the toc's active entry")
	}
}

// A section scrolled behind the sticky header is out of view: the
// observer's band starts at --ui-sticky-top plus a phone sheet's bar
// pinned under it (--ui-sheet-bar-offset). With the last 50px of "one"
// under 100px of header and bar and "two" filling the view below, the
// active entry is "two". The header height is any length expression:
// a toolbar's workspace multiplies inside its calc.
func TestE2E_RailSkipsSectionUnderStickyHeader(t *testing.T) {
	for _, top := range []string{
		"--ui-sticky-top:100px",
		"--ui-sticky-top:calc(40px + calc(2 * 25px + 10px))",
		"--ui-sticky-top:40px;--ui-sheet-bar-offset:60px",
		"--ui-sheet-bar-offset:100px",
	} {
		t.Run(top, func(t *testing.T) {
			nav := TableOfContents(TableOfContentsProps{
				TargetSelector: "#region",
				Items:          []TOCItem{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}},
			}, nil)
			b := startBehaviorServer(t, `<style>:root{`+top+`}</style>`+railPage(t, nav))
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, railActiveExpr("[data-hui-toc]", "one")) {
				t.Fatal("the bootstrap pass did not mark the first entry")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const one = document.getElementById('one');
					scrollTo(0, one.offsetTop + one.offsetHeight - 50); })()`, nil),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, railActiveExpr("[data-hui-toc]", "two")) {
				t.Fatal("the section behind the sticky header kept the active entry")
			}
		})
	}
}

// A section left with less than a pixel in the band is not in view:
// the scroll position rounds to whole pixels, so a nav of fractional
// height (CI's fonts) leaves a sliver of the section above when the
// next one scrolls to the top, and an edge that only touches the band
// reports as intersecting with no change when it gets there.
func TestE2E_RailIgnoresSliverInBand(t *testing.T) {
	for _, spacer := range []string{"0px", "100.4px"} {
		t.Run(spacer, func(t *testing.T) {
			nav := TableOfContents(TableOfContentsProps{
				TargetSelector: "#region",
				Items:          []TOCItem{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}},
			}, nil)
			page := `<div style="height:` + spacer + `"></div>` + string(nav) +
				`<div id="region"><section id="one" style="height:1000px">One</section>` +
				`<section id="two" style="height:2000px">Two</section></div>`
			b := startBehaviorServer(t, `<style>body{margin:0}</style>`+page)
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, railActiveExpr("[data-hui-toc]", "one")) {
				t.Fatal("the bootstrap pass did not mark the first entry")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.getElementById('two').scrollIntoView()`, nil),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, railActiveExpr("[data-hui-toc]", "two")) {
				var at float64
				_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('one').getBoundingClientRect().bottom`, &at))
				t.Fatalf("section one (bottom at %vpx) kept the active entry", at)
			}
		})
	}
}

// Markup that arrives after the module is armed by the kernel's
// insertion scan, once — the second rail on the page has its own
// observer and its own active anchor, and the first rail's state is
// untouched.
func TestE2E_RailArmsInsertedMarkupWithoutDoubleBinding(t *testing.T) {
	first := Rail(RailProps{Label: "First", ObserveSelector: "#region", Items: []RailItem{
		{Anchor: "one", Text: "One"},
	}}, nil)
	b := startBehaviorServer(t, railPage(t, first))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-rail'])`) {
		t.Fatal("the module never loaded")
	}
	second := string(Rail(RailProps{Label: "Second", ObserveSelector: "#region", ID: "late-rail", Items: []RailItem{
		{Anchor: "one", Text: "One"},
	}}, nil))
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`const d = document.createElement('div'); d.innerHTML = `+jsString(second)+`; document.body.appendChild(d.firstElementChild)`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, railActiveExpr("#late-rail", "one")) {
		t.Fatal("the inserted rail was never armed by the arrival pass")
	}
	// Both rails hold exactly one marked anchor each: the insertion
	// armed the new one without double-binding the old one.
	var marks int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('a[aria-current="true"]').length`, &marks)); err != nil {
		t.Fatal(err)
	}
	if marks != 2 {
		t.Fatalf("%d anchors marked in total, want one per rail (2)", marks)
	}
}
