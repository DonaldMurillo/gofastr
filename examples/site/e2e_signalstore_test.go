package main

// Browser-level e2e for the client-side interactivity components and the
// signal-store primitive. These assert RENDERED BEHAVIOR (computed styles,
// fan-out), not just DOM attributes — and a console/CSP-error guard that
// catches the class of breakage where a demo used inline style="…" that
// strict CSP silently strips.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// consoleErrSink collects browser-level errors: console.error calls,
// uncaught exceptions, AND Log-domain entries (where CSP violations land).
type consoleErrSink struct {
	mu   sync.Mutex
	errs []string
}

func (s *consoleErrSink) add(msg string) {
	s.mu.Lock()
	s.errs = append(s.errs, msg)
	s.mu.Unlock()
}

func (s *consoleErrSink) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type == "error" {
				s.add("console.error")
			}
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails != nil {
				s.add("exception: " + e.ExceptionDetails.Text)
			}
		case *cdplog.EventEntryAdded:
			if e.Entry != nil && e.Entry.Level == cdplog.LevelError {
				s.add("log[" + string(e.Entry.Source) + "]: " + e.Entry.Text)
			}
		}
	})
}

func (s *consoleErrSink) errors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.errs...)
}

// interactiveSlugs are the client-side interactivity demos. Every one must
// load and run with ZERO console/CSP errors.
var interactiveSlugs = []string{
	"counter", "tabs", "toggle", "collapsible",
	"dropdown", "scroll-reveal", "signal-animate", "signal-store",
	"section-menu",
}

// TestE2E_InteractiveComponents_NoConsoleErrors is the keystone guard: an
// inline style="…" that strict CSP strips shows up as a console/CSP error.
// This is exactly what made signal-animate / scroll-reveal "completely
// broken" while the DOM-only assertions still passed.
func TestE2E_InteractiveComponents_NoConsoleErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	for _, slug := range interactiveSlugs {
		t.Run(slug, func(t *testing.T) {
			ctx := siteBrowserCtx(t)
			sink := &consoleErrSink{}
			sink.listen(ctx)
			if err := chromedp.Run(ctx,
				runtime.Enable(),
				cdplog.Enable(),
				chromedp.Navigate(base+"/components/"+slug),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Sleep(700*time.Millisecond), // let demand-load modules run
			); err != nil {
				t.Fatalf("navigate %s: %v", slug, err)
			}
			if errs := sink.errors(); len(errs) > 0 {
				t.Errorf("/components/%s produced %d console/CSP error(s):\n  %s",
					slug, len(errs), strings.Join(errs, "\n  "))
			}
		})
	}
}

// TestE2E_InteractiveComponents_RenderCorrectly asserts the components
// actually RENDER as UI — not just that the elements exist. It catches
// unstyled controls (transparent bg + no border = bare text), dropdowns
// that aren't floating menus, switches whose thumb doesn't move, and
// pages that overflow horizontally. These are the failures DOM-presence
// and single-computed-value assertions miss.
func TestE2E_InteractiveComponents_RenderCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)

	t.Run("no-horizontal-overflow", func(t *testing.T) {
		ctx := siteBrowserCtx(t)
		for _, slug := range interactiveSlugs {
			var overflow bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/components/"+slug),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth + 1`, &overflow),
			); err != nil {
				t.Fatalf("%s: %v", slug, err)
			}
			if overflow {
				t.Errorf("/components/%s overflows horizontally (broken layout)", slug)
			}
		}
	})

	t.Run("store-producers-are-styled-buttons", func(t *testing.T) {
		ctx := siteBrowserCtx(t)
		var styled bool
		var h float64
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/components/signal-store"),
			chromedp.WaitReady(`.demo-row button`, chromedp.ByQuery),
			// a real button: sized + (filled OR bordered), not bare text
			chromedp.Evaluate(`(()=>{const b=document.querySelector('.demo-row button');const c=getComputedStyle(b);return (c.backgroundColor!=='rgba(0, 0, 0, 0)'||c.borderStyle!=='none')})()`, &styled),
			chromedp.Evaluate(`document.querySelector('.demo-row button').getBoundingClientRect().height`, &h),
		); err != nil {
			t.Fatal(err)
		}
		if !styled {
			t.Error("store producer buttons render unstyled (transparent + no border = bare text)")
		}
		if h < 36 {
			t.Errorf("store producer button too short (%.0fpx) — unstyled control", h)
		}
	})

	t.Run("dropdown-renders-as-floating-menu", func(t *testing.T) {
		ctx := siteBrowserCtx(t)
		var pos, shadow, bg string
		var triggerStyled bool
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/components/dropdown"),
			chromedp.WaitReady(`[data-fui-dropdown]`, chromedp.ByQuery),
			chromedp.Evaluate(`(()=>{const c=getComputedStyle(document.querySelector('[data-fui-dropdown]'));return c.backgroundColor!=='rgba(0, 0, 0, 0)'||c.borderStyle!=='none'})()`, &triggerStyled),
			chromedp.Click(`[data-fui-dropdown]`, chromedp.ByQuery),
			chromedp.Sleep(150*time.Millisecond),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-fui-dropdown-panel]')).position`, &pos),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-fui-dropdown-panel]')).boxShadow`, &shadow),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-fui-dropdown-panel]')).backgroundColor`, &bg),
		); err != nil {
			t.Fatal(err)
		}
		if !triggerStyled {
			t.Error("dropdown trigger renders unstyled (bare text, not a button)")
		}
		if pos != "absolute" {
			t.Errorf("dropdown panel not floating: position=%q, want absolute", pos)
		}
		if shadow == "none" {
			t.Error("dropdown panel has no shadow — renders flat, not as a menu surface")
		}
		if bg == "rgba(0, 0, 0, 0)" {
			t.Error("dropdown panel is transparent — not a real menu surface")
		}
		// Not clipped: the last menu item must be the topmost element at
		// its own center (a frame with overflow:hidden would clip it).
		var lastItemVisible bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{
			const items=[...document.querySelectorAll('[data-fui-dropdown-panel] a')];
			const el=items[items.length-1]; const r=el.getBoundingClientRect();
			const top=document.elementFromPoint(r.left+r.width/2, r.top+r.height/2);
			return top===el||el.contains(top);
		})()`, &lastItemVisible)); err != nil {
			t.Fatal(err)
		}
		if !lastItemVisible {
			t.Error("last dropdown item is clipped/covered — menu overflows its frame")
		}
	})

	// dropdown-themes-with-the-page catches the white-menu-on-dark-theme
	// bug: framework components must use the host's theme tokens, so the
	// panel surface lands on the SAME light/dark side as the page — never
	// a hardcoded white on a dark theme.
	t.Run("dropdown-themes-with-page", func(t *testing.T) {
		ctx := siteBrowserCtx(t)
		var sameMode bool
		var panelBg string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/components/dropdown"),
			chromedp.WaitReady(`[data-fui-dropdown]`, chromedp.ByQuery),
			chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme','dark')`, nil),
			chromedp.Click(`[data-fui-dropdown]`, chromedp.ByQuery),
			chromedp.Sleep(120*time.Millisecond),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-fui-dropdown-panel]')).backgroundColor`, &panelBg),
			chromedp.Evaluate(`(()=>{
				const L=s=>{const m=/oklch\(([\d.]+)/.exec(s);if(m)return parseFloat(m[1]);
					const n=/rgb[a]?\((\d+), (\d+), (\d+)/.exec(s);return n?(+n[1]+ +n[2]+ +n[3])/765:null;};
				const pg=L(getComputedStyle(document.body).backgroundColor);
				const pn=L(getComputedStyle(document.querySelector('[data-fui-dropdown-panel]')).backgroundColor);
				if(pg==null||pn==null)return false;
				return (pg<0.5)===(pn<0.5); // same light/dark mode
			})()`, &sameMode),
		); err != nil {
			t.Fatal(err)
		}
		if !sameMode {
			t.Errorf("dropdown panel does not follow the dark theme (panelBg=%s) — uses hardcoded light fallback instead of host --fui-* tokens", panelBg)
		}
	})

	t.Run("interactive-pages-show-example-code", func(t *testing.T) {
		ctx := siteBrowserCtx(t)
		for _, slug := range interactiveSlugs {
			var hasCode bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/components/"+slug),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Evaluate(`!!document.querySelector('.doc-usage')`, &hasCode),
			); err != nil {
				t.Fatalf("%s: %v", slug, err)
			}
			if !hasCode {
				t.Errorf("/components/%s shows no example code (.doc-usage missing)", slug)
			}
		}
	})

	t.Run("toggle-thumb-moves-on-flip", func(t *testing.T) {
		ctx := siteBrowserCtx(t)
		var onT, offT string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/components/toggle"),
			chromedp.WaitReady(`.fui-toggle__thumb`, chromedp.ByQuery),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-toggle__thumb')).transform`, &offT),
			chromedp.Click(`[data-fui-comp="fui-toggle"]`, chromedp.ByQuery),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-toggle__thumb')).transform`, &onT),
		); err != nil {
			t.Fatal(err)
		}
		if onT == offT {
			t.Errorf("toggle thumb did not move on flip: transform stayed %q — switch is visually static", offT)
		}
	})
}

// TestE2E_BreadcrumbCategoryScrollsToSection proves the fragment-nav fix:
// clicking the category crumb (href="/components/#<category>") must
// navigate to the index AND scroll to that category section — not land at
// the top. Exercises both the section id and the SPA router preserving +
// honoring the hash on client-side navigation.
func TestE2E_BreadcrumbCategoryScrollsToSection(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var hash string
	var scrollY, sectionTop, headerBottom, innerH float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/signal-store"),
		chromedp.WaitReady(`.ui-doc-layout__crumbs`, chromedp.ByQuery),
		// The category crumb is the breadcrumb link whose href has a #.
		chromedp.Click(`.ui-doc-layout__crumbs a[href*="#"]`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond), // SPA nav + scroll + rAF re-correct
		chromedp.Evaluate(`location.hash`, &hash),
		chromedp.Evaluate(`window.scrollY`, &scrollY),
		chromedp.Evaluate(`(()=>{const el=document.getElementById(location.hash.slice(1));return el?el.getBoundingClientRect().top:-9999})()`, &sectionTop),
		// The sticky site header that the section heading must clear.
		chromedp.Evaluate(`(()=>{const h=document.querySelector('header, [data-fui-comp="ui-site-header"], nav');return h?h.getBoundingClientRect().bottom:0})()`, &headerBottom),
		chromedp.Evaluate(`window.innerHeight`, &innerH),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hash != "#clientside-interactivity" {
		t.Errorf("URL hash = %q, want #clientside-interactivity (router dropped the fragment)", hash)
	}
	if scrollY < 50 {
		t.Errorf("page did not scroll to the section (scrollY=%.0f) — landed at the top", scrollY)
	}
	if sectionTop >= innerH || sectionTop < -20 {
		t.Errorf("section not scrolled into view (top=%.0f, viewport=%.0f)", sectionTop, innerH)
	}
	// The heading must clear the sticky header — otherwise the fragment
	// lands the section *under* the header and the title is covered.
	if sectionTop < headerBottom-2 {
		t.Errorf("section heading is covered by the sticky header: section top=%.0f but header bottom=%.0f — needs scroll-margin-top", sectionTop, headerBottom)
	}
}

// TestE2E_ScrollRevealAnimates proves the framework Reveal CSS: the box is
// opacity:0 (hidden) on load and animates to opacity:1 after it scrolls
// into view. Without the registered fui-reveal CSS the box never hides and
// the reveal does nothing.
func TestE2E_ScrollRevealAnimates(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var hiddenOpacity, revealedOpacity string
	var revealedClasses string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/scroll-reveal"),
		chromedp.WaitReady(`[data-fui-reveal]`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond), // reveal.js adds fui-hidden
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-fui-reveal]')).opacity`, &hiddenOpacity),
		chromedp.Evaluate(`(()=>{document.querySelector('[data-fui-reveal]').scrollIntoView({block:'center'});return true})()`, nil),
		chromedp.Sleep(900*time.Millisecond), // IntersectionObserver + transition
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-fui-reveal]')).opacity`, &revealedOpacity),
		chromedp.Evaluate(`document.querySelector('[data-fui-reveal]').className`, &revealedClasses),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenOpacity != "0" {
		t.Errorf("reveal box should start hidden (opacity 0), got %q — fui-reveal CSS missing?", hiddenOpacity)
	}
	if revealedOpacity != "1" {
		t.Errorf("reveal box should be opacity 1 after scroll, got %q", revealedOpacity)
	}
	if !strings.Contains(revealedClasses, "fui-revealed") {
		t.Errorf("reveal box missing fui-revealed class after scroll: %q", revealedClasses)
	}
}

// TestE2E_SignalAnimateExpands proves the signal→class animation: toggling
// the signal adds the class and the panel's computed max-height grows from
// 0 to its expanded value.
func TestE2E_SignalAnimateExpands(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var collapsedMaxH, expandedMaxH string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/signal-animate"),
		chromedp.WaitReady(`.demo-animate-panel`, chromedp.ByQuery),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.demo-animate-panel')).maxHeight`, &collapsedMaxH),
		chromedp.Click(`[data-fui-signal-toggle="demo-anim-slide"]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.demo-animate-panel')).maxHeight`, &expandedMaxH),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if collapsedMaxH != "0px" {
		t.Errorf("panel should start collapsed (max-height 0), got %q", collapsedMaxH)
	}
	if expandedMaxH == "0px" || expandedMaxH == collapsedMaxH {
		t.Errorf("panel did not expand on toggle: max-height stayed %q", expandedMaxH)
	}
}

// TestE2E_SignalStoreFanout dogfoods the store primitive end-to-end in the
// live site: the value is seeded before interaction, and one producer click
// updates every bound consumer client-side.
func TestE2E_SignalStoreFanout(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var seeded, h0, h1, inline1, badge1 string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/signal-store"),
		chromedp.WaitReady(`#store-consumer-heading`, chromedp.ByQuery),
		// seeded value present before any interaction (gap #1)
		chromedp.Evaluate(`String(window.__gofastr.getSignal('sitedemo.company'))`, &seeded),
		chromedp.Text(`#store-consumer-heading`, &h0, chromedp.ByQuery),
		// producer click → fan-out
		chromedp.Click(`//button[contains(.,'Globex')]`),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Text(`#store-consumer-heading`, &h1, chromedp.ByQuery),
		chromedp.Text(`#store-consumer-inline`, &inline1, chromedp.ByQuery),
		chromedp.Text(`#store-consumer-badge`, &badge1, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if seeded != "Acme Corp" {
		t.Errorf("getSignal before interaction = %q, want seeded \"Acme Corp\"", seeded)
	}
	if h0 != "Acme Corp" {
		t.Errorf("consumer initial = %q, want Acme Corp", h0)
	}
	if h1 != "Globex" || inline1 != "Globex" || badge1 != "Globex" {
		t.Errorf("fan-out failed: heading=%q inline=%q badge=%q, want all Globex", h1, inline1, badge1)
	}
}
