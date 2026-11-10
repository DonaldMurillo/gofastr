package headless

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The trap posture is for a phone drawer: the site header's menu
// (examples/acme-site/siteheader, the package the app generator writes
// into every app) is a Disclosure{Trap: true} its sheet hides from a
// breakpoint up. A trap holds while its disclosure is open AND drawn.
// When the sheet stops drawing an open one (the window widens, a tablet
// rotates), the module closes it and lets the page go: Tab moves through
// the controls on screen and the body-level regions the trap made inert
// (the toast stack, the route announcer, the skip link) come back.
// The trap used to let go only on the details' toggle or its detach, so
// a menu hidden while open swallowed every Tab on the page and left
// those regions inert, with no control on screen that closed it.
func TestE2E_TrapHiddenByBreakpointLetsGo(t *testing.T) {
	for _, tc := range []struct {
		name string
		// observer: the browser has ResizeObserver, so the trap lets go
		// when its box goes, before any key. Without it the next Tab is
		// what finds the trap undrawn.
		observer bool
	}{{"resize-observer", true}, {"no-resize-observer", false}} {
		t.Run(tc.name, func(t *testing.T) {
			menu := Disclosure(DisclosureProps{ID: "menu", Summary: render.Text("Menu"), Trap: true,
				Content: render.Raw(`<a id="in-menu" href="#x">Pricing</a>`)}, nil)
			body := `<style>@media (min-width: 700px) { #menu { display: none; } }</style>` +
				string(menu) +
				`<a id="desktop-link" href="#y">Pricing</a><button id="outside" type="button">Save</button>`
			b := startBehaviorServer(t, body)
			ctx := behaviorBrowserCtx(t)
			if !tc.observer {
				if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := page.AddScriptToEvaluateOnNewDocument(`Object.defineProperty(window, 'ResizeObserver', { value: undefined, configurable: true });`).Do(ctx)
					return err
				})); err != nil {
					t.Fatal(err)
				}
			}
			if err := chromedp.Run(ctx,
				emulation.SetDeviceMetricsOverride(400, 800, 1, false),
				chromedp.Navigate(b.srv.URL+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, disclosureLoaded) {
				t.Fatal("module never loaded")
			}
			if got := evalString(t, ctx, `String(!!window.ResizeObserver)`); got != map[bool]string{true: "true", false: "false"}[tc.observer] {
				t.Fatalf("setup: window.ResizeObserver present = %s, want %v", got, tc.observer)
			}
			// A body-level region beside the page, as the toast stack is.
			const armed = `document.getElementById('menu').open && document.getElementById('region').hasAttribute('inert')`
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const r = document.createElement('div'); r.id = 'region'; r.innerHTML = '<button id="dismiss" type="button">Dismiss</button>'; document.body.appendChild(r); })()`, nil),
				chromedp.Click(`#menu > summary`, chromedp.ByQuery),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, armed) {
				t.Fatal("setup: opening the trap disclosure on the narrow viewport did not arm it")
			}

			// The viewport widens past the breakpoint: the menu is no longer drawn.
			if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(1000, 800, 1, false)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, `getComputedStyle(document.getElementById('menu')).display === 'none'`) {
				t.Fatal("setup: the wide viewport did not hide the menu")
			}
			const free = `!document.getElementById('region').hasAttribute('inert') && !document.getElementById('menu').open`
			if tc.observer && !pollTrue(ctx, free) {
				t.Fatal("the trap disclosure stopped being drawn and, with no key pressed, it is still open or the body-level region is still inert")
			}

			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.activeElement && document.activeElement.blur()`, nil),
				chromedp.KeyEvent(kb.Tab),
			); err != nil {
				t.Fatal(err)
			}
			focused := evalString(t, ctx, `document.activeElement.id || document.activeElement.tagName`)
			if focused != "desktop-link" {
				t.Fatalf("with the trap disclosure hidden by its breakpoint, Tab put focus on %q, want desktop-link (the first control on screen)", focused)
			}
			if !pollTrue(ctx, free) {
				t.Fatalf("after a Tab with the trap disclosure hidden: menu open = %s, body-level region inert = %s (want both false)",
					evalString(t, ctx, `String(document.getElementById('menu').open)`),
					evalString(t, ctx, `String(document.getElementById('region').hasAttribute('inert'))`))
			}
			if !pollTrue(ctx, `document.querySelector('#menu > summary').getAttribute('aria-expanded') === 'false'`) {
				t.Error("the closed menu's summary still reads aria-expanded=true")
			}

			// Narrow again: the menu is back, closed, and opening it traps
			// the page as before (the release left nothing half-armed).
			if err := chromedp.Run(ctx,
				emulation.SetDeviceMetricsOverride(400, 800, 1, false),
				chromedp.WaitVisible(`#menu > summary`, chromedp.ByQuery),
				chromedp.Click(`#menu > summary`, chromedp.ByQuery),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, armed) {
				t.Fatal("back on the narrow viewport, opening the menu did not trap the page again")
			}
			if err := chromedp.Run(ctx, chromedp.Click(`#menu > summary`, chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, free) {
				t.Fatal("closing the re-opened menu left the page inert")
			}
		})
	}
}

// A trap disclosure that is drawn keeps its trap through a resize: only
// an undrawn one lets go. (The observer fires on every size change of
// the open drawer, and on its first observation.)
func TestE2E_TrapDrawnSurvivesResize(t *testing.T) {
	menu := Disclosure(DisclosureProps{ID: "menu", Summary: render.Text("Menu"), Trap: true,
		Content: render.Raw(`<a id="in-menu" href="#x">Pricing</a>`)}, nil)
	b := startBehaviorServer(t, string(menu)+`<button id="outside" type="button">Save</button>`)
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(400, 800, 1, false),
		chromedp.Navigate(b.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("module never loaded")
	}
	const armed = `document.getElementById('menu').open && document.getElementById('region').hasAttribute('inert')`
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const r = document.createElement('div'); r.id = 'region'; document.body.appendChild(r); })()`, nil),
		chromedp.Click(`#menu > summary`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, armed) {
		t.Fatal("setup: the trap never armed")
	}
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(1000, 600, 1, false),
		// Two frames: the observer has reported the new size.
		chromedp.Evaluate(`new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))`, nil,
			func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }),
		chromedp.KeyEvent(kb.Tab),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `String(`+armed+`)`); got != "true" {
		t.Fatal("a drawn trap disclosure lost its trap (or closed) when the viewport resized")
	}
	if focused := evalString(t, ctx, `document.activeElement.id || document.activeElement.tagName`); focused == "outside" {
		t.Fatal("Tab left the drawn trap disclosure after a resize")
	}
}
