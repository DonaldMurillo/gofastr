package headless

// Browser coverage for headless-panehost and headless-sidebar: the
// pane lifecycle (open by control, focus handoff into the pane, close
// restores focus to the trigger, Escape is light-dismiss for the
// overlay drawer ONLY — an inline column survives it), the deep
// link's URL write, and the sidebar's storage contract (a keyed
// sidebar persists its collapse; a keyless one never writes).

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

const paneHostLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-panehost'])`

// The pane lifecycle: the control opens the pane (hidden removed,
// focus lands inside), close restores focus to the trigger, and ESC
// closes the topmost.
func TestE2E_PaneHostLifecycleAndFocus(t *testing.T) {
	host := PaneHost(PaneHostProps{
		Primary:   render.Text("The list"),
		Secondary: render.HTML(`<a id="in-pane" href="/x">detail</a>`),
	}, nil)
	page := string(host) +
		`<button type="button" id="open-btn" data-hui-pane-open-control="secondary">Open</button>` +
		`<button type="button" id="close-btn" data-hui-pane-close="secondary">Close</button>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	// Open: hidden removed, focus inside the pane.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('open-btn').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden') &&
		document.activeElement === document.getElementById('in-pane')`) {
		t.Fatal("opening the pane did not reveal it and hand focus to its first tabbable")
	}
	// Close: hidden back, focus on the trigger.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('in-pane').focus()`, nil),
		chromedp.Evaluate(`document.getElementById('close-btn').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`) {
		t.Fatal("closing the pane did not hide it")
	}
	// ESC on an open pane at DESKTOP width: the pane is an inline
	// column, page content, and must survive. The harness boots with a
	// narrow default viewport, so widen past the 768px breakpoint
	// first (the module's shared matchMedia listener clears overlay
	// mode on the change). The overlay-drawer Escape is pinned by
	// framework/ui's chromium suite, which owns the stylesheet.
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Sleep(200*1e6),
		chromedp.Evaluate(`document.getElementById('open-btn').click()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))`, nil),
		chromedp.Sleep(100*1e6),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`) {
		t.Fatal("Escape closed an inline column at desktop width: Escape is light-dismiss for the overlay drawer only")
	}
}

const sidebarLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-sidebar'])`

// The sidebar storage contract: a keyed collapsible sidebar persists
// its collapse under the namespaced, component-encoded key; a keyless
// one never writes a thing.
func TestE2E_SidebarStorageContract(t *testing.T) {
	keyed := Sidebar(SidebarProps{NavLabel: "Primary", Variant: "collapsible",
		Collapse: "auto", CollapseStorageKey: "nav.side",
		Items: []SidebarItem{{Label: "One", Href: "/one"}}}, nil)
	keyless := Sidebar(SidebarProps{NavLabel: "Other", Variant: "collapsible",
		Items: []SidebarItem{{Label: "Two", Href: "/two"}}}, nil)
	b := startBehaviorServer(t, string(keyed)+string(keyless))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sidebarLoaded) {
		t.Fatal("the sidebar marker never loaded headless-sidebar")
	}
	// Toggle the keyed sidebar: data-collapsed flips and the store
	// gains exactly the namespaced key.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-hui-sidebar-storage] [data-hui-sidebar-toggle]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-sidebar-storage]').getAttribute('data-collapsed') === 'true' &&
		document.querySelector('[data-hui-sidebar-storage] [data-hui-sidebar-toggle]').getAttribute('aria-expanded') === 'false' &&
		localStorage.getItem('gofastr.sidebar-collapse.' + encodeURIComponent('nav.side')) === 'true'`) {
		t.Fatal("the keyed sidebar did not persist its collapse under the namespaced key")
	}
	// Toggle the keyless one: state flips, nothing written.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(() => { const roots = document.querySelectorAll('[data-hui-sidebar]'); const bare = Array.from(roots).find(r => !r.hasAttribute('data-hui-sidebar-storage')); bare.querySelector('[data-hui-sidebar-toggle]').click(); return true; })()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(() => { const roots = document.querySelectorAll('[data-hui-sidebar]'); const bare = Array.from(roots).find(r => !r.hasAttribute('data-hui-sidebar-storage')); return bare.getAttribute('data-collapsed') === 'true'; })() &&
		Object.keys(localStorage).filter(k => k.indexOf('gofastr.sidebar-collapse.') === 0).length === 1`) {
		t.Fatal("the keyless sidebar wrote storage, or the keyed one lost its single entry")
	}
}
